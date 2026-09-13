package chexport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kshitijmishradev/cassette/internal/record"
	"github.com/Kshitijmishradev/cassette/internal/safety"
	"github.com/Kshitijmishradev/cassette/internal/tape"
)

func buildSuite(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "case-01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	tapePath := filepath.Join(dir, "srv.cas")
	w, err := tape.Create(tapePath)
	if err != nil {
		t.Fatal(err)
	}
	recs := []tape.Record{
		{
			Method: "tools/call", ToolName: "read_file",
			Request:      []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/x"}}}`),
			Response:     []byte(`{"jsonrpc":"2.0","id":1,"result":{"content":"hello"}}`),
			StartedNanos: 1_700_000_000_000_000_000, DurationNs: 12_000_000,
			Flags: tape.EntryIsToolCall,
		},
		{
			Method: "tools/call", ToolName: "create_pull_request",
			Request:      []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_pull_request","arguments":{}}}`),
			Response:     []byte(`{"jsonrpc":"2.0","id":2,"error":{"code":-1,"message":"no"}}`),
			StartedNanos: 1_700_000_001_000_000_000, DurationNs: 3_000_000,
			Flags: tape.EntryIsToolCall | tape.EntryIsError,
		},
	}
	for _, r := range recs {
		if err := w.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	tapes, err := record.ScanDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := record.WriteManifest(dir, record.Manifest{
		Name: "case-01", RunID: "r1", Agent: []string{"agent"}, Tapes: tapes,
	}); err != nil {
		t.Fatal(err)
	}
	return root
}

func readJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("invalid JSONEachRow line: %v\n%s", err, line)
		}
		out = append(out, m)
	}
	return out
}

func TestExportProducesLoadableRows(t *testing.T) {
	out := t.TempDir()
	st, err := Export(Options{
		SuiteDir:        buildSuite(t),
		OutDir:          out,
		Classifier:      safety.New(safety.Config{}),
		IncludePayloads: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Cassettes != 1 || st.Spans != 2 {
		t.Fatalf("stats = %+v, want 1 cassette and 2 spans", st)
	}

	for _, f := range []string{"spans.jsonl", "payloads.jsonl", "runs.jsonl", "schema.sql", "queries.sql", "load.sh"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}

	spans := readJSONL(t, filepath.Join(out, "spans.jsonl"))
	if len(spans) != 2 {
		t.Fatalf("got %d spans", len(spans))
	}

	// The classifier has to reach the export, or every write-versus-read
	// query silently answers about nothing.
	byTool := map[string]map[string]any{}
	for _, s := range spans {
		byTool[s["tool"].(string)] = s
	}
	if got := byTool["read_file"]["is_write"]; got != float64(0) {
		t.Errorf("read_file is_write = %v, want 0", got)
	}
	if got := byTool["create_pull_request"]["is_write"]; got != float64(1) {
		t.Errorf("create_pull_request is_write = %v, want 1", got)
	}
	if got := byTool["create_pull_request"]["is_error"]; got != float64(1) {
		t.Errorf("error flag lost")
	}
}

// JSONEachRow matches columns by name, so a field name that drifts from the
// DDL is a silent import failure rather than a compile error.
func TestSpanFieldsAllExistInTheSchema(t *testing.T) {
	out := t.TempDir()
	if _, err := Export(Options{
		SuiteDir: buildSuite(t), OutDir: out,
		Classifier: safety.New(safety.Config{}), IncludePayloads: true,
	}); err != nil {
		t.Fatal(err)
	}

	for _, f := range []struct{ file, table string }{
		{"spans.jsonl", "spans"},
		{"payloads.jsonl", "payloads"},
		{"runs.jsonl", "runs"},
	} {
		rows := readJSONL(t, filepath.Join(out, f.file))
		if len(rows) == 0 {
			continue
		}
		ddl := tableDDL(t, f.table)
		for col := range rows[0] {
			if !strings.Contains(ddl, "\n    "+col+" ") {
				t.Errorf("%s emits column %q that the %s DDL does not declare", f.file, col, f.table)
			}
		}
	}
}

// tableDDL slices one CREATE TABLE block out of the schema.
func tableDDL(t *testing.T, table string) string {
	t.Helper()
	marker := "CREATE TABLE IF NOT EXISTS " + table + "\n"
	i := strings.Index(Schema, marker)
	if i < 0 {
		t.Fatalf("schema has no table %q", table)
	}
	rest := Schema[i:]
	end := strings.Index(rest, "ENGINE =")
	if end < 0 {
		t.Fatalf("table %q has no ENGINE clause", table)
	}
	return rest[:end]
}

// Payload rows reference spans by id, so re-exporting must not renumber them.
func TestSpanIDsAreStableAcrossExports(t *testing.T) {
	suiteDir := buildSuite(t)

	ids := func() []any {
		out := t.TempDir()
		if _, err := Export(Options{
			SuiteDir: suiteDir, OutDir: out,
			Classifier: safety.New(safety.Config{}), IncludePayloads: true,
		}); err != nil {
			t.Fatal(err)
		}
		var got []any
		for _, s := range readJSONL(t, filepath.Join(out, "spans.jsonl")) {
			got = append(got, s["span_id"])
		}
		return got
	}

	first, second := ids(), ids()
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("span %d renumbered between exports: %v then %v", i, first[i], second[i])
		}
	}
}

// A cassette that has never been replayed must not default to a passing
// verdict, or every query grouping by verdict quietly inflates the pass rate.
func TestNeverReplayedCassetteHasUnknownVerdict(t *testing.T) {
	out := t.TempDir()
	if _, err := Export(Options{
		SuiteDir: buildSuite(t), OutDir: out,
		Classifier: safety.New(safety.Config{}),
	}); err != nil {
		t.Fatal(err)
	}
	runs := readJSONL(t, filepath.Join(out, "runs.jsonl"))
	if got := runs[0]["verdict"]; got != "unknown" {
		t.Errorf("verdict = %v, want unknown", got)
	}
}

func TestNoPayloadsOption(t *testing.T) {
	out := t.TempDir()
	st, err := Export(Options{
		SuiteDir: buildSuite(t), OutDir: out,
		Classifier: safety.New(safety.Config{}), IncludePayloads: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Payloads != 0 {
		t.Errorf("wrote %d payloads with payloads disabled", st.Payloads)
	}
	if _, err := os.Stat(filepath.Join(out, "payloads.jsonl")); err == nil {
		t.Error("payloads.jsonl written despite being disabled")
	}
}
