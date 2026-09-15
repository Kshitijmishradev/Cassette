// Package match finds the recording that answers a live request.
//
// This is the part of cassette that is genuinely hard, and the reason is
// simple: the agent does not ask the same questions twice. A prompt change
// makes it grep for "authentication token" where it once grepped for "auth
// token", or read a file with a different limit, or pass the same arguments
// with the keys in a different order. Exact byte matching would miss all of
// those, and every miss is either a fall-through to the real world or a
// failed replay.
//
// So matching is a ladder of increasingly forgiving tiers, and each rung
// reports itself. Knowing that a run was served 80% exact and 20% fuzzy is
// the difference between trusting a result and wondering about it, which is
// why Tier travels with every answer rather than being an internal detail.
package match

import (
	"bytes"
	"github.com/Kshitijmishradev/cassette/internal/jsonrpc"
	"github.com/Kshitijmishradev/cassette/internal/tape"
)

// Tier says how a request was matched. It is reported per call, because a
// replay served mostly by loose matches is a weaker result than one served
// exactly, and the user deserves to see which they got.
type Tier uint8

const (
	// TierMiss means nothing on the tape answers this request.
	TierMiss Tier = iota

	// TierExact: identical method and identical argument bytes.
	TierExact

	// TierNormalized: same arguments with keys sorted and whitespace
	// removed. Catches an agent that serialized the same call differently.
	TierNormalized

	// TierMethod: same method, arguments ignored.
	//
	// This exists because of a failure the verification caught. An agent's
	// initialize params carry its own name, version and capabilities, which
	// differ between any two clients and across versions of one client. So
	// hashing those means the handshake never matches and replay refuses the
	// very first message, leaving the agent unable to start at all.
	//
	// The justification is narrow and specific: for these methods the
	// response describes the server, not the request. What the server
	// supports does not depend on who asked. That is true of initialize and
	// ping; it is emphatically not true of tools/call, and it is not true of
	// tools/list either, which takes a pagination cursor. So this tier is
	// restricted to an explicit set rather than applied as a general
	// fallback.
	TierMethod

	// TierFuzzy: nearest neighbour within the same tool, above a similarity
	// floor. Reserved; the vector section of the format is not populated
	// yet, so nothing returns this today.
	TierFuzzy
)

func (t Tier) String() string {
	switch t {
	case TierExact:
		return "exact"
	case TierNormalized:
		return "norm"
	case TierMethod:
		return "method"
	case TierFuzzy:
		return "fuzzy"
	default:
		return "miss"
	}
}

// Result is what the tape had for a request.
type Result struct {
	Index int // entry index on the tape
	Tier  Tier
	Found bool

	// Repeat is true when this recording had already been used and is being
	// served again. The agent asked more times than the recording saw, which
	// is worth surfacing rather than hiding: it usually means the new run is
	// looping where the old one did not.
	Repeat bool
}

// Matcher answers requests from a tape.
//
// Both hash tiers are rebuilt from recorded requests at load. This validates
// current matching semantics even for tapes written by older versions.
type identity struct {
	method string
	tool   string
	hash   uint64
}

type Matcher struct {
	r *tape.Reader

	// A hash maps to a list, not a single entry, because an agent can make
	// the same call more than once and each occurrence may have returned
	// something different. Reading a file before and after editing it is the
	// obvious case, and collapsing those would serve the pre-edit contents
	// to a post-edit read.
	byKey  map[identity][]int32
	byNorm map[identity][]int32

	// byMethod backs the method-only tier. Kept separate rather than folded
	// into the hash maps so that tier stays deliberate and reportable: a run
	// served partly by method-only matches is a weaker result than one
	// served exactly, and the summary should be able to say so.
	byMethod map[string][]int32

	used []bool
}

// argsInsensitive lists methods whose response describes the server rather
// than the request, so the arguments can be ignored when matching.
//
// Deliberately tiny. Every entry here is a place where cassette decides two
// different requests are the same, which is exactly the kind of judgement
// that should have to be argued for one method at a time.
var argsInsensitive = map[string]bool{
	"initialize":      true,
	"server/discover": true,
	"ping":            true,
}

