package commands

import (
	"flag"

	"github.com/Kshitijmishradev/cassette/internal/cli"
)

func serveCmd() *cli.Command {
	return &cli.Command{
		Name:  "serve",
		Usage: "serve [--addr <host:port>]",
		Short: "Serve the local web UI for exploring runs and diffs",
		Long: `Serve starts a local web UI for browsing recorded runs, inspecting
trajectories, and reading diffs.

    cassette serve --addr localhost:7070

Four screens: the run list, a per-run waterfall showing which match tier
served each call, the trajectory diff, and the suite grid. There is
deliberately no metrics dashboard; that space is well served already and is
not what this tool is for.

The UI is compiled into the binary, so this needs no node runtime and no
network. It binds to localhost by design. Cassettes contain production
payloads and credentials, which is the same reason this tool is not a hosted
service.`,
		Flags: func(fs *flag.FlagSet) {
			fs.String("addr", "localhost:7070", "address to listen on")
			fs.Bool("open", false, "open a browser once listening")
		},
		Run: func(ctx *cli.Context) error {
			return pending(7, "serve")
		},
	}
}
