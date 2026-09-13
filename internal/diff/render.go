package diff

import (
	"fmt"
	"io"
	"strings"
)

// Render writes a side-by-side alignment.
//
// This is the headline output of the whole tool, so it gets more care than a
// debug dump would. Two columns, baseline left and candidate right, with the
// marker between them carrying the meaning:
//
//	·   the same call in both
//	~   same tool, different arguments
//	>   only the candidate made this call
//	<   only the baseline made it
//	!   a write call that differs, which is what decides the verdict
//
// Matching runs of calls are collapsed, because a hundred identical reads
// between two interesting differences is noise, and a diff nobody reads to
// the end is not doing its job.
func Render(w io.Writer, d Diff, opts RenderOptions) {
	if opts.Width <= 0 {
		opts.Width = 38
	}
	if opts.Context < 0 {
		opts.Context = 0
	}

	fmt.Fprintf(w, "  %-*s   %-*s\n", opts.Width, d.Baseline.Name, opts.Width, d.Candidate.Name)
	fmt.Fprintf(w, "  %s   %s\n", strings.Repeat("─", opts.Width), strings.Repeat("─", opts.Width))

	keep := visible(d.Pairs, opts.Context)

	skipped := 0
	for i, p := range d.Pairs {
		if !keep[i] {
			skipped++
			continue
		}
		if skipped > 0 {
			fmt.Fprintf(w, "  %s\n", center(fmt.Sprintf("… %d identical calls …", skipped), opts.Width*2+3))
			skipped = 0
		}
		renderPair(w, p, opts.Width)
	}
	if skipped > 0 {
		fmt.Fprintf(w, "  %s\n", center(fmt.Sprintf("… %d identical calls …", skipped), opts.Width*2+3))
	}

	fmt.Fprintf(w, "\n  %s\n", d.Summary())
	fmt.Fprintf(w, "  %d matched · %d changed · %d added · %d dropped",
		d.Matched, d.Substituted, d.Inserted, d.Deleted)
	if d.Missed > 0 {
		fmt.Fprintf(w, " · %d unmatched by the tape", d.Missed)
	}
	fmt.Fprintln(w)
}

// RenderOptions controls the rendering.
type RenderOptions struct {
	Width int

	// Context is how many identical calls to keep around each difference.
	// Zero collapses every identical run.
	Context int
}

func renderPair(w io.Writer, p Pair, width int) {
	marker := "·"
	switch p.Op {
	case OpSubstitute:
		marker = "~"
		if isWritePair(p) {
			marker = "!"
		}
	case OpInsert:
		marker = ">"
		if p.Candidate.IsWrite() {
			marker = "!"
		}
	case OpDelete:
		marker = "<"
		if p.Baseline.IsWrite() {
			marker = "!"
		}
	}

	left, right := "", ""
	if p.Baseline != nil {
		left = cell(*p.Baseline, width)
	}
	if p.Candidate != nil {
		right = cell(*p.Candidate, width)
	}
	fmt.Fprintf(w, "  %-*s %s %-*s\n", width, left, marker, width, right)
}

func isWritePair(p Pair) bool {
	return (p.Baseline != nil && p.Baseline.IsWrite()) ||
		(p.Candidate != nil && p.Candidate.IsWrite())
}

// cell renders one call, fitting the label and as much of the arguments as
// the column allows. Arguments earn their space: "read_file" twice tells you
// nothing, "read_file(auth.go)" versus "read_file(main.go)" tells you
// everything.
func cell(c Call, width int) string {
	label := c.Label()
	if c.Args == "" {
		return truncateArgs(label, width)
	}
	room := width - len(label) - 3
	if room < 6 {
		return truncateArgs(label, width)
	}
	return label + " " + truncateArgs(c.Args, room)
}

// visible marks which pairs to print: every difference, plus the requested
// context around each.
func visible(pairs []Pair, context int) []bool {
	keep := make([]bool, len(pairs))
	for i, p := range pairs {
		if p.Op == OpMatch {
			continue
		}
		lo := max(0, i-context)
		hi := min(len(pairs)-1, i+context)
		for j := lo; j <= hi; j++ {
			keep[j] = true
		}
	}
	return keep
}

func center(s string, width int) string {
	if len(s) >= width {
		return s
	}
	pad := (width - len(s)) / 2
	return strings.Repeat(" ", pad) + s
}
