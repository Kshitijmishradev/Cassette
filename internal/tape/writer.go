package tape

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Record is one exchange handed to the writer.
//
// Request and Response are the exact message bytes that crossed the wire,
// without their trailing newline; the writer adds it, so what lands on disk
// is what goes back out during replay with no serialization step.
type Record struct {
	Method   string
	ToolName string

	Request  []byte
	Response []byte

	StartedNanos int64
	DurationNs   int64

	KeyHash  uint64
	NormHash uint64
	Flags    uint32
}

// Writer builds a tape.
//
// Blobs stream to a scratch file while the run is in progress and the index
// is held in memory, because the header cannot be written until the section
// sizes are known and those are not known until the run ends. The index is
// cheap to hold: 72 bytes per call means a 10,000-call run costs 720 KB.
//
// The final file is assembled under a temporary name and renamed into place,
// so a crash during assembly leaves no half-written tape for a later run to
// read as if it were whole.
type Writer struct {
	path    string
	scratch *os.File
	buf     *bufio.Writer

	entries []Entry
	strings stringTable

	blobLen uint32
	created int64
	closed  bool
}

// Create opens a writer for the tape at path.
func Create(path string) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("tape: create directory: %w", err)
	}
	scratch, err := os.CreateTemp(filepath.Dir(path), ".cassette-blobs-*")
	if err != nil {
		return nil, fmt.Errorf("tape: scratch file: %w", err)
	}

	w := &Writer{
		path:    path,
		scratch: scratch,
		buf:     bufio.NewWriterSize(scratch, 256<<10),
		created: time.Now().UnixNano(),
	}
	w.strings.init()
	return w, nil
}

// Append adds one exchange.
func (w *Writer) Append(r Record) error {
	if w.closed {
		return errors.New("tape: append after close")
	}

	e := Entry{
		StartedNanos: r.StartedNanos,
		DurationNs:   r.DurationNs,
		KeyHash:      r.KeyHash,
		NormHash:     r.NormHash,
		Seq:          uint32(len(w.entries)),
		Flags:        r.Flags,
		MethodID:     w.strings.intern(r.Method),
		ToolID:       w.strings.intern(r.ToolName),
	}

	var err error
	if e.ReqOff, e.ReqLen, err = w.writeBlob(r.Request); err != nil {
		return err
	}
	if r.Response != nil {
		if e.RespOff, e.RespLen, err = w.writeBlob(r.Response); err != nil {
			return err
		}
		e.Flags |= EntryHasResponse
	}

	w.entries = append(w.entries, e)
	return nil
}

// writeBlob appends a pre-framed message and returns its span.
func (w *Writer) writeBlob(msg []byte) (off, length uint32, err error) {
	if msg == nil {
		return 0, 0, nil
	}

	need := uint64(w.blobLen) + uint64(len(msg)) + 1
	if need > MaxTapeBytes {
		return 0, 0, fmt.Errorf("tape: blob section would exceed %d bytes", uint64(MaxTapeBytes))
	}

	off = w.blobLen
	if _, err := w.buf.Write(msg); err != nil {
		return 0, 0, fmt.Errorf("tape: write blob: %w", err)
	}
	// The newline is stored, not implied. Replay then writes the slice
	// verbatim with no framing step of its own.
	if err := w.buf.WriteByte('\n'); err != nil {
		return 0, 0, fmt.Errorf("tape: write blob: %w", err)
	}

	length = uint32(len(msg)) + 1
	w.blobLen += length
	return off, length, nil
}

// Len reports how many exchanges have been appended.
func (w *Writer) Len() int { return len(w.entries) }

