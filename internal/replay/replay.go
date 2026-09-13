// Package replay answers an agent's requests from a tape instead of a server.
//
// The important structural fact: in replay there is no server process. The
// agent talks to cassette, cassette reads the tape, and nothing leaves the
// machine. That is what makes a replay free, fast, and side-effect free, and
// it is why everything an agent needs to open a session has to be on the tape.
//
// A live server is spawned only when a call misses and the tool is known to
// be read-only. That path exists because tapes are never complete: an agent
// whose prompt changed will grep for something slightly different, and
// refusing every such call would make replay useless for exactly the
// experiments it is meant to support. What it must never do is let an
// unmatched write escape, which is the safety package's job.
package replay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/Kshitijmishradev/cassette/internal/jsonrpc"
	"github.com/Kshitijmishradev/cassette/internal/match"
	"github.com/Kshitijmishradev/cassette/internal/safety"
	"github.com/Kshitijmishradev/cassette/internal/tape"
)

// Options configures a replay.
type Options struct {
	// Tape is the recording to serve from.
	Tape *tape.Reader

	Stdin  io.Reader
	Stdout io.Writer

	Classifier *safety.Classifier

	// LiveCommand is the real server, spawned lazily on a read-class miss.
	// Nil makes the replay hermetic: nothing can leave the process.
	LiveCommand []string
	LiveEnv     []string
	LiveStderr  io.Writer

	Logf func(format string, args ...any)
}