// New builds a matcher over a tape.
func New(r *tape.Reader) *Matcher {
	entries := r.Entries()
	m := &Matcher{
		r:        r,
		byKey:    make(map[identity][]int32, len(entries)),
		byNorm:   make(map[identity][]int32, len(entries)),
		byMethod: make(map[string][]int32, 4),
		used:     make([]bool, len(entries)),
	}

	for i := range entries {
		e := &entries[i]
		// Server-initiated messages were never requested, so nothing can
		// match them. They are emitted by position instead.
		if e.ServerInitiated() {
			continue
		}
		// Derive current keys from recorded arguments so old tapes benefit
		// from precision-preserving normalization without a format migration.
		args, err := m.arguments(i)
		if err != nil {
			continue
		}
		key := identity{r.Method(i), r.ToolName(i), tape.HashKey(r.Method(i), args)}
		m.byKey[key] = append(m.byKey[key], int32(i))
		if norm := tape.HashNorm(key.method, args); norm != key.hash {
			key.hash = norm
			m.byNorm[key] = append(m.byNorm[key], int32(i))
		}
		if method := r.Method(i); argsInsensitive[method] {
			m.byMethod[method] = append(m.byMethod[method], int32(i))
		}
	}
	return m
}

// Match walks the ladder for a live request.
//
// The tiers are tried in order and the first hit wins. Exact before
// normalized matters when a tape holds both a call and a differently
// serialized version of it: the closer match should win.
func (m *Matcher) Match(method, tool string, args []byte) Result {
	key := identity{method, tool, tape.HashKey(method, args)}
	if r, ok := m.pick(m.byKey[key], TierExact, args); ok {
		return r
	}

	norm := identity{method, tool, tape.HashNorm(method, args)}
	if r, ok := m.pick(m.byNorm[norm], TierNormalized, args); ok {
		return r
	}
	// A normalized hash can also land on an entry whose arguments were
	// already canonical, in which case it was filed under byKey only.
	if r, ok := m.pick(m.byKey[norm], TierNormalized, args); ok {
		return r
	}

	if argsInsensitive[method] {
		if r, ok := m.pick(m.byMethod[method], TierMethod, args); ok {
			return r
		}
	}

	// The fuzzy tier goes here once the vector section exists. The bucketing
	// is already in the format: entries carry a tool id, so the candidate set
	// is one tool's calls rather than the whole tape.
	return Result{Tier: TierMiss}
}

// pick returns the first unused candidate, or the last one if all are spent.
//
// Serving a used recording again is deliberate. An agent that loops one more
// time than the recording did should keep making progress rather than hit a
// wall, and the Repeat flag makes it visible in the report. Failing here
// would turn a small behavioral difference into a dead replay, which is
// exactly the brittleness this tool exists to remove.
func (m *Matcher) pick(candidates []int32, tier Tier, args []byte) (Result, bool) {
	last := -1
	for _, candidate := range candidates {
		i := int(candidate)
		if tier != TierMethod {
			recorded, err := m.arguments(i)
			if err != nil {
				continue
			}
			wanted := args
			if tier == TierNormalized {
				recorded, err = tape.Canonicalize(recorded)
				if err != nil {
					continue
				}
				wanted, err = tape.Canonicalize(args)
				if err != nil {
					continue
				}
			}
			if !bytes.Equal(recorded, wanted) {
				continue
			}
		}
		last = i
		if !m.used[i] {
			m.used[i] = true
			return Result{Index: i, Tier: tier, Found: true}, true
		}
	}
	if last >= 0 {
		return Result{Index: last, Tier: tier, Found: true, Repeat: true}, true
	}
	return Result{}, false
}

func (m *Matcher) arguments(i int) ([]byte, error) {
	request := bytes.TrimSpace(m.r.Request(i))
	if m.r.Method(i) == "tools/call" {
		call, err := jsonrpc.ParseToolCall(request)
		if err != nil {
			return nil, err
		}
		return call.Arguments, nil
	}
	return jsonrpc.ParseParams(request)
}

// Unused reports entries that were never served: calls the recording made
// that this run did not. That is half of a trajectory diff, available for
// free from the matcher's own bookkeeping.
func (m *Matcher) Unused() []int {
	var out []int
	for i, used := range m.used {
		if used {
			continue
		}
		if m.r.Entry(i).ServerInitiated() {
			continue
		}
		out = append(out, i)
	}
	return out
}
