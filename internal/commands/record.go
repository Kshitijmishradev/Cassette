package commands

import "github.com/Kshitijmishradev/cassette/internal/cli"

func recordCmd() *cli.Command {
	return &cli.Command{
		Name:  "record",
		Usage: "record <name> -- <agent command>",
		Short: "Run an agent for real and record every tool call to a cassette",
		Long: `Record runs an agent against the real world and captures every tool
call and response into a cassette file.

    cassette record fix-auth -- claude -p "fix the failing auth test"

It sets CASSETTE_MODE=record on the agent process, which every wrapped MCP
server inherits, so a single run is captured across all of them into one
ordered trajectory.

Recording is the slow path and that is fine. It happens once, offline, and
everything expensive belongs here rather than in replay: response bodies are
stored pre-framed so replay never parses them, and match embeddings are
computed now so replay never has to.`,
		Run: func(ctx *cli.Context) error {
			if len(ctx.Args) != 1 {
				return cli.Usagef("expected exactly one cassette name, got %d", len(ctx.Args))
			}
			if !ctx.HasChild || len(ctx.Child) == 0 {
				return cli.Usagef("missing agent command; put it after --")
			}
			return pending(2, "record")
		},
	}
}
