package chexport

import (
	"bufio"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kshitijmishradev/cassette/internal/diff"
	"github.com/Kshitijmishradev/cassette/internal/replay"
	"github.com/Kshitijmishradev/cassette/internal/safety"
	"github.com/Kshitijmishradev/cassette/internal/suite"
	"github.com/Kshitijmishradev/cassette/internal/tape"
)

// Stats describes what an export produced.
type Stats struct {
	Cassettes int
	Spans     int
	Payloads  int
	Bytes     int64
}

// Options configures an export.
type Options struct {
	SuiteDir string
	OutDir   string

	Classifier *safety.Classifier

	// IncludePayloads writes the request and response bodies. On by default;
	// turning it off produces an export that answers every aggregate question
	// at a fraction of the size, which matters when a suite's payloads are
	// the overwhelming majority of its bytes.
	IncludePayloads bool
}

// span is one row of the spans table, in JSONEachRow form.
//
// Field names match the DDL exactly. JSONEachRow matches on name, so a
// mismatch here is a silent import failure rather than a compile error, which
// is why the struct tags are spelled out rather than derived.
type span struct {
	SpanID uint64 `json:"span_id"`

	Cassette string `json:"cassette"`
	Tape     string `json:"tape"`
	RunID    string `json:"run_id"`

	Seq        uint32  `json:"seq"`
	StartedAt  string  `json:"started_at"`
	DurationMs float64 `json:"duration_ms"`

	Method string `json:"method"`
	Tool   string `json:"tool"`

	KeyHash  uint64 `json:"key_hash"`
	NormHash uint64 `json:"norm_hash"`

	IsToolCall      uint8 `json:"is_tool_call"`
	IsWrite         uint8 `json:"is_write"`
	IsError         uint8 `json:"is_error"`
	Truncated       uint8 `json:"truncated"`
	ServerInitiated uint8 `json:"server_initiated"`

	ReqBytes  uint32 `json:"req_bytes"`
	RespBytes uint32 `json:"resp_bytes"`
}

type payload struct {
	SpanID uint64 `json:"span_id"`
	Kind   string `json:"kind"`
	Body   string `json:"body"`
}

type run struct {
	Cassette   string `json:"cassette"`
	RunID      string `json:"run_id"`
	RecordedAt string `json:"recorded_at"`
	Agent      string `json:"agent"`
	Build      string `json:"build"`

	Verdict     string `json:"verdict"`
	Served      uint32 `json:"served"`
	Exact       uint32 `json:"exact"`
	Normalized  uint32 `json:"normalized"`
	ByMethod    uint32 `json:"by_method"`
	FellThrough uint32 `json:"fell_through"`
	Refused     uint32 `json:"refused"`
	Unused      uint32 `json:"unused"`

	Messages  uint32 `json:"messages"`
	ToolCalls uint32 `json:"tool_calls"`
	Errors    uint32 `json:"errors"`
}

