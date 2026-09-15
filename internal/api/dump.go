package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Dump writes the whole API as files, at the same paths the server answers.
//
// This is what makes the static demo possible and, just as usefully, what
// gives the frontend fixtures to develop against without running anything.
// The frontend fetches the same relative paths either way, so there is no
// build-time switch between "demo mode" and "real mode" to get wrong.
func Dump(b *Builder, outDir string) (int, error) {
	if entries, err := os.ReadDir(filepath.Join(outDir, "api")); err == nil && len(entries) != 0 {
		return 0, fmt.Errorf("API export destination is not empty; choose a fresh directory to avoid retaining old payloads")
	} else if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	s, err := b.BuildSuite()
	if err != nil {
		return 0, err
	}
	s.Live = false

	written := 0
	write := func(rel string, v any) error {
		path := filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(rel, "/")))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			return err
		}
		written++
		return nil
	}

	if err := write(PathSuite, s); err != nil {
		return written, err
	}

	for _, r := range s.Runs {
		run, err := b.BuildRun(r.Name)
		if err != nil {
			return written, fmt.Errorf("%s: %w", r.Name, err)
		}
		if err := write(RunDir+r.Name+"/run.json", run); err != nil {
			return written, err
		}

		for i := range run.Steps {
			call, err := b.BuildCall(r.Name, i)
			if err != nil {
				return written, err
			}
			if err := write(fmt.Sprintf("%s%s/calls/%d.json", RunDir, r.Name, i), call); err != nil {
				return written, err
			}
		}

		// A run that has never been replayed has no diff, and writing an
		// empty one would make the UI show a comparison that did not happen.
		d, err := b.BuildDiff(r.Name)
		if err != nil {
			continue
		}
		if err := write(RunDir+r.Name+"/diff.json", d); err != nil {
			return written, err
		}
	}

	return written, nil
}
