package record

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/Kshitijmishradev/cassette/internal/tape"
)

// Step is one message in a run's merged trajectory.
type Step struct {
	Tape     string // which server's tape it came from
	Index    int    // position within that tape
	Entry    tape.Entry
	Method   string
	ToolName string
}

// Run is a recorded session opened for reading: every tape, plus the merged
// ordering across them.
//
// Ordering across tapes is the interesting part. Servers run as separate
// processes, so there is no shared sequence number to sort by, and
// introducing one would mean cross-process coordination on the recording
// path, which is exactly where it must not go.
//
// So ordering comes from wall-clock timestamps taken when each message was
// seen. Same machine, same clock, nanosecond resolution: good enough to
// reconstruct what happened. Ties break on tape name then local sequence,
// which is arbitrary but deterministic, so the same recording always
// produces the same trajectory. Deterministic matters more than perfect
// here, because this ordering feeds a diff.
type Run struct {
	Dir      string
	Manifest Manifest
	Steps    []Step

	readers map[string]*tape.Reader
}

// OpenRun opens every tape in a recorded run and merges them.
func OpenRun(dir string) (*Run, error) {
	m, err := ReadManifest(dir)
	if err != nil {
		return nil, err
	}

	r := &Run{Dir: dir, Manifest: m, readers: make(map[string]*tape.Reader, len(m.Tapes))}

	for _, t := range m.Tapes {
		rd, err := tape.Open(filepath.Join(dir, t.File))
		if err != nil {
			r.Close()
			return nil, err
		}
		r.readers[t.File] = rd

		for i := range rd.Entries() {
			r.Steps = append(r.Steps, Step{
				Tape:     t.File,
				Index:    i,
				Entry:    rd.Entry(i),
				Method:   rd.Method(i),
				ToolName: rd.ToolName(i),
			})
		}
	}

	sort.SliceStable(r.Steps, func(i, j int) bool {
		a, b := r.Steps[i], r.Steps[j]
		if a.Entry.StartedNanos != b.Entry.StartedNanos {
			return a.Entry.StartedNanos < b.Entry.StartedNanos
		}
		if a.Tape != b.Tape {
			return a.Tape < b.Tape
		}
		return a.Entry.Seq < b.Entry.Seq
	})

	return r, nil
}

// Request returns the pre-framed request bytes for a step, borrowed from the
// mapping and valid until Close.
func (r *Run) Request(s Step) []byte { return r.readers[s.Tape].Request(s.Index) }

// Response returns the pre-framed response bytes for a step, or nil.
func (r *Run) Response(s Step) []byte { return r.readers[s.Tape].Response(s.Index) }

// Close releases every mapping. All borrowed slices become invalid.
func (r *Run) Close() error {
	var first error
	for _, rd := range r.readers {
		if err := rd.Close(); err != nil && first == nil {
			first = err
		}
	}
	clear(r.readers)
	return first
}

// String describes a step compactly for a listing.
func (s Step) String() string {
	label := s.Method
	if s.ToolName != "" {
		label = fmt.Sprintf("%s(%s)", s.Method, s.ToolName)
	}
	return label
}
