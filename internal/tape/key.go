package tape

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
)

// Hashes here go on disk and must mean the same thing in a process that
// writes a tape today and one that reads it next month. That rules out
// hash/maphash, which is seeded randomly per process, and it rules out
// anything whose output could change with a Go release. FNV-1a is stable,
// specified, and in the standard library, which matters given the
// zero-dependency constraint.
//
// Hashes are lookup accelerators, not proof of equality. The replay matcher
// also checks method, tool, and argument bytes before serving a response.

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
// Object keys and whitespace normalize together. Numeric spellings are kept
// exact: converting through float64 would collapse distinct large identifiers
// and amounts above 2^53 into the same call.
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
	if !json.Valid(args) {
		return nil, fmt.Errorf("invalid JSON arguments")
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.UseNumber()
	if err := decoder.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
