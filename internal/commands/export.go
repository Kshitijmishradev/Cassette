package commands

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/Kshitijmishradev/cassette/internal/api"
	"github.com/Kshitijmishradev/cassette/internal/chexport"
	"github.com/Kshitijmishradev/cassette/internal/cli"
	"github.com/Kshitijmishradev/cassette/internal/config"
	webui "github.com/Kshitijmishradev/cassette/web"
)

func exportCmd() *cli.Command {
	return &cli.Command{
		Name:  "export",
		Usage: "export --clickhouse <dir> | --static <dir> [--suite <dir>]",
		Short: "Export a suite for querying or for hosting",
		Long: `Export writes a recorded suite into a form something else can consume.

    cassette export --clickhouse ./analytics
    cd analytics && ./load.sh && clickhouse local --path ./db --queries-file queries.sql

--clickhouse writes JSONEachRow data plus the schema and the queries the
schema was designed around. Exporting rather than embedding ClickHouse keeps
this binary pure Go, statically linked and cross-compilable, and the export
works against clickhouse-local, a self-hosted cluster, or ClickHouse Cloud
without changing anything.

The parts that carry the actual thinking are in schema.sql: the ordering key
chosen to match how the data is interrogated, dictionary encoding on the
repeated name columns, payloads kept out of the scanned table, and a rollup
that stores quantile states rather than finished numbers so any time range
can be answered by merging them.

--static writes a self-contained web UI with precomputed data. The result can
be opened through any static file server or deployed directly to Cloudflare
Pages; it has no backend and performs no network writes.`,
		Flags: func(fs *flag.FlagSet) {
			fs.String("clickhouse", "", "write ClickHouse-loadable data to this directory")
			fs.String("static", "", "write a self-contained static site to this directory")
			fs.String("fixtures", "", "write only the API JSON, for frontend development")
			fs.String("suite", "./cassettes", "directory of recorded runs")
			fs.Bool("no-payloads", false, "omit request and response bodies")
		},
		Run: runExport,
	}
}

func runExport(ctx *cli.Context) error {
	ch := ctx.Flags.Lookup("clickhouse").Value.String()
	static := ctx.Flags.Lookup("static").Value.String()
	fixtures := ctx.Flags.Lookup("fixtures").Value.String()

	chosen := 0
	for _, v := range []string{ch, static, fixtures} {
		if v != "" {
			chosen++
		}
	}
	switch {
	case chosen == 0:
		return cli.Usagef("choose a destination: --clickhouse, --static or --fixtures")
	case chosen > 1:
		return cli.Usagef("these are separate exports; run one at a time")
	}

	if ch == "" && ctx.Flags.Lookup("no-payloads").Value.String() == "true" {
		return cli.Usagef("--no-payloads is only supported with --clickhouse; static and fixture exports contain full payloads")
	}
	cfg, _, err := config.Load(".")
	if err != nil {
		return err
	}

	if fixtures != "" {
		n, err := api.Dump(&api.Builder{
			SuiteDir:   ctx.Flags.Lookup("suite").Value.String(),
			Classifier: cfg.Classifier(),
		}, fixtures)
		if err != nil {
			return err
		}
		fmt.Fprintf(ctx.Out, "wrote %d JSON files to %s\n", n, filepath.Clean(fixtures))
		return nil
	}

	if static != "" {
		assets, err := webui.WriteAssets(static)
		if err != nil {
			return err
		}
		jsonFiles, err := api.Dump(&api.Builder{
			SuiteDir:   ctx.Flags.Lookup("suite").Value.String(),
			Classifier: cfg.Classifier(),
		}, static)
		if err != nil {
			return err
		}
		fmt.Fprintf(ctx.Out, "exported static site to %s: %d assets, %d JSON files\n",
			filepath.Clean(static), assets, jsonFiles)
		return nil
	}

	st, err := chexport.Export(chexport.Options{
		SuiteDir:        ctx.Flags.Lookup("suite").Value.String(),
		OutDir:          ch,
		Classifier:      cfg.Classifier(),
		IncludePayloads: ctx.Flags.Lookup("no-payloads").Value.String() != "true",
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(ctx.Out, "exported %d cassettes: %d spans, %d payloads, %s\n",
		st.Cassettes, st.Spans, st.Payloads, humanBytes(st.Bytes))
	fmt.Fprintf(ctx.Out, "\nnext:\n  cd %s && ./load.sh\n  clickhouse local --path ./db --queries-file queries.sql\n",
		filepath.Clean(ch))
	return nil
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
