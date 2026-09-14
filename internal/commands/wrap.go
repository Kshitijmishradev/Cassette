package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kshitijmishradev/cassette/internal/cli"
	"github.com/Kshitijmishradev/cassette/internal/config"
	"github.com/Kshitijmishradev/cassette/internal/env"
	"github.com/Kshitijmishradev/cassette/internal/proxy"
	"github.com/Kshitijmishradev/cassette/internal/record"
	"github.com/Kshitijmishradev/cassette/internal/replay"
	"github.com/Kshitijmishradev/cassette/internal/tape"
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
		Flags: func(fs *flag.FlagSet) {
			fs.String("name", "", "tape name for this server (default: derived from the command)")
		},
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
		dir := os.Getenv(env.TapeVar)
		if dir == "" {
			return fmt.Errorf("%s=record but %s is unset; run this through `cassette record`", env.ModeVar, env.TapeVar)
		}

		name := ctx.Flags.Lookup("name").Value.String()
		if name == "" {
			name = record.TapeName(ctx.Child)
		}

		rec, err := record.New(filepath.Join(dir, name+".cas"))
		if err != nil {
			return err
		}
		// Closed before returning, whatever happens, so a tape is finalized
		// even when the server dies badly. An unclosed tape is missing its
		// header and is unreadable.
		defer func() {
			if cerr := rec.Close(); cerr != nil {
				fmt.Fprintf(os.Stderr, "cassette: finalizing tape %s: %v\n", name, cerr)
			}
		}()
		observer = rec

	case env.ModeReplay:
		// Replay never reaches the proxy: there is no server to proxy to.
		// The tape answers directly, and a live process is spawned only if
		// a read-class call misses.
		return runReplay(ctx, logfFor())
	}

	logf := logfFor()

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

// logfFor returns a diagnostic logger, or nil when debugging is off.
//
// Diagnostics can only go to stderr, which the agent may be capturing and
// showing to a user who did not ask to see our internals, so they are off by
// default.
func logfFor() func(string, ...any) {
	if os.Getenv(debugVar) == "" {
		return nil
	}
	return func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "cassette: "+format+"\n", args...)
	}
}

// runReplay serves this server's traffic from its tape.
func runReplay(ctx *cli.Context, logf func(string, ...any)) error {
	dir := os.Getenv(env.TapeVar)
	if dir == "" {
		return fmt.Errorf("%s=replay but %s is unset; run this through `cassette replay`", env.ModeVar, env.TapeVar)
	}

	name := ctx.Flags.Lookup("name").Value.String()
	if name == "" {
		name = record.TapeName(ctx.Child)
	}

	t, err := tape.Open(filepath.Join(dir, name+".cas"))
	if err != nil {
		return fmt.Errorf("opening tape for %s: %w", name, err)
	}
	defer t.Close()

	if !t.Header().Complete() {
		fmt.Fprintf(os.Stderr, "cassette: tape %s is incomplete; the recording was interrupted\n", name)
	}

	cfg, _, err := config.Load(".")
	if err != nil {
		return err
	}
	if p := os.Getenv(env.ConfigVar); p != "" {
		if cfg, err = config.LoadFile(p); err != nil {
			return err
		}
	}

	// Fall-through needs the real server command, which is exactly the argv
	// the agent handed us. Nil disables it entirely, which is what makes a
	// hermetic replay provable rather than merely intended.
	var liveCmd []string
	if cfg.Replay.AllowFallThrough() {
		liveCmd = ctx.Child
	}

	writeReport := func(res replay.Result) {
		if werr := replay.NewReport(name, res).Write(dir); werr != nil && logf != nil {
			logf("writing replay report: %v", werr)
		}
	}

	// Publish an initial report before reading stdin, then checkpoint after
	// every message. Some MCP clients terminate their server children as soon
	// as the task ends instead of closing stdin; waiting for a clean EOF would
	// lose the whole trajectory in that common lifecycle.
	writeReport(replay.Result{})
	res, err := replay.Run(ctx.Ctx, replay.Options{
		Tape:        t,
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Classifier:  cfg.Classifier(),
		LiveCommand: liveCmd,
		LiveEnv:     replayEnv(),
		LiveStderr:  os.Stderr,
		Logf:        logf,
		Progress:    writeReport,
	})
	writeReport(res)
	if err != nil {
		return err
	}
	return nil
}

// replayEnv strips cassette's own variables before spawning a live server.
// Without this, a server that happens to be another cassette shim would try
// to replay from the same tape, recursively.
func replayEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, env.ModeVar+"="),
			strings.HasPrefix(kv, env.TapeVar+"="),
			strings.HasPrefix(kv, env.RunIDVar+"="):
			continue
		}
		out = append(out, kv)
	}
	return out
}
