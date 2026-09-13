package tape

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

// The reader casts mapped bytes straight to []Entry. That is only correct if
// the struct is exactly the size the format promises. If a field is added and
// EntrySize is not bumped, every tape written afterwards is silently
// unreadable, so this assertion is the guard.
func TestEntrySizeMatchesFormatConstant(t *testing.T) {
	if got := unsafe.Sizeof(Entry{}); got != EntrySize {
		t.Fatalf("unsafe.Sizeof(Entry{}) = %d, format constant says %d", got, EntrySize)
	}
	if EntrySize%8 != 0 {
		t.Errorf("EntrySize %d is not 8-byte aligned; the index cast requires it", EntrySize)
	}
	if HeaderSize%8 != 0 {
		t.Errorf("HeaderSize %d is not 8-byte aligned; the index starts right after it", HeaderSize)
	}
}

func TestHeaderRoundTrip(t *testing.T) {
	want := Header{
		Magic: Magic, Version: Version, Flags: FlagComplete,
		Count: 42, VectorDim: 128,
		IndexOff: 64, StringsOff: 3088, VectorsOff: 3200, BlobsOff: 24704,
		CreatedNanos: 1757700000000000000,
	}
	b, err := want.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != HeaderSize {
		t.Fatalf("header encoded to %d bytes, want %d", len(b), HeaderSize)
	}
	got, err := UnmarshalHeader(b)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("round trip changed the header:\n got %+v\nwant %+v", got, want)
	}
}

func TestUnmarshalHeaderRejectsForeignFiles(t *testing.T) {
	b := make([]byte, HeaderSize)
	copy(b, "NOTATAPE")
	if _, err := UnmarshalHeader(b); err == nil {
		t.Error("want an error for a file that is not a tape")
	}
	if _, err := UnmarshalHeader(b[:10]); err == nil {
		t.Error("want an error for a short header")
	}
}

// writeTape builds a tape from a compact description and returns its path.
func writeTape(t *testing.T, recs []Record) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.cas")

	w, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range recs {
		if err := w.Append(r); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func sampleRecords() []Record {
	return []Record{
		{
			Method:       "initialize",
			Request:      []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`),
			Response:     []byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`),
			StartedNanos: 1000, DurationNs: 500,
			KeyHash: HashKey("initialize", []byte(`{}`)),
		},
		{
			Method:       "notifications/initialized",
			Request:      []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`),
			Response:     nil, // notifications never get one
			StartedNanos: 2000,
		},
		{
			Method:       "tools/call",
			ToolName:     "read_file",
			Request:      []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/tmp/x"}}}`),
			Response:     []byte(`{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"hello"}]}}`),
			StartedNanos: 3000, DurationNs: 12000,
			KeyHash:  HashKey("tools/call", []byte(`{"path":"/tmp/x"}`)),
			NormHash: HashNorm("tools/call", []byte(`{"path":"/tmp/x"}`)),
			Flags:    EntryIsToolCall,
		},
		{
			Method:       "tools/call",
			ToolName:     "read_file",
			Request:      []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/nope"}}}`),
			Response:     []byte(`{"jsonrpc":"2.0","id":3,"error":{"code":-32602,"message":"no such file"}}`),
			StartedNanos: 4000, DurationNs: 900,
			Flags: EntryIsToolCall | EntryIsError,
		},
	}
}

func TestWriteThenReadRoundTrip(t *testing.T) {
	recs := sampleRecords()
	path := writeTape(t, recs)

	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if !r.Header().Complete() {
		t.Error("tape closed cleanly but is not marked complete")
	}
	if r.Len() != len(recs) {
		t.Fatalf("Len = %d, want %d", r.Len(), len(recs))
	}

	for i, want := range recs {
		e := r.Entry(i)

		if got := r.Method(i); got != want.Method {
			t.Errorf("entry %d Method = %q, want %q", i, got, want.Method)
		}
		if got := r.ToolName(i); got != want.ToolName {
			t.Errorf("entry %d ToolName = %q, want %q", i, got, want.ToolName)
		}
		if e.Seq != uint32(i) {
			t.Errorf("entry %d Seq = %d", i, e.Seq)
		}
		if e.StartedNanos != want.StartedNanos {
			t.Errorf("entry %d StartedNanos = %d, want %d", i, e.StartedNanos, want.StartedNanos)
		}

		// Blobs come back pre-framed: exactly the wire bytes plus the
		// newline, so replay can write them without a serialization step.
		wantReq := string(want.Request) + "\n"
		if got := string(r.Request(i)); got != wantReq {
			t.Errorf("entry %d request = %q, want %q", i, got, wantReq)
		}

		if want.Response == nil {
			if e.HasResponse() {
				t.Errorf("entry %d claims a response but none was recorded", i)
			}
			if r.Response(i) != nil {
				t.Errorf("entry %d returned a response blob for a notification", i)
			}
			continue
		}
		if !e.HasResponse() {
			t.Errorf("entry %d has a response but the flag is unset", i)
		}
		wantResp := string(want.Response) + "\n"
		if got := string(r.Response(i)); got != wantResp {
			t.Errorf("entry %d response = %q, want %q", i, got, wantResp)
		}
	}
}

// A notification and a request whose response was lost must not look alike.
// Without the distinction, replay would hang waiting for a reply that was
// never coming.
func TestNotificationDistinguishedFromMissingResponse(t *testing.T) {
	path := writeTape(t, []Record{
		{Method: "notifications/initialized", Request: []byte(`{"method":"notifications/initialized"}`)},
		{Method: "tools/call", Request: []byte(`{"id":9,"method":"tools/call"}`), Flags: EntryTruncated},
	})

	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if r.Entry(0).Truncated() {
		t.Error("notification wrongly marked truncated")
	}
	if !r.Entry(1).Truncated() {
		t.Error("interrupted call not marked truncated")
	}
}

