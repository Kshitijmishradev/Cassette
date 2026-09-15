package match

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Kshitijmishradev/cassette/internal/tape"
)

type rec struct {
	method string
	tool   string
	args   string
	body   string
}

func build(t *testing.T, recs []rec) *tape.Reader {
	t.Helper()
	path := filepath.Join(t.TempDir(), "m.cas")

	w, err := tape.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range recs {
		params := json.RawMessage(r.args)
		if len(params) == 0 {
			params = json.RawMessage(`{}`)
		}
		if r.tool != "" {
			params, _ = json.Marshal(map[string]any{"name": r.tool, "arguments": params})
		}
		request, _ := json.Marshal(map[string]any{"id": 1, "method": r.method, "params": params})
		flags := uint32(0)
		if r.tool != "" {
			flags |= tape.EntryIsToolCall
		}
		if err := w.Append(tape.Record{
			Method:       r.method,
			ToolName:     r.tool,
			Request:      request,
			Response:     []byte(r.body),
			StartedNanos: int64(i + 1),
			KeyHash:      tape.HashKey(r.method, []byte(r.args)),
			NormHash:     tape.HashNorm(r.method, []byte(r.args)),
			Flags:        flags,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	rd, err := tape.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rd.Close() })
	return rd
}

func TestExactBeatsNormalized(t *testing.T) {
	rd := build(t, []rec{
		{method: "tools/call", tool: "read", args: `{"b":2,"a":1}`, body: `{"which":"reordered"}`},
		{method: "tools/call", tool: "read", args: `{"a":1,"b":2}`, body: `{"which":"canonical"}`},
	})
	m := New(rd)

	// The canonical form is present verbatim, so it must win on the exact
	// tier rather than being served the reordered recording.
	res := m.Match("tools/call", "read", []byte(`{"a":1,"b":2}`))
	if !res.Found || res.Tier != TierExact {
		t.Fatalf("got tier %v found %v, want exact", res.Tier, res.Found)
	}
	if res.Index != 1 {
		t.Errorf("matched entry %d, want 1 (the canonical recording)", res.Index)
	}
}

func TestNormalizedMatchesReorderedArguments(t *testing.T) {
	rd := build(t, []rec{
		{method: "tools/call", tool: "read", args: `{"path":"/x","limit":10}`, body: `{"ok":true}`},
	})
	m := New(rd)

	res := m.Match("tools/call", "read", []byte(`{ "limit" : 10 , "path" : "/x" }`))
	if !res.Found {
		t.Fatal("reordered arguments did not match")
	}
	if res.Tier != TierNormalized {
		t.Errorf("tier = %v, want normalized", res.Tier)
	}
}

func TestMiss(t *testing.T) {
	rd := build(t, []rec{
		{method: "tools/call", tool: "read", args: `{"path":"/x"}`, body: `{}`},
	})
	m := New(rd)

	if res := m.Match("tools/call", "read", []byte(`{"path":"/totally/different"}`)); res.Found {
		t.Errorf("unrelated arguments matched entry %d at tier %v", res.Index, res.Tier)
	}
}

// An agent's initialize params carry its own name, version and capabilities,
// which differ between clients and across versions of one client. Matching on
// those means the handshake never matches and replay refuses the very first
// message, leaving the agent unable to start. This was found by running a
// real replay, not by reading the code.
func TestInitializeMatchesDespiteDifferentClientInfo(t *testing.T) {
	recorded := `{"protocolVersion":"2024-11-05","clientInfo":{"name":"claude","version":"1.2"}}`
	rd := build(t, []rec{
		{method: "initialize", args: recorded, body: `{"serverInfo":{"name":"srv"}}`},
	})
	m := New(rd)

	live := `{"protocolVersion":"2024-11-05","clientInfo":{"name":"other-agent","version":"9.9"}}`
	res := m.Match("initialize", "", []byte(live))
	if !res.Found {
		t.Fatal("initialize did not match across different client info")
	}
	if res.Tier != TierMethod {
		t.Errorf("tier = %v, want method", res.Tier)
	}
}

// The method-only tier must stay narrow. tools/call responses depend
// entirely on their arguments, so ignoring arguments there would serve one
// tool's answer to a different tool's question.
func TestMethodTierDoesNotApplyToToolCalls(t *testing.T) {
	rd := build(t, []rec{
		{method: "tools/call", tool: "read", args: `{"path":"/a"}`, body: `{"a":1}`},
	})
	m := New(rd)

	if res := m.Match("tools/call", "read", []byte(`{"path":"/b"}`)); res.Found {
		t.Errorf("tools/call matched by method alone: entry %d tier %v", res.Index, res.Tier)
	}
}

// tools/list takes a pagination cursor, so its response does depend on its
// arguments and it must not be args-insensitive.
func TestToolsListIsNotArgsInsensitive(t *testing.T) {
	if argsInsensitive["tools/list"] {
		t.Error("tools/list is args-insensitive, but it takes a pagination cursor")
	}
}

// The same call made twice may have returned different things: reading a
// file before and after editing it is the obvious case. Collapsing them
// would serve the pre-edit contents to a post-edit read.
func TestRepeatedCallsAreServedInOrder(t *testing.T) {
	rd := build(t, []rec{
		{method: "tools/call", tool: "read", args: `{"path":"/f"}`, body: `{"gen":1}`},
		{method: "tools/call", tool: "read", args: `{"path":"/f"}`, body: `{"gen":2}`},
	})
	m := New(rd)

	first := m.Match("tools/call", "read", []byte(`{"path":"/f"}`))
	second := m.Match("tools/call", "read", []byte(`{"path":"/f"}`))

	if first.Index == second.Index {
		t.Fatalf("both calls served entry %d; the second recording was never used", first.Index)
	}
	if first.Repeat || second.Repeat {
		t.Error("distinct recordings were reported as repeats")
	}
}

// An agent that loops one more time than the recording did should keep
// making progress rather than hit a wall. The Repeat flag makes that
// visible instead of silent.
func TestExhaustedRecordingIsReusedAndFlagged(t *testing.T) {
	rd := build(t, []rec{
		{method: "tools/call", tool: "read", args: `{"path":"/f"}`, body: `{"gen":1}`},
	})
	m := New(rd)

	m.Match("tools/call", "read", []byte(`{"path":"/f"}`))
	again := m.Match("tools/call", "read", []byte(`{"path":"/f"}`))

	if !again.Found {
		t.Fatal("an extra call failed instead of reusing the recording")
	}
	if !again.Repeat {
		t.Error("reuse was not flagged as a repeat")
	}
}

// Calls the recording made that this run did not are half of a trajectory
// diff, and the matcher already knows them from its own bookkeeping.
func TestUnusedReportsRecordedCallsThisRunSkipped(t *testing.T) {
	rd := build(t, []rec{
		{method: "tools/call", tool: "read", args: `{"path":"/a"}`, body: `{}`},
		{method: "tools/call", tool: "read", args: `{"path":"/b"}`, body: `{}`},
		{method: "tools/call", tool: "read", args: `{"path":"/c"}`, body: `{}`},
	})
	m := New(rd)

	m.Match("tools/call", "read", []byte(`{"path":"/b"}`))

	unused := m.Unused()
	if len(unused) != 2 {
		t.Fatalf("Unused() = %v, want 2 entries", unused)
	}
	for _, i := range unused {
		if i == 1 {
			t.Error("the call this run did make is reported as unused")
		}
	}
}

func TestTierNames(t *testing.T) {
	want := map[Tier]string{
		TierMiss: "miss", TierExact: "exact",
		TierNormalized: "norm", TierMethod: "method", TierFuzzy: "fuzzy",
	}
	for tier, name := range want {
		if got := tier.String(); got != name {
			t.Errorf("Tier(%d).String() = %q, want %q", tier, got, name)
		}
	}
}

func TestToolIdentitySeparatesIdenticalArguments(t *testing.T) {
	rd := build(t, []rec{
		{method: "tools/call", tool: "read", args: `{"id":1}`, body: `{"safe":true}`},
		{method: "tools/call", tool: "delete", args: `{"id":1}`, body: `{"deleted":true}`},
	})
	m := New(rd)
	if got := m.Match("tools/call", "delete", []byte(`{"id":1}`)); !got.Found || got.Index != 1 {
		t.Fatalf("wrong tool matched: %+v", got)
	}
	if got := m.Match("tools/call", "refund", []byte(`{"id":1}`)); got.Found {
		t.Fatalf("unrecorded write matched: %+v", got)
	}
	if got := m.Match("tools/call", "Read", []byte(`{ "id" : 1 }`)); got.Found {
		t.Fatalf("case-distinct tool matched: %+v", got)
	}
	if got := m.Match("tools/call", "read", []byte(`{ "id" : 1 }`)); !got.Found || got.Index != 0 {
		t.Fatalf("normalized matching failed: %+v", got)
	}
}

func TestLargeIdentifierCannotMatchRoundedValue(t *testing.T) {
	rd := build(t, []rec{{method: "tools/call", tool: "read", args: `{"id":9007199254740993}`, body: `{}`}})
	m := New(rd)
	if res := m.Match("tools/call", "read", []byte(`{ "id": 9007199254740992 }`)); res.Found {
		t.Fatal("rounded identifier matched")
	}
}