func (o *Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// Miss records a request the tape could not answer.
type Miss struct {
	Method   string
	Tool     string
	Class    safety.Class
	Args     string
	Resolved string // "live", "refused"
}

// Result summarizes what happened.
type Result struct {
	Served      int
	ByTier      map[match.Tier]int
	Repeats     int
	Misses      []Miss
	FellThrough int
	Refused     int
	Unused      []int
	Notifies    int
}

// Diverged reports whether anything happened that makes this replay
// something other than a faithful re-run.
func (r Result) Diverged() bool { return r.Refused > 0 }

// Run serves the agent until its stdin closes.
func Run(ctx context.Context, opts Options) (Result, error) {
	if opts.Tape == nil {
		return Result{}, errors.New("replay: no tape")
	}
	if opts.Classifier == nil {
		opts.Classifier = safety.New(safety.Config{})
	}

	e := &engine{
		opts:    opts,
		matcher: match.New(opts.Tape),
		out:     jsonrpc.NewWriter(opts.Stdout),
		emitted: make([]bool, opts.Tape.Len()),
		result:  Result{ByTier: make(map[match.Tier]int, 4)},
	}
	defer e.closeLive()

	err := e.serve(ctx)
	e.result.Unused = e.matcher.Unused()
	return e.result, err
}

type engine struct {
	opts    Options
	matcher *match.Matcher
	out     *jsonrpc.Writer
	emitted []bool
	result  Result

	live     *liveServer
	liveOnce sync.Once
	liveErr  error
}

func (e *engine) serve(ctx context.Context) error {
	r := jsonrpc.NewReader(e.opts.Stdin, 0)

	for {
		if ctx.Err() != nil {
			return nil
		}

		msg, err := r.ReadMessage()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		env, perr := jsonrpc.ParseEnvelope(msg)
		if perr != nil {
			// Unparseable input cannot be matched or answered. Nothing
			// useful to do but note it; dropping it silently would hide a
			// real protocol problem.
			e.opts.logf("unparseable request (%d bytes), ignored: %v", len(msg), perr)
			continue
		}

		if err := e.handle(msg, env); err != nil {
			return err
		}
	}
}

func (e *engine) handle(msg []byte, env jsonrpc.Envelope) error {
	method, tool, args := describe(msg, env)
	res := e.matcher.Match(method, args)

	// A notification expects nothing back. Matching it still matters: it
	// marks the recording as used, so the unused-entry report does not claim
	// the run skipped something it actually sent.
	if !env.HasID() {
		e.result.Notifies++
		if res.Found {
			e.markEmitted(res.Index)
			return e.drainServerInitiated(res.Index)
		}
		return nil
	}

	if res.Found {
		entry := e.opts.Tape.Entry(res.Index)
		if !entry.HasResponse() {
			// The recording captured this request but never its reply,
			// because that run was cut short. Replaying the same silence
			// would hang the agent, so it is treated as a miss.
			e.opts.logf("%s matched a truncated recording; treating as a miss", method)
		} else {
			if err := e.serveFromTape(res, env); err != nil {
				return err
			}
			return e.drainServerInitiated(res.Index)
		}
	}

	return e.handleMiss(msg, env, method, tool, args)
}

// serveFromTape writes the recorded response with the live id spliced in.
func (e *engine) serveFromTape(res match.Result, env jsonrpc.Envelope) error {
	blob := e.opts.Tape.Response(res.Index)

	// The recorded bytes carry the id the agent used when recording, and
	// this run is using a different one. Splicing costs one allocation
	// against a model turn measured in seconds; the alternative was
	// rewriting ids at record time, which would mean the tape no longer
	// holds what the server actually sent.
	out, ok := jsonrpc.ReplaceID(trimNewline(blob), env.ID)
	if !ok {
		out = trimNewline(blob)
	}

	e.result.Served++
	e.result.ByTier[res.Tier]++
	if res.Repeat {
		e.result.Repeats++
	}
	e.markEmitted(res.Index)

	return e.out.WriteMessage(out)
}

// drainServerInitiated emits recordings the server sent unprompted.
//
// They are attached to whatever exchange preceded them on the tape, which
// reproduces the interleaving that was recorded without needing a clock.
// Emitting them in the right place matters: an agent that was told the tool
// list changed mid-session behaved differently because of it.
func (e *engine) drainServerInitiated(after int) error {
	for i := after + 1; i < e.opts.Tape.Len(); i++ {
		entry := e.opts.Tape.Entry(i)
		if !entry.ServerInitiated() {
			return nil
		}
		if e.emitted[i] {
			continue
		}
		e.emitted[i] = true
		if blob := e.opts.Tape.Response(i); len(blob) > 0 {
			if err := e.out.WriteMessage(trimNewline(blob)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *engine) markEmitted(i int) {
	if i >= 0 && i < len(e.emitted) {
		e.emitted[i] = true
	}
}

// handleMiss decides what to do about a request the tape cannot answer.
func (e *engine) handleMiss(msg []byte, env jsonrpc.Envelope, method, tool string, args []byte) error {
	class := e.opts.Classifier.Classify(method, tool)
	miss := Miss{Method: method, Tool: tool, Class: class, Args: preview(args)}

	if class == safety.ClassRead && len(e.opts.LiveCommand) > 0 {
		resp, err := e.callLive(msg, env)
		if err == nil {
			miss.Resolved = "live"
			e.result.Misses = append(e.result.Misses, miss)
			e.result.FellThrough++
			return e.out.WriteMessage(resp)
		}
		e.opts.logf("fall-through for %s failed: %v", label(method, tool), err)
	}

	// Refusing. The agent is told the call failed rather than being left to
	// hang, because a hung agent produces no trajectory at all and a failed
	// tool call produces one that can be compared.
	miss.Resolved = "refused"
	e.result.Misses = append(e.result.Misses, miss)
	e.result.Refused++

	reason := "no recording matches this call"
	if class == safety.ClassWrite {
		reason = "no recording matches this call, and it is not known to be read-only so it was not run for real"
	}
	e.opts.logf("REFUSED %s (%s): %s", label(method, tool), class, miss.Args)

	return e.out.WriteMessage(errorResponse(env.ID, reason))
}

// callLive forwards one request to a lazily spawned real server.
func (e *engine) callLive(msg []byte, env jsonrpc.Envelope) ([]byte, error) {
	e.liveOnce.Do(func() { e.liveErr = e.startLive() })
	if e.liveErr != nil {
		return nil, e.liveErr
	}
	return e.live.call(msg, env.ID, e.out)
}

// startLive spawns the real server and fast-forwards it through the recorded
// handshake.
//
// This is the part that is easy to get wrong. A freshly spawned MCP server
// has not been initialized, so handing it a tools/call would fail. The tape
// already holds the exact initialize and initialized messages from the
// recording, so they are replayed into the new process first, putting it in
// the state the agent believes it is in.
func (e *engine) startLive() error {
	cmd := exec.Command(e.opts.LiveCommand[0], e.opts.LiveCommand[1:]...)
	cmd.Env = e.opts.LiveEnv
	cmd.Stderr = e.opts.LiveStderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawning live server: %w", err)
	}

	l := &liveServer{
		cmd: cmd, in: stdin,
		w: jsonrpc.NewWriter(stdin),
		r: jsonrpc.NewReader(stdout, 0),
	}
	e.live = l
	e.opts.logf("spawned live server for fall-through (pid %d)", cmd.Process.Pid)

	return l.handshake(e.opts.Tape, e.opts.logf)
}

func (e *engine) closeLive() {
	if e.live != nil {
		e.live.close()
	}
}

// liveServer is a real MCP server used only to answer misses.
type liveServer struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	w   *jsonrpc.Writer
	r   *jsonrpc.Reader

	nextID int
}

// handshake replays the recorded initialization into a fresh server.
func (l *liveServer) handshake(t *tape.Reader, logf func(string, ...any)) error {
	for i := range t.Len() {
		method := t.Method(i)
		if method != "initialize" && method != "notifications/initialized" {
			continue
		}
		req := trimNewline(t.Request(i))
		if len(req) == 0 {
			continue
		}
		if err := l.w.WriteMessage(req); err != nil {
			return fmt.Errorf("live handshake: %w", err)
		}
		if method == "initialize" {
			// Drain until the initialize response arrives, so the server is
			// ready before anything else is sent.
			if _, err := l.readUntil(t, nil); err != nil {
				return fmt.Errorf("live handshake: %w", err)
			}
		}
		logf("replayed %s into the live server", method)
	}
	return nil
}

// call sends a request and returns the matching response.
//
// Notifications the server emits while we wait are forwarded to the agent
// rather than discarded: they are part of what the server actually said.
func (l *liveServer) call(msg, id []byte, out *jsonrpc.Writer) ([]byte, error) {
	if err := l.w.WriteMessage(msg); err != nil {
		return nil, err
	}
	return l.readUntil(nil, out)
}

// readUntil reads until a response with an id arrives, forwarding anything
// unprompted along the way.
func (l *liveServer) readUntil(_ *tape.Reader, forward *jsonrpc.Writer) ([]byte, error) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for the live server")
		}
		msg, err := l.r.ReadMessage()
		if err != nil {
			return nil, err
		}
		env, perr := jsonrpc.ParseEnvelope(msg)
		if perr == nil && env.HasID() && env.Kind == jsonrpc.KindResponse {
			return append([]byte(nil), msg...), nil
		}
		if forward != nil {
			if err := forward.WriteMessage(msg); err != nil {
				return nil, err
			}
		}
	}
}

