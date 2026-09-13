package commands

import (
	"flag"

	"github.com/Kshitijmishradev/cassette/internal/cli"
)

func exportCmd() *cli.Command {
	return &cli.Command{
		Name:  "export",
		Usage: "export --static <dir>",
		Short: "Precompute every view as static files for hosting",
		Long: `Export writes the UI plus precomputed JSON for every run and diff into
a directory that can be served as plain static files.

    cassette export --static ./public

This is how a public demo works without a backend: there is no server to run,
nothing to keep online, and no possibility of a hosted instance holding
someone's recorded production payloads.`,
		Flags: func(fs *flag.FlagSet) {
			fs.String("static", "", "output directory (required)")
			fs.String("suite", "./cassettes", "directory of cassettes to include")
		},
		Run: func(ctx *cli.Context) error {
			if ctx.Flags.Lookup("static").Value.String() == "" {
				return cli.Usagef("--static <dir> is required")
			}
			return pending(8, "export")
		},
	}
}
