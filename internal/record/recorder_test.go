package record

import (
	"path/filepath"
	"testing"

	"github.com/Kshitijmishradev/cassette/internal/jsonrpc"
	"github.com/Kshitijmishradev/cassette/internal/proxy"
	"github.com/Kshitijmishradev/cassette/internal/tape"
)

// feed pushes a message through the recorder the way the proxy would.
func feed(t *testing.T, r *Recorder, dir proxy.Direction, msg string) {
	t.Helper()
	env, err := jsonrpc.ParseEnvelope([]byte(msg))
	if err != nil {
		env = jsonrpc.Envelope{Kind: jsonrpc.KindUnknown}
	}
	// Deliberately passed as a fresh slice that the test then reuses
	// conceptually: the observer contract says the bytes are borrowed, so
	// the recorder must copy them.
	r.OnMessage(dir, []byte(msg), env)
}

func openTape(t *testing.T, path string) *tape.Reader {
	t.Helper()
	rd, err := tape.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rd.Close() })
	return rd
}

func TestRecorderPairsRequestsWithResponses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.cas")
	r, err := New(path)
	if err != nil {
		t.Fatal(err)
	}

	feed(t, r, proxy.ToServer, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`)
	feed(t, r, proxy.ToClient, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`)
	feed(t, r, proxy.ToServer, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	feed(t, r, proxy.ToServer, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/x"}}}`)
	feed(t, r, proxy.ToClient, `{"jsonrpc":"2.0","id":2,"result":{"content":[]}}`)

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	rd := openTape(t, path)
	if rd.Len() != 3 {
		t.Fatalf("recorded %d entries, want 3", rd.Len())
	}

	byMethod := map[string]int{}
	for i := range rd.Len() {
		byMethod[rd.Method(i)] = i
	}

	init, ok := byMethod["initialize"]
	if !ok {
		t.Fatal("initialize was not recorded")
	}
	if !rd.Entry(init).HasResponse() {
		t.Error("initialize has no paired response")
	}

	// initialize must be on the tape. During replay there is no server, so a
	// tape without it cannot get the agent as far as its first tool call.
	if got := string(rd.Request(init)); got == "" {
		t.Error("initialize request body missing")
	}

	notif, ok := byMethod["notifications/initialized"]
	if !ok {
		t.Fatal("notification was not recorded")
	}
	if rd.Entry(notif).HasResponse() {
		t.Error("a notification was given a response")
	}

	call, ok := byMethod["tools/call"]
	if !ok {
		t.Fatal("tools/call was not recorded")
	}
	if !rd.Entry(call).IsToolCall() {
		t.Error("tools/call not flagged as a tool call")
	}
	if got := rd.ToolName(call); got != "read_file" {
		t.Errorf("tool name = %q, want read_file", got)
	}
}

// Interleaved requests are normal: an agent can have several in flight.
// Correlation must be by id, not by arrival order.
func TestRecorderCorrelatesOutOfOrderResponses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.cas")
	r, err := New(path)
	if err != nil {
		t.Fatal(err)
	}

	feed(t, r, proxy.ToServer, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"slow","arguments":{}}}`)
	feed(t, r, proxy.ToServer, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"fast","arguments":{}}}`)
	feed(t, r, proxy.ToClient, `{"jsonrpc":"2.0","id":2,"result":{"who":"fast"}}`)
	feed(t, r, proxy.ToClient, `{"jsonrpc":"2.0","id":1,"result":{"who":"slow"}}`)

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	rd := openTape(t, path)
	if rd.Len() != 2 {
		t.Fatalf("recorded %d entries, want 2", rd.Len())
	}

	for i := range rd.Len() {
		name := rd.ToolName(i)
		resp := string(rd.Response(i))
		if name == "fast" && !contains(resp, `"who":"fast"`) {
			t.Errorf("fast paired with the wrong response: %s", resp)
		}
		if name == "slow" && !contains(resp, `"who":"slow"`) {
			t.Errorf("slow paired with the wrong response: %s", resp)
		}
	}
}