func (l *liveServer) close() {
	_ = l.in.Close()
	done := make(chan struct{})
	go func() { _ = l.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = l.cmd.Process.Kill()
		<-done
	}
}

// describe extracts the fields matching needs from a request.
func describe(msg []byte, env jsonrpc.Envelope) (method, tool string, args []byte) {
	method = env.Method
	args, _ = jsonrpc.ParseParams(msg)

	if env.IsToolCall() {
		if tc, err := jsonrpc.ParseToolCall(msg); err == nil {
			// For a tool call the arguments identify the call, not the
			// whole params object. This has to mirror the recorder exactly
			// or nothing would ever match.
			return method, tc.Name, tc.Arguments
		}
	}
	return method, "", args
}

func errorResponse(id []byte, reason string) []byte {
	if len(id) == 0 {
		id = []byte("null")
	}
	// Built by hand rather than marshalled so the shape is obvious and the
	// reason string is the only thing that varies.
	b, _ := jsonEscape(reason)
	return []byte(fmt.Sprintf(
		`{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":%s,"data":{"cassette":"replay-miss"}}}`,
		id, b))
}

func trimNewline(b []byte) []byte {
	if n := len(b); n > 0 && b[n-1] == '\n' {
		return b[:n-1]
	}
	return b
}

func label(method, tool string) string {
	if tool == "" {
		return method
	}
	return method + "(" + tool + ")"
}

func preview(args []byte) string {
	const max = 120
	s := string(args)
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}
