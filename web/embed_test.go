package webui

import (
	"io/fs"
	"os"
	"path/filepath"
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
