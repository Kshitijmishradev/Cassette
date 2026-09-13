// Package commands defines cassette's command surface.
//
// Each command lives in its own file and is filled in by the phase that owns
// it (see CASSETTE_PLAN.md). Until then it is registered as a stub that says
// which phase implements it. Registering the full surface up front is
// deliberate: the shape of the CLI is a design decision, and it is easier to
// argue with when you can run --help against it.
package commands

import (
	"fmt"

	"github.com/Kshitijmishradev/cassette/internal/cli"
)

// App builds the fully registered command set.
func App() *cli.App {
	a := cli.New("cassette", "record and replay MCP tool calls to make agent runs testable")

	a.Register(wrapCmd())
	a.Register(recordCmd())
	a.Register(replayCmd())
	a.Register(inspectCmd())
	a.Register(testCmd())
	a.Register(serveCmd())
	a.Register(exportCmd())
	a.Register(versionCmd())

	return a
}

// pending reports that a command exists but its phase has not landed. It is
// an error, not a silent no-op, so nothing downstream can mistake an
// unimplemented step for a successful one.
func pending(phase int, name string) error {
	return fmt.Errorf("%s is not implemented yet (phase %d, see CASSETTE_PLAN.md)", name, phase)
}
