// Package suite runs recorded cassettes, one or many, and aggregates the
// results.
//
// The parallelism here is the payoff of a decision made in the very first
// phase: replay touches nothing external. No network, no shared database, no
// rate-limited API, no file the cases contend over. Cases are therefore
// embarrassingly parallel in the strict sense, and the only real ceiling is
// whatever the agent under test talks to.
package suite

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Kshitijmishradev/cassette/internal/record"
)

// Case is one recorded run in a suite.
type Case struct {
	Name string
	Dir  string
	record.Manifest
}

// Discover finds every recorded run under dir.
//
// A directory counts as a case only if it holds a manifest. That is a
// deliberate filter rather than globbing for tapes: a directory with tapes
// and no manifest is a recording that never finished, and running it would
// compare against a partial baseline while looking like a normal result.
func Discover(dir string) ([]Case, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading suite %s: %w", dir, err)
	}

	var cases []Case
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		caseDir := filepath.Join(dir, e.Name())
		m, err := record.ReadManifest(caseDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		cases = append(cases, Case{Name: e.Name(), Dir: caseDir, Manifest: m})
	}

	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases, nil
}
