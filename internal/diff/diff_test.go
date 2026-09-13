package diff

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Kshitijmishradev/cassette/internal/safety"
)

func read(tool string, hash uint64) Call {
	return Call{Method: "tools/call", Tool: tool, ArgsHash: hash, Class: safety.ClassRead}
}

func write(tool string, hash uint64) Call {
	return Call{Method: "tools/call", Tool: tool, ArgsHash: hash, Class: safety.ClassWrite}
}

func traj(name string, calls ...Call) Trajectory {
	return Trajectory{Name: name, Calls: calls}
}

func TestIdenticalRuns(t *testing.T) {
	calls := []Call{read("grep", 1), read("read_file", 2), write("edit_file", 3)}
	d := Compare(traj("a", calls...), traj("b", calls...))

	if d.Verdict != VerdictIdentical {
		t.Errorf("verdict = %v, want identical", d.Verdict)
	}
	if d.Matched != 3 || d.Substituted+d.Inserted+d.Deleted != 0 {
		t.Errorf("counts = %+v", d)
	}
	if d.Changed() {
		t.Error("identical runs reported as changed")
	}
}

// The verdict the three-valued scheme exists for. The agent still made the
// same edits, it just took twelve more reads to get there. A two-valued
// answer would bury this, and it is the most useful signal in the tool.
func TestExtraReadsAreDriftNotOutcomeChange(t *testing.T) {
	baseline := traj("recorded",
		read("grep", 1), read("read_file", 2), write("edit_file", 9))

	candidate := traj("replay",
		read("grep", 1), read("grep", 5), read("grep", 6), read("grep", 7),
		read("read_file", 2), write("edit_file", 9))

	d := Compare(baseline, candidate)

	if d.Verdict != VerdictPathChanged {
		t.Fatalf("verdict = %v, want path changed", d.Verdict)
	}
	if d.StepDelta() != 3 {
		t.Errorf("StepDelta = %d, want 3", d.StepDelta())
	}
	if d.Inserted != 3 {
		t.Errorf("inserted = %d, want 3", d.Inserted)
	}
}

// A different write call means the agent did something else to the world,
// however similar the path looked.
func TestDifferentWriteIsOutcomeChange(t *testing.T) {
	baseline := traj("recorded", read("grep", 1), write("edit_file", 9))
	candidate := traj("replay", read("grep", 1), write("edit_file", 10))

	d := Compare(baseline, candidate)
	if d.Verdict != VerdictOutcomeChanged {
		t.Errorf("verdict = %v, want outcome changed", d.Verdict)
	}
}

func TestDroppedWriteIsOutcomeChange(t *testing.T) {
	baseline := traj("recorded", read("grep", 1), write("create_pr", 9))
	candidate := traj("replay", read("grep", 1))

	d := Compare(baseline, candidate)
	if d.Verdict != VerdictOutcomeChanged {
		t.Errorf("verdict = %v, want outcome changed", d.Verdict)
	}
}

func TestAddedWriteIsOutcomeChange(t *testing.T) {
	baseline := traj("recorded", read("grep", 1))
	candidate := traj("replay", read("grep", 1), write("send_email", 4))

	d := Compare(baseline, candidate)
	if d.Verdict != VerdictOutcomeChanged {
		t.Errorf("verdict = %v, want outcome changed", d.Verdict)
	}
}

// Write calls are aligned separately, not filtered out of the full
// alignment. In the full alignment a write can end up paired against a read
// that happened to sit at the same index, which says nothing about whether
// the two runs did the same thing.
func TestWritesAreAlignedSeparatelyFromReads(t *testing.T) {
	// Same single write in both, but surrounded by wildly different reads
	// so the full alignment cannot pair them positionally.
	baseline := traj("recorded",
		write("edit_file", 100),
		read("a", 1), read("b", 2), read("c", 3), read("d", 4))

	candidate := traj("replay",
		read("x", 11), read("y", 12), read("z", 13), read("w", 14),
		write("edit_file", 100))

	d := Compare(baseline, candidate)
	if d.Verdict != VerdictPathChanged {
		t.Errorf("verdict = %v, want path changed; the same write happened in both", d.Verdict)
	}
}

