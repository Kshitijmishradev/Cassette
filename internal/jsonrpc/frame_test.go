package jsonrpc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestReaderBasicMessages(t *testing.T) {
	in := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n" +
		`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}` + "\n"

	r := NewReader(strings.NewReader(in), 0)

	want := []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`,
	}
	for i, w := range want {
		got, err := r.ReadMessage()
		if err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if string(got) != w {
			t.Errorf("message %d = %q, want %q", i, got, w)
		}
	}
	if _, err := r.ReadMessage(); !errors.Is(err, io.EOF) {
		t.Errorf("want io.EOF, got %v", err)
	}
}

// A message larger than the read window must survive intact. Tool results
// carrying file contents routinely exceed it, so this is the normal case for
// the slow path, not an edge case.
func TestReaderLargeMessageCrossesReadWindow(t *testing.T) {
	for _, size := range []int{
		readBufSize - 1,
		readBufSize,
		readBufSize + 1,
		readBufSize * 3,
		readBufSize*4 + 7,
	} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			payload := strings.Repeat("x", size)
			msg := `{"jsonrpc":"2.0","id":1,"result":"` + payload + `"}`

			r := NewReader(strings.NewReader(msg+"\n"), 0)
			got, err := r.ReadMessage()
			if err != nil {
				t.Fatalf("ReadMessage: %v", err)
			}
			if string(got) != msg {
				t.Errorf("length %d, want %d; content mismatch", len(got), len(msg))
			}
		})
	}
}

// Several large messages in a row exercise overflow-buffer reuse. If the
// buffer were mishandled, message N would be contaminated by message N-1.
func TestReaderConsecutiveLargeMessages(t *testing.T) {
	var in bytes.Buffer
	var want []string
	for i := range 4 {
		msg := fmt.Sprintf(`{"id":%d,"result":"%s"}`, i, strings.Repeat(string(rune('a'+i)), readBufSize+1000))
		want = append(want, msg)
		in.WriteString(msg)
		in.WriteByte('\n')
	}

	r := NewReader(&in, 0)
	for i, w := range want {
		got, err := r.ReadMessage()
		if err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if string(got) != w {
			t.Errorf("message %d corrupted (len %d, want %d)", i, len(got), len(w))
		}
	}
}

// A server that exits mid-write leaves an unterminated final line. Dropping
// it would silently lose a message, which is worse than forwarding something
// the peer may reject.
func TestReaderUnterminatedFinalMessage(t *testing.T) {
	r := NewReader(strings.NewReader(`{"id":1}`+"\n"+`{"id":2}`), 0)

	first, err := r.ReadMessage()
	if err != nil || string(first) != `{"id":1}` {
		t.Fatalf("first = %q, %v", first, err)
	}
	second, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("second: unexpected error %v", err)
	}
	if string(second) != `{"id":2}` {
		t.Errorf("second = %q, want %q", second, `{"id":2}`)
	}
	if _, err := r.ReadMessage(); !errors.Is(err, io.EOF) {
		t.Errorf("want io.EOF, got %v", err)
	}
}

func TestReaderTrimsCRLF(t *testing.T) {
	r := NewReader(strings.NewReader(`{"id":1}`+"\r\n"+`{"id":2}`+"\n"), 0)

	for _, want := range []string{`{"id":1}`, `{"id":2}`} {
		got, err := r.ReadMessage()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if string(got) != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

// Blank lines are not valid MCP messages, but the reader is not the layer
// that decides that. It reports what was on the wire and lets the proxy
// forward it untouched.
func TestReaderEmptyLine(t *testing.T) {
	r := NewReader(strings.NewReader("\n"+`{"id":1}`+"\n"), 0)

	got, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("%v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty message, got %q", got)
	}
}

func TestReaderEnforcesMaxSize(t *testing.T) {
	// Below the read window, so this only passes if the limit is enforced on
	// the fast path as well as the slow one.
	small := NewReader(strings.NewReader(strings.Repeat("x", 500)+"\n"), 100)
	if _, err := small.ReadMessage(); !errors.Is(err, ErrMessageTooLarge) {
		t.Errorf("fast path: want ErrMessageTooLarge, got %v", err)
	}

	// Above the read window, exercising the accumulation path.
	big := NewReader(strings.NewReader(strings.Repeat("x", readBufSize*3)+"\n"), readBufSize+10)
	if _, err := big.ReadMessage(); !errors.Is(err, ErrMessageTooLarge) {
		t.Errorf("slow path: want ErrMessageTooLarge, got %v", err)
	}
}

func TestWriterFramesWithNewline(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteMessage([]byte(`{"id":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteMessage([]byte(`{"id":2}`)); err != nil {
		t.Fatal(err)
	}

	want := `{"id":1}` + "\n" + `{"id":2}` + "\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

// countingWriter records each Write call so the test can assert that a
// message and its newline leave in one syscall rather than two.
type countingWriter struct {
	mu     sync.Mutex
	writes [][]byte
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, append([]byte(nil), p...))
	return len(p), nil
}

func TestWriterSingleWritePerMessage(t *testing.T) {
	cw := &countingWriter{}
	w := NewWriter(cw)

	if err := w.WriteMessage([]byte(`{"id":1}`)); err != nil {
		t.Fatal(err)
	}
	if len(cw.writes) != 1 {
		t.Fatalf("got %d writes, want 1", len(cw.writes))
	}
	if string(cw.writes[0]) != `{"id":1}`+"\n" {
		t.Errorf("write = %q", cw.writes[0])
	}
}

// The proxy has a goroutine per direction, and a recorder may write from
// another. Interleaved writes must never tear a message.
func TestWriterConcurrentWritesDoNotTear(t *testing.T) {
	cw := &countingWriter{}
	w := NewWriter(cw)

	const goroutines, each = 8, 50
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			msg := []byte(fmt.Sprintf(`{"g":%d,"pad":"%s"}`, g, strings.Repeat("y", 1000)))
			for range each {
				if err := w.WriteMessage(msg); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()

	if len(cw.writes) != goroutines*each {
		t.Fatalf("got %d writes, want %d", len(cw.writes), goroutines*each)
	}
	for i, wr := range cw.writes {
		if wr[len(wr)-1] != '\n' {
			t.Fatalf("write %d does not end in newline", i)
		}
		if bytes.Count(wr, []byte("\n")) != 1 {
			t.Fatalf("write %d contains %d newlines, want 1", i, bytes.Count(wr, []byte("\n")))
		}
	}
}

// Round-tripping proves the two halves agree, which is what the proxy relies
// on when it reads from one pipe and writes to another.
func TestFrameRoundTrip(t *testing.T) {
	msgs := []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_file"}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"` + strings.Repeat("z", readBufSize+5) + `"}]}}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`,
		`{}`,
	}

	var buf bytes.Buffer
	w := NewWriter(&buf)
	for _, m := range msgs {
		if err := w.WriteMessage([]byte(m)); err != nil {
			t.Fatal(err)
		}
	}

	r := NewReader(&buf, 0)
	for i, want := range msgs {
		got, err := r.ReadMessage()
		if err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if string(got) != want {
			t.Errorf("message %d mismatch (len %d vs %d)", i, len(got), len(want))
		}
	}
}

// cycleReader replays a fixed byte slice forever, so the benchmark measures
// ReadMessage rather than Reader construction. The earlier version built a
// new Reader per iteration, which charged every run for a 256 KiB bufio
// allocation and buried the number actually being measured.
type cycleReader struct {
	data []byte
	pos  int
}

func (c *cycleReader) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		m := copy(p[n:], c.data[c.pos:])
		n += m
		c.pos += m
		if c.pos == len(c.data) {
			c.pos = 0
		}
	}
	return n, nil
}

func BenchmarkReadMessage(b *testing.B) {
	for _, size := range []int{256, 8 << 10, 512 << 10} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			msg := append([]byte(`{"id":1,"result":"`), bytes.Repeat([]byte("x"), size)...)
			msg = append(msg, []byte(`"}`+"\n")...)

			r := NewReader(&cycleReader{data: msg}, 0)
			b.SetBytes(int64(len(msg)))
			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				if _, err := r.ReadMessage(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkWriteMessage(b *testing.B) {
	for _, size := range []int{256, 8 << 10, 512 << 10} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			msg := append([]byte(`{"id":1,"result":"`), bytes.Repeat([]byte("x"), size)...)
			msg = append(msg, []byte(`"}`)...)

			w := NewWriter(io.Discard)
			b.SetBytes(int64(len(msg) + 1))
			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				if err := w.WriteMessage(msg); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
