package tape

import (
	"encoding/binary"
	"fmt"
	"os"
	"unsafe"
)

// Reader gives read access to a tape.
//
// Every slice and string it returns points into the memory mapping and stays
// valid only until Close. Nothing is copied on the way out, which is the
// whole reason the format exists: serving a recorded response is a hash
// lookup and a write, with no allocation in between.
type Reader struct {
	path    string
	data    []byte
	unmap   func() error
	header  Header
	entries []Entry

	stringsOff uint64
	blobsOff   uint64
	blobsLen   uint64
}

// Open maps the tape at path and validates it.
func Open(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("tape: stat %s: %w", path, err)
	}
	size := st.Size()
	if size < HeaderSize {
		return nil, fmt.Errorf("tape: %s is too small to be a tape (%d bytes)", path, size)
	}
	if size > MaxTapeBytes {
		return nil, fmt.Errorf("tape: %s exceeds the %d byte limit", path, uint64(MaxTapeBytes))
	}

	data, unmap, err := mapFile(f, int(size))
	if err != nil {
		return nil, err
	}

	r := &Reader{path: path, data: data, unmap: unmap}
	if err := r.load(); err != nil {
		_ = unmap()
		return nil, err
	}
	return r, nil
}

func (r *Reader) load() error {
	h, err := UnmarshalHeader(r.data)
	if err != nil {
		return fmt.Errorf("tape: %s: %w", r.path, err)
	}
	r.header = h

	// Everything below this point trusts offsets taken from the file, and
	// the index is then accessed through an unsafe cast. So the offsets are
	// validated once, here, against the actual file size. A truncated or
	// corrupt tape must fail to open rather than produce a slice pointing
	// past the end of the mapping.
	total := uint64(len(r.data))
	indexEnd := h.IndexOff + uint64(h.Count)*EntrySize

	switch {
	case h.IndexOff < HeaderSize:
		return fmt.Errorf("tape: %s: index overlaps the header", r.path)
	case indexEnd > total:
		return fmt.Errorf("tape: %s: index runs past end of file (%d > %d)", r.path, indexEnd, total)
	case h.StringsOff < indexEnd || h.StringsOff > total:
		return fmt.Errorf("tape: %s: string table offset out of range", r.path)
	case h.VectorsOff < h.StringsOff || h.VectorsOff > total:
		return fmt.Errorf("tape: %s: vector offset out of range", r.path)
	case h.BlobsOff < h.VectorsOff || h.BlobsOff > total:
		return fmt.Errorf("tape: %s: blob offset out of range", r.path)
	}

	r.stringsOff = h.StringsOff
	r.blobsOff = h.BlobsOff
	r.blobsLen = total - h.BlobsOff

	if err := r.loadIndex(indexEnd); err != nil {
		return err
	}

	// Validate blob spans once so every later access can skip the check.
	// n is a few thousand, so this costs microseconds and buys the right to
	// hand out mmap slices without bounds-checking on the hot path.
	for i := range r.entries {
		e := &r.entries[i]
		if uint64(e.ReqOff)+uint64(e.ReqLen) > r.blobsLen {
			return fmt.Errorf("tape: %s: entry %d request blob out of range", r.path, i)
		}
		if uint64(e.RespOff)+uint64(e.RespLen) > r.blobsLen {
			return fmt.Errorf("tape: %s: entry %d response blob out of range", r.path, i)
		}
	}
	return nil
}

// loadIndex exposes the index region as a slice of Entry.
//
// The fast path casts the mapped bytes directly, so the index costs nothing
// to load regardless of size: no decode loop, no allocation, just a pointer.
// That is legal only when the region is aligned for the struct's widest
// field, which a page-aligned mapping plus a 64-byte header guarantees. The
// check is still made, and a misaligned region falls back to decoding,
// because silently producing misaligned pointers would fault on some
// architectures and quietly misread on others.
func (r *Reader) loadIndex(indexEnd uint64) error {
	if r.header.Count == 0 {
		r.entries = nil
		return nil
	}

	base := unsafe.Pointer(&r.data[r.header.IndexOff])
	if uintptr(base)%unsafe.Alignof(Entry{}) == 0 {
		r.entries = unsafe.Slice((*Entry)(base), int(r.header.Count))
		return nil
	}
	return r.decodeIndex()
}

