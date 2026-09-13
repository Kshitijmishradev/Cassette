// Package proxy interposes cassette between an agent and an MCP server.
//
// The agent launches this process believing it is the server. This package
// launches the real server and pumps messages between the two, tapping each
// one on the way past.
//
// Everything here is subordinate to one requirement: an agent running through
// the proxy must be unable to tell. That is not a quality goal, it is the
// precondition for the rest of the tool. If the proxy perturbs behavior, then
// every recording it produces describes an agent that does not exist, and
// every replay built on those recordings is measuring the wrong thing.
//
// Concretely, transparency means:
//
//   - Messages are forwarded as the exact bytes that arrived. Never
//     reserialized, because key order and whitespace are observable.
//   - stderr is connected straight through. The spec says a server may write
//     anything there and that clients must not read it as an error channel,
//     so it is not ours to interpret.
//   - Anything unparseable is forwarded anyway. A proxy that drops what it
//     does not understand breaks agents on protocol revisions it has never
//     seen.
//   - An observer that panics is contained. A recording bug must never take
//     down the agent's tooling.
//   - Exit status and shutdown semantics are preserved in both directions.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Kshitijmishradev/cassette/internal/jsonrpc"
)

// DefaultShutdownGrace is how long the child gets to exit on its own at each
// escalation step. The spec tells servers to exit when stdin closes, so this
// is a backstop for ones that do not, not the expected path.
const DefaultShutdownGrace = 5 * time.Second

// Direction identifies which way a message was travelling.
type Direction uint8

const (
	// ToServer is agent to MCP server: requests and notifications.
	ToServer Direction = iota
	// ToClient is MCP server to agent: responses and notifications.
	ToClient
)

func (d Direction) String() string {
	if d == ToServer {
		return "to-server"
	}
	return "to-client"
}

// Observer is notified of every message that passes through.
//
// The raw slice is borrowed and is only valid for the duration of the call.
// An observer that needs to keep a message must copy it. This is what lets a
// megabyte tool result cross the proxy without being duplicated, and it puts
// the cost of retention at the place that chose to retain.
//
// Observers run inline on the forwarding path, so they are on the latency
// path of every tool call. Anything slow belongs on a queue behind this
// interface, not inside it.
type Observer interface {
	OnMessage(dir Direction, raw []byte, env jsonrpc.Envelope)
}

// Options configures a proxy run.
type Options struct {
	// Command is the MCP server to launch. Required.
	Command []string

	// Dir and Env are passed to the child. A nil Env inherits ours, which is
	// what the agent intended when it launched us.
	Dir string
	Env []string

	// Stdin, Stdout and Stderr are the agent-facing streams. Stderr is wired
	// straight to the child and never inspected.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// Observer may be nil, in which case this is pure passthrough and no
	// message is even parsed.
	Observer Observer

	MaxMessageBytes int
	ShutdownGrace   time.Duration

	// Logf receives diagnostics. Nil discards them. It must not write to the
	// agent-facing stdout, which carries protocol traffic only.
	Logf func(format string, args ...any)
}

