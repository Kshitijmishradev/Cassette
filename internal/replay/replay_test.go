package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kshitijmishradev/cassette/internal/match"
	"github.com/Kshitijmishradev/cassette/internal/safety"
	"github.com/Kshitijmishradev/cassette/internal/tape"
)

type rec struct {
	method   string
	tool     string
	args     string
	request  string
	response string
	flags    uint32
}

func buildTape(t *testing.T, recs []rec) *tape.Reader {
	t.Helper()
	path := filepath.Join(t.TempDir(), "r.cas")

	w, err := tape.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range recs {
		flags := r.flags
		if r.tool != "" {
			flags |= tape.EntryIsToolCall
		}
		var resp []byte
		if r.response != "" {
			resp = []byte(r.response)
		}
		if err := w.Append(tape.Record{
			Method: r.method, ToolName: r.tool,
			Request: []byte(r.request), Response: resp,
			StartedNanos: int64(i + 1),
			KeyHash:      tape.HashKey(r.method, []byte(r.args)),
			NormHash:     tape.HashNorm(r.method, []byte(r.args)),
			Flags:        flags,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	rd, err := tape.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rd.Close() })
	return rd
}

func runReplay(t *testing.T, rd *tape.Reader, input string, mutate func(*Options)) (string, Result) {
	t.Helper()
	var out bytes.Buffer

	opts := Options{
		Tape:   rd,
		Stdin:  strings.NewReader(input),
		Stdout: &out,
		Logf:   func(f string, a ...any) { t.Logf("replay: "+f, a...) },
	}
	if mutate != nil {
		mutate(&opts)
	}

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return out.String(), res
}

func lines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// The central property: the recorded response comes back, with the live
// request's id spliced in and every other byte untouched.
func TestServesRecordedResponseWithLiveID(t *testing.T) {
	rd := buildTape(t, []rec{{
		method: "tools/call", tool: "read", args: `{"path":"/x"}`,
		request:  `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"read","arguments":{"path":"/x"}}}`,
		response: `{"jsonrpc":"2.0","id":7,"result":{"content":"hello"}}`,
	}})

	out, res := runReplay(t, rd,
		`{"jsonrpc":"2.0","id":991,"method":"tools/call","params":{"name":"read","arguments":{"path":"/x"}}}`+"\n", nil)

	got := lines(out)
	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1: %q", len(got), out)
	}
	want := `{"jsonrpc":"2.0","id":991,"result":{"content":"hello"}}`
	if got[0] != want {
		t.Errorf("got  %s\nwant %s", got[0], want)
	}
	if res.Served != 1 || res.ByTier[match.TierExact] != 1 {
		t.Errorf("result = %+v", res)
	}
}

// MCP clients are allowed to keep the server's stdin open until they tear
// the child process down. A report checkpoint must therefore be available
// before Run sees EOF, or real agent runs lose all of their results.
func TestProgressIsReportedBeforeStdinCloses(t *testing.T) {
	rd := buildTape(t, []rec{{
		method: "tools/call", tool: "read", args: `{"path":"/x"}`,
		request:  `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"read","arguments":{"path":"/x"}}}`,
		response: `{"jsonrpc":"2.0","id":7,"result":{"content":"hello"}}`,
	}})

	inR, inW := io.Pipe()
	defer inW.Close()
	var out bytes.Buffer
	progress := make(chan Result, 1)
	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), Options{
			Tape: rd, Stdin: inR, Stdout: &out,
			Progress: func(r Result) { progress <- r },
		})
		done <- err
	}()

	if _, err := io.WriteString(inW, `{"jsonrpc":"2.0","id":991,"method":"tools/call","params":{"name":"read","arguments":{"path":"/x"}}}`+"\n"); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-progress:
		if got.Served != 1 || len(got.Calls) != 1 {
			t.Fatalf("checkpoint = %+v, want the served call", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no checkpoint arrived while stdin remained open")
	}

	if err := inW.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// Nothing may be spawned and nothing may leave the process when no live
// command is configured. This is what makes a hermetic replay provable
// rather than merely intended.
func TestHermeticReplayRefusesEveryMiss(t *testing.T) {
	rd := buildTape(t, []rec{{
		method: "tools/call", tool: "read", args: `{"path":"/x"}`,
		request:  `{"id":1,"method":"tools/call","params":{"name":"read","arguments":{"path":"/x"}}}`,
		response: `{"jsonrpc":"2.0","id":1,"result":{}}`,
	}})

	out, res := runReplay(t, rd,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"read","arguments":{"path":"/never"}}}`+"\n", nil)

	if res.Refused != 1 || res.FellThrough != 0 {
		t.Errorf("result = %+v, want 1 refused and 0 fall-through", res)
	}
	if !res.Diverged() {
		t.Error("a refused call should mark the replay as diverged")
	}

	// The agent gets an error rather than silence. A hung agent produces no
	// trajectory at all; a failed tool call produces one that can be compared.
	var msg struct {
		ID    int `json:"id"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines(out)[0]), &msg); err != nil {
		t.Fatalf("refusal is not valid JSON: %v", err)
	}
	if msg.Error == nil {
		t.Fatal("refusal carried no error object")
	}
	if msg.ID != 5 {
		t.Errorf("refusal id = %d, want the live request's id 5", msg.ID)
	}
}