// Same tool with different arguments is the cheapest difference, because an
// agent grepping a slightly different string is doing the same thing.
// Swapping tools entirely is a real behavioral change and must cost more.
func TestSubstitutionCostOrdering(t *testing.T) {
	a := read("grep", 1)
	sameToolDiffArgs := read("grep", 2)
	differentTool := read("read_file", 2)

	if substitutionCost(a, a) != costMatch {
		t.Error("identical calls do not cost zero")
	}
	if substitutionCost(a, sameToolDiffArgs) >= substitutionCost(a, differentTool) {
		t.Error("swapping tools is not more expensive than varying arguments")
	}
	if substitutionCost(a, differentTool) <= costIndel {
		t.Error("substituting across tools is not more expensive than an indel; " +
			"the alignment would pair unrelated calls rather than admit an insertion")
	}
}

// An insertion in the middle must be reported as an insertion, not as a
// cascade of substitutions. This is what makes the diff readable.
func TestInsertionIsLocalized(t *testing.T) {
	baseline := traj("recorded", read("a", 1), read("b", 2), read("c", 3))
	candidate := traj("replay", read("a", 1), read("NEW", 99), read("b", 2), read("c", 3))

	d := Compare(baseline, candidate)
	if d.Inserted != 1 || d.Matched != 3 {
		t.Errorf("got %d inserted %d matched %d substituted, want 1 inserted and 3 matched",
			d.Inserted, d.Matched, d.Substituted)
	}
}

// Both empty, one empty: neither should panic or mis-verdict.
func TestEmptyTrajectories(t *testing.T) {
	if d := Compare(traj("a"), traj("b")); d.Verdict != VerdictIdentical {
		t.Errorf("two empty runs = %v, want identical", d.Verdict)
	}

	d := Compare(traj("a", read("grep", 1)), traj("b"))
	if d.Verdict != VerdictPathChanged {
		t.Errorf("dropped read = %v, want path changed", d.Verdict)
	}
	if d.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", d.Deleted)
	}
}

// The alignment feeds a diff a human reads and a CI job compares. Varying
// between runs on identical input would make both untrustworthy.
func TestAlignmentIsDeterministic(t *testing.T) {
	baseline := traj("recorded", read("a", 1), read("b", 2), read("c", 3), write("w", 9))
	candidate := traj("replay", read("a", 1), read("x", 7), read("c", 3), write("w", 9))

	first := Compare(baseline, candidate)
	for range 20 {
		got := Compare(baseline, candidate)
		if got.Verdict != first.Verdict || len(got.Pairs) != len(first.Pairs) {
			t.Fatal("alignment is not deterministic")
		}
		for i := range got.Pairs {
			if got.Pairs[i].Op != first.Pairs[i].Op {
				t.Fatalf("pair %d differs between runs", i)
			}
		}
	}
}

func TestRenderMarksWriteDifferencesDistinctly(t *testing.T) {
	baseline := traj("recorded", read("grep", 1), write("edit_file", 9))
	candidate := traj("replay", read("grep", 1), write("edit_file", 10))

	var buf bytes.Buffer
	Render(&buf, Compare(baseline, candidate), RenderOptions{Width: 30})
	out := buf.String()

	if !strings.Contains(out, "!") {
		t.Errorf("a differing write call was not marked with !:\n%s", out)
	}
	if !strings.Contains(out, "outcome changed") {
		t.Errorf("verdict missing from output:\n%s", out)
	}
}

// A hundred identical reads between two interesting differences is noise,
// and a diff nobody reads to the end is not doing its job.
func TestRenderCollapsesLongIdenticalRuns(t *testing.T) {
	var base, cand []Call
	for i := range 50 {
		c := read("grep", uint64(i))
		base = append(base, c)
		cand = append(cand, c)
	}
	cand = append(cand, read("extra", 999))

	var buf bytes.Buffer
	Render(&buf, Compare(traj("a", base...), traj("b", cand...)), RenderOptions{Width: 30})
	out := buf.String()

	if !strings.Contains(out, "identical calls") {
		t.Errorf("identical runs were not collapsed:\n%s", out)
	}
	if n := strings.Count(out, "grep"); n > 5 {
		t.Errorf("collapsed output still printed %d grep rows", n)
	}
}
