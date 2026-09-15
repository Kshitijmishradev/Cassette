package api

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Kshitijmishradev/cassette/internal/diff"
	"github.com/Kshitijmishradev/cassette/internal/record"
	"github.com/Kshitijmishradev/cassette/internal/replay"
	"github.com/Kshitijmishradev/cassette/internal/safety"
	"github.com/Kshitijmishradev/cassette/internal/suite"
	"github.com/Kshitijmishradev/cassette/internal/tape"
)

// ErrDiffUnavailable means a run has no replay report to compare yet.
var ErrDiffUnavailable = errors.New("trajectory diff is unavailable until the run has been replayed")

// Builder turns on-disk suites into wire types.
//
// Used by both `serve` and the static export, so the two cannot drift. A
// second implementation for the static path is exactly how a demo starts
// showing something the live tool does not.
type Builder struct {
	SuiteDir   string
	Classifier *safety.Classifier

	// Live marks responses as coming from a running server rather than a
	// static snapshot.
	Live bool
}

// BuildSuite produces the suite index.
func (b *Builder) BuildSuite() (*Suite, error) {
	cases, err := suite.Discover(b.SuiteDir)
	if err != nil {
		return nil, err
	}

	out := &Suite{
		Generated: time.Now().UTC().Format(time.RFC3339),
		Live:      b.Live,
		Runs:      make([]RunSummary, 0, len(cases)),
	}

	for _, c := range cases {
		messages, toolCalls, errs, _ := c.Manifest.Totals()
		if out.Cassette == "" {
			out.Cassette = c.Manifest.Cassette
		}

		s := RunSummary{
			Name:       c.Name,
			RecordedAt: c.Manifest.CreatedAt.UTC().Format(time.RFC3339),
			Agent:      joinAgent(c.Manifest.Agent),
			Verdict:    "unknown",
			Tapes:      len(c.Manifest.Tapes),
			Messages:   messages,
			ToolCalls:  toolCalls,
			Errors:     errs,
			DurationMs: float64(c.Manifest.Duration()) / 1e6,
		}
		for _, t := range c.Manifest.Tapes {
			s.Bytes += t.Bytes
		}

		verdict, stats, err := b.verdictFor(c)
		if err != nil {
			return nil, err
		}
		s.Verdict = verdict
		s.Replay = stats

		switch verdict {
		case "identical":
			out.Totals.Identical++
		case "drift":
			out.Totals.Drift++
		case "changed":
			out.Totals.Changed++
		default:
			out.Totals.Unknown++
		}
		out.Totals.Messages += messages
		out.Totals.ToolCalls += toolCalls
		out.Totals.Errors += errs

		out.Runs = append(out.Runs, s)
	}

	out.Totals.Runs = len(out.Runs)
	return out, nil
}

// verdictFor computes a case's verdict from the same diff the test command
// uses, never from a rule of thumb over the counters. Two definitions of
// "changed" in one codebase is one too many; that mistake was already made
// once in the ClickHouse export and silently emptied a query.
func (b *Builder) verdictFor(c suite.Case) (string, *ReplayStats, error) {
	reports, err := replay.CollectReports(c.Dir)
	if err != nil || len(reports) == 0 {
		return "unknown", nil, nil
	}

	stats := &ReplayStats{}
	worst := diff.VerdictIdentical

	for _, rep := range reports {
		stats.Served += rep.Served
		stats.Exact += rep.ByTier["exact"]
		stats.Normalized += rep.ByTier["norm"]
		stats.ByMethod += rep.ByTier["method"]
		stats.Fuzzy += rep.ByTier["fuzzy"]
		stats.FellThrough += rep.FellThrough
		stats.Refused += rep.Refused
		stats.Repeats += rep.Repeats
		stats.Unused += rep.Unused

		rd, err := tape.Open(filepath.Join(c.Dir, rep.Tape+".cas"))
		if err != nil {
			return "unknown", stats, err
		}
		d := diff.Compare(
			diff.FromTape("recorded", rd, b.Classifier),
			diff.FromReport("this run", rep, b.Classifier),
		)
		rd.Close()
		if d.Verdict > worst {
			worst = d.Verdict
		}
	}
	return verdictName(worst), stats, nil
}

// BuildRun produces one run's full trajectory.
func (b *Builder) BuildRun(name string) (*Run, error) {
	dir, err := record.ChildPath(b.SuiteDir, name)
	if err != nil {
		return nil, err
	}

	run, err := record.OpenRun(dir)
	if err != nil {
		return nil, err
	}
	defer run.Close()

	out := &Run{
		Name:       name,
		RecordedAt: run.Manifest.CreatedAt.UTC().Format(time.RFC3339),
		Agent:      run.Manifest.Agent,
		Cassette:   run.Manifest.Cassette,
		Verdict:    "unknown",
		Steps:      make([]Step, 0, len(run.Steps)),
	}
	for _, t := range run.Manifest.Tapes {
		out.Tapes = append(out.Tapes, TapeInfo{
			File:     t.File,
			Server:   trimExt(t.File),
			Entries:  t.Entries,
			Bytes:    t.Bytes,
			Complete: t.Complete,
		})
	}

	// Tiers come from the replay report, matched to steps by position within
	// each tape. A recorded-only run simply has none.
	tiers := b.tiersByTape(dir)

	var base int64
	if len(run.Steps) > 0 {
		base = run.Steps[0].Entry.StartedNanos
	}

	for i, s := range run.Steps {
		step := Step{
			Index:           i,
			Tape:            trimExt(s.Tape),
			Seq:             int(s.Entry.Seq),
			Method:          s.Method,
			Tool:            s.ToolName,
			OffsetMs:        float64(s.Entry.StartedNanos-base) / 1e6,
			DurationMs:      float64(s.Entry.DurationNs) / 1e6,
			Class:           b.Classifier.Classify(s.Method, s.ToolName).String(),
			HasResponse:     s.Entry.HasResponse(),
			IsError:         s.Entry.IsError(),
			Truncated:       s.Entry.Truncated(),
			ServerInitiated: s.Entry.ServerInitiated(),
			ReqBytes:        int(s.Entry.ReqLen),
			RespBytes:       int(s.Entry.RespLen),
		}
		if byTape := tiers[trimExt(s.Tape)]; int(s.Entry.Seq) < len(byTape) {
			step.Tier = byTape[s.Entry.Seq]
		}
		out.Steps = append(out.Steps, step)
	}

	if v, _, err := b.verdictFor(suite.Case{Name: name, Dir: dir, Manifest: run.Manifest}); err == nil {
		out.Verdict = v
	}
	return out, nil
}

