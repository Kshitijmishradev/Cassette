package commands

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/Kshitijmishradev/cassette/internal/cli"
	"github.com/Kshitijmishradev/cassette/internal/env"
	"github.com/Kshitijmishradev/cassette/internal/record"
	"github.com/Kshitijmishradev/cassette/internal/replay"
)

func replayCmd() *cli.Command {
	return &cli.Command{
		Name:  "replay",
		Usage: "replay <name> [--suite <dir>] [--hermetic] [-- <agent command>]",
		Short: "Re-run an agent against a recorded cassette instead of the real world",
		Long: `Replay runs an agent with its tool responses served from a tape.

    cassette replay fix-auth -- claude -p "fix the failing auth test"

With no agent command, the one recorded in the manifest is reused.

There is no server process during replay. The tape answers directly, which
is what makes a replay free, fast and side-effect free.

When a call has no recording, what happens depends on the tool, not on a
global setting. A read-only tool may be run for real, which requires
spawning the server and fast-forwarding it through the recorded handshake.
Anything else is refused, because letting an unmatched write through turns a
free experiment into a production action. Tools are read-only if listed in
cassette.json or if their name follows a conventional read pattern;
everything else is a write.

--hermetic disables fall-through entirely, so nothing can leave the process
and any miss stops being answerable. That is the honest setting for CI,
where a replay that quietly talked to the network is not the experiment
anyone thought they were running.`,
		Flags: func(fs *flag.FlagSet) {
			fs.String("suite", "./cassettes", "directory holding recorded runs")
			fs.Bool("hermetic", false, "refuse every miss; never spawn a live server")
		},
		Run: runReplayCommand,
	}
}

func runReplayCommand(ctx *cli.Context) error {
	if len(ctx.Args) != 1 {
		return cli.Usagef("expected exactly one cassette name, got %d", len(ctx.Args))
	}

	name := ctx.Args[0]
	suite := ctx.Flags.Lookup("suite").Value.String()
	hermetic := ctx.Flags.Lookup("hermetic").Value.String() == "true"
	dir := filepath.Join(suite, name)

	m, err := record.ReadManifest(dir)
	if err != nil {
		return fmt.Errorf("reading recording %q: %w", name, err)
	}

	// Reusing the recorded command is the default because the whole point is
	// to change one thing at a time. Typing the agent command again by hand
	// is a chance to change it by accident.
	agentArgv := ctx.Child
	if len(agentArgv) == 0 {
		agentArgv = m.Agent
		if len(agentArgv) == 0 {
			return cli.Usagef("no agent command given and none recorded in the manifest")
		}
	}

	// Stale reports from a previous replay would otherwise be collected as
	// if they belonged to this one.
	if err := replay.CleanReports(dir); err != nil {
		return err
	}

	started := time.Now()

	agent := exec.CommandContext(ctx.Ctx, agentArgv[0], agentArgv[1:]...)
	agent.Stdin = os.Stdin
	agent.Stdout = ctx.Out
	agent.Stderr = ctx.Err
	agent.Env = append(os.Environ(),
		env.ModeVar+"="+string(env.ModeReplay),
		env.TapeVar+"="+mustAbs(dir),
		env.RunIDVar+"="+newRunID(),
	)
	if hermetic {
		agent.Env = append(agent.Env, env.ConfigVar+"="+mustAbs(hermeticConfig(dir)))
	}

	mode := "replaying"
	if hermetic {
		mode = "replaying (hermetic)"
	}
	fmt.Fprintf(ctx.Err, "cassette: %s %q from %s\n", mode, name, dir)

	exitCode := 0
	if err := agent.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			return fmt.Errorf("running agent: %w", err)
		}
		exitCode = ee.ExitCode()
	}

	reports, err := replay.CollectReports(dir)
	if err != nil {
		return fmt.Errorf("collecting replay reports: %w", err)
	}
	printReplaySummary(ctx, reports, time.Since(started))

	if len(reports) == 0 {
		fmt.Fprintf(ctx.Err, "\ncassette: no shim reported in. Is `cassette wrap` in the agent's MCP config?\n")
		return cli.Failuref("nothing was replayed")
	}

	var refused, fell int
	for _, r := range reports {
		refused += r.Refused
		fell += r.FellThrough
	}
	if refused > 0 {
		return cli.Failuref("%d call(s) had no recording and could not be run safely", refused)
	}
	if exitCode != 0 {
		return &cli.ExitCodeError{Code: exitCode}
	}
	return nil
}

func printReplaySummary(ctx *cli.Context, reports []replay.Report, wall time.Duration) {
	if len(reports) == 0 {
		return
	}

	fmt.Fprintf(ctx.Err, "\n  %-28s %7s %7s %7s %7s %7s %7s\n",
		"tape", "served", "exact", "norm", "method", "live", "refused")

	var served, exact, norm, byMethod, fell, refused, unused, repeats int
	for _, r := range reports {
		fmt.Fprintf(ctx.Err, "  %-28s %7d %7d %7d %7d %7d %7d\n",
			r.Tape, r.Served, r.ByTier["exact"], r.ByTier["norm"], r.ByTier["method"], r.FellThrough, r.Refused)
		served += r.Served
		exact += r.ByTier["exact"]
		norm += r.ByTier["norm"]
		byMethod += r.ByTier["method"]
		fell += r.FellThrough
		refused += r.Refused
		unused += r.Unused
		repeats += r.Repeats
	}

	fmt.Fprintf(ctx.Err, "\n  %d served", served)
	if served > 0 {
		fmt.Fprintf(ctx.Err, " (%d%% exact)", exact*100/served)
	}
	if byMethod > 0 {
		fmt.Fprintf(ctx.Err, " · %d by method only", byMethod)
	}
	if repeats > 0 {
		fmt.Fprintf(ctx.Err, " · %d repeated", repeats)
	}
	if fell > 0 {
		fmt.Fprintf(ctx.Err, " · %d fell through to a live server", fell)
	}
	if refused > 0 {
		fmt.Fprintf(ctx.Err, " · %d REFUSED", refused)
	}
	if unused > 0 {
		// Recorded calls this run never made. Half of a trajectory diff,
		// available for free from the matcher's own bookkeeping.
		fmt.Fprintf(ctx.Err, " · %d recorded calls unused", unused)
	}
	fmt.Fprintf(ctx.Err, " · %s\n", wall.Round(time.Millisecond))

	// Misses are the actionable output: they say exactly what the new run
	// asked for that the old one never did.
	var misses []replay.Miss
	for _, r := range reports {
		misses = append(misses, r.Misses...)
	}
	if len(misses) == 0 {
		return
	}
	sort.SliceStable(misses, func(i, j int) bool { return misses[i].Resolved > misses[j].Resolved })

	fmt.Fprintf(ctx.Err, "\n  unmatched calls:\n")
	for _, m := range misses {
		label := m.Method
		if m.Tool != "" {
			label = fmt.Sprintf("%s(%s)", m.Method, m.Tool)
		}
		fmt.Fprintf(ctx.Err, "    %-10s %-34s %s\n", m.Resolved, truncate(label, 34), truncate(m.Args, 60))
	}
}

// hermeticConfig writes a config that forbids fall-through, so the setting
// reaches every shim through the same channel as any other config rather
// than needing its own environment variable.
func hermeticConfig(dir string) string {
	path := filepath.Join(dir, ".hermetic.json")
	_ = os.WriteFile(path, []byte(`{"replay":{"fallThrough":false}}`+"\n"), 0o644)
	return path
}
