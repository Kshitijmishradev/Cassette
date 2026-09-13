package commands

import "github.com/Kshitijmishradev/cassette/internal/cli"

func wrapCmd() *cli.Command {
	return &cli.Command{
		Name:  "wrap",
		Usage: "wrap -- <mcp server command>",
		Short: "Proxy an MCP server, recording or replaying per the environment",
		Long: `Wrap spawns an MCP server as a child process and sits on the stdio
JSON-RPC stream between it and the agent.

This is the shim. It goes in your agent's MCP configuration once and then
stays there:

    {
      "mcpServers": {
        "github": {
          "command": "cassette",
          "args": ["wrap", "--", "npx", "-y", "@modelcontextprotocol/server-github"]
        }
      }
    }

What it does depends entirely on CASSETTE_MODE in the environment it was
spawned into, which the outer record/replay commands set. With nothing set it
forwards every frame untouched and records nothing, so a shim left in a config
is invisible during ordinary work. That property is not a convenience, it is
the condition for anyone leaving it installed.`,
		Run: func(ctx *cli.Context) error {
			if !ctx.HasChild {
				return cli.Usagef("missing server command; put it after --")
			}
			if len(ctx.Child) == 0 {
				return cli.Usagef("-- was given but no server command followed it")
			}
			return pending(1, "wrap")
		},
	}
}
