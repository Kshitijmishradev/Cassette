package proxy

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kshitijmishradev/cassette/internal/jsonrpc"
)

// recorder is a test Observer. It copies, because the contract says the
// slice it is handed is only valid during the call.
type recorder struct {
	mu   sync.Mutex
	msgs []observed
}

type observed struct {
	dir Direction
	raw string
	env jsonrpc.Envelope
}

func (r *recorder) OnMessage(dir Direction, raw []byte, env jsonrpc.Envelope) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, observed{dir: dir, raw: string(raw), env: env})
}

func (r *recorder) byDirection(d Direction) []observed {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []observed
	for _, m := range r.msgs {
		if m.dir == d {
			out = append(out, m)
		}
	}
	return out
}

func run(t *testing.T, mode string, stdin string, mutate func(*Options)) (stdout, stderr string, code int) {
	t.Helper()

	cmd, env := helperCommand(mode)
	var out, errb bytes.Buffer

	opts := Options{
		Command:       cmd,
		Env:           env,
		Stdin:         strings.NewReader(stdin),
		Stdout:        &out,
		Stderr:        &errb,
		ShutdownGrace: 2 * time.Second,
		Logf:          func(f string, a ...any) { t.Logf("proxy: "+f, a...) },
	}
	if mutate != nil {
		mutate(&opts)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	code, err := Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return out.String(), errb.String(), code
}

// The headline property. Whatever the agent wrote must come back exactly,
// having crossed the proxy in both directions. Key order, spacing and
// unicode escapes are all observable to a peer, so none of them may change.
func TestProxyIsByteTransparent(t *testing.T) {
	msgs := []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		// Deliberately ugly: non-canonical spacing and reversed key order.
		`{ "method" : "tools/call" , "id" : 2 , "jsonrpc" : "2.0" }`,
		// Escapes that a reserializer would normalize.
		`{"jsonrpc":"2.0","id":3,"params":{"path":"café/naïve"}}`,
		// Duplicate keys, which encoding/json would silently collapse.
		`{"jsonrpc":"2.0","id":4,"x":1,"x":2}`,
		`{}`,
	}
	in := strings.Join(msgs, "\n") + "\n"

	out, _, code := run(t, "echo", in, nil)

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if out != in {
		t.Errorf("proxy altered the stream.\n got: %q\nwant: %q", out, in)
	}
}

// Tapping must not change the bytes either. This is the same assertion with
// an observer attached, because it would be easy to make transparency hold
// only on the path that skips observation.
func TestProxyIsByteTransparentWithObserver(t *testing.T) {
	in := `{ "id" : 1 , "method" : "tools/call" , "params" : { "name" : "read_file" } }` + "\n"

	rec := &recorder{}
	out, _, _ := run(t, "echo", in, func(o *Options) { o.Observer = rec })

	if out != in {
		t.Errorf("observer changed the stream.\n got: %q\nwant: %q", out, in)
	}

	toServer := rec.byDirection(ToServer)
	if len(toServer) != 1 {
		t.Fatalf("observed %d to-server messages, want 1", len(toServer))
	}
	if toServer[0].raw != strings.TrimSuffix(in, "\n") {
		t.Errorf("observer saw %q", toServer[0].raw)
	}
	if !toServer[0].env.IsToolCall() {
		t.Errorf("envelope not classified as a tool call: %+v", toServer[0].env)
	}
	if len(rec.byDirection(ToClient)) != 1 {
		t.Errorf("observed %d to-client messages, want 1", len(rec.byDirection(ToClient)))
	}
}

// stderr belongs to the server. The spec says a client must not read it as an
// error channel, so the proxy must pass it along without touching it.
func TestProxyForwardsStderrUntouched(t *testing.T) {
	_, errOut, code := run(t, "stderr", "", nil)

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	want := "server starting up\nthis is not an error, per the spec\n"
	if errOut != want {
		t.Errorf("stderr = %q, want %q", errOut, want)
	}
}

// The agent reads the server's exit status to decide whether to restart it.
// Swallowing or rewriting that status would change agent behavior.
func TestProxyPropagatesExitCode(t *testing.T) {
	_, _, code := run(t, "exit3", "", nil)
	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
}

