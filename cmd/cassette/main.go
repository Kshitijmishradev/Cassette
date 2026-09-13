// Command cassette records and replays MCP tool calls so that agent runs
// become deterministic, testable, and free to re-run.
//
// See CASSETTE_PLAN.md for the premise and the architecture.
package main

import (
	"os"

	"github.com/Kshitijmishradev/cassette/internal/commands"
)

func main() {
	os.Exit(commands.App().Run(os.Args[1:], os.Stdout, os.Stderr))
}