func TestLargeBlobsSurvive(t *testing.T) {
	big := `{"jsonrpc":"2.0","id":1,"result":"` + strings.Repeat("x", 2<<20) + `"}`
	path := writeTape(t, []Record{
		{Method: "tools/call", Request: []byte(`{"id":1}`), Response: []byte(big)},
	})

	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if got := string(r.Response(0)); got != big+"\n" {
		t.Errorf("2 MB response did not survive: got %d bytes, want %d", len(got), len(big)+1)
	}
}

func TestManyEntries(t *testing.T) {
	const n = 5000
	recs := make([]Record, n)
	for i := range recs {
		recs[i] = Record{
			Method:       "tools/call",
			ToolName:     fmt.Sprintf("tool_%d", i%7),
			Request:      []byte(fmt.Sprintf(`{"id":%d,"method":"tools/call"}`, i)),
			Response:     []byte(fmt.Sprintf(`{"id":%d,"result":{"n":%d}}`, i, i)),
			StartedNanos: int64(i) * 1000,
			KeyHash:      uint64(i),
			Flags:        EntryIsToolCall,
		}
	}
	path := writeTape(t, recs)

	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if r.Len() != n {
		t.Fatalf("Len = %d, want %d", r.Len(), n)
	}
	for _, i := range []int{0, 1, n / 2, n - 1} {
		if got, want := r.Entry(i).KeyHash, uint64(i); got != want {
			t.Errorf("entry %d KeyHash = %d, want %d", i, got, want)
		}
		if got, want := r.ToolName(i), fmt.Sprintf("tool_%d", i%7); got != want {
			t.Errorf("entry %d ToolName = %q, want %q", i, got, want)
		}
	}

	// Interning: seven distinct tool names across 5000 entries should leave
	// the string table tiny. If it grows with entry count, names are being
	// stored per entry.
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	h := r.Header()
	stringsLen := h.VectorsOff - h.StringsOff
	if stringsLen > 512 {
		t.Errorf("string table is %d bytes for 7 distinct names; interning is not working", stringsLen)
	}
	t.Logf("tape: %d bytes total, index %d, strings %d, %d entries",
		st.Size(), uint64(n)*EntrySize, stringsLen, n)
}

func TestEmptyTape(t *testing.T) {
	path := writeTape(t, nil)

	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if r.Len() != 0 {
		t.Errorf("Len = %d, want 0", r.Len())
	}
	if !r.Header().Complete() {
		t.Error("empty tape should still be marked complete")
	}
}

// A tape is a committed test fixture. A corrupt one must fail to open rather
// than produce slices pointing outside the mapping, because the index is
// reached through an unsafe cast.
func TestOpenRejectsCorruptTapes(t *testing.T) {
	good := writeTape(t, sampleRecords())
	original, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}

	corrupt := func(name string, mutate func([]byte) []byte) {
		t.Run(name, func(t *testing.T) {
			b := append([]byte(nil), original...)
			b = mutate(b)
			p := filepath.Join(t.TempDir(), "corrupt.cas")
			if err := os.WriteFile(p, b, 0o644); err != nil {
				t.Fatal(err)
			}
			r, err := Open(p)
			if err == nil {
				r.Close()
				t.Error("corrupt tape opened successfully")
			}
		})
	}

	corrupt("truncated mid-index", func(b []byte) []byte { return b[:HeaderSize+EntrySize] })
	corrupt("count claims more entries than exist", func(b []byte) []byte {
		b[16] = 0xff
		b[17] = 0xff
		return b
	})
	corrupt("blob offset past end of file", func(b []byte) []byte {
		b[48] = 0xff
		b[49] = 0xff
		b[50] = 0xff
		b[51] = 0x7f
		return b
	})
	corrupt("bad magic", func(b []byte) []byte {
		b[0] = 'X'
		return b
	})
}

func TestOpenRejectsTooSmall(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tiny.cas")
	if err := os.WriteFile(p, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p); err == nil {
		t.Error("want an error for a file smaller than a header")
	}
}

// A crash mid-assembly must not leave something a later run mistakes for a
// finished tape.
func TestCloseIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.cas")

	w, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(sampleRecords()[0]); err != nil {
		t.Fatal(err)
	}

	// Before Close the destination must not exist at all.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("tape appeared at its final path before Close")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("tape missing after Close: %v", err)
	}

	// No scratch or temp files left behind.
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.Name() != "run.cas" {
			t.Errorf("leftover file after Close: %s", e.Name())
		}
	}
}

func TestAppendAfterCloseFails(t *testing.T) {
	w, err := Create(filepath.Join(t.TempDir(), "x.cas"))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Append(Record{Method: "x", Request: []byte("{}")}); err == nil {
		t.Error("want an error appending after close")
	}
}

// Names must outlive the reader. An earlier version returned strings
// pointing into the mapping, and building a value object from a tape then
// closing the reader before rendering it segfaulted. Blobs are still
// borrowed, which is where the megabytes are; names are not worth a
// use-after-free.
func TestNamesSurviveClose(t *testing.T) {
	path := writeTape(t, sampleRecords())

	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	type held struct{ method, tool string }
	var kept []held
	for i := range r.Len() {
		kept = append(kept, held{r.Method(i), r.ToolName(i)})
	}

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// Reading these after the mapping is gone must be safe and correct.
	want := []held{
		{"initialize", ""},
		{"notifications/initialized", ""},
		{"tools/call", "read_file"},
		{"tools/call", "read_file"},
	}
	for i, w := range want {
		if kept[i].method != w.method || kept[i].tool != w.tool {
			t.Errorf("entry %d after close = %+v, want %+v", i, kept[i], w)
		}
	}
}