// A write-class miss must be refused even when fall-through is available.
// This is the boundary between a test and an accident.
func TestWriteClassMissIsRefusedEvenWithFallThroughAvailable(t *testing.T) {
	rd := buildTape(t, []rec{{
		method: "tools/call", tool: "read", args: `{"path":"/x"}`,
		request: `{"id":1}`, response: `{"jsonrpc":"2.0","id":1,"result":{}}`,
	}})

	_, res := runReplay(t, rd,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"create_pull_request","arguments":{"title":"oops"}}}`+"\n",
		func(o *Options) {
			// A command that would fail loudly if it were ever spawned.
			o.LiveCommand = []string{"/nonexistent/server/binary"}
		})

	if res.FellThrough != 0 {
		t.Fatal("a write-class call was allowed to reach a live server")
	}
	if res.Refused != 1 {
		t.Errorf("result = %+v, want 1 refused", res)
	}
	if len(res.Misses) != 1 || res.Misses[0].Class != safety.ClassWrite {
		t.Errorf("miss = %+v, want write class", res.Misses)
	}
}

// Recorded but unanswered calls cannot be replayed: reproducing the silence
// would hang the agent forever.
func TestTruncatedRecordingIsTreatedAsAMiss(t *testing.T) {
	rd := buildTape(t, []rec{{
		method: "tools/call", tool: "read", args: `{"path":"/x"}`,
		request: `{"id":1,"method":"tools/call"}`,
		flags:   tape.EntryTruncated,
	}})

	out, res := runReplay(t, rd,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"read","arguments":{"path":"/x"}}}`+"\n", nil)

	if res.Served != 0 || res.Refused != 1 {
		t.Errorf("result = %+v, want the truncated entry treated as a miss", res)
	}
	if len(lines(out)) != 1 {
		t.Errorf("the agent was not answered at all: %q", out)
	}
}

// Notifications expect nothing back, and answering one would be a protocol
// violation the agent has no way to interpret.
func TestNotificationsGetNoResponse(t *testing.T) {
	rd := buildTape(t, []rec{{
		method:  "notifications/initialized",
		args:    ``,
		request: `{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	}})

	out, res := runReplay(t, rd, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n", nil)

	if out != "" {
		t.Errorf("a notification was answered: %q", out)
	}
	if res.Notifies != 1 {
		t.Errorf("result = %+v, want 1 notification", res)
	}
}

// Unprompted messages are attached to the exchange that preceded them on the
// tape, which reproduces the recorded interleaving without needing a clock.
// Position matters: an agent told mid-session that the tool list changed
// behaved differently because of it.
func TestServerInitiatedMessagesAreEmittedInPosition(t *testing.T) {
	rd := buildTape(t, []rec{
		{
			method: "tools/call", tool: "read", args: `{"path":"/x"}`,
			request:  `{"id":1,"method":"tools/call","params":{"name":"read","arguments":{"path":"/x"}}}`,
			response: `{"jsonrpc":"2.0","id":1,"result":{"first":true}}`,
		},
		{
			method:   "notifications/tools/list_changed",
			response: `{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`,
			flags:    tape.EntryServerInitiated,
		},
	})

	out, _ := runReplay(t, rd,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"read","arguments":{"path":"/x"}}}`+"\n", nil)

	got := lines(out)
	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2 (response then notification): %q", len(got), out)
	}
	if !strings.Contains(got[0], `"first":true`) {
		t.Errorf("first message is not the response: %s", got[0])
	}
	if !strings.Contains(got[1], "list_changed") {
		t.Errorf("second message is not the unprompted notification: %s", got[1])
	}
}
