package webui

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundleContainsIndexAndWritesStaticAssets(t *testing.T) {
	if _, err := fs.Stat(Assets(), "index.html"); err != nil {
		t.Fatalf("embedded index: %v", err)
	}

	dir := t.TempDir()
	n, err := WriteAssets(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("wrote %d assets, want index plus built assets", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		t.Fatalf("written index: %v", err)
	}
}

// Release binaries must never embed fixture payloads or the public story bundle.
func TestBundleContainsOnlyViewerAssets(t *testing.T) {
	err := fs.WalkDir(Assets(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if path != "index.html" && path != "_headers" && !(strings.HasPrefix(path, "assets/") && (strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css"))) {
			t.Errorf("unexpected embedded release file: %s", path)
		}
		data, err := fs.ReadFile(Assets(), path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "Change the agent. Keep the world still.") || strings.Contains(string(data), "story-page") {
			t.Errorf("public storytelling bundle embedded in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
