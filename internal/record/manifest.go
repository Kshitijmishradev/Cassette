package record

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Kshitijmishradev/cassette/internal/tape"
)

// ManifestName is the file describing one recorded run.
const ManifestName = "manifest.json"

// Manifest describes a recorded run and the tapes it produced.
//
// It is written by the outer record command after the agent exits, by
// scanning the directory, rather than by the shims as they go. Several shims
// run concurrently as separate processes, and having them all append to one
// file would mean either a lock on the recording path or a corrupt manifest.
// Scanning afterwards needs neither.
type Manifest struct {
	Name      string    `json:"name"`
	RunID     string    `json:"runId"`
	CreatedAt time.Time `json:"createdAt"`

	// Agent is the command that was recorded, kept so a replay can be
	// launched the same way without the user having to remember it.
	Agent []string `json:"agent"`

	// Cassette is the proxy build that produced this run. A replay served by
	// a different build is a result worth questioning, and this is what
	// makes that detectable.
	Cassette string `json:"cassette"`

	AgentExitCode int    `json:"agentExitCode"`
	Tapes         []Tape `json:"tapes"`
}

// Tape summarizes one server's recording.
type Tape struct {
	File       string `json:"file"`
	Entries    int    `json:"entries"`
	Bytes      int64  `json:"bytes"`
	ToolCalls  int    `json:"toolCalls"`
	Errors     int    `json:"errors"`
	Truncated  int    `json:"truncated"`
	FirstNanos int64  `json:"firstNanos"`
	LastNanos  int64  `json:"lastNanos"`
	Complete   bool   `json:"complete"`
}

// Duration is how long the run took, measured from the earliest message to
// the latest across every tape.
func (m Manifest) Duration() time.Duration {
	var first, last int64
	for _, t := range m.Tapes {
		if t.Entries == 0 {
			continue
		}
		if first == 0 || t.FirstNanos < first {
			first = t.FirstNanos
		}
		if t.LastNanos > last {
			last = t.LastNanos
		}
	}
	if first == 0 {
		return 0
	}
	return time.Duration(last - first)
}

// Totals sums entries, tool calls and errors across tapes.
func (m Manifest) Totals() (entries, toolCalls, errs, truncated int) {
	for _, t := range m.Tapes {
		entries += t.Entries
		toolCalls += t.ToolCalls
		errs += t.Errors
		truncated += t.Truncated
	}
	return
}

// ScanDir builds a manifest by inspecting every tape in dir.
func ScanDir(dir string) ([]Tape, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.cas"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	tapes := make([]Tape, 0, len(matches))
	for _, path := range matches {
		t, err := summarize(path)
		if err != nil {
			return nil, err
		}
		tapes = append(tapes, t)
	}
	return tapes, nil
}

func summarize(path string) (Tape, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Tape{}, err
	}

	r, err := tape.Open(path)
	if err != nil {
		return Tape{}, fmt.Errorf("reading %s: %w", filepath.Base(path), err)
	}
	defer r.Close()

	t := Tape{
		File:     filepath.Base(path),
		Entries:  r.Len(),
		Bytes:    st.Size(),
		Complete: r.Header().Complete(),
	}

	for _, e := range r.Entries() {
		if e.IsToolCall() {
			t.ToolCalls++
		}
		if e.IsError() {
			t.Errors++
		}
		if e.Truncated() {
			t.Truncated++
		}
		if t.FirstNanos == 0 || e.StartedNanos < t.FirstNanos {
			t.FirstNanos = e.StartedNanos
		}
		end := e.StartedNanos + e.DurationNs
		if end > t.LastNanos {
			t.LastNanos = end
		}
	}
	return t, nil
}

// WriteManifest writes the manifest into dir.
func WriteManifest(dir string, m Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(filepath.Join(dir, ManifestName), b, 0o644)
}

// ReadManifest loads the manifest from dir.
func ReadManifest(dir string) (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("parsing %s: %w", ManifestName, err)
	}
	return m, nil
}
