// Package diff compares two agent trajectories and says what changed.
//
// A trajectory is the ordered sequence of calls an agent made. Comparing two
// of them is the payoff of everything before it: recording captured a run,
// replay pinned the world so a second run could be made comparable, and this
// is where the comparison finally produces an answer someone can act on.
//
// The answer is deliberately three-valued, not two. "Identical" and
// "different" would throw away the interesting case: an agent that reached
// the same place by a longer route. That is where you learn a prompt change
// cost nine extra steps without changing the result, which is exactly the
// kind of regression nobody currently catches.
package diff

import (
	"strings"

	"github.com/Kshitijmishradev/cassette/internal/safety"
)

// Call is one message in a trajectory, reduced to what comparison needs.
type Call struct {
	Method string
	Tool   string

	// ArgsHash is the normalized argument hash. Comparison uses this rather
	// than the argument text, so two calls that differ only in key order or
	// whitespace count as the same call.
	ArgsHash uint64

	// Args is a truncated, human-readable form used only for display. It
	// never participates in comparison, so truncating it cannot change a
	// verdict.
	Args string

	Class safety.Class

	// Tier records how this call was matched during replay. Empty on the
	// baseline side, which was recorded rather than matched.
	Tier string

	// Missed marks a call the tape could not answer.
	Missed bool
}

// Label is the short display form, e.g. tools/call(read_file).
func (c Call) Label() string {
	if c.Tool == "" {
		return c.Method
	}
	return c.Method + "(" + c.Tool + ")"
}

// IsWrite reports whether this call could have changed the world.
func (c Call) IsWrite() bool { return c.Class == safety.ClassWrite }

// same reports whether two calls are the same call: same tool, same
// normalized arguments.
func (c Call) same(o Call) bool {
	return c.Method == o.Method && c.Tool == o.Tool && c.ArgsHash == o.ArgsHash
}

// sameTool reports whether two calls invoke the same thing with different
// arguments. This is a near-miss, not a match, and the alignment scores it
// as cheaper than swapping one tool for a completely different one.
func (c Call) sameTool(o Call) bool {
	return c.Method == o.Method && c.Tool == o.Tool
}

// Trajectory is an ordered sequence of calls.
type Trajectory struct {
	Name  string
	Calls []Call
}

// Writes returns only the calls that could have changed the world.
//
// This is what "the outcome" means here, and the choice is worth defending.
// The alternative was the agent's final prose answer, which is closer to what
// a user cares about but requires capturing the agent's own output rather
// than just its tool traffic, and is itself model-generated and therefore
// noisy. Write calls are the agent's actual effect on the world: the files it
// edited, the pull requests it opened, the refunds it issued. Two runs that
// made the same write calls did the same thing, whatever they said about it.
func (t Trajectory) Writes() []Call {
	var out []Call
	for _, c := range t.Calls {
		if c.IsWrite() {
			out = append(out, c)
		}
	}
	return out
}

func truncateArgs(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