// Export writes a suite into ClickHouse-loadable files.
func Export(opts Options) (Stats, error) {
	var st Stats
	if entries, err := os.ReadDir(opts.OutDir); err == nil && len(entries) != 0 {
		return st, fmt.Errorf("export destination is not empty; choose a fresh directory to avoid retaining old payloads")
	} else if err != nil && !os.IsNotExist(err) {
		return st, err
	}

	cases, err := suite.Discover(opts.SuiteDir)
	if err != nil {
		return st, err
	}
	if len(cases) == 0 {
		return st, fmt.Errorf("no recorded runs found in %s", opts.SuiteDir)
	}
	if opts.Classifier == nil {
		opts.Classifier = safety.New(safety.Config{})
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return st, err
	}

	spansW, err := newLineWriter(filepath.Join(opts.OutDir, "spans.jsonl"))
	if err != nil {
		return st, err
	}
	defer spansW.Close()

	runsW, err := newLineWriter(filepath.Join(opts.OutDir, "runs.jsonl"))
	if err != nil {
		return st, err
	}
	defer runsW.Close()

	var payloadsW *lineWriter
	if opts.IncludePayloads {
		payloadsW, err = newLineWriter(filepath.Join(opts.OutDir, "payloads.jsonl"))
		if err != nil {
			return st, err
		}
		defer payloadsW.Close()
	}

	for _, c := range cases {
		reports, _ := replay.CollectReports(c.Dir) // absent is fine; a run may never have been replayed

		if err := writeRun(runsW, c, reports, opts); err != nil {
			return st, err
		}

		for _, t := range c.Manifest.Tapes {
			n, p, err := writeTape(spansW, payloadsW, c, t.File, opts)
			if err != nil {
				return st, err
			}
			st.Spans += n
			st.Payloads += p
		}
		st.Cassettes++
	}

	for _, f := range []struct{ name, body string }{
		{"schema.sql", Schema},
		{"queries.sql", Queries},
		{"load.sh", loadScript},
	} {
		path := filepath.Join(opts.OutDir, f.name)
		mode := os.FileMode(0o644)
		if strings.HasSuffix(f.name, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(path, []byte(f.body), mode); err != nil {
			return st, err
		}
	}

	st.Bytes = dirSize(opts.OutDir)
	return st, nil
}

func writeTape(spans, payloads *lineWriter, c suite.Case, tapeFile string, opts Options) (int, int, error) {
	r, err := tape.Open(filepath.Join(c.Dir, tapeFile))
	if err != nil {
		return 0, 0, err
	}
	defer r.Close()

	nSpans, nPayloads := 0, 0
	for i := range r.Len() {
		e := r.Entry(i)
		method := r.Method(i)
		tool := r.ToolName(i)

		// span_id has to be stable across exports so a re-export does not
		// orphan the payload rows that reference it.
		id := spanID(c.Name, tapeFile, uint32(i))

		s := span{
			SpanID:          id,
			Cassette:        c.Name,
			Tape:            strings.TrimSuffix(tapeFile, ".cas"),
			RunID:           c.Manifest.RunID,
			Seq:             e.Seq,
			StartedAt:       formatTime(e.StartedNanos),
			DurationMs:      float64(e.DurationNs) / 1e6,
			Method:          method,
			Tool:            tool,
			KeyHash:         e.KeyHash,
			NormHash:        e.NormHash,
			IsToolCall:      b2u(e.IsToolCall()),
			IsWrite:         b2u(opts.Classifier.Classify(method, tool) == safety.ClassWrite),
			IsError:         b2u(e.IsError()),
			Truncated:       b2u(e.Truncated()),
			ServerInitiated: b2u(e.ServerInitiated()),
			ReqBytes:        e.ReqLen,
			RespBytes:       e.RespLen,
		}
		if err := spans.encode(s); err != nil {
			return nSpans, nPayloads, err
		}
		nSpans++

		if payloads == nil {
			continue
		}
		for kind, body := range map[string][]byte{
			"request":  r.Request(i),
			"response": r.Response(i),
		} {
			if len(body) == 0 {
				continue
			}
			if err := payloads.encode(payload{SpanID: id, Kind: kind, Body: string(body)}); err != nil {
				return nSpans, nPayloads, err
			}
			nPayloads++
		}
	}
	return nSpans, nPayloads, nil
}

func writeRun(w *lineWriter, c suite.Case, reports []replay.Report, opts Options) error {
	messages, toolCalls, errs, _ := c.Manifest.Totals()

	r := run{
		Cassette:   c.Name,
		RunID:      c.Manifest.RunID,
		RecordedAt: c.Manifest.CreatedAt.UTC().Format("2006-01-02 15:04:05.000000"),
		Agent:      strings.Join(c.Manifest.Agent, " "),
		Build:      c.Manifest.Cassette,
		Verdict:    "unknown",
		Messages:   uint32(messages),
		ToolCalls:  uint32(toolCalls),
		Errors:     uint32(errs),
	}

	for _, rep := range reports {
		r.Served += uint32(rep.Served)
		r.Exact += uint32(rep.ByTier["exact"])
		r.Normalized += uint32(rep.ByTier["norm"])
		r.ByMethod += uint32(rep.ByTier["method"])
		r.FellThrough += uint32(rep.FellThrough)
		r.Refused += uint32(rep.Refused)
		r.Unused += uint32(rep.Unused)
	}

	// A cassette that has never been replayed has no verdict, and saying so
	// is better than defaulting it to "identical" and quietly inflating the
	// pass rate in every query that groups by verdict.
	if len(reports) == 0 {
		return w.encode(r)
	}

	// The verdict comes from the same diff the test command uses, not from a
	// rule of thumb over the counters.
	//
	// An earlier version guessed: refused meant changed, unused meant drift.
	// It disagreed with the tool's own output on the very first real suite,
	// labelling as drift three cassettes that `cassette test` called CHANGED,
	// which silently emptied the query that asks what failing runs do
	// differently. Two definitions of "changed" in one codebase is one too
	// many.
	worst := diff.VerdictIdentical
	for _, rep := range reports {
		rd, err := tape.Open(filepath.Join(c.Dir, rep.Tape+".cas"))
		if err != nil {
			return err
		}
		d := diff.Compare(
			diff.FromTape("recorded", rd, opts.Classifier),
			diff.FromReport("replay", rep, opts.Classifier),
		)
		rd.Close()
		if d.Verdict > worst {
			worst = d.Verdict
		}
	}

	switch worst {
	case diff.VerdictIdentical:
		r.Verdict = "identical"
	case diff.VerdictPathChanged:
		r.Verdict = "drift"
	default:
		r.Verdict = "changed"
	}
	return w.encode(r)
}

// spanID derives a stable id from where a span lives rather than from a
// counter, so re-exporting a suite produces the same ids and payload rows
// keep pointing at the right spans.
func spanID(cassette, tapeFile string, index uint32) uint64 {
	h := fnv.New64a()
	h.Write([]byte(cassette))
	h.Write([]byte{0})
	h.Write([]byte(tapeFile))
	h.Write([]byte{0})
	var b [4]byte
	b[0], b[1], b[2], b[3] = byte(index), byte(index>>8), byte(index>>16), byte(index>>24)
	h.Write(b[:])
	return h.Sum64()
}

// formatTime renders a DateTime64(6) literal. ClickHouse parses this form
// directly from JSONEachRow.
func formatTime(nanos int64) string {
	if nanos == 0 {
		return "1970-01-01 00:00:00.000000"
	}
	return time.Unix(0, nanos).UTC().Format("2006-01-02 15:04:05.000000")
}

func b2u(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

type lineWriter struct {
	f   *os.File
	buf *bufio.Writer
	enc *json.Encoder
}

func newLineWriter(path string) (*lineWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	buf := bufio.NewWriterSize(f, 256<<10)
	enc := json.NewEncoder(buf)
	return &lineWriter{f: f, buf: buf, enc: enc}, nil
}

func (w *lineWriter) encode(v any) error { return w.enc.Encode(v) }

func (w *lineWriter) Close() error {
	if err := w.buf.Flush(); err != nil {
		w.f.Close()
		return err
	}
	return w.f.Close()
}

func dirSize(dir string) int64 {
	var total int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}

const loadScript = `#!/usr/bin/env bash
# Load this export into a local ClickHouse and run the shipped queries.
#
#   ./load.sh            # uses clickhouse-local, no server needed
#   ./load.sh --client   # uses clickhouse-client against a running server
set -euo pipefail
cd "$(dirname "$0")"

if [ "${1:-}" = "--client" ]; then
  RUN=(clickhouse-client)
  clickhouse-client --queries-file schema.sql
  for t in spans payloads runs; do
    [ -f "$t.jsonl" ] || continue
    clickhouse-client --query "INSERT INTO $t FORMAT JSONEachRow" < "$t.jsonl"
  done
else
  DB=${CH_PATH:-./db}
  mkdir -p "$DB"
  RUN=(clickhouse local --path "$DB")
  "${RUN[@]}" --queries-file schema.sql
  for t in spans payloads runs; do
    [ -f "$t.jsonl" ] || continue
    "${RUN[@]}" --query "INSERT INTO $t FORMAT JSONEachRow" < "$t.jsonl"
  done
fi

echo
echo "loaded. try:"
echo "  clickhouse local --path ./db --queries-file queries.sql"
`
