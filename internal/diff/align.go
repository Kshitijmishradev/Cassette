package diff

// Op is what happened to one position in the alignment.
type Op uint8

const (
	// OpMatch: the same call appears in both runs.
	OpMatch Op = iota

	// OpSubstitute: a call was replaced by a different one at the same
	// position.
	OpSubstitute

	// OpInsert: the candidate made a call the baseline did not.
	OpInsert

	// OpDelete: the baseline made a call the candidate did not.
	OpDelete
)

func (o Op) String() string {
	switch o {
	case OpMatch:
		return "match"
	case OpSubstitute:
		return "substitute"
	case OpInsert:
		return "insert"
	default:
		return "delete"
	}
}

// Pair is one aligned position. Baseline or Candidate is nil for an
// insertion or deletion respectively.
type Pair struct {
	Op        Op
	Baseline  *Call
	Candidate *Call
}

// Alignment costs.
//
// These are ordinal rather than measured, and the ordering is what carries
// the meaning. Same tool with different arguments is the cheapest kind of
// difference, because an agent grepping for a slightly different string is
// doing the same thing. Swapping one tool for another is a real behavioral
// change and costs more. An insert or delete sits between them: adding a step
// is more surprising than varying an argument, less surprising than replacing
// one action with a different action.
//
// Getting the ordering right matters more than the absolute values. If
// substitution across tools were cheaper than an indel, the alignment would
// happily pair unrelated calls rather than admit one run did something the
// other did not, and the diff would read as a garbled sequence of swaps
// instead of a clear insertion.
const (
	costMatch     = 0
	costSameTool  = 1
	costIndel     = 2
	costDifferent = 3
)

// substitutionCost prices replacing a with b.
func substitutionCost(a, b Call) int {
	switch {
	case a.same(b):
		return costMatch
	case a.sameTool(b):
		return costSameTool
	default:
		return costDifferent
	}
}

// Align computes a minimum-cost alignment of two trajectories.
//
// This is Needleman-Wunsch, the same dynamic program used for sequence
// alignment in bioinformatics, and it is the right tool for the same reason:
// the sequences are of different lengths, order matters, and the question is
// which elements correspond rather than merely whether the sequences differ.
// A set comparison would say "run B called grep three more times" without
// being able to say where, and where is the whole point.
//
// Cost is O(n*m) in time and space. For agent runs that is trivially fine:
// a thousand calls against a thousand calls is a million int cells, a few
// megabytes, computed in milliseconds. If trajectories ever reach a scale
// where that matters, Hirschberg's algorithm gives the same alignment in
// linear space, but spending that complexity now would be optimizing a
// dimension nothing is near.
func Align(baseline, candidate []Call) []Pair {
	n, m := len(baseline), len(candidate)

	// cost[i][j] is the cheapest way to align the first i baseline calls
	// with the first j candidate calls.
	cost := make([][]int, n+1)
	for i := range cost {
		cost[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		cost[i][0] = i * costIndel
	}
	for j := 1; j <= m; j++ {
		cost[0][j] = j * costIndel
	}

	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			sub := cost[i-1][j-1] + substitutionCost(baseline[i-1], candidate[j-1])
			del := cost[i-1][j] + costIndel
			ins := cost[i][j-1] + costIndel

			best := sub
			if del < best {
				best = del
			}
			if ins < best {
				best = ins
			}
			cost[i][j] = best
		}
	}

	return traceback(baseline, candidate, cost)
}

// traceback walks the cost table backwards to recover the alignment.
//
// Ties are broken toward substitution first, then deletion, then insertion.
// The choice is arbitrary but it must be deterministic: this output feeds a
// diff someone will read and a CI job that will compare runs, and an
// alignment that varied between invocations on identical input would make
// both untrustworthy.
func traceback(baseline, candidate []Call, cost [][]int) []Pair {
	i, j := len(baseline), len(candidate)
	var rev []Pair

	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && cost[i][j] == cost[i-1][j-1]+substitutionCost(baseline[i-1], candidate[j-1]):
			op := OpSubstitute
			if baseline[i-1].same(candidate[j-1]) {
				op = OpMatch
			}
			b, c := baseline[i-1], candidate[j-1]
			rev = append(rev, Pair{Op: op, Baseline: &b, Candidate: &c})
			i--
			j--

		case i > 0 && cost[i][j] == cost[i-1][j]+costIndel:
			b := baseline[i-1]
			rev = append(rev, Pair{Op: OpDelete, Baseline: &b})
			i--

		default:
			c := candidate[j-1]
			rev = append(rev, Pair{Op: OpInsert, Candidate: &c})
			j--
		}
	}

	// Built backwards; reverse in place.
	for l, r := 0, len(rev)-1; l < r; l, r = l+1, r-1 {
		rev[l], rev[r] = rev[r], rev[l]
	}
	return rev
}
