// Package webui exposes the production web bundle to the Go binary.
package webui

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

// dist is checked in so a clean Go checkout still builds one self-contained
// binary. Regenerate it with `npm run build --prefix web` after UI changes.
//
//go:embed dist
var bundle embed.FS

// Assets returns the production bundle rooted at its index.html.
func Assets() fs.FS {
	assets, err := fs.Sub(bundle, "dist")
	if err != nil {
		panic(err)
	}
	return assets
}

// WriteAssets copies the embedded bundle into dir for a static export.
func WriteAssets(dir string) (int, error) {
	assets := Assets()
	written := 0
	err := fs.WalkDir(assets, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		target := filepath.Join(dir, filepath.FromSlash(path))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(assets, path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
		written++
		return nil
	})
	return written, err
}