func (o *Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// Run launches the server and pumps messages until either side finishes.
// It returns the child's exit code.
func Run(ctx context.Context, opts Options) (int, error) {
	if len(opts.Command) == 0 {
		return 0, errors.New("proxy: no server command given")
	}
	if opts.ShutdownGrace <= 0 {
		opts.ShutdownGrace = DefaultShutdownGrace
	}

	cmd := exec.Command(opts.Command[0], opts.Command[1:]...)
	cmd.Dir = opts.Dir
	cmd.Env = opts.Env

	// Handing the child our own stderr lets the runtime pass the file
	// descriptor straight through when it is an *os.File. No copying
	// goroutine, no buffering, no chance of reordering the server's logs.
	cmd.Stderr = opts.Stderr

	serverIn, err := cmd.StdinPipe()
	if err != nil {
		return 0, fmt.Errorf("proxy: stdin pipe: %w", err)
	}
	serverOut, err := cmd.StdoutPipe()
	if err != nil {
		return 0, fmt.Errorf("proxy: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("proxy: start %q: %w", opts.Command[0], err)
	}
	opts.logf("started %v (pid %d)", opts.Command, cmd.Process.Pid)

	stopSignals := forwardSignals(cmd, opts.logf)
	defer stopSignals()

	// clientDone closes when the agent stops talking to us; serverDone when
	// the server stops talking back.
	clientDone := make(chan struct{})
	serverDone := make(chan struct{})

	// Agent to server. EOF here is the agent saying it is finished, and the
	// correct response is to close the child's stdin so it can exit on its
	// own rather than be killed.
	go func() {
		defer close(clientDone)
		err := pump(opts.Stdin, serverIn, ToServer, &opts)
		if err != nil && !errors.Is(err, io.EOF) {
			opts.logf("to-server pump stopped: %v", err)
		}
	}()

	// Server to agent. EOF here means the server has closed its stdout,
	// which for a child process means it has exited.
	go func() {
		defer close(serverDone)
		err := pump(serverOut, opts.Stdout, ToClient, &opts)
		if err != nil && !errors.Is(err, io.EOF) {
			opts.logf("to-client pump stopped: %v", err)
		}
	}()

	// Shutdown begins as soon as either side goes quiet, or the caller
	// cancels. Whichever happens first, closing the child's stdin is the
	// spec's graceful signal and is safe to do more than once.
	select {
	case <-clientDone:
		opts.logf("agent closed its side, closing server stdin")
	case <-serverDone:
		opts.logf("server closed its side")
	case <-ctx.Done():
		opts.logf("context cancelled, closing server stdin")
	}
	_ = serverIn.Close()

	// Escalation runs on its own clock, deliberately not gated on the drain
	// below. An earlier version reaped the child only after both pumps had
	// finished, which deadlocked against exactly the case escalation exists
	// for: a server that ignores stdin close never closes its stdout, so the
	// drain never completes and the SIGTERM never fires. The timer has to be
	// independent of the thing it is rescuing.
	stopEscalation := escalate(cmd, opts.ShutdownGrace, opts.logf)

	// Drain before reaping. cmd.Wait closes the stdout pipe when it returns,
	// so calling it while the to-client pump is still reading would truncate
	// the server's final messages. Once the child is gone its stdout closes
	// and this returns promptly; escalation guarantees the child does go.
	<-serverDone
	stopEscalation()

	// The to-server pump may still be blocked reading an stdin the agent is
	// holding open. It is not waited on: the process is exiting, and there is
	// nothing useful left for that goroutine to do.
	return wait(cmd, opts.logf)
}

// escalate enforces the shutdown ladder the spec prescribes to clients:
// having closed the child's stdin, give it time, then SIGTERM, then SIGKILL.
// Having interposed ourselves, we owe the child the same discipline the agent
// would otherwise have given it.
//
// The returned function cancels the remaining steps and is safe to call more
// than once.
func escalate(cmd *exec.Cmd, grace time.Duration, logf func(string, ...any)) func() {
	done := make(chan struct{})

	go func() {
		select {
		case <-done:
			return
		case <-time.After(grace):
		}
		if cmd.Process == nil {
			return
		}
		logf("child still running %s after stdin closed, sending SIGTERM", grace)
		_ = cmd.Process.Signal(syscall.SIGTERM)

		select {
		case <-done:
			return
		case <-time.After(grace):
		}
		logf("child ignored SIGTERM after %s, sending SIGKILL", grace)
		_ = cmd.Process.Kill()
	}()

	return sync.OnceFunc(func() { close(done) })
}

// pump moves messages one at a time from src to dst, tapping each.
//
// It is deliberately message-at-a-time rather than an io.Copy. A byte-level
// copy would be faster and would still be transparent, but it would leave
// nothing to observe, and observation is the entire product.
func pump(src io.Reader, dst io.Writer, dir Direction, opts *Options) error {
	r := jsonrpc.NewReader(src, opts.MaxMessageBytes)
	w := jsonrpc.NewWriter(dst)

	for {
		msg, err := r.ReadMessage()
		if err != nil {
			return err
		}

		// Forwarding comes first and unconditionally. Observation must never
		// be able to delay or suppress a message.
		if werr := w.WriteMessage(msg); werr != nil {
			return fmt.Errorf("%s: write: %w", dir, werr)
		}

		if opts.Observer != nil {
			observe(opts, dir, msg)
		}
	}
}

// observe parses and hands the message to the observer, absorbing anything
// that goes wrong. Neither a malformed message nor a buggy observer is
// allowed to interrupt the stream.
func observe(opts *Options, dir Direction, msg []byte) {
	defer func() {
		if r := recover(); r != nil {
			opts.logf("observer panicked on %s message, continuing: %v", dir, r)
		}
	}()

	env, err := jsonrpc.ParseEnvelope(msg)
	if err != nil {
		// Already forwarded. Report it as unclassified rather than dropping
		// it, so a recorder still sees that something crossed the wire.
		opts.logf("unparseable %s message (%d bytes), forwarded anyway: %v", dir, len(msg), err)
		env = jsonrpc.Envelope{Kind: jsonrpc.KindUnknown}
	}
	opts.Observer.OnMessage(dir, msg, env)
}

// forwardSignals relays termination signals to the child so that a Ctrl-C in
// the agent's terminal reaches the real server rather than orphaning it.
func forwardSignals(cmd *exec.Cmd, logf func(string, ...any)) func() {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-ch:
				if cmd.Process != nil {
					logf("forwarding %v to child", sig)
					_ = cmd.Process.Signal(sig)
				}
			case <-done:
				return
			}
		}
	}()

	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// wait reaps the child and reports its exit status. Escalation has already
// been handled by the time this runs.
func wait(cmd *exec.Cmd, logf func(string, ...any)) (int, error) {
	code, err := exitCode(cmd.Wait())
	if err == nil {
		logf("child exited with status %d", code)
	}
	return code, err
}

// exitCode turns a Wait error into a process exit code.
//
// A child killed by a signal reports -1 through ExitCode, which is not a
// usable status. It is mapped to the shell convention of 128 plus the signal
// number so the agent sees something meaningful.
func exitCode(err error) (int, error) {
	if err == nil {
		return 0, nil
	}

	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return 0, fmt.Errorf("proxy: waiting for server: %w", err)
	}

	if code := ee.ExitCode(); code >= 0 {
		return code, nil
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal()), nil
	}
	return 1, nil
}
