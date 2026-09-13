package replay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/Kshitijmishradev/cassette/internal/match"
)

// ReportSuffix is appended to a tape's base name to form its report file.
const ReportSuffix = ".replay.json"

// Report is what one shim tells the outer replay command about its run.
//
// Shims are separate processes, so results have to travel somehow. Each one
// writes a small JSON file next to its tape and the outer command collects
// them afterwards, which is the same trick the manifest uses: no IPC, no
// locking, no coordination on the path that has to stay fast.
type Report struct {
	Tape        string         `json:"tape"`
	Served      int            `json:"served"`
	ByTier      map[string]int `json:"byTier"`
	Repeats     int            `json:"repeats"`
	FellThrough int            `json:"fellThrough"`
	Refused     int            `json:"refused"`
	Notifies    int            `json:"notifications"`
	Unused      int            `json:"unused"`
	Misses      []Miss         `json:"misses,omitempty"`

	// Calls is the trajectory this run produced, which is what a diff
	// compares against the tape.
	Calls []Call `json:"calls,omitempty"`
}

// NewReport converts a result into its serializable form.
func NewReport(tapeName string, r Result) Report {
	byTier := make(map[string]int, len(r.ByTier))
	for tier, n := range r.ByTier {
		byTier[tier.String()] = n
	}
	return Report{
		Tape:        tapeName,
		Served:      r.Served,
		ByTier:      byTier,
		Repeats:     r.Repeats,
		FellThrough: r.FellThrough,
		Refused:     r.Refused,
		Notifies:    r.Notifies,
		Unused:      len(r.Unused),
		Misses:      r.Misses,
		Calls:       r.Calls,
	}
}

// Write saves the report beside its tape.
func (r Report) Write(dir string) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, r.Tape+ReportSuffix), append(b, '\n'), 0o644)
}

// CollectReports gathers every shim's report from a replay directory.
func CollectReports(dir string) ([]Report, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*"+ReportSuffix))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	reports := make([]Report, 0, len(matches))
	for _, p := range matches {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var r Report
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, err
		}
		reports = append(reports, r)
	}
	return reports, nil
}

// CleanReports removes stale reports before a run, so a shim that fails to
// start cannot leave the previous run's numbers behind to be read as this
// run's.
func CleanReports(dir string) error {
	matches, err := filepath.Glob(filepath.Join(dir, "*"+ReportSuffix))
	if err != nil {
		return err
	}
	for _, p := range matches {
		if err := os.Remove(p); err != nil {
			return err
		}
	}
	return nil
}

// TierOrder is the order tiers are displayed in, best match first.
var TierOrder = []match.Tier{match.TierExact, match.TierNormalized, match.TierFuzzy}
