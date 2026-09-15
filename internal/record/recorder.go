// Package record captures a live agent session onto a tape.
//
// The recorder is a proxy.Observer, so it sits inline on the forwarding path
// of every message. That placement dictates its shape: whatever it does per
// message is added to the latency of every tool call the agent makes.
//
// So it does almost nothing inline. It copies the borrowed bytes, correlates
// a response to its request, and hands the result to a writer goroutine. All
// file I/O happens off the forwarding path.
package record

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Kshitijmishradev/cassette/internal/jsonrpc"
	"github.com/Kshitijmishradev/cassette/internal/proxy"
	"github.com/Kshitijmishradev/cassette/internal/tape"
)

// queueDepth bounds how far the writer may fall behind.
//
// When it fills, OnMessage blocks. That is the deliberate choice: the
// alternative is dropping records, and a tape with silent holes is worse
// than a tape that cost a few microseconds to produce. A recording that
// quietly omits calls would make every replay built on it wrong in a way
// nothing downstream could detect.
//
// In practice it never fills. The writer's per-record work is a buffered
// append, and the producer is gated by a model turn between calls.
const queueDepth = 256

// Recorder writes every message crossing the proxy to a tape.
type Recorder struct {
	w  *tape.Writer
	ch chan tape.Record

	// pending holds requests awaiting a response, keyed by the raw JSON-RPC
	// id. The raw bytes are the key rather than a decoded value, because
	// that is what correlation has to match on.
	mu      sync.Mutex
	pending map[string]*inflight

	wg     sync.WaitGroup
	closed bool

	errMu sync.Mutex
	errs  []error
}

type inflight struct {
	method   string
	toolName string
	request  []byte
	started  time.Time
	keyHash  uint64
	normHash uint64
	flags    uint32
}

// New opens a recorder writing to the tape at path.
func New(path string) (*Recorder, error) {
	w, err := tape.Create(path)
	if err != nil {
		return nil, err
	}

	r := &Recorder{
		w:       w,
		ch:      make(chan tape.Record, queueDepth),
		pending: make(map[string]*inflight, 64),
	}

	r.wg.Add(1)
	go r.writeLoop()
	return r, nil
}

func (r *Recorder) writeLoop() {
	defer r.wg.Done()
	for rec := range r.ch {
		if err := r.w.Append(rec); err != nil {
			r.note(fmt.Errorf("append %s: %w", rec.Method, err))
		}
	}
}

func (r *Recorder) note(err error) {
	r.errMu.Lock()
	defer r.errMu.Unlock()
	r.errs = append(r.errs, err)
}

// OnMessage implements proxy.Observer.
func (r *Recorder) OnMessage(dir proxy.Direction, raw []byte, env jsonrpc.Envelope) {
	if dir == proxy.ToServer {
		r.onClientMessage(raw, env)
		return
	}
	r.onServerMessage(raw, env)
}

func (r *Recorder) onClientMessage(raw []byte, env jsonrpc.Envelope) {
	now := time.Now()

	// The slice is borrowed and dies when OnMessage returns, so anything
	// kept has to be copied here. This is the one unavoidable copy in the
	// recording path, and it is why recording costs more than passthrough.
	msg := append([]byte(nil), raw...)

	call := &inflight{
		method:  env.Method,
		request: msg,
		started: now,
	}

	args, err := jsonrpc.ParseParams(msg)
	if err != nil {
		r.note(fmt.Errorf("params of %s: %w", env.Method, err))
	}

	if env.IsToolCall() {
		call.flags |= tape.EntryIsToolCall
		if tc, err := jsonrpc.ParseToolCall(msg); err == nil {
			call.toolName = tc.Name
			// Hash arguments separately. The matcher also requires the
			// stored method and ToolName to agree; hashes alone cannot
			// distinguish different tools with the same arguments.
			args = tc.Arguments
		} else {
			r.note(fmt.Errorf("tools/call: %w", err))
		}
	}

	call.keyHash = tape.HashKey(env.Method, args)
	call.normHash = tape.HashNorm(env.Method, args)

	// A notification gets no reply, so it is complete the moment it is seen.
	if env.Kind == jsonrpc.KindNotification || !env.HasID() {
		r.emit(tape.Record{
			Method:       call.method,
			ToolName:     call.toolName,
			Request:      call.request,
			StartedNanos: now.UnixNano(),
			KeyHash:      call.keyHash,
			NormHash:     call.normHash,
			Flags:        call.flags,
		})
		return
	}

	r.mu.Lock()
	r.pending[string(env.ID)] = call
	r.mu.Unlock()
}

