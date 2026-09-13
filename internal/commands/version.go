package commands

import (
	"flag"
	"fmt"
	"github.com/Kshitijmishradev/cassette/internal/buildinfo"
	"github.com/Kshitijmishradev/cassette/internal/cli"
)

func versionCmd() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "version [--short]",
		Short: "Print build version, commit, and toolchain",
		Long: `Version prints the identity of this binary.
This is not boilerplate. Every cassette records the proxy build that produced
it, and a replay run by a different build than the recording is a result you
cannot trust. Being able to print and compare that identity is what makes the
mismatch detectable.`,
		Flags: func(fs *flag.FlagSet) {
			fs.Bool("short", false, "print just the version identifier")
		},
		Run: func(ctx *cli.Context) error {
			info := buildinfo.Get()
			if ctx.Flags.Lookup("short").Value.String() == "true" {
				fmt.Fprintln(ctx.Out, info.Short())
				return nil
			}
			fmt.Fprintln(ctx.Out, info.String())
			return nil
		},
	}
}
