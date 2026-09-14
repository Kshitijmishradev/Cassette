// Package api defines the wire types shared by the local server and the
// static export.
//
// There is one contract, not two. `cassette serve` answers these over HTTP
// and `cassette export --static` writes the identical JSON to files at the
// identical paths, so the frontend cannot tell which it is talking to and
// needs no build-time switch.
//
// That constraint shapes the URLs: every endpoint ends in .json and takes no
// query parameters, because a static host serves files, not routes. An
// endpoint that needed ?filter= would work live and silently break on the
// demo, which is the deployment a stranger actually sees.
package api

// Paths. Both the server and the static export use these verbatim.
const (
	PathSuite = "/api/suite.json"

	// PathRun and friends take a cassette name:
	//   /api/runs/<name>/run.json
	//   /api/runs/<name>/diff.json
	//   /api/runs/<name>/calls/<index>.json
	RunDir = "/api/runs/"
)

// Suite is the index: every recorded run and how its last replay went.
// Backs the runs list and the suite grid.
type Suite struct {
	// Generated is when this data was produced, so a static demo can say how
	// old it is rather than pretending to be live.
	Generated string `json:"generated"`

	// Live is true when served by `cassette serve`, false for a static
	// export. The UI uses it to hide controls that cannot work without a
	// backend.
	Live bool `json:"live"`

	Cassette string       `json:"cassette"` // the cassette build that produced this
	Totals   SuiteTotals  `json:"totals"`
	Runs     []RunSummary `json:"runs"`
}

// SuiteTotals is the headline row.
type SuiteTotals struct {
	Runs      int `json:"runs"`
	Identical int `json:"identical"`
	Drift     int `json:"drift"`
	Changed   int `json:"changed"`
	Unknown   int `json:"unknown"`

	Messages  int `json:"messages"`
	ToolCalls int `json:"toolCalls"`
	Errors    int `json:"errors"`
}

// RunSummary is one row in the runs list.
type RunSummary struct {
	Name       string `json:"name"`
	RecordedAt string `json:"recordedAt"`
	Agent      string `json:"agent"`

	// Verdict is identical | drift | changed | unknown. "unknown" means the
	// cassette has never been replayed, which is deliberately distinct from
	// passing.
	Verdict string `json:"verdict"`

	Tapes     int `json:"tapes"`
	Messages  int `json:"messages"`
	ToolCalls int `json:"toolCalls"`
	Errors    int `json:"errors"`

	DurationMs float64 `json:"durationMs"`
	Bytes      int64   `json:"bytes"`

	// Replay is nil when the cassette has never been replayed.
	Replay *ReplayStats `json:"replay,omitempty"`
}

// ReplayStats summarizes how a replay was served.
type ReplayStats struct {
	Served      int `json:"served"`
	Exact       int `json:"exact"`
	Normalized  int `json:"normalized"`
	ByMethod    int `json:"byMethod"`
	Fuzzy       int `json:"fuzzy"`
	FellThrough int `json:"fellThrough"`
	Refused     int `json:"refused"`
	Repeats     int `json:"repeats"`
	Unused      int `json:"unused"`
}

// Run is one recorded session in full: the merged trajectory across every
// server involved. Backs the waterfall screen.
type Run struct {
	Name       string   `json:"name"`
	RecordedAt string   `json:"recordedAt"`
	Agent      []string `json:"agent"`
	Cassette   string   `json:"cassette"`
	Verdict    string   `json:"verdict"`

	Tapes []TapeInfo `json:"tapes"`

	// Steps are ordered by when the request was sent, merged across tapes.
	// Not by tape position: the recorder writes an entry when the response
	// arrives, so a fast call started second lands ahead of a slow one
	// started first.
	Steps []Step `json:"steps"`
}

// TapeInfo is one server's recording.
type TapeInfo struct {
	File     string `json:"file"`
	Server   string `json:"server"`
	Entries  int    `json:"entries"`
	Bytes    int64  `json:"bytes"`
	Complete bool   `json:"complete"`
}

// Step is one message in a run's trajectory.
type Step struct {
	Index int    `json:"index"`
	Tape  string `json:"tape"`
	Seq   int    `json:"seq"`

	Method string `json:"method"`
	Tool   string `json:"tool,omitempty"`

	// OffsetMs is milliseconds from the first message of the run, which is
	// what a waterfall lays out against.
	OffsetMs   float64 `json:"offsetMs"`
	DurationMs float64 `json:"durationMs"`

	Class string `json:"class"` // read | write

	// Tier is how replay matched this call: exact | norm | method | fuzzy |
	// miss. Empty on a run that has only been recorded.
	Tier string `json:"tier,omitempty"`

	HasResponse     bool `json:"hasResponse"`
	IsError         bool `json:"isError"`
	Truncated       bool `json:"truncated"`
	ServerInitiated bool `json:"serverInitiated"`

	// Args is a truncated preview. Full bodies come from the calls endpoint,
	// so a run with a thousand large payloads still loads as one small
	// document.
	Args string `json:"args,omitempty"`

	ReqBytes  int `json:"reqBytes"`
	RespBytes int `json:"respBytes"`
}

// CallBodies is the full request and response for one step, fetched on
// demand. Kept out of Run so the trajectory stays small enough to load at
// once.
type CallBodies struct {
	Index    int    `json:"index"`
	Method   string `json:"method"`
	Tool     string `json:"tool,omitempty"`
	Request  string `json:"request,omitempty"`
	Response string `json:"response,omitempty"`
}

// Diff is a trajectory comparison. Backs the hero screen.
type Diff struct {
	Name string `json:"name"`

	// Verdict is identical | drift | changed.
	Verdict string `json:"verdict"`

	BaselineName  string `json:"baselineName"`
	CandidateName string `json:"candidateName"`

	BaselineSteps  int `json:"baselineSteps"`
	CandidateSteps int `json:"candidateSteps"`
	StepDelta      int `json:"stepDelta"`

	Matched     int `json:"matched"`
	Substituted int `json:"substituted"`
	Inserted    int `json:"inserted"`
	Deleted     int `json:"deleted"`
	Missed      int `json:"missed"`

	Pairs []DiffPair `json:"pairs"`
}

// DiffPair is one aligned position.
//
// Baseline is null on an insertion, Candidate is null on a deletion. The
// frontend renders those as an empty cell on that side.
type DiffPair struct {
	// Op is match | substitute | insert | delete.
	Op string `json:"op"`

	// Write is true when either side of this pair is a write-class call.
	// Write differences are what decide the verdict, so they are marked
	// differently from read differences.
	Write bool `json:"write"`

	Baseline  *DiffCall `json:"baseline"`
	Candidate *DiffCall `json:"candidate"`
}

// DiffCall is one call as it appears in a diff.
type DiffCall struct {
	Method string `json:"method"`
	Tool   string `json:"tool,omitempty"`
	Args   string `json:"args,omitempty"`
	Class  string `json:"class"`
	Tier   string `json:"tier,omitempty"`
	Missed bool   `json:"missed,omitempty"`
}

// Error is returned with a non-2xx status.
type Error struct {
	Error string `json:"error"`
}
