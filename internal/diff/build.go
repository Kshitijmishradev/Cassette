package diff

import (
	"sort"

	"github.com/Kshitijmishradev/cassette/internal/jsonrpc"
	"github.com/Kshitijmishradev/cassette/internal/replay"
	"github.com/Kshitijmishradev/cassette/internal/safety"
	"github.com/Kshitijmishradev/cassette/internal/tape"
)

// argsPreviewWidth bounds the argument text kept for display. Comparison
// uses hashes, so this can never affect a verdict.
const argsPreviewWidth = 160

// FromTape builds the baseline trajectory out of a recording.
//
// Entries are ordered by when the request was sent, not by their position on
// the tape. That distinction is not cosmetic and it produced a wrong verdict
// before it was fixed: the recorder writes an entry when the *response*
// arrives, because that is the first moment the exchange is complete, so a
// fast call started second can land on the tape ahead of a slow call started
// first. The replay trajectory is in request order, so comparing it against
// tape order made an unchanged re-run look like a reordered one, complete
// with a spurious "outcome changed".
//
// StartedNanos exists for exactly this. Ties break on tape position so the
// ordering is deterministic, since it feeds a diff that CI will compare.
//
// Notifications and unprompted server messages are excluded. Neither
// represents a decision the agent made: one is an announcement, the other was
// not the agent's doing at all. Including them would add noise to every diff
// without ever changing a verdict.
func FromTape(name string, r *tape.Reader, c *safety.Classifier) Trajectory {
	t := Trajectory{Name: name}

	order := make([]int, 0, r.Len())
	for i := range r.Len() {
		order = append(order, i)
	}
	sort.SliceStable(order, func(a, b int) bool {
		ea, eb := r.Entry(order[a]), r.Entry(order[b])
		if ea.StartedNanos != eb.StartedNanos {
			return ea.StartedNanos < eb.StartedNanos
		}
		return ea.Seq < eb.Seq
	})

	for _, i := range order {
		e := r.Entry(i)
		if e.ServerInitiated() {
			continue
		}
		method := r.Method(i)
		tool := r.ToolName(i)

		req := r.Request(i)
		if len(req) == 0 {
			continue
		}
		// A recorded request without an id was a notification.
		env, err := jsonrpc.ParseEnvelope(trimNewline(req))
		if err != nil || !env.HasID() {
			continue
		}

		t.Calls = append(t.Calls, Call{
			Method:   method,
			Tool:     tool,
			ArgsHash: tape.HashNorm(method, []byte(argsOf(req, env))),
			Args:     truncateArgs(argsOf(req, env), argsPreviewWidth),
			Class:    c.Classify(method, tool),
		})
	}
	return t
}

// FromReport builds the candidate trajectory out of a replay report.
func FromReport(name string, rep replay.Report, c *safety.Classifier) Trajectory {
	t := Trajectory{Name: name, Calls: make([]Call, 0, len(rep.Calls))}
	for _, call := range rep.Calls {
		t.Calls = append(t.Calls, Call{
			Method:   call.Method,
			Tool:     call.Tool,
			ArgsHash: call.ArgsHash,
			Args:     truncateArgs(call.Args, argsPreviewWidth),
			Class:    c.Classify(call.Method, call.Tool),
			Tier:     call.Tier,
			Missed:   call.Missed,
		})
	}
	return t
}

// argsOf extracts the display arguments from a recorded request. For a tool
// call that is params.arguments, matching what the recorder hashed; for
// anything else it is params.
func argsOf(req []byte, env jsonrpc.Envelope) string {
	msg := trimNewline(req)
	if env.IsToolCall() {
		if tc, err := jsonrpc.ParseToolCall(msg); err == nil {
			return string(tc.Arguments)
		}
	}
	if params, err := jsonrpc.ParseParams(msg); err == nil {
		return string(params)
	}
	return ""
}

func trimNewline(b []byte) []byte {
	if n := len(b); n > 0 && b[n-1] == '\n' {
		return b[:n-1]
	}
	return b
}
