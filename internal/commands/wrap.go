package commands

import (
	"fmt"
	"os"

	"github.com/Kshitijmishradev/cassette/internal/cli"
	"github.com/Kshitijmishradev/cassette/internal/env"
	"github.com/Kshitijmishradev/cassette/internal/proxy"
)

// debugVar turns on proxy diagnostics. Off by default, because those
// diagnostics go to stderr, which the agent may be capturing and showing to
// a user who did not ask to see our internals.
const debugVar = "CASSETTE_DEBUG"

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
spawned into, which the outer record and replay commands set. With nothing
set it forwards every frame untouched and records nothing, so a shim left in
a config is invisible during ordinary work. That property is not a
convenience, it is the condition for anyone leaving it installed.

Set CASSETTE_DEBUG=1 for diagnostics on stderr.`,
		Run: runWrap,
	}
}

func runWrap(ctx *cli.Context) error {
	if !ctx.HasChild {
		return cli.Usagef("missing server command; put it after --")
	}
	if len(ctx.Child) == 0 {
		return cli.Usagef("-- was given but no server command followed it")
	}

	mode, err := env.ParseMode(os.Getenv(env.ModeVar))
	if err != nil {
		return err
	}

	// The observer stays nil until the phase that fills it in. A nil observer
	// means the proxy does not even parse messages, which is the cheapest and
	// most faithful passthrough available.
	var observer proxy.Observer
	switch mode {
	case env.ModeOff:
		// Pure passthrough.
	case env.ModeRecord:
		return pending(2, "wrap in record mode")
	case env.ModeReplay:
		return pending(3, "wrap in replay mode")
	}

	var logf func(string, ...any)
	if os.Getenv(debugVar) != "" {
		logf = func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "cassette: "+format+"\n", args...)
		}
	}

	code, err := proxy.Run(ctx.Ctx, proxy.Options{
		Command:  ctx.Child,
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Observer: observer,
		Logf:     logf,
	})
	if err != nil {
		return err
	}
	if code != 0 {
		// The wrapped server's status belongs to the server. Reporting our
		// own instead would tell the agent something untrue about it.
		return &cli.ExitCodeError{Code: code}
	}
	return nil
}
