package commands

import (
	"bytes"
	"flag"
	"github.com/Kshitijmishradev/cassette/internal/cli"
	"github.com/Kshitijmishradev/cassette/internal/diff"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kshitijmishradev/cassette/internal/env"
	"github.com/Kshitijmishradev/cassette/internal/replay"
	"github.com/Kshitijmishradev/cassette/internal/tape"
)

func TestHermeticShimOverridesLiveConfig(t *testing.T) {
	dir := t.TempDir()
	w, err := tape.Create(filepath.Join(dir, "srv.cas"))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, []byte(`{"replay":{"fallThrough":true},"tools":{"read":["get_signal"]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	// A pre-existing unwritable .hermetic.json used to silently disable isolation.
	if err := os.Mkdir(filepath.Join(dir, ".hermetic.json"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(env.ModeVar, "replay")
	t.Setenv(env.TapeVar, dir)
	t.Setenv(env.ConfigVar, config)
	t.Setenv(env.HermeticVar, "1")
	input, err := os.CreateTemp(dir, "input")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	_, err = input.WriteString("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"get_signal\",\"arguments\":{}}}\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	output, err := os.CreateTemp(dir, "output")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = input, output
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	var out, errs bytes.Buffer
	sentinel := filepath.Join(dir, "server-started")
	code := App().Run([]string{"wrap", "--name", "srv", "--", "/bin/sh", "-c", `printf started > "$1"`, "--", sentinel}, &out, &errs)
	if code != 0 {
		t.Fatalf("wrap failed: %s", errs.String())
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("hermetic replay started the live server")
	}
	reports, err := replay.CollectReports(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].Refused != 1 || reports[0].FellThrough != 0 {
		t.Fatalf("hermetic boundary failed: %+v", reports)
	}
}

func TestNoDiffDoesNotDisableOutcomeChecking(t *testing.T) {
	dir := t.TempDir()
	w, err := tape.Create(filepath.Join(dir, "srv.cas"))
	if err != nil {
		t.Fatal(err)
	}
	err = w.Append(tape.Record{Method: "tools/call", ToolName: "refund", Request: []byte(`{"id":1,"method":"tools/call","params":{"name":"refund","arguments":{}}}`), Response: []byte(`{"id":1,"result":{}}`), Flags: tape.EntryIsToolCall})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	flags := flag.NewFlagSet("replay", flag.ContinueOnError)
	flags.Bool("no-diff", true, "")
	flags.Int("context", 1, "")
	var output bytes.Buffer
	verdict, err := renderDiffs(&cli.Context{Flags: flags, Err: &output, Out: &output}, dir, []replay.Report{{Tape: "srv"}})
	if err != nil {
		t.Fatal(err)
	}
	if verdict != diff.VerdictOutcomeChanged {
		t.Fatalf("suppressed outcome check: %v", verdict)
	}
	if output.Len() != 0 {
		t.Fatal("--no-diff still printed the comparison")
	}
}
