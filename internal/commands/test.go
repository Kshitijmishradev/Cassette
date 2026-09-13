package commands

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Kshitijmishradev/cassette/internal/cli"
	"github.com/Kshitijmishradev/cassette/internal/config"
	"github.com/Kshitijmishradev/cassette/internal/diff"
	"github.com/Kshitijmishradev/cassette/internal/suite"
)

func testCmd() *cli.Command {
	return &cli.Command{
		Name:  "test",
		Usage: "test [--suite <dir>] [--jobs N] [--fail-on <policy>] [-- <agent command>]",
		Short: "Replay every cassette in a suite and report how behavior changed",
		Long: `Test replays every recorded run in a suite and diffs each resulting
trajectory against its recording.

    cassette test --suite ./cassettes

Each case gets one of three verdicts: identical, drift (the same write calls
reached by a different path), or CHANGED (the agent did something else to the
world). Drift is the interesting one, because it is where a change made the
agent take nine extra steps to reach the same place, and the result looks
fine so nobody catches it.

Cases run in parallel because replay touches nothing external: no network, no
shared database, no rate-limited API. The only real ceiling is whatever the
agent under test talks to. --jobs 1 forces serial, which is useful mainly for
measuring what the parallelism bought.

Exit status is the contract with CI. 0 when the suite passes the --fail-on
policy, 1 when behavior changed in a way that violates it, and 2 when the
tool itself failed. A job that cannot tell the last two apart is useless.`,
		Flags: func(fs *flag.FlagSet) {
			fs.String("suite", "./cassettes", "directory of recorded runs")
			fs.Int("jobs", 0, "parallel replays (default: number of CPUs)")
			fs.String("fail-on", "outcome", "fail when behavior changed: outcome, path, or never")
			fs.Bool("hermetic", true, "refuse every miss; never spawn a live server")
			fs.Bool("verbose", false, "show the full diff for every case, not just changed ones")
			fs.Int("context", 1, "identical calls to show around each difference")
		},
		Run: runTest,
	}
}

func runTest(ctx *cli.Context) error {
	suiteDir := ctx.Flags.Lookup("suite").Value.String()
	jobs, _ := strconv.Atoi(ctx.Flags.Lookup("jobs").Value.String())
	verbose := ctx.Flags.Lookup("verbose").Value.String() == "true"
	context, _ := strconv.Atoi(ctx.Flags.Lookup("context").Value.String())

	cases, err := suite.Discover(suiteDir)
	if err != nil {
		return err
	}
	if len(cases) == 0 {
		return cli.Failuref("no recorded runs found in %s", suiteDir)
	}

	cfg, _, err := config.Load(".")
	if err != nil {
		return err
	}

	opts := suite.Options{
		Agent:      ctx.Child,
		Hermetic:   ctx.Flags.Lookup("hermetic").Value.String() == "true",
		Jobs:       jobs,
		Config:     cfg,
		Classifier: cfg.Classifier(),
	}

	effectiveJobs := jobs
	if effectiveJobs <= 0 {
		effectiveJobs = min(len(cases), runtimeCPUs())
	}
	fmt.Fprintf(ctx.Err, "cassette: replaying %d cassette(s) from %s, %d at a time\n\n",
		len(cases), suiteDir, effectiveJobs)

	started := time.Now()
	done := 0
	results := suite.Run(ctx.Ctx, cases, opts, func(r suite.Result) {
		done++
		fmt.Fprintf(ctx.Err, "  %s\n", caseLine(r, done, len(cases)))
	})
	wall := time.Since(started)

	printSuiteSummary(ctx, results, wall, verbose, context)

	return suiteExit(results, ctx.Flags.Lookup("fail-on").Value.String())
}

// caseLine is the progress line printed as each case finishes.
func caseLine(r suite.Result, n, total int) string {
	status := verdictMark(r.Verdict)
	if r.Failed() {
		status = "ERROR"
	}

	steps := 0
	delta := 0
	for _, d := range r.Diffs {
		steps += len(d.Candidate.Calls)
		delta += d.StepDelta()
	}

	line := fmt.Sprintf("%-24s %-8s %4d steps", truncate(r.Case.Name, 24), status, steps)
	if delta != 0 {
		line += fmt.Sprintf(" %+d", delta)
	} else {
		line += "   "
	}
	line += fmt.Sprintf("  %8s", r.Duration.Round(time.Millisecond))
	if r.Refused > 0 {
		line += fmt.Sprintf("  %d refused", r.Refused)
	}
	if r.Err != nil {
		line += "  " + truncate(r.Err.Error(), 50)
	}
	return line
}

func verdictMark(v diff.Verdict) string {
	switch v {
	case diff.VerdictIdentical:
		return "ok"
	case diff.VerdictPathChanged:
		return "drift"
	default:
		return "CHANGED"
	}
}

func printSuiteSummary(ctx *cli.Context, results []suite.Result, wall time.Duration, verbose bool, context int) {
	var identical, drift, changed, errored, refused int
	for _, r := range results {
		switch {
		case r.Failed():
			errored++
		case r.Verdict == diff.VerdictIdentical:
			identical++
		case r.Verdict == diff.VerdictPathChanged:
			drift++
		default:
			changed++
		}
		refused += r.Refused
	}

	// Diffs are printed after the progress lines rather than interleaved
	// with them, because parallel workers finish out of order and a diff
	// spliced between two unrelated progress lines is unreadable.
	for _, r := range results {
		if r.Failed() {
			fmt.Fprintf(ctx.Err, "\n  %s: could not run\n    %v\n", r.Case.Name, r.Err)
			if len(r.Output) > 0 {
				fmt.Fprintf(ctx.Err, "    agent output:\n%s\n", indent(lastLines(string(r.Output), 10), "      "))
			}
			continue
		}
		for _, d := range r.Diffs {
			if !d.Changed() && !verbose {
				continue
			}
			fmt.Fprintf(ctx.Err, "\n  %s\n\n", r.Case.Name)
			diff.Render(ctx.Err, d, diff.RenderOptions{Width: 38, Context: context})
		}
	}

	fmt.Fprintf(ctx.Err, "\n  %d cassettes · %d identical · %d drift · %d CHANGED",
		len(results), identical, drift, changed)
	if errored > 0 {
		fmt.Fprintf(ctx.Err, " · %d errored", errored)
	}
	if refused > 0 {
		fmt.Fprintf(ctx.Err, " · %d refused calls", refused)
	}
	fmt.Fprintf(ctx.Err, " · %s\n", wall.Round(time.Millisecond))
}

// suiteExit turns the results into the process exit status.
func suiteExit(results []suite.Result, policy string) error {
	var errored, changed, drifted int
	for _, r := range results {
		switch {
		case r.Failed():
			errored++
		case r.Verdict == diff.VerdictOutcomeChanged:
			changed++
		case r.Verdict == diff.VerdictPathChanged:
			drifted++
		}
	}

	// A harness that could not run is not the same as code that regressed,
	// and reporting it as a behavioral failure would send someone hunting
	// for a change that does not exist.
	if errored > 0 {
		return fmt.Errorf("%d cassette(s) could not be run", errored)
	}

	switch policy {
	case "never":
		return nil
	case "path":
		if changed+drifted > 0 {
			return cli.Failuref("%d cassette(s) changed behavior", changed+drifted)
		}
	default:
		if changed > 0 {
			return cli.Failuref("%d cassette(s) changed what the agent did", changed)
		}
	}
	return nil
}

func runtimeCPUs() int { return max(1, numCPU()) }

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
