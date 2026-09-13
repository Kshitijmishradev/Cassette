package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
)

// Run dispatches argv (without the program name) to a registered command and
// returns a process exit code.
func (a *App) Run(argv []string, out, errw io.Writer) int {
	if len(argv) == 0 {
		a.PrintHelp(out)
		return ExitOK
	}

	switch argv[0] {
	case "help", "-h", "--help":
		if len(argv) > 1 {
			if c, ok := a.Lookup(argv[1]); ok {
				a.PrintCommandHelp(out, c)
				return ExitOK
			}
			fmt.Fprintf(errw, "%s: unknown command %q\n", a.Name, argv[1])
			return ExitError
		}
		a.PrintHelp(out)
		return ExitOK
	}

	cmd, ok := a.Lookup(argv[0])
	if !ok {
		fmt.Fprintf(errw, "%s: unknown command %q\n", a.Name, argv[0])
		fmt.Fprintf(errw, "Run \"%s help\" for the command list.\n", a.Name)
		return ExitError
	}

	// Split at the first bare "--" before flag parsing. Everything after it
	// belongs to a child process and must survive untouched, including
	// arguments that look like our own flags.
	own, child, hasChild := SplitDashDash(argv[1:])

	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(errw)
	fs.Usage = func() { a.PrintCommandHelp(errw, cmd) }
	if cmd.Flags != nil {
		cmd.Flags(fs)
	}
	if err := fs.Parse(own); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			a.PrintCommandHelp(out, cmd)
			return ExitOK
		}
		return ExitError
	}

	ctx := &Context{
		Ctx:      context.Background(),
		Args:     fs.Args(),
		Child:    child,
		HasChild: hasChild,
		Flags:    fs,
		Out:      out,
		Err:      errw,
	}

	err := cmd.Run(ctx)
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, ErrUsage):
		if msg := err.Error(); msg != "usage" {
			fmt.Fprintf(errw, "%s %s: %s\n\n", a.Name, cmd.Name, msg)
		}
		a.PrintCommandHelp(errw, cmd)
		return ExitError
	default:
		// A wrapped process owns its own exit status, and rewriting it would
		// change what the agent sees. Pass it through verbatim.
		var ce *ExitCodeError
		if errors.As(err, &ce) {
			return ce.Code
		}

		var fe *FailureError
		if errors.As(err, &fe) {
			// A real, reportable negative result. The command has already
			// explained itself; do not decorate it.
			return ExitFailure
		}
		fmt.Fprintf(errw, "%s: %v\n", a.Name, err)
		return ExitError
	}
}

// ExitCodeError carries a child process's exit status out to our own.
//
// This exists because cassette wraps other programs. An agent reads the
// server's exit status to decide whether to restart it, so collapsing an
// arbitrary status into our own 0/1/2 scheme would change agent behavior.
type ExitCodeError struct {
	Code int
	Msg  string
}

func (e *ExitCodeError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return fmt.Sprintf("exited with status %d", e.Code)
}

// FailureError marks a negative result that is not a tool malfunction, such
// as `cassette test` finding that agent behavior changed. It maps to
// ExitFailure so CI can tell the two apart.
type FailureError struct{ Msg string }

func (e *FailureError) Error() string { return e.Msg }

// Failuref builds a FailureError.
func Failuref(format string, args ...any) error {
	return &FailureError{Msg: fmt.Sprintf(format, args...)}
}

// Usagef builds a usage error carrying an explanation.
func Usagef(format string, args ...any) error {
	return fmt.Errorf("%w", &usageError{fmt.Sprintf(format, args...)})
}

type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }
func (e *usageError) Unwrap() error { return ErrUsage }
