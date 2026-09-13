package tape

import (
	"encoding/json"
	"hash/fnv"
)

// Hashes here go on disk and must mean the same thing in a process that
// writes a tape today and one that reads it next month. That rules out
// hash/maphash, which is seeded randomly per process, and it rules out
// anything whose output could change with a Go release. FNV-1a is stable,
// specified, and in the standard library, which matters given the
// zero-dependency constraint.
//
// Collision resistance is not a security property here. A collision means
// replay serves the wrong recorded response, which a trajectory diff would
// immediately show as divergent behavior. 64 bits of FNV over a few thousand
// distinct calls per run is far past sufficient.

// HashKey is the exact-match key: the method plus the arguments byte for
// byte as they arrived. Two calls hash the same only if the agent sent
// literally identical bytes.
func HashKey(method string, args []byte) uint64 {
	h := fnv.New64a()
	h.Write([]byte(method))
	h.Write([]byte{0}) // separator, so ("ab","c") and ("a","bc") differ
	h.Write(args)
	return h.Sum64()
}

// HashNorm is the normalized-match key: the method plus arguments with key
// order and whitespace removed.
//
// This is deliberately lossy, and the loss is the point. An agent that
// serializes the same logical arguments with keys in a different order on a
// second run is making the same call, and exact matching would miss it. What
// is given up is that JSON numbers round-trip through float64, so 1e10 and
// 10000000000 normalize together. For matching a recorded tool call that is
// correct behavior, not a bug.
//
// Falls back to the exact hash when the arguments are not valid JSON, which
// keeps a malformed call matchable rather than unmatchable.
func HashNorm(method string, args []byte) uint64 {
	canon, err := Canonicalize(args)
	if err != nil {
		return HashKey(method, args)
	}
	return HashKey(method, canon)
}

// Canonicalize returns arguments with object keys sorted and insignificant
// whitespace removed.
//
// It relies on encoding/json marshalling map keys in sorted order, which is
// a documented guarantee rather than an implementation detail.
func Canonicalize(args []byte) ([]byte, error) {
	if len(args) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(args, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
