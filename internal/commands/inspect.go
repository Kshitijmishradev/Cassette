package commands

import (
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/Kshitijmishradev/cassette/internal/cli"
	"github.com/Kshitijmishradev/cassette/internal/record"
)

func inspectCmd() *cli.Command {
	return &cli.Command{
		Name:  "inspect",
		Usage: "inspect <name> [--suite <dir>] [--full]",
		Short: "Show what a recorded run contains",
		Long: `Inspect prints the trajectory of a recorded run: every message in the
order it happened, merged across all of the servers involved.

    cassette inspect fix-auth

Messages from different servers are separate processes with no shared
sequence number, so ordering comes from wall-clock timestamps. Ties break
deterministically, since this ordering is what a diff will later compare.

--full also prints the request and response bodies, truncated. Useful when a
replay misses and the question is what the arguments actually looked like.`,
		Flags: func(fs *flag.FlagSet) {
			fs.String("suite", "./cassettes", "directory holding recorded runs")
			fs.Bool("full", false, "include request and response bodies")
			fs.Int("width", 96, "truncate bodies to this width with --full")
		},
		Run: runInspect,
	}
}

func runInspect(ctx *cli.Context) error {
	if len(ctx.Args) != 1 {
		return cli.Usagef("expected exactly one cassette name, got %d", len(ctx.Args))
	}

	suite := ctx.Flags.Lookup("suite").Value.String()
	full := ctx.Flags.Lookup("full").Value.String() == "true"
	width := 96
	fmt.Sscanf(ctx.Flags.Lookup("width").Value.String(), "%d", &width)

	dir, err := record.ChildPath(suite, ctx.Args[0])
	if err != nil {
		return err
	}
	run, err := record.OpenRun(dir)
	if err != nil {
		return err
	}
	defer run.Close()

	m := run.Manifest
	entries, toolCalls, errs, truncated := m.Totals()

	fmt.Fprintf(ctx.Out, "%s\n", m.Name)
	fmt.Fprintf(ctx.Out, "  recorded  %s\n", m.CreatedAt.Local().Format(time.RFC1123))
	fmt.Fprintf(ctx.Out, "  agent     %s\n", strings.Join(m.Agent, " "))
	fmt.Fprintf(ctx.Out, "  build     %s\n", m.Cassette)
	fmt.Fprintf(ctx.Out, "  tapes     %d\n", len(m.Tapes))
	fmt.Fprintf(ctx.Out, "  messages  %d (%d tool calls", entries, toolCalls)
	if errs > 0 {
		fmt.Fprintf(ctx.Out, ", %d errors", errs)
	}
	if truncated > 0 {
		fmt.Fprintf(ctx.Out, ", %d truncated", truncated)
	}
	fmt.Fprintf(ctx.Out, ")\n")
	fmt.Fprintf(ctx.Out, "  duration  %s\n\n", m.Duration().Round(time.Millisecond))

	if len(run.Steps) == 0 {
		fmt.Fprintf(ctx.Out, "  (no messages recorded)\n")
		return nil
	}

	base := run.Steps[0].Entry.StartedNanos
	fmt.Fprintf(ctx.Out, "  %6s %5s  %-38s %10s\n", "at", "", "message", "took")

	for i, s := range run.Steps {
		at := time.Duration(s.Entry.StartedNanos - base)
		took := "-"
		if s.Entry.DurationNs > 0 {
			took = time.Duration(s.Entry.DurationNs).Round(time.Microsecond).String()
		}

		fmt.Fprintf(ctx.Out, "  %6s %5d  %-38s %10s%s\n",
			at.Round(time.Millisecond), i, truncate(s.String(), 38), took, stepFlags(s))

		if full {
			if req := run.Request(s); len(req) > 0 {
				fmt.Fprintf(ctx.Out, "         ->  %s\n", truncate(oneLine(req), width))
			}
			if resp := run.Response(s); len(resp) > 0 {
				fmt.Fprintf(ctx.Out, "         <-  %s\n", truncate(oneLine(resp), width))
			}
		}
	}
	return nil
}

func stepFlags(s record.Step) string {
	var f []string
	if s.Entry.IsError() {
		f = append(f, "error")
	}
	if s.Entry.Truncated() {
		f = append(f, "truncated")
	}
	if s.Entry.ServerInitiated() {
		f = append(f, "unprompted")
	}
	if !s.Entry.HasResponse() && !s.Entry.ServerInitiated() && !s.Entry.Truncated() {
		f = append(f, "notify")
	}
	if len(f) == 0 {
		return ""
	}
	return "  " + strings.Join(f, ",")
}

func oneLine(b []byte) string {
	return strings.TrimSpace(strings.ReplaceAll(string(b), "\n", " "))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
