package suite

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kshitijmishradev/cassette/internal/record"
)

func writeCase(t *testing.T, root, name string, withManifest bool) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Every case directory gets a tape, so the manifest is the only thing
	// distinguishing a finished recording from an abandoned one.
	if err := os.WriteFile(filepath.Join(dir, "srv.cas"), []byte("not a real tape"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !withManifest {
		return
	}
	b, _ := json.Marshal(record.Manifest{Name: name, Agent: []string{"echo", "hi"}})
	if err := os.WriteFile(filepath.Join(dir, record.ManifestName), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A directory with tapes but no manifest is a recording that never finished.
// Running it would compare against a partial baseline while looking like a
// perfectly normal result, which is the worst way for a test suite to lie.
func TestDiscoverSkipsRecordingsWithoutAManifest(t *testing.T) {
	root := t.TempDir()
	writeCase(t, root, "finished", true)
	writeCase(t, root, "abandoned", false)
	writeCase(t, root, "also-finished", true)

	// Loose files at the suite root are not cases either.
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 {
		t.Fatalf("found %d cases, want 2: %+v", len(cases), cases)
	}
	for _, c := range cases {
		if c.Name == "abandoned" {
			t.Error("an unfinished recording was treated as a runnable case")
		}
	}
}

// Row order in a suite report must not depend on the filesystem.
func TestDiscoverIsSorted(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"zebra", "alpha", "middle"} {
		writeCase(t, root, n, true)
	}

	cases, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "middle", "zebra"}
	for i, w := range want {
		if cases[i].Name != w {
			t.Errorf("position %d = %q, want %q", i, cases[i].Name, w)
		}
	}
}

func TestDiscoverEmptyAndMissing(t *testing.T) {
	cases, err := Discover(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 0 {
		t.Errorf("empty directory produced %d cases", len(cases))
	}

	if _, err := Discover(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("want an error for a missing suite directory")
	}
}

// A corrupt manifest must fail loudly. Silently skipping it would quietly
// shrink the suite, and a test suite that runs fewer cases than you think is
// worse than one that fails.
func TestDiscoverFailsOnCorruptManifest(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, record.ManifestName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Discover(root); err == nil {
		t.Error("a corrupt manifest was skipped silently")
	}
}

// The runner has to report a case it could not run, not pretend it passed.
func TestRunCaseWithoutAnAgentCommandIsAnError(t *testing.T) {
	root := t.TempDir()
	writeCase(t, root, "no-agent", true)

	c := Case{Name: "no-agent", Dir: filepath.Join(root, "no-agent")}
	res := RunCase(t.Context(), c, Options{})

	if !res.Failed() {
		t.Error("a case with no agent command reported success")
	}
}

func TestHermeticEnvironmentHelper(t *testing.T) {
	if os.Getenv("CASSETTE_SECURITY_HELPER") != "1" {
		return
	}
	if err := os.WriteFile(os.Getenv("CASSETTE_SECURITY_SENTINEL"), []byte(os.Getenv("CASSETTE_HERMETIC")), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestHermeticModeDoesNotDependOnWritableConfig(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "observed")
	if err := os.Mkdir(filepath.Join(dir, ".hermetic.json"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CASSETTE_SECURITY_HELPER", "1")
	t.Setenv("CASSETTE_SECURITY_SENTINEL", sentinel)
	c := Case{Name: "example", Dir: dir}
	// No replay reports are expected from the helper. The security assertion
	// is the flag inherited by the actual child when config writes cannot work.
	RunCase(t.Context(), c, Options{Hermetic: true, Agent: []string{os.Args[0], "-test.run=^TestHermeticEnvironmentHelper$"}})
	got, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "1" {
		t.Fatalf("hermetic flag was not inherited: %q", got)
	}
}