func (r *Recorder) onServerMessage(raw []byte, env jsonrpc.Envelope) {
	now := time.Now()

	// A server message with no id was not asked for: progress updates, log
	// messages. Replay has to emit these unprompted, so they are recorded
	// as response-only entries rather than discarded.
	if !env.HasID() {
		flags := uint32(tape.EntryServerInitiated)
		if env.IsError {
			flags |= tape.EntryIsError
		}
		r.emit(tape.Record{
			Method:       env.Method,
			Response:     append([]byte(nil), raw...),
			StartedNanos: now.UnixNano(),
			Flags:        flags,
		})
		return
	}

	r.mu.Lock()
	call, ok := r.pending[string(env.ID)]
	if ok {
		delete(r.pending, string(env.ID))
	}
	r.mu.Unlock()

	if !ok {
		// A response to a request we never saw. Should not happen through a
		// proxy that sees both directions, but recording it unattached is
		// better than dropping evidence that it occurred.
		r.note(fmt.Errorf("response with unknown id %s", env.ID))
		r.emit(tape.Record{
			Response:     append([]byte(nil), raw...),
			StartedNanos: now.UnixNano(),
			Flags:        tape.EntryServerInitiated,
		})
		return
	}

	flags := call.flags
	if env.IsError {
		// Errors are recorded and replayed like any other response. How an
		// agent behaves when a tool fails is exactly the behavior worth
		// being able to test.
		flags |= tape.EntryIsError
	}

	r.emit(tape.Record{
		Method:       call.method,
		ToolName:     call.toolName,
		Request:      call.request,
		Response:     append([]byte(nil), raw...),
		StartedNanos: call.started.UnixNano(),
		DurationNs:   now.Sub(call.started).Nanoseconds(),
		KeyHash:      call.keyHash,
		NormHash:     call.normHash,
		Flags:        flags,
	})
}

func (r *Recorder) emit(rec tape.Record) {
	defer func() {
		// The channel is closed by Close. A message arriving after that is
		// a race between the proxy shutting down and the last frames
		// crossing, and it must not panic the agent's tooling.
		if p := recover(); p != nil {
			r.note(errors.New("record emitted after close"))
		}
	}()
	r.ch <- rec
}

// Close flushes and finalizes the tape.
//
// Requests still awaiting a response are written as truncated. They are
// evidence that the run was cut short, and a tape that simply omitted them
// would look like a clean but shorter session.
func (r *Recorder) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true

	r.mu.Lock()
	for _, call := range r.pending {
		r.ch <- tape.Record{
			Method:       call.method,
			ToolName:     call.toolName,
			Request:      call.request,
			StartedNanos: call.started.UnixNano(),
			KeyHash:      call.keyHash,
			NormHash:     call.normHash,
			Flags:        call.flags | tape.EntryTruncated,
		}
	}
	clear(r.pending)
	r.mu.Unlock()

	close(r.ch)
	r.wg.Wait()

	if err := r.w.Close(); err != nil {
		return err
	}

	r.errMu.Lock()
	defer r.errMu.Unlock()
	return errors.Join(r.errs...)
}

// Len reports how many exchanges have been written so far.
func (r *Recorder) Len() int { return r.w.Len() }
