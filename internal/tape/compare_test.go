package tape

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// This file answers the question the format has to justify: is CAS1 actually
// better than writing the obvious thing, a file of JSON lines?
//
// The obvious thing is what most tools would do, and it is a real contender.
// It is trivial to write, human-readable, and greppable. So the comparison
// is made rather than assumed, on a corpus shaped like a real agent run:
// a few hundred calls with responses averaging several kilobytes.

const (
	benchEntries  = 200
	benchRespSize = 8 << 10
)

// jsonlRecord is the naive alternative: one JSON object per line holding the
// request, the response, and the metadata.
type jsonlRecord struct {
	Seq          int    `json:"seq"`
	Method       string `json:"method"`
	ToolName     string `json:"tool,omitempty"`
	KeyHash      uint64 `json:"key"`
	StartedNanos int64  `json:"started"`
	DurationNs   int64  `json:"duration"`
	Request      string `json:"req"`
	Response     string `json:"resp"`
}

func benchCorpus() []Record {
	recs := make([]Record, benchEntries)
	for i := range recs {
		args := fmt.Sprintf(`{"path":"/src/pkg/file_%d.go","limit":200}`, i)
		req := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"read_file","arguments":%s}}`, i, args)
		resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"%s"}]}}`,
			i, strings.Repeat("x", benchRespSize))

		recs[i] = Record{
			Method:       "tools/call",
			ToolName:     "read_file",
			Request:      []byte(req),
			Response:     []byte(resp),
			StartedNanos: int64(i) * 1_000_000,
			DurationNs:   12_000_000,
			KeyHash:      HashKey("tools/call", []byte(args)),
			Flags:        EntryIsToolCall,
		}
	}
	return recs
}

func buildCAS(tb testing.TB, dir string) string {
	tb.Helper()
	path := filepath.Join(dir, "bench.cas")
	w, err := Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	for _, r := range benchCorpus() {
		if err := w.Append(r); err != nil {
			tb.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		tb.Fatal(err)
	}
	return path
}

func buildJSONL(tb testing.TB, dir string) string {
	tb.Helper()
	path := filepath.Join(dir, "bench.jsonl")
	f, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()

	out := bufio.NewWriterSize(f, 256<<10)
	enc := json.NewEncoder(out)
	for i, r := range benchCorpus() {
		rec := jsonlRecord{
			Seq: i, Method: r.Method, ToolName: r.ToolName,
			KeyHash: r.KeyHash, StartedNanos: r.StartedNanos, DurationNs: r.DurationNs,
			Request: string(r.Request), Response: string(r.Response),
		}
		if err := enc.Encode(rec); err != nil {
			tb.Fatal(err)
		}
	}
	if err := out.Flush(); err != nil {
		tb.Fatal(err)
	}
	return path
}

// loadJSONL is what a replay built on JSON lines would have to do before it
// could serve its first call: decode every record into Go values.
func loadJSONL(path string) (map[uint64]jsonlRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	byKey := make(map[uint64]jsonlRecord, benchEntries)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var rec jsonlRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return nil, err
		}
		byKey[rec.KeyHash] = rec
	}
	return byKey, sc.Err()
}

// BenchmarkLoadCAS measures opening a tape and building the lookup map, which
// is everything a replay must do before serving its first call.
func BenchmarkLoadCAS(b *testing.B) {
	path := buildCAS(b, b.TempDir())
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		r, err := Open(path)
		if err != nil {
			b.Fatal(err)
		}
		byKey := make(map[uint64]int32, r.Len())
		for i, e := range r.Entries() {
			byKey[e.KeyHash] = int32(i)
		}
		if len(byKey) == 0 {
			b.Fatal("empty index")
		}
		r.Close()
	}
}

func BenchmarkLoadJSONL(b *testing.B) {
	path := buildJSONL(b, b.TempDir())
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		byKey, err := loadJSONL(path)
		if err != nil {
			b.Fatal(err)
		}
		if len(byKey) == 0 {
			b.Fatal("empty index")
		}
	}
}