// decodeIndex is the portable fallback for a misaligned mapping.
func (r *Reader) decodeIndex() error {
	r.entries = make([]Entry, r.header.Count)
	for i := range r.entries {
		b := r.data[r.header.IndexOff+uint64(i)*EntrySize:]
		e := &r.entries[i]
		e.StartedNanos = int64(binary.LittleEndian.Uint64(b[0:]))
		e.DurationNs = int64(binary.LittleEndian.Uint64(b[8:]))
		e.KeyHash = binary.LittleEndian.Uint64(b[16:])
		e.NormHash = binary.LittleEndian.Uint64(b[24:])
		e.Seq = binary.LittleEndian.Uint32(b[32:])
		e.Flags = binary.LittleEndian.Uint32(b[36:])
		e.MethodID = binary.LittleEndian.Uint32(b[40:])
		e.ToolID = binary.LittleEndian.Uint32(b[44:])
		e.VecIdx = binary.LittleEndian.Uint32(b[48:])
		e.ReqOff = binary.LittleEndian.Uint32(b[52:])
		e.ReqLen = binary.LittleEndian.Uint32(b[56:])
		e.RespOff = binary.LittleEndian.Uint32(b[60:])
		e.RespLen = binary.LittleEndian.Uint32(b[64:])
	}
	return nil
}

// Close releases the mapping. Every slice and string previously returned
// becomes invalid.
func (r *Reader) Close() error {
	if r.unmap == nil {
		return nil
	}
	err := r.unmap()
	r.unmap = nil
	r.data = nil
	r.entries = nil
	return err
}

// Header returns the tape header.
func (r *Reader) Header() Header { return r.header }

// Len is the number of recorded exchanges.
func (r *Reader) Len() int { return len(r.entries) }

// Entries exposes the index directly for scanning.
func (r *Reader) Entries() []Entry { return r.entries }

// Entry returns the i'th index entry.
func (r *Reader) Entry(i int) Entry { return r.entries[i] }

// Request returns the pre-framed request bytes for entry i, including the
// trailing newline. Borrowed from the mapping; valid until Close.
func (r *Reader) Request(i int) []byte {
	e := r.entries[i]
	return r.blob(e.ReqOff, e.ReqLen)
}

// Response returns the pre-framed response bytes for entry i, or nil when
// the exchange had no response. Borrowed from the mapping; valid until Close.
func (r *Reader) Response(i int) []byte {
	e := r.entries[i]
	if !e.HasResponse() {
		return nil
	}
	return r.blob(e.RespOff, e.RespLen)
}

func (r *Reader) blob(off, length uint32) []byte {
	if length == 0 {
		return nil
	}
	start := r.blobsOff + uint64(off)
	return r.data[start : start+uint64(length)]
}

// Method returns the method name for entry i.
func (r *Reader) Method(i int) string { return r.str(r.entries[i].MethodID) }

// ToolName returns the tool name for entry i, empty when not a tool call.
func (r *Reader) ToolName(i int) string { return r.str(r.entries[i].ToolID) }

// str decodes an interned string at the given table offset.
//
// The result points into the mapping rather than copying. Names are read on
// every lookup during replay, and at a few thousand calls per run an
// allocation each would be pure waste.
func (r *Reader) str(off uint32) string {
	buf := r.data[r.stringsOff:r.header.VectorsOff]
	if uint64(off) >= uint64(len(buf)) {
		return ""
	}
	n, used := binary.Uvarint(buf[off:])
	if used <= 0 || n == 0 {
		return ""
	}
	start := uint64(off) + uint64(used)
	end := start + n
	if end > uint64(len(buf)) {
		return ""
	}
	return unsafe.String(&buf[start], int(n))
}
