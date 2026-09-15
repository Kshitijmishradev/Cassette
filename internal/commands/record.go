package commands

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Kshitijmishradev/cassette/internal/buildinfo"
	"github.com/Kshitijmishradev/cassette/internal/cli"
	"github.com/Kshitijmishradev/cassette/internal/env"
	"github.com/Kshitijmishradev/cassette/internal/record"
)

func recordCmd() *cli.Command {
	return &cli.Command{
		Name:  "record",
		Usage: "record <name> [--suite <dir>] [--force] -- <agent command>",
		Short: "Run an agent for real and record every tool call to a cassette",
		Long: `Record runs an agent against the real world and captures every message
it exchanges with its MCP servers.

    cassette record fix-auth -- claude -p "fix the failing auth test"

It sets CASSETTE_MODE on the agent process, which every wrapped MCP server
inherits, so one run is captured across all of them.

Each server writes its own tape, because they are separate processes and
making them share one file would mean a lock on the recording path. The
manifest is assembled afterwards by scanning the directory, which needs no
coordination at all.

Recording is the slow path, and that is where the expensive work belongs.
Responses are stored pre-framed so replay never has to serialize, and match
hashes are computed now so replay never has to.`,
		Flags: func(fs *flag.FlagSet) {
			fs.String("suite", "./cassettes", "directory holding recorded runs")
			fs.Bool("force", false, "overwrite an existing recording with this name")
		},
		Run: runRecord,
	}
}

func runRecord(ctx *cli.Context) error {
	if len(ctx.Args) != 1 {
		return cli.Usagef("expected exactly one cassette name, got %d", len(ctx.Args))
	}
	if !ctx.HasChild || len(ctx.Child) == 0 {
		return cli.Usagef("missing agent command; put it after --")
	}

	name := ctx.Args[0]
	if err := record.ValidateName(name); err != nil {
		return err
	}
	suite := ctx.Flags.Lookup("suite").Value.String()
	force := ctx.Flags.Lookup("force").Value.String() == "true"

	dir := filepath.Join(suite, name)
	if err := prepareDir(dir, force); err != nil {
		return err
	}

	runID := newRunID()
	started := time.Now()

	// The agent inherits our stdio: it is an interactive program and the
	// user is watching it work. Wrapping its output would be the second
	// place this tool could accidentally change what it observes.
	agent := exec.CommandContext(ctx.Ctx, ctx.Child[0], ctx.Child[1:]...)
	agent.Stdin = os.Stdin
	agent.Stdout = ctx.Out
	agent.Stderr = ctx.Err
	agent.Env = append(os.Environ(),
		env.ModeVar+"="+string(env.ModeRecord),
		env.TapeVar+"="+mustAbs(dir),
		env.RunIDVar+"="+runID,
	)

	fmt.Fprintf(ctx.Err, "cassette: recording %q to %s\n", name, dir)

	agentErr := agent.Run()
	exitCode := 0
	if agentErr != nil {
		if ee, ok := agentErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			return fmt.Errorf("running agent: %w", agentErr)
		}
	}

	// The manifest is written even when the agent failed. A run that crashed
	// partway is still a recording, and often the most interesting one.
	tapes, err := record.ScanDir(dir)
	if err != nil {
		return fmt.Errorf("scanning tapes: %w", err)
	}

	m := record.Manifest{
		Name:          name,
		RunID:         runID,
		CreatedAt:     started,
		Agent:         ctx.Child,
		Cassette:      buildinfo.Get().Short(),
		AgentExitCode: exitCode,
		Tapes:         tapes,
	}
	if err := record.WriteManifest(dir, m); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}

	printRecordSummary(ctx, m, time.Since(started))

	if len(tapes) == 0 {
		fmt.Fprintf(ctx.Err, "\ncassette: no tapes were written. Is `cassette wrap` in the agent's MCP config?\n")
	}
	if exitCode != 0 {
		return &cli.ExitCodeError{Code: exitCode}
	}
	return nil
}

func printRecordSummary(ctx *cli.Context, m record.Manifest, wall time.Duration) {
	entries, toolCalls, errs, truncated := m.Totals()

	fmt.Fprintf(ctx.Err, "\n  %-28s %8s %8s %8s\n", "tape", "msgs", "calls", "size")
	for _, t := range m.Tapes {
		flag := ""
		if !t.Complete {
			flag = "  INCOMPLETE"
		}
		fmt.Fprintf(ctx.Err, "  %-28s %8d %8d %7dK%s\n",
			t.File, t.Entries, t.ToolCalls, t.Bytes>>10, flag)
	}

	fmt.Fprintf(ctx.Err, "\n  %d tapes · %d messages · %d tool calls", len(m.Tapes), entries, toolCalls)
	if errs > 0 {
		fmt.Fprintf(ctx.Err, " · %d errors", errs)
	}
	if truncated > 0 {
		fmt.Fprintf(ctx.Err, " · %d truncated", truncated)
	}
	fmt.Fprintf(ctx.Err, " · %s\n", wall.Round(time.Millisecond))
}

// prepareDir makes the recording directory, refusing to silently replace an
// existing run. A recording can represent a session that took real time and
// real money, so overwriting one has to be asked for.
func prepareDir(dir string, force bool) error {
	if _, err := os.Stat(dir); err == nil {
		if !force {
			return fmt.Errorf("%s already exists; pass --force to overwrite it", dir)
		}
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("removing existing recording: %w", err)
		}
	}
	return os.MkdirAll(dir, 0o700)
}

func mustAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// newRunID identifies one execution across every shim it spawns. Time-based
// rather than random so a directory listing sorts chronologically, with a
// process id to separate runs started in the same millisecond.
func newRunID() string {
	return time.Now().UTC().Format("20060102T150405.000") + "-" + strconv.Itoa(os.Getpid())
}