// BenchmarkLookupCAS is the replay hot path: hash hit, then hand back the
// pre-framed bytes ready to write.
func BenchmarkLookupCAS(b *testing.B) {
	path := buildCAS(b, b.TempDir())
	r, err := Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()

	byKey := make(map[uint64]int32, r.Len())
	for i, e := range r.Entries() {
		byKey[e.KeyHash] = int32(i)
	}
	keys := make([]uint64, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		idx, ok := byKey[keys[i%len(keys)]]
		if !ok {
			b.Fatal("miss")
		}
		if blob := r.Response(int(idx)); len(blob) == 0 {
			b.Fatal("empty blob")
		}
	}
}

// BenchmarkLookupJSONL is the same operation against decoded records. The
// lookup is equally fast; the cost is that serving requires a string to be
// converted back to bytes, because the payload was decoded on the way in.
func BenchmarkLookupJSONL(b *testing.B) {
	path := buildJSONL(b, b.TempDir())
	byKey, err := loadJSONL(path)
	if err != nil {
		b.Fatal(err)
	}
	keys := make([]uint64, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		rec, ok := byKey[keys[i%len(keys)]]
		if !ok {
			b.Fatal("miss")
		}
		if blob := []byte(rec.Response); len(blob) == 0 {
			b.Fatal("empty blob")
		}
	}
}

// TestFormatComparison is not a pass/fail test. It prints the numbers that
// justify the format choice, so they can be regenerated rather than trusted
// from a commit message.
//
//	go test ./internal/tape/ -run TestFormatComparison -v
func TestFormatComparison(t *testing.T) {
	dir := t.TempDir()
	casPath := buildCAS(t, dir)
	jsonlPath := buildJSONL(t, dir)

	casInfo, err := os.Stat(casPath)
	if err != nil {
		t.Fatal(err)
	}
	jsonlInfo, err := os.Stat(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}

	casMem := measure(t, func() any {
		r, err := Open(casPath)
		if err != nil {
			t.Fatal(err)
		}
		byKey := make(map[uint64]int32, r.Len())
		for i, e := range r.Entries() {
			byKey[e.KeyHash] = int32(i)
		}
		return []any{r, byKey}
	})

	jsonlMem := measure(t, func() any {
		m, err := loadJSONL(jsonlPath)
		if err != nil {
			t.Fatal(err)
		}
		return m
	})

	t.Logf("corpus: %d calls, ~%d KB responses", benchEntries, benchRespSize>>10)
	t.Logf("")
	t.Logf("%-8s %12s %14s", "format", "file bytes", "heap bytes")
	t.Logf("%-8s %12d %14d", "CAS1", casInfo.Size(), casMem)
	t.Logf("%-8s %12d %14d", "JSONL", jsonlInfo.Size(), jsonlMem)
	t.Logf("")

	// CAS1's payloads live in the page cache, not the heap, so heap growth
	// can legitimately measure as zero. That is the finding, not a broken
	// measurement: the bytes exist, they are just off-heap and shared.
	if casMem == 0 {
		t.Logf("CAS1 heap growth is below measurement noise: payloads are in the")
		t.Logf("page cache, not the heap, so nothing was allocated to hold them.")
	} else {
		t.Logf("JSONL uses %.1fx the heap of CAS1", float64(jsonlMem)/float64(casMem))
	}

	// The number that actually matters is at suite scale. Replays are
	// independent processes, so a shared read-only mapping is paid for once
	// by the kernel while decoded Go values are paid for by every worker.
	const workers = 50
	t.Logf("")
	t.Logf("projected at %d parallel workers over this corpus:", workers)
	t.Logf("  CAS1   ~%d KB heap total, one shared %d KB mapping",
		(casMem*workers)>>10, casInfo.Size()>>10)
	t.Logf("  JSONL  ~%d MB heap total, nothing shared",
		(jsonlMem*workers)>>20)
}

// measure reports heap growth caused by fn, keeping its result alive so the
// collector cannot reclaim what is being measured.
func measure(t *testing.T, fn func() any) uint64 {
	t.Helper()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	kept := fn()

	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(kept)

	if after.HeapAlloc < before.HeapAlloc {
		return 0
	}
	return after.HeapAlloc - before.HeapAlloc
}
