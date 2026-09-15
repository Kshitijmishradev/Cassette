package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kshitijmishradev/cassette/internal/cli"
)

func TestStaticExportWritesBundleAndOfflineAPI(t *testing.T) {
	suite := filepath.Join(t.TempDir(), "suite")
	out := filepath.Join(t.TempDir(), "site")
	if err := os.MkdirAll(suite, 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := App().Run([]string{"export", "--static", out, "--suite", suite}, &stdout, &stderr)
	if code != cli.ExitOK {
		t.Fatalf("exit = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}

	for _, path := range []string{"index.html", filepath.Join("api", "suite.json")} {
		if _, err := os.Stat(filepath.Join(out, path)); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
	suiteJSON, err := os.ReadFile(filepath.Join(out, "api", "suite.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(suiteJSON), `"live": false`) {
		t.Fatalf("static suite is not marked offline: %s", suiteJSON)
	}
}

func TestStaticExportRejectsUnsupportedPayloadOmission(t *testing.T) {
	out := filepath.Join(t.TempDir(), "site")
	var stdout, stderr bytes.Buffer
	code := App().Run([]string{"export", "--static", out, "--no-payloads"}, &stdout, &stderr)
	if code == cli.ExitOK {
		t.Fatal("silently ignored --no-payloads")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("export wrote files before rejecting flag")
	}
}

func TestRecordTraversalCannotDeleteOutsideSuite(t *testing.T) {
	root := t.TempDir()
	victim := filepath.Join(root, "keep")
	if err := os.MkdirAll(victim, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(victim, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := App().Run([]string{"record", "../keep", "--suite", filepath.Join(root, "suite"), "--force", "--", "echo", "unused"}, &stdout, &stderr)
	if code == cli.ExitOK {
		t.Fatal("accepted traversal recording name")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal("deleted outside suite")
	}
}
