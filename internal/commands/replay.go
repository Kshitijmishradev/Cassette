package commands

import "github.com/Kshitijmishradev/cassette/internal/cli"

func replayCmd() *cli.Command {
	return &cli.Command{
		Name:  "replay",
		Usage: "replay <name> -- <agent command>",
		Short: "Re-run an agent against a recorded cassette instead of the real world",
		Long: `Replay runs an agent with its tool responses served from a cassette.

    cassette replay fix-auth -- claude -p "fix the failing auth test"

The agent cannot tell the difference. It issues the same calls and gets the
world as it looked at record time, which is the point: the environment is
pinned, so a change in behavior is attributable to the thing you actually
changed rather than to the world having moved.

When a call does not match anything on the tape, what happens depends on the
tool's safety class and not on a global setting. Read-class tools may fall
through to the real server and extend the tape. Write-class tools halt the
replay, because falling through means performing a real side effect. Unknown
tools are treated as write-class.`,
		Run: func(ctx *cli.Context) error {
			if len(ctx.Args) != 1 {
				return cli.Usagef("expected exactly one cassette name, got %d", len(ctx.Args))
			}
			if !ctx.HasChild || len(ctx.Child) == 0 {
				return cli.Usagef("missing agent command; put it after --")
			}
			return pending(3, "replay")
		},
	}
}
