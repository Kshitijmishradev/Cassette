package api

import (
	"github.com/Kshitijmishradev/cassette/internal/safety"
	"os"
	"path/filepath"
	"testing"
)

func TestExportDoesNotRetainOldPayloads(t *testing.T) {
	out := t.TempDir()
	apiDir := filepath.Join(out, "api")
	if err := os.Mkdir(apiDir, 0700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(apiDir, "old-secret.json")
	if err := os.WriteFile(old, []byte(`{"secret":"example"}`), 0600); err != nil {
		t.Fatal(err)
	}
	b := &Builder{SuiteDir: t.TempDir(), Classifier: safety.New(safety.Config{})}
	if _, err := Dump(b, out); err == nil {
		t.Fatal("export succeeded over stale payloads")
	}
	if _, err := os.Stat(filepath.Join(apiDir, "suite.json")); !os.IsNotExist(err) {
		t.Fatal("partially exported after stale payload failure")
	}
}
