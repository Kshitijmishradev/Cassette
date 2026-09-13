package jsonrpc

import (
	"strings"
	"testing"
)

func TestReplaceIDRewritesOnlyTheID(t *testing.T) {
	tests := []struct {
		name  string
		msg   string
		newID string
		want  string
	}{
		{
			name:  "numeric id",
			msg:   `{"jsonrpc":"2.0","id":7,"result":{"ok":true}}`,
			newID: "42",
			want:  `{"jsonrpc":"2.0","id":42,"result":{"ok":true}}`,
		},
		{
			name:  "string id",
			msg:   `{"jsonrpc":"2.0","id":"req-7","result":{}}`,
			newID: `"live-42"`,
			want:  `{"jsonrpc":"2.0","id":"live-42","result":{}}`,
		},
		{
			name:  "id last",
			msg:   `{"jsonrpc":"2.0","result":{"a":1},"id":7}`,
			newID: "42",
			want:  `{"jsonrpc":"2.0","result":{"a":1},"id":42}`,
		},
		{
			// Whitespace and key order are preserved, because a tape holds
			// exactly what crossed the wire and replay must not tidy it.
			name:  "non-canonical spacing survives",
			msg:   `{ "jsonrpc" : "2.0" , "id" : 7 , "result" : { } }`,
			newID: "42",
			want:  `{ "jsonrpc" : "2.0" , "id" : 42 , "result" : { } }`,
		},
		{
			name:  "number to string",
			msg:   `{"id":7,"result":{}}`,
			newID: `"abc"`,
			want:  `{"id":"abc","result":{}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ReplaceID([]byte(tt.msg), []byte(tt.newID))
			if !ok {
				t.Fatal("ReplaceID reported no id")
			}
			if string(got) != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

// The critical correctness case. Tool payloads routinely contain their own
// "id" fields, and rewriting one would corrupt the data the agent receives.
func TestReplaceIDIgnoresNestedIDs(t *testing.T) {
	msgs := []string{
		`{"jsonrpc":"2.0","id":7,"result":{"id":"inner","items":[{"id":1},{"id":2}]}}`,
		`{"jsonrpc":"2.0","result":{"id":"inner"},"id":7}`,
		`{"jsonrpc":"2.0","params":{"arguments":{"id":"do-not-touch"}},"id":7}`,
	}
	for _, msg := range msgs {
		got, ok := ReplaceID([]byte(msg), []byte("42"))
		if !ok {
			t.Fatalf("no id found in %s", msg)
		}
		if strings.Contains(string(got), `"id":42,"result":{"id":42`) {
			t.Errorf("nested id was rewritten: %s", got)
		}
		if !strings.Contains(string(got), `"id":42`) {
			t.Errorf("top-level id not rewritten: %s", got)
		}
		// Exactly one id changed: the rest of the message is byte-identical
		// apart from the single span.
		if want := strings.Replace(msg, `"id":7`, `"id":42`, 1); string(got) != want {
			t.Errorf("more than the top-level id changed\n got %s\nwant %s", got, want)
		}
	}
}

// A structural character inside a string must not be read as structure, or
// the scanner would lose track of depth and rewrite the wrong field.
func TestReplaceIDHandlesBracesInsideStrings(t *testing.T) {
	msg := `{"jsonrpc":"2.0","result":{"text":"a } b { \" c \\ ","id":"nested"},"id":7}`
	got, ok := ReplaceID([]byte(msg), []byte("42"))
	if !ok {
		t.Fatal("no id found")
	}
	want := strings.Replace(msg, `"id":7`, `"id":42`, 1)
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// Notifications have no id, which is a normal shape and not an error.
func TestReplaceIDOnMessageWithoutID(t *testing.T) {
	msg := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	got, ok := ReplaceID([]byte(msg), []byte("42"))
	if ok {
		t.Error("reported an id where there is none")
	}
	if string(got) != msg {
		t.Errorf("message was modified: %s", got)
	}
}

func TestReplaceIDOnMalformedInput(t *testing.T) {
	for _, msg := range []string{``, `not json`, `{`, `{"id"`, `[1,2,3]`, `{"id":}`} {
		if _, ok := ReplaceID([]byte(msg), []byte("42")); ok {
			t.Errorf("%q: reported success on malformed input", msg)
		}
	}
}

// Round-tripping through the parser proves the splice produced valid JSON
// that still parses to the id we asked for.
func TestReplaceIDProducesParseableOutput(t *testing.T) {
	msg := []byte(`{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"x"}]}}`)
	got, _ := ReplaceID(msg, []byte(`"live-1"`))

	env, err := ParseEnvelope(got)
	if err != nil {
		t.Fatalf("spliced message does not parse: %v\n%s", err, got)
	}
	if string(env.ID) != `"live-1"` {
		t.Errorf("id after splice = %s", env.ID)
	}
}

func BenchmarkReplaceID(b *testing.B) {
	msg := []byte(`{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"` +
		strings.Repeat("x", 8<<10) + `"}]}}`)
	newID := []byte("424242")

	b.SetBytes(int64(len(msg)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, ok := ReplaceID(msg, newID); !ok {
			b.Fatal("no id")
		}
	}
}