// Close assembles the tape and renames it into place.
func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	defer func() {
		w.scratch.Close()
		os.Remove(w.scratch.Name())
	}()

	if err := w.buf.Flush(); err != nil {
		return fmt.Errorf("tape: flush blobs: %w", err)
	}
	if _, err := w.scratch.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("tape: rewind blobs: %w", err)
	}

	strs := w.strings.bytes()

	indexOff := uint64(HeaderSize)
	indexLen := uint64(len(w.entries)) * EntrySize
	stringsOff := indexOff + indexLen
	vectorsOff := stringsOff + uint64(len(strs))
	blobsOff := vectorsOff // no vector section yet; phase 3 fills this in

	h := Header{
		Magic:        Magic,
		Version:      Version,
		Flags:        FlagComplete,
		Count:        uint32(len(w.entries)),
		VectorDim:    0,
		IndexOff:     indexOff,
		StringsOff:   stringsOff,
		VectorsOff:   vectorsOff,
		BlobsOff:     blobsOff,
		CreatedNanos: w.created,
	}

	f, err := os.CreateTemp(filepath.Dir(w.path), ".cassette-final-*")
	if err != nil {
		return fmt.Errorf("tape: create temporary tape: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op once the rename below succeeds

	out := bufio.NewWriterSize(f, 256<<10)

	hb, _ := h.MarshalBinary()
	if _, err := out.Write(hb); err != nil {
		f.Close()
		return fmt.Errorf("tape: write header: %w", err)
	}
	if err := writeIndex(out, w.entries); err != nil {
		f.Close()
		return err
	}
	if _, err := out.Write(strs); err != nil {
		f.Close()
		return fmt.Errorf("tape: write strings: %w", err)
	}
	if _, err := io.Copy(out, w.scratch); err != nil {
		f.Close()
		return fmt.Errorf("tape: copy blobs: %w", err)
	}
	if err := out.Flush(); err != nil {
		f.Close()
		return fmt.Errorf("tape: flush tape: %w", err)
	}

	// fsync before rename. A tape is a test fixture that gets committed to
	// git; one that survives a crash as a valid-looking file with garbage
	// blobs would be worse than one that is simply absent.
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("tape: sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("tape: close: %w", err)
	}
	if err := os.Rename(tmp, w.path); err != nil {
		return fmt.Errorf("tape: rename into place: %w", err)
	}
	return nil
}

// writeIndex emits entries in explicit little-endian rather than casting the
// slice. The reader casts on load because that is the hot path; writing
// happens once per run, where being obviously correct is worth more.
func writeIndex(w io.Writer, entries []Entry) error {
	b := make([]byte, EntrySize)
	for i := range entries {
		e := &entries[i]
		binary.LittleEndian.PutUint64(b[0:], uint64(e.StartedNanos))
		binary.LittleEndian.PutUint64(b[8:], uint64(e.DurationNs))
		binary.LittleEndian.PutUint64(b[16:], e.KeyHash)
		binary.LittleEndian.PutUint64(b[24:], e.NormHash)
		binary.LittleEndian.PutUint32(b[32:], e.Seq)
		binary.LittleEndian.PutUint32(b[36:], e.Flags)
		binary.LittleEndian.PutUint32(b[40:], e.MethodID)
		binary.LittleEndian.PutUint32(b[44:], e.ToolID)
		binary.LittleEndian.PutUint32(b[48:], e.VecIdx)
		binary.LittleEndian.PutUint32(b[52:], e.ReqOff)
		binary.LittleEndian.PutUint32(b[56:], e.ReqLen)
		binary.LittleEndian.PutUint32(b[60:], e.RespOff)
		binary.LittleEndian.PutUint32(b[64:], e.RespLen)
		binary.LittleEndian.PutUint32(b[68:], 0)
		if _, err := w.Write(b); err != nil {
			return fmt.Errorf("tape: write index: %w", err)
		}
	}
	return nil
}

// stringTable interns method and tool names.
//
// A run makes thousands of calls across a handful of distinct method and
// tool names, so storing the name on every entry would waste most of the
// index. Interning turns each into a 4-byte offset.
//
// Layout is a uvarint length followed by the bytes, so a reader can decode a
// name from an offset without an auxiliary table. Offset 0 is always the
// empty string, which lets a zero ToolID mean "not a tool call" for free.
type stringTable struct {
	buf    []byte
	offset map[string]uint32
}

func (s *stringTable) init() {
	s.offset = make(map[string]uint32, 16)
	s.intern("") // reserve offset 0
}

func (s *stringTable) intern(v string) uint32 {
	if off, ok := s.offset[v]; ok {
		return off
	}
	off := uint32(len(s.buf))
	s.buf = binary.AppendUvarint(s.buf, uint64(len(v)))
	s.buf = append(s.buf, v...)
	s.offset[v] = off
	return off
}

func (s *stringTable) bytes() []byte { return s.buf }
