package tape

import "testing"

func TestHashKeyIsExact(t *testing.T) {
	a := HashKey("tools/call", []byte(`{"a":1,"b":2}`))
	b := HashKey("tools/call", []byte(`{"b":2,"a":1}`))
	if a == b {
		t.Error("exact hash collapsed two different byte sequences")
	}

	same := HashKey("tools/call", []byte(`{"a":1,"b":2}`))
	if a != same {
		t.Error("exact hash is not deterministic")
	}
}

// The separator keeps method and arguments from bleeding into each other.
// Without it, ("ab", "c") and ("a", "bc") would hash identically.
func TestHashKeySeparatesMethodFromArgs(t *testing.T) {
	if HashKey("ab", []byte("c")) == HashKey("a", []byte("bc")) {
		t.Error("method and arguments are not separated in the hash")
	}
}

// The whole point of the normalized tier: an agent that emits the same
// logical arguments with keys in a different order is making the same call.
func TestHashNormIgnoresKeyOrderAndWhitespace(t *testing.T) {
	forms := [][]byte{
		[]byte(`{"a":1,"b":2}`),
		[]byte(`{"b":2,"a":1}`),
		[]byte(`{ "a" : 1 , "b" : 2 }`),
		[]byte("{\n  \"b\": 2,\n  \"a\": 1\n}"),
	}
	want := HashNorm("tools/call", forms[0])
	for _, f := range forms[1:] {
		if got := HashNorm("tools/call", f); got != want {
			t.Errorf("normalized hash differs for %s", f)
		}
	}
}

func TestHashNormStillDistinguishesRealDifferences(t *testing.T) {
	a := HashNorm("tools/call", []byte(`{"path":"/etc/hosts"}`))
	b := HashNorm("tools/call", []byte(`{"path":"/etc/passwd"}`))
	if a == b {
		t.Error("normalization collapsed genuinely different arguments")
	}
}

// Nested objects must normalize too, since tool arguments are routinely
// nested a level or two deep.
func TestHashNormRecursesIntoNestedObjects(t *testing.T) {
	a := HashNorm("t", []byte(`{"outer":{"x":1,"y":2},"z":3}`))
	b := HashNorm("t", []byte(`{"z":3,"outer":{"y":2,"x":1}}`))
	if a != b {
		t.Error("nested object keys were not normalized")
	}
}

// Invalid JSON must stay matchable. Falling back to the exact hash means a
// malformed call can still find its recording, which is better than being
// permanently unmatchable.
func TestHashNormFallsBackForInvalidJSON(t *testing.T) {
	bad := []byte(`{not json`)
	if HashNorm("t", bad) != HashKey("t", bad) {
		t.Error("invalid JSON did not fall back to the exact hash")
	}
}

func TestCanonicalizeSortsKeys(t *testing.T) {
	got, err := Canonicalize([]byte(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"a":1,"b":2}` {
		t.Errorf("Canonicalize = %s", got)
	}
}

func TestCanonicalizeEmpty(t *testing.T) {
	got, err := Canonicalize(nil)
	if err != nil || got != nil {
		t.Errorf("Canonicalize(nil) = %q, %v", got, err)
	}
}

func BenchmarkHashKey(b *testing.B) {
	args := []byte(`{"path":"/some/long/path/to/a/file.go","limit":100,"offset":0}`)
	b.ReportAllocs()
	for range b.N {
		HashKey("tools/call", args)
	}
}

func BenchmarkHashNorm(b *testing.B) {
	args := []byte(`{"path":"/some/long/path/to/a/file.go","limit":100,"offset":0}`)
	b.ReportAllocs()
	for range b.N {
		HashNorm("tools/call", args)
	}
}

func TestNormalizationPreservesLargeIdentifiers(t *testing.T) {
	a := []byte(`{"id":9007199254740992}`)
	b := []byte(`{"id":9007199254740993}`)
	if HashNorm("tools/call", a) == HashNorm("tools/call", b) {
		t.Fatal("distinct large identifiers collapsed")
	}
}
