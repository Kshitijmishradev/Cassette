package safety

import "testing"

// The property that matters most: anything unrecognized is a write. Being
// wrong in that direction stops a replay; being wrong the other way opens a
// real pull request or refunds a real customer.
func TestUnknownToolsAreWrites(t *testing.T) {
	c := New(Config{})
	for _, tool := range []string{
		"create_pull_request", "send_email", "refund_charge",
		"deploy", "rm", "publish", "somethingNobodyHasSeen",
	} {
		if got := c.Classify("tools/call", tool); got != ClassWrite {
			t.Errorf("Classify(%q) = %v, want write", tool, got)
		}
	}
}

func TestExplicitListsWin(t *testing.T) {
	c := New(Config{Read: []string{"custom_lookup"}, Write: []string{"read_and_delete"}})

	if got := c.Classify("tools/call", "custom_lookup"); got != ClassRead {
		t.Errorf("explicit read listing ignored: %v", got)
	}
	// The name looks safe to the heuristic, and the explicit listing has to
	// override it. This is the case where a heuristic would do real damage.
	if got := c.Classify("tools/call", "read_and_delete"); got != ClassWrite {
		t.Errorf("explicit write listing lost to the heuristic: %v", got)
	}
}

func TestWriteListingBeatsReadListing(t *testing.T) {
	c := New(Config{Read: []string{"ambiguous"}, Write: []string{"ambiguous"}})
	if got := c.Classify("tools/call", "ambiguous"); got != ClassWrite {
		t.Errorf("conflicting listings resolved to %v, want the restrictive one", got)
	}
}

func TestHeuristicRecognizesConventionalReadNames(t *testing.T) {
	on := true
	c := New(Config{Heuristic: &on})
	for _, tool := range []string{
		"read_file", "get_issue", "list_files", "search_code",
		"grep", "glob", "fetch_url", "describe_table", "ls",
	} {
		if got := c.Classify("tools/call", tool); got != ClassRead {
			t.Errorf("Classify(%q) = %v, want read", tool, got)
		}
	}
}

func TestHeuristicCanBeDisabled(t *testing.T) {
	off := false
	c := New(Config{Heuristic: &off})
	if got := c.Classify("tools/call", "read_file"); got != ClassWrite {
		t.Errorf("heuristic still applied when disabled: %v", got)
	}
	// Explicit listings must still work with the heuristic off, or there
	// would be no way to allow anything.
	c2 := New(Config{Read: []string{"read_file"}, Heuristic: &off})
	if got := c2.Classify("tools/call", "read_file"); got != ClassRead {
		t.Errorf("explicit listing ignored with heuristic off: %v", got)
	}
}

// Protocol methods describe what a server offers and never change anything,
// so a tape missing one can still fall through and finish the handshake.
func TestProtocolMethodsAreReads(t *testing.T) {
	c := New(Config{})
	for _, m := range []string{"initialize", "tools/list", "ping", "resources/read"} {
		if got := c.Classify(m, ""); got != ClassRead {
			t.Errorf("Classify(%q) = %v, want read", m, got)
		}
	}
}

func TestUnknownProtocolMethodIsWrite(t *testing.T) {
	c := New(Config{})
	if got := c.Classify("some/futureMethod", ""); got != ClassWrite {
		t.Errorf("unknown method = %v, want write", got)
	}
}

func TestReadAllowlistIsCaseSensitive(t *testing.T) {
	c := New(Config{Read: []string{"MyTool"}})
	if c.Classify("tools/call", "MyTool") != ClassRead {
		t.Fatal("explicit read not recognized")
	}
	if c.Classify("tools/call", "mytool") != ClassWrite {
		t.Fatal("different tool inherited read permission")
	}
}

func TestHeuristicIsOffByDefault(t *testing.T) {
	c := New(Config{})
	for _, tool := range []string{"get_and_delete", "read_file", "query", "fetch_url"} {
		if c.Classify("tools/call", tool) != ClassWrite {
			t.Fatalf("unreviewed tool %q treated as safe", tool)
		}
	}
}