// String ids are legal JSON-RPC and plenty of clients use them.
func TestRecorderHandlesStringIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.cas")
	r, _ := New(path)

	feed(t, r, proxy.ToServer, `{"jsonrpc":"2.0","id":"req-abc","method":"tools/list"}`)
	feed(t, r, proxy.ToClient, `{"jsonrpc":"2.0","id":"req-abc","result":{"tools":[]}}`)

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	rd := openTape(t, path)
	if rd.Len() != 1 || !rd.Entry(0).HasResponse() {
		t.Errorf("string id was not correlated: %d entries", rd.Len())
	}
}

// A tool failing is behavior worth being able to test, so errors are
// recorded like any other response rather than discarded.
func TestRecorderKeepsErrorResponses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.cas")
	r, _ := New(path)

	feed(t, r, proxy.ToServer, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/nope"}}}`)
	feed(t, r, proxy.ToClient, `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"no such file"}}`)

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	rd := openTape(t, path)
	if rd.Len() != 1 {
		t.Fatalf("recorded %d entries, want 1", rd.Len())
	}
	if !rd.Entry(0).IsError() {
		t.Error("error response not flagged")
	}
	if !rd.Entry(0).HasResponse() {
		t.Error("error response not stored")
	}
}

// Messages the server sends unprompted must be kept, because replay has to
// emit them without being asked and cannot invent them.
func TestRecorderKeepsServerInitiatedMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.cas")
	r, _ := New(path)

	feed(t, r, proxy.ToClient, `{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`)

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	rd := openTape(t, path)
	if rd.Len() != 1 {
		t.Fatalf("recorded %d entries, want 1", rd.Len())
	}
	if !rd.Entry(0).ServerInitiated() {
		t.Error("unprompted server message not flagged")
	}
	if rd.Entry(0).ReqLen != 0 {
		t.Error("unprompted message has a request body; nothing asked for it")
	}
}

// A run cut short leaves requests unanswered. Writing them as truncated is
// evidence the session was interrupted; omitting them would make it look
// like a clean, shorter session.
func TestRecorderMarksUnansweredRequestsTruncated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.cas")
	r, _ := New(path)

	feed(t, r, proxy.ToServer, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hang","arguments":{}}}`)

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	rd := openTape(t, path)
	if rd.Len() != 1 {
		t.Fatalf("recorded %d entries, want 1", rd.Len())
	}
	if !rd.Entry(0).Truncated() {
		t.Error("unanswered request not marked truncated")
	}
	if rd.Entry(0).HasResponse() {
		t.Error("truncated entry claims a response")
	}
}

// The recorder is handed borrowed bytes that the proxy reuses. If it stored
// the slice instead of copying, later messages would overwrite earlier ones.
func TestRecorderCopiesBorrowedBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.cas")
	r, _ := New(path)

	buf := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"first","arguments":{}}}`)
	env, _ := jsonrpc.ParseEnvelope(buf)
	r.OnMessage(proxy.ToServer, buf, env)

	// Simulate the proxy reusing its read buffer for the next message.
	for i := range buf {
		buf[i] = 'Z'
	}

	feed(t, r, proxy.ToClient, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	rd := openTape(t, path)
	if got := string(rd.Request(0)); contains(got, "ZZZ") {
		t.Errorf("recorder kept the borrowed slice; request is now %q", got)
	}
	if got := rd.ToolName(0); got != "first" {
		t.Errorf("tool name = %q, want first", got)
	}
}

func TestRecorderEmptySession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.cas")
	r, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	rd := openTape(t, path)
	if rd.Len() != 0 {
		t.Errorf("Len = %d, want 0", rd.Len())
	}
	if !rd.Header().Complete() {
		t.Error("cleanly closed tape not marked complete")
	}
}

func TestRecorderCloseIsIdempotent(t *testing.T) {
	r, err := New(filepath.Join(t.TempDir(), "t.cas"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("second Close returned %v", err)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
