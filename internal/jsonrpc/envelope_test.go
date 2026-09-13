package jsonrpc

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestParseEnvelopeKinds(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		wantKind Kind
		wantMeth string
		wantID   string
		wantErr  bool
	}{
		{
			name:     "request",
			msg:      `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"read_file"}}`,
			wantKind: KindRequest,
			wantMeth: "tools/call",
			wantID:   "7",
		},
		{
			name:     "notification has no id",
			msg:      `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7}}`,
			wantKind: KindNotification,
			wantMeth: "notifications/cancelled",
		},
		{
			name:     "response",
			msg:      `{"jsonrpc":"2.0","id":7,"result":{"content":[]}}`,
			wantKind: KindResponse,
			wantID:   "7",
		},
		{
			name:     "error response",
			msg:      `{"jsonrpc":"2.0","id":7,"error":{"code":-32601,"message":"no"}}`,
			wantKind: KindResponse,
			wantID:   "7",
			wantErr:  true,
		},
		{
			// JSON-RPC permits string ids and plenty of clients use them.
			name:     "string id",
			msg:      `{"jsonrpc":"2.0","id":"req-abc","result":{}}`,
			wantKind: KindResponse,
			wantID:   `"req-abc"`,
		},
		{
			// A null id is what a server sends when it could not parse the
			// request well enough to learn one. It is not a usable id.
			name:     "null id error response",
			msg:      `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"parse error"}}`,
			wantKind: KindResponse,
			wantErr:  true,
		},
		{
			name:     "empty object",
			msg:      `{}`,
			wantKind: KindUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := ParseEnvelope([]byte(tt.msg))
			if err != nil {
				t.Fatalf("ParseEnvelope: %v", err)
			}
			if env.Kind != tt.wantKind {
				t.Errorf("Kind = %v, want %v", env.Kind, tt.wantKind)
			}
			if env.Method != tt.wantMeth {
				t.Errorf("Method = %q, want %q", env.Method, tt.wantMeth)
			}
			if string(env.ID) != tt.wantID {
				t.Errorf("ID = %q, want %q", env.ID, tt.wantID)
			}
			if env.IsError != tt.wantErr {
				t.Errorf("IsError = %v, want %v", env.IsError, tt.wantErr)
			}
		})
	}
}

// Ids must survive correlation byte for byte. A large integer id decoded into
// float64 and re-encoded would come back as 1.2345678901234568e+19, which
// would no longer match the request it belongs to.
func TestParseEnvelopePreservesIDBytesExactly(t *testing.T) {
	for _, id := range []string{
		"7",
		"0",
		"-1",
		"12345678901234567890",
		`"req-abc"`,
		`"id with spaces"`,
	} {
		msg := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{}}`, id)
		env, err := ParseEnvelope([]byte(msg))
		if err != nil {
			t.Fatalf("id %s: %v", id, err)
		}
		if string(env.ID) != id {
			t.Errorf("id %s round-tripped as %s", id, env.ID)
		}
	}
}

func TestHasID(t *testing.T) {
	cases := map[string]bool{
		`{"id":7,"result":{}}`:   true,
		`{"id":"x","result":{}}`: true,
		`{"id":null,"error":{}}`: false,
		`{"method":"notify"}`:    false,
	}
	for msg, want := range cases {
		env, err := ParseEnvelope([]byte(msg))
		if err != nil {
			t.Fatalf("%s: %v", msg, err)
		}
		if env.HasID() != want {
			t.Errorf("%s: HasID = %v, want %v", msg, env.HasID(), want)
		}
	}
}

func TestIsToolCall(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{`{"id":1,"method":"tools/call","params":{"name":"x"}}`, true},
		{`{"id":1,"method":"tools/list"}`, false},
		{`{"method":"tools/call","params":{"name":"x"}}`, false}, // notification, not a request
		{`{"id":1,"result":{}}`, false},
	}
	for _, c := range cases {
		env, err := ParseEnvelope([]byte(c.msg))
		if err != nil {
			t.Fatalf("%s: %v", c.msg, err)
		}
		if env.IsToolCall() != c.want {
			t.Errorf("%s: IsToolCall = %v, want %v", c.msg, env.IsToolCall(), c.want)
		}
	}
}

func TestParseToolCall(t *testing.T) {
	msg := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/etc/hosts","limit":100}}}`

	tc, err := ParseToolCall([]byte(msg))
	if err != nil {
		t.Fatal(err)
	}
	if tc.Name != "read_file" {
		t.Errorf("Name = %q", tc.Name)
	}
	// Arguments stay raw. Decoding and re-encoding would reorder keys and
	// change the hash used to match against a tape.
	want := `{"path":"/etc/hosts","limit":100}`
	if string(tc.Arguments) != want {
		t.Errorf("Arguments = %s, want %s", tc.Arguments, want)
	}
}

func TestParseToolCallArgumentByteOrderPreserved(t *testing.T) {
	// Same logical arguments, different key order. Both must come back
	// exactly as written, since the raw bytes are what get hashed.
	a := `{"id":1,"method":"tools/call","params":{"name":"q","arguments":{"b":2,"a":1}}}`
	b := `{"id":1,"method":"tools/call","params":{"name":"q","arguments":{"a":1,"b":2}}}`

	ta, err := ParseToolCall([]byte(a))
	if err != nil {
		t.Fatal(err)
	}
	tb, err := ParseToolCall([]byte(b))
	if err != nil {
		t.Fatal(err)
	}
	if string(ta.Arguments) != `{"b":2,"a":1}` {
		t.Errorf("a: %s", ta.Arguments)
	}
	if string(tb.Arguments) != `{"a":1,"b":2}` {
		t.Errorf("b: %s", tb.Arguments)
	}
	if bytes.Equal(ta.Arguments, tb.Arguments) {
		t.Error("key order was normalized away; matching would treat these as identical")
	}
}

func TestParseToolCallMissingName(t *testing.T) {
	if _, err := ParseToolCall([]byte(`{"id":1,"method":"tools/call","params":{}}`)); err == nil {
		t.Error("want error for missing params.name")
	}
}

func TestParseEnvelopeRejectsMalformed(t *testing.T) {
	for _, msg := range []string{`{`, `not json`, ``, `{"id":}`} {
		if _, err := ParseEnvelope([]byte(msg)); err == nil {
			t.Errorf("%q: want error", msg)
		}
	}
}

// The point of the two-stage design: a response carrying a large result must
// not allocate in proportion to that result. If this regresses, every
// response an agent receives starts costing a full copy.
func BenchmarkParseEnvelopeLargeResult(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 512 << 10} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			msg := []byte(`{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"` +
				strings.Repeat("x", size) + `"}]}}`)

			b.SetBytes(int64(len(msg)))
			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				if _, err := ParseEnvelope(msg); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkParseToolCall(b *testing.B) {
	msg := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/some/long/path/to/a/file.go","limit":100,"offset":0}}}`)
	b.SetBytes(int64(len(msg)))
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if _, err := ParseToolCall(msg); err != nil {
			b.Fatal(err)
		}
	}
}
