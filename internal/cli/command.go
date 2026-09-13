// Package cli implements a small, dependency-free command router.
//
// Cassette ships with no third-party modules (see go.mod for why), so this
// stands in for a library like cobra. It covers exactly what this tool
// needs and nothing more: a flat set of subcommands, per-command flag sets,
// and help output that reads well in a terminal.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// Exit codes. These are part of the tool's contract with CI, so they are
// fixed: a caller distinguishing "the agent's behavior changed" from "the
// tool broke" depends on them.
const (
	ExitOK      = 0 // success, and for `test`, no behavioral change
	ExitFailure = 1 // the command ran correctly and reported a negative result
	ExitError   = 2 // the tool itself failed: bad usage, I/O error, crash
)

// ErrUsage signals that the user invoked a command incorrectly. Returning it
// makes the runner print usage and exit with ExitError rather than dumping a
// stack-shaped error message.
var ErrUsage = errors.New("usage")

// Command is a single subcommand, such as `cassette record`.
type Command struct {
	Name string

	// Usage is the one-line invocation form shown in help, without the
	// binary name. Example: "record <name> -- <agent command>".
	Usage string

	// Short is one sentence, shown in the top-level command list. It should
	// fit on a line alongside the command name.
	Short string

	// Long is optional prose shown by `cassette help <name>`.
	Long string

	// Flags registers command-specific flags. May be nil.
	Flags func(fs *flag.FlagSet)

	// Run executes the command.
	Run func(ctx *Context) error
}

// Context carries everything a command needs to do its work. Output writers
// are injected rather than using os.Stdout directly so commands stay
// testable, which matters here: this tool's whole premise is that behavior
// should be verifiable.
type Context struct {
	// Args are the positional arguments that appeared before any "--".
	Args []string

	// Child is the argv that followed a bare "--", passed through
	// untouched and in order. For `cassette wrap -- npx server`, this is
	// ["npx", "server"].
	Child []string

	// HasChild distinguishes "no -- was given" from "-- was given with
	// nothing after it". The difference is a usage error worth reporting.
	HasChild bool

	Flags *flag.FlagSet
	Out   io.Writer
	Err   io.Writer
}

// App is the set of registered commands.
type App struct {
	Name     string
	Short    string
	commands map[string]*Command
	order    []string
}

// New creates an App.
func New(name, short string) *App {
	return &App{
		Name:     name,
		Short:    short,
		commands: make(map[string]*Command),
	}
}

// Register adds a command. It panics on a duplicate name, because that is a
// programming error that should never reach a user.
func (a *App) Register(c *Command) {
	if _, dup := a.commands[c.Name]; dup {
		panic("cli: duplicate command " + c.Name)
	}
	a.commands[c.Name] = c
	a.order = append(a.order, c.Name)
}

// Lookup returns a registered command by name.
func (a *App) Lookup(name string) (*Command, bool) {
	c, ok := a.commands[name]
	return c, ok
}

// PrintHelp writes the top-level help listing.
func (a *App) PrintHelp(w io.Writer) {
	fmt.Fprintf(w, "%s - %s\n\n", a.Name, a.Short)
	fmt.Fprintf(w, "Usage:\n  %s <command> [flags]\n\nCommands:\n", a.Name)

	names := make([]string, len(a.order))
	copy(names, a.order)
	sort.Strings(names)

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	for _, n := range names {
		fmt.Fprintf(tw, "  %s\t%s\n", n, a.commands[n].Short)
	}
	tw.Flush()

	fmt.Fprintf(w, "\nRun \"%s help <command>\" for details on a command.\n", a.Name)
}

// PrintCommandHelp writes help for a single command.
func (a *App) PrintCommandHelp(w io.Writer, c *Command) {
	fmt.Fprintf(w, "Usage:\n  %s %s\n", a.Name, c.Usage)
	if c.Long != "" {
		fmt.Fprintf(w, "\n%s\n", strings.TrimSpace(c.Long))
	} else if c.Short != "" {
		fmt.Fprintf(w, "\n%s\n", c.Short)
	}

	if c.Flags != nil {
		fs := flag.NewFlagSet(c.Name, flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		c.Flags(fs)

		var any bool
		fs.VisitAll(func(*flag.Flag) { any = true })
		if any {
			fmt.Fprintf(w, "\nFlags:\n")
			fs.SetOutput(w)
			fs.PrintDefaults()
		}
	}
}
