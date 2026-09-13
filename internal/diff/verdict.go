package diff

import "fmt"

// Verdict is the three-valued answer.
type Verdict uint8

const (
	// VerdictIdentical: the same calls in the same order.
	VerdictIdentical Verdict = iota

	// VerdictPathChanged: the same write calls, reached differently.
	//
	// This is the verdict the whole three-valued scheme exists for. A
	// two-valued answer would fold it into "different" and bury the most
	// useful signal there is: the agent still did the right thing, but it
	// took nine more steps to get there. That is a regression nobody
	// currently catches, because the result looks fine.
	VerdictPathChanged

	// VerdictOutcomeChanged: the write calls differ. The agent did
	// something else to the world.
	VerdictOutcomeChanged
)

func (v Verdict) String() string {
	switch v {
	case VerdictIdentical:
		return "identical"
	case VerdictPathChanged:
		return "same outcome, different path"
	default:
		return "outcome changed"
	}
}

// Short is the compact form used in suite tables.
func (v Verdict) Short() string {
	switch v {
	case VerdictIdentical:
		return "same"
	case VerdictPathChanged:
		return "drift"
	default:
		return "CHANGED"
	}
}

// Diff is the full comparison of two trajectories.
type Diff struct {
	Baseline  Trajectory
	Candidate Trajectory
	Pairs     []Pair
	Verdict   Verdict

	Matched     int
	Substituted int
	Inserted    int
	Deleted     int

	// WritePairs is the alignment restricted to write-class calls, which is
	// what the verdict is actually decided on.
	WritePairs []Pair

	Missed int
}

// Compare aligns two trajectories and reaches a verdict.
func Compare(baseline, candidate Trajectory) Diff {
	d := Diff{
		Baseline:  baseline,
		Candidate: candidate,
		Pairs:     Align(baseline.Calls, candidate.Calls),
	}

	for _, p := range d.Pairs {
		switch p.Op {
		case OpMatch:
			d.Matched++
		case OpSubstitute:
			d.Substituted++
		case OpInsert:
			d.Inserted++
		case OpDelete:
			d.Deleted++
		}
		if p.Candidate != nil && p.Candidate.Missed {
			d.Missed++
		}
	}

	// The verdict is decided on write calls alone, aligned separately.
	// Aligning them separately rather than filtering the full alignment
	// matters: in the full alignment a write call can end up paired against
	// a read that happened to sit at the same position, which says nothing
	// about whether the two runs did the same thing.
	d.WritePairs = Align(baseline.Writes(), candidate.Writes())

	d.Verdict = VerdictIdentical
	for _, p := range d.WritePairs {
		if p.Op != OpMatch {
			d.Verdict = VerdictOutcomeChanged
			break
		}
	}
	if d.Verdict == VerdictIdentical && (d.Substituted > 0 || d.Inserted > 0 || d.Deleted > 0) {
		d.Verdict = VerdictPathChanged
	}

	return d
}

// StepDelta is how many more or fewer calls the candidate made.
func (d Diff) StepDelta() int {
	return len(d.Candidate.Calls) - len(d.Baseline.Calls)
}

// Summary is a one-line description.
func (d Diff) Summary() string {
	s := fmt.Sprintf("%d steps vs %d", len(d.Candidate.Calls), len(d.Baseline.Calls))
	if delta := d.StepDelta(); delta != 0 {
		s += fmt.Sprintf(" (%+d)", delta)
	}
	return s + " · " + d.Verdict.String()
}

// Changed reports whether anything at all differed.
func (d Diff) Changed() bool { return d.Verdict != VerdictIdentical }