func (b *Builder) tiersByTape(dir string) map[string][]string {
	out := map[string][]string{}
	reports, err := replay.CollectReports(dir)
	if err != nil {
		return out
	}
	for _, rep := range reports {
		tiers := make([]string, 0, len(rep.Calls))
		for _, c := range rep.Calls {
			tiers = append(tiers, c.Tier)
		}
		out[rep.Tape] = tiers
	}
	return out
}

// BuildCall produces the full bodies for one step.
func (b *Builder) BuildCall(name string, index int) (*CallBodies, error) {
	dir, err := record.ChildPath(b.SuiteDir, name)
	if err != nil {
		return nil, err
	}

	run, err := record.OpenRun(dir)
	if err != nil {
		return nil, err
	}
	defer run.Close()

	if index < 0 || index >= len(run.Steps) {
		return nil, fmt.Errorf("call %d out of range (run has %d)", index, len(run.Steps))
	}
	s := run.Steps[index]

	return &CallBodies{
		Index:    index,
		Method:   s.Method,
		Tool:     s.ToolName,
		Request:  string(run.Request(s)),
		Response: string(run.Response(s)),
	}, nil
}

// BuildDiff compares a run's recording against its last replay.
func (b *Builder) BuildDiff(name string) (*Diff, error) {
	dir, err := record.ChildPath(b.SuiteDir, name)
	if err != nil {
		return nil, err
	}

	reports, err := replay.CollectReports(dir)
	if err != nil {
		return nil, err
	}
	if len(reports) == 0 {
		return nil, fmt.Errorf("%s: %w", name, ErrDiffUnavailable)
	}

	// One diff per run. With several servers the first tape is the one shown;
	// a multi-tape run gets a tape selector in a later iteration rather than
	// a silently merged alignment, which would imply an ordering between
	// servers that the data does not support.
	rep := reports[0]

	rd, err := tape.Open(filepath.Join(dir, rep.Tape+".cas"))
	if err != nil {
		return nil, err
	}
	defer rd.Close()

	d := diff.Compare(
		diff.FromTape("recorded", rd, b.Classifier),
		diff.FromReport("this run", rep, b.Classifier),
	)

	out := &Diff{
		Name:           name,
		Verdict:        verdictName(d.Verdict),
		BaselineName:   d.Baseline.Name,
		CandidateName:  d.Candidate.Name,
		BaselineSteps:  len(d.Baseline.Calls),
		CandidateSteps: len(d.Candidate.Calls),
		StepDelta:      d.StepDelta(),
		Matched:        d.Matched,
		Substituted:    d.Substituted,
		Inserted:       d.Inserted,
		Deleted:        d.Deleted,
		Missed:         d.Missed,
		Pairs:          make([]DiffPair, 0, len(d.Pairs)),
	}

	for _, p := range d.Pairs {
		pair := DiffPair{Op: opName(p.Op)}
		if p.Baseline != nil {
			pair.Baseline = diffCall(*p.Baseline)
			pair.Write = pair.Write || p.Baseline.IsWrite()
		}
		if p.Candidate != nil {
			pair.Candidate = diffCall(*p.Candidate)
			pair.Write = pair.Write || p.Candidate.IsWrite()
		}
		out.Pairs = append(out.Pairs, pair)
	}
	return out, nil
}

func diffCall(c diff.Call) *DiffCall {
	return &DiffCall{
		Method: c.Method,
		Tool:   c.Tool,
		Args:   c.Args,
		Class:  c.Class.String(),
		Tier:   c.Tier,
		Missed: c.Missed,
	}
}

func verdictName(v diff.Verdict) string {
	switch v {
	case diff.VerdictIdentical:
		return "identical"
	case diff.VerdictPathChanged:
		return "drift"
	default:
		return "changed"
	}
}

func opName(o diff.Op) string {
	switch o {
	case diff.OpMatch:
		return "match"
	case diff.OpSubstitute:
		return "substitute"
	case diff.OpInsert:
		return "insert"
	default:
		return "delete"
	}
}

func trimExt(f string) string {
	if ext := filepath.Ext(f); ext != "" {
		return f[:len(f)-len(ext)]
	}
	return f
}

func joinAgent(argv []string) string {
	s := ""
	for i, a := range argv {
		if i > 0 {
			s += " "
		}
		s += a
	}
	return s
}
