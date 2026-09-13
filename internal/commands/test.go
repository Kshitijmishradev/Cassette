package commands

import (
	"flag"

	"github.com/Kshitijmishradev/cassette/internal/cli"
)

func testCmd() *cli.Command {
	return &cli.Command{
		Name:  "test",
		Usage: "test [--suite <dir>] [--baseline <ref>] [--fail-on <policy>]",
		Short: "Replay a suite of cassettes and report how behavior changed",
		Long: `Test replays every cassette in a suite and diffs each resulting
trajectory against its recorded baseline.

    cassette test --suite ./cassettes --baseline main

Each cassette gets one of three verdicts: identical, same outcome by a
different path, or outcome changed. The middle verdict is the interesting one,
because it is where you learn that a change made the agent take nine extra
steps to reach the same place.

Replay touches nothing external, so cassettes are fully independent and run in
parallel. Wall clock is bounded by model rate limits, not by anything here.

Exit status is part of the contract: 0 when the suite passes the --fail-on
policy, 1 when behavior changed in a way that violates it, and 2 when the tool
itself failed. A CI job that cannot tell the last two apart is useless.`,
		Flags: func(fs *flag.FlagSet) {
			fs.String("suite", "./cassettes", "directory of cassettes to replay")
			fs.String("baseline", "", "git ref to diff trajectories against (default: the recorded baseline)")
			fs.String("fail-on", "outcome", "fail the run on: outcome, path, or never")
			fs.Int("jobs", 0, "parallel replays (default: number of CPUs)")
		},
		Run: func(ctx *cli.Context) error {
			return pending(5, "test")
		},
	}
}