// A server that never exits on stdin close must be escalated, exactly as a
// client would escalate against us. 128+15 is the shell convention for
// death by SIGTERM.
func TestProxyEscalatesToSignalWhenChildIgnoresStdinClose(t *testing.T) {
	start := time.Now()
	_, _, code := run(t, "ignore-stdin", "", func(o *Options) {
		o.ShutdownGrace = 300 * time.Millisecond
	})
	elapsed := time.Since(start)

	if code != 128+15 {
		t.Errorf("exit code = %d, want %d (SIGTERM)", code, 128+15)
	}
	if elapsed > 10*time.Second {
		t.Errorf("escalation took %s, far longer than the grace period", elapsed)
	}
}

// A message larger than the read window must survive the crossing intact.
func TestProxyForwardsLargeMessages(t *testing.T) {
	out, _, _ := run(t, "big", `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`+"\n", nil)

	if !strings.Contains(out, strings.Repeat("x", 1<<20)) {
		t.Errorf("large payload did not survive (got %d bytes)", len(out))
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("expected exactly one framed message, got %d newlines", strings.Count(out, "\n"))
	}
}

// Forwarding what it cannot parse is what keeps the proxy from breaking
// agents on protocol revisions it has never seen.
func TestProxyForwardsUnparseableMessages(t *testing.T) {
	rec := &recorder{}
	out, _, _ := run(t, "garbage", "", func(o *Options) { o.Observer = rec })

	if !strings.Contains(out, "this is not json at all") {
		t.Errorf("unparseable line was dropped; got %q", out)
	}
	if !strings.Contains(out, `{"jsonrpc":"2.0","id":1,"result":{}}`) {
		t.Errorf("valid message after the bad one was lost; got %q", out)
	}

	seen := rec.byDirection(ToClient)
	if len(seen) != 2 {
		t.Fatalf("observed %d messages, want 2", len(seen))
	}
	if seen[0].env.Kind != jsonrpc.KindUnknown {
		t.Errorf("bad message classified as %v, want unknown", seen[0].env.Kind)
	}
	if seen[1].env.Kind != jsonrpc.KindResponse {
		t.Errorf("good message classified as %v, want response", seen[1].env.Kind)
	}
}

// panicObserver fails on every message. The counter is atomic because the
// proxy runs a pump goroutine per direction and both call into the observer;
// a plain int here is a data race in the test, not in the proxy.
type panicObserver struct{ calls atomic.Int64 }

func (p *panicObserver) OnMessage(Direction, []byte, jsonrpc.Envelope) {
	p.calls.Add(1)
	panic("observer is broken")
}

// A recording bug must never take down the agent's tooling. The stream has to
// survive an observer that fails on every single message.
func TestProxySurvivesPanickingObserver(t *testing.T) {
	in := `{"id":1,"method":"tools/list"}` + "\n" + `{"id":2,"method":"tools/list"}` + "\n"

	obs := &panicObserver{}
	out, _, code := run(t, "echo", in, func(o *Options) { o.Observer = obs })

	if out != in {
		t.Errorf("panicking observer disrupted the stream.\n got: %q\nwant: %q", out, in)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if got := obs.calls.Load(); got < 4 {
		t.Errorf("observer called %d times, want at least 4; it stopped being invoked after panicking", got)
	}
}

// Cancelling the context closes the child's stdin, which is the graceful
// shutdown signal a well-behaved server acts on.
func TestProxyShutsDownOnContextCancel(t *testing.T) {
	cmd, env := helperCommand("echo")
	var out bytes.Buffer

	pr, pw := blockingPipe()
	defer pw.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)

	go func() {
		code, err := Run(ctx, Options{
			Command:       cmd,
			Env:           env,
			Stdin:         pr,
			Stdout:        &out,
			Stderr:        &bytes.Buffer{},
			ShutdownGrace: 2 * time.Second,
		})
		if err != nil {
			t.Errorf("Run: %v", err)
		}
		done <- code
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("proxy did not shut down after context cancellation")
	}
}

func TestRunRejectsEmptyCommand(t *testing.T) {
	if _, err := Run(context.Background(), Options{}); err == nil {
		t.Error("want an error for an empty command")
	}
}
