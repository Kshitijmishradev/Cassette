// Package jsonrpc implements framing and minimal envelope parsing for the
// MCP stdio transport.
//
// The transport is newline-delimited JSON: one JSON-RPC message per line,
// with embedded newlines forbidden by the spec. That last rule is what makes
// splitting on '\n' correct rather than merely convenient.
//
// The guiding constraint for this whole package is that cassette must be
// undetectable. A proxy that reserializes JSON, reorders keys, or reformats
// whitespace changes bytes the peer may be depending on. So messages move
// through as opaque bytes, and parsing only ever reads out the few fields
// needed for routing.
//
// Spec: https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio
package jsonrpc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sync"
)

// DefaultMaxMessageBytes caps a single message. Tool results are routinely
// hundreds of kilobytes (file contents, API payloads), so the limit is
// generous. It exists so a malformed or hostile server that never emits a
// newline cannot drive the proxy into unbounded allocation.
const DefaultMaxMessageBytes = 64 << 20 // 64 MiB

// ErrMessageTooLarge is returned when a single message exceeds the limit.
var ErrMessageTooLarge = errors.New("jsonrpc: message exceeds maximum size")

// readBufSize is the bufio window. Chosen so that typical messages are served
// entirely from the fast path without the overflow copy.
const readBufSize = 256 << 10 // 256 KiB

// Reader reads newline-delimited messages from a stream.
type Reader struct {
	br       *bufio.Reader
	overflow []byte
	max      int
}

// NewReader wraps r. A max of 0 uses DefaultMaxMessageBytes.
func NewReader(r io.Reader, max int) *Reader {
	if max <= 0 {
		max = DefaultMaxMessageBytes
	}
	return &Reader{
		br:  bufio.NewReaderSize(r, readBufSize),
		max: max,
	}
}

// ReadMessage returns the next message with its line terminator stripped.
//
// The returned slice is only valid until the next call to ReadMessage. This
// is deliberate: the proxy's common case is read, inspect, forward, and
// returning borrowed bytes keeps a megabyte-scale tool result from being
// copied on every hop. Callers that need to retain a message (the recorder)
// copy it explicitly, which makes that cost visible at the call site instead
// of hidden in here.
//
// A final line without a trailing newline is returned as a message, followed
// by io.EOF on the next call. Truncating it would silently drop a message
// from a server that exited mid-write, and losing data is worse than
// forwarding something the peer may reject.
func (r *Reader) ReadMessage() ([]byte, error) {
	line, err := r.br.ReadSlice('\n')

	if err == nil {
		// The limit is enforced here too, not only on the slow path. A max
		// smaller than the read window would otherwise never be checked.
		if len(line) > r.max {
			return nil, fmt.Errorf("%w (%d bytes, limit %d)", ErrMessageTooLarge, len(line), r.max)
		}
		return trimEOL(line), nil
	}

	if errors.Is(err, bufio.ErrBufferFull) {
		// Slow path: the message is larger than the read window, so it has
		// to be accumulated across several fills. Reuse the overflow buffer
		// so a stream of large messages does not allocate every time.
		buf := append(r.overflow[:0], line...)
		for {
			if len(buf) > r.max {
				r.overflow = buf[:0]
				return nil, fmt.Errorf("%w (%d bytes, limit %d)", ErrMessageTooLarge, len(buf), r.max)
			}
			line, err = r.br.ReadSlice('\n')
			buf = append(buf, line...)
			if !errors.Is(err, bufio.ErrBufferFull) {
				break
			}
		}
		r.overflow = buf
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if len(buf) == 0 {
			return nil, io.EOF
		}
		return trimEOL(buf), nil
	}

	if errors.Is(err, io.EOF) {
		if len(line) > 0 {
			// Unterminated trailing data. Hand it over; report EOF next call.
			r.overflow = append(r.overflow[:0], line...)
			return trimEOL(r.overflow), nil
		}
		return nil, io.EOF
	}

	return nil, err
}

// trimEOL removes a trailing "\n" or "\r\n".
//
// The spec says newline, not CRLF, but a server running under a Windows
// runtime can still emit CRLF. Tolerating it costs two comparisons and
// prevents a whole class of report that would be miserable to debug.
func trimEOL(b []byte) []byte {
	if n := len(b); n > 0 && b[n-1] == '\n' {
		b = b[:n-1]
		if n = len(b); n > 0 && b[n-1] == '\r' {
			b = b[:n-1]
		}
	}
	return b
}

// Writer writes newline-delimited messages to a stream.
type Writer struct {
	mu      sync.Mutex
	w       io.Writer
	scratch []byte
}

// NewWriter wraps w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// WriteMessage frames msg and writes it.
//
// The message and its newline go out in a single Write. Two writes under the
// same mutex would be equally safe within this process, but one syscall is
// cheaper and keeps the message intact for anything watching the pipe.
//
// There is no buffering between messages, and there must not be. This sits
// on the latency path of every tool call an agent makes; a buffered writer
// that waits for more data before flushing would deadlock the moment a
// request and its response are the only traffic on the wire.
func (w *Writer) WriteMessage(msg []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	need := len(msg) + 1
	if cap(w.scratch) < need {
		w.scratch = make([]byte, 0, need*2)
	}
	buf := append(append(w.scratch[:0], msg...), '\n')
	w.scratch = buf

	_, err := w.w.Write(buf)
	return err
}
