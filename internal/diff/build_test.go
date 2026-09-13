package diff

import (
	"path/filepath"
	"testing"

	"github.com/Kshitijmishradev/cassette/internal/safety"
	"github.com/Kshitijmishradev/cassette/internal/tape"
)

// The regression that produced a wrong verdict on an unchanged re-run.
//
// The recorder writes an entry when the response arrives, because that is
// the first moment the exchange is complete. So a fast call started second
// lands on the tape ahead of a slow call started first. The replay
// trajectory is in request order, so reading the tape in tape order made an
// identical re-run look reordered, with a spurious "outcome changed".
func TestFromTapeOrdersByRequestTimeNotTapePosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "order.cas")
	w, err := tape.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	// Written in completion order: "fast" was sent second but finished
	// first, so it is entry 0 on the tape.
	entries := []struct {
		tool    string
		started int64
	}{
		{"fast", 2000},
		{"slow", 1000},
		{"last", 3000},
	}
	for _, e := range entries {
		if err := w.Append(tape.Record{
			Method:       "tools/call",
			ToolName:     e.tool,
			Request:      []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + e.tool + `","arguments":{}}}`),
			Response:     []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`),
			StartedNanos: e.started,
			Flags:        tape.EntryIsToolCall,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := tape.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	got := FromTape("recorded", r, safety.New(safety.Config{}))

	want := []string{"slow", "fast", "last"}
	if len(got.Calls) != len(want) {
		t.Fatalf("got %d calls, want %d", len(got.Calls), len(want))
	}
	for i, w := range want {
		if got.Calls[i].Tool != w {
			t.Errorf("position %d = %q, want %q (tape order would give %q)",
				i, got.Calls[i].Tool, w, entries[i].tool)
		}
	}
}

// Notifications and unprompted server messages are not decisions the agent
// made, so they must not appear in a trajectory or every diff would carry
// noise that can never change a verdict.
func TestFromTapeExcludesNotificationsAndServerMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mixed.cas")
	w, err := tape.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	recs := []tape.Record{
		{
			Method:       "tools/call",
			ToolName:     "grep",
			Request:      []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"grep","arguments":{}}}`),
			Response:     []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`),
			StartedNanos: 1000,
			Flags:        tape.EntryIsToolCall,
		},
		{
			Method:       "notifications/initialized",
			Request:      []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`),
			StartedNanos: 2000,
		},
		{
			Method:       "notifications/tools/list_changed",
			Response:     []byte(`{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`),
			StartedNanos: 3000,
			Flags:        tape.EntryServerInitiated,
		},
	}
	for _, rec := range recs {
		if err := w.Append(rec); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := tape.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	got := FromTape("recorded", r, safety.New(safety.Config{}))
	if len(got.Calls) != 1 {
		t.Fatalf("got %d calls, want 1 (only the tool call is a decision): %+v", len(got.Calls), got.Calls)
	}
	if got.Calls[0].Tool != "grep" {
		t.Errorf("kept %q", got.Calls[0].Tool)
	}
}

// A trajectory is a detached value that outlives the reader it came from.
// Holding borrowed strings here segfaulted once already.
func TestFromTapeSurvivesReaderClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live.cas")
	w, err := tape.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(tape.Record{
		Method:       "tools/call",
		ToolName:     "read_file",
		Request:      []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/x"}}}`),
		Response:     []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`),
		StartedNanos: 1,
		Flags:        tape.EntryIsToolCall,
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := tape.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := FromTape("recorded", r, safety.New(safety.Config{}))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// Every field must still be readable after the mapping is gone.
	if got.Calls[0].Label() != "tools/call(read_file)" {
		t.Errorf("after close, label = %q", got.Calls[0].Label())
	}
	if got.Calls[0].Args == "" {
		t.Error("arguments lost after close")
	}
}
