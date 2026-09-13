// Package tape implements CAS1, the on-disk format for a recorded run.
//
// # Why a custom format
//
// The replay hot path is a point lookup: given a request the agent just made,
// find the recorded response. That happens once per tool call, and the thing
// it competes with is a model turn of roughly 1.5 seconds. A lookup is
// therefore allowed to be slow in absolute terms and still be free in
// relative terms, which rules out building anything clever.
//
// What it must not do is allocate in proportion to payload size. Responses
// are routinely hundreds of kilobytes, and a format that decodes a record
// into Go values before handing it back would pay that cost on every call,
// on both the recording and the replay side.
//
// So CAS1 is laid out like a small SSTable: a fixed-width index that can be
// cast straight out of a memory mapping, and a blob section that is never
// parsed at all. Replay is a hash lookup followed by a single write of an
// mmap slice.
//
// # What is recorded
//
// Everything, not just tools/call. During replay there is no server process:
// the tape is the only thing answering. An agent opens a session by sending
// initialize, then notifications/initialized, then tools/list, and a tape
// that only held tool calls could not get the agent as far as its first one.
//
// # Layout
//
//	┌────────────────────────────────────────────────┐
//	│ header    64 B, fixed, carries section offsets  │
//	├────────────────────────────────────────────────┤
//	│ index     n × 72 B, fixed width                 │
//	├────────────────────────────────────────────────┤
//	│ strings   method and tool names, interned       │
//	├────────────────────────────────────────────────┤
//	│ vectors   n × dim × float32, optional           │
//	├────────────────────────────────────────────────┤
//	│ blobs     pre-framed message bytes              │
//	└────────────────────────────────────────────────┘
//
// Blobs are stored pre-framed, meaning the exact bytes that crossed the wire
// including the trailing newline. Replay then needs no serialization step at
// all: it patches the JSON-RPC id in place and writes the slice.
package tape

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Magic identifies the format and its byte order at once. A tape written on
// a little-endian machine and read on a big-endian one would decode into
// nonsense through the unsafe index cast, so the magic is checked byte for
// byte rather than as an integer. Every platform in the release matrix is
// little-endian; a mismatch is an error, not a slow path.
var Magic = [8]byte{'C', 'A', 'S', 'S', 'E', 'T', '0', '1'}

// Version is bumped on any incompatible layout change. A tape records the
// proxy build that produced it too (see buildinfo), but the version is what
// the reader actually gates on.
const Version uint32 = 1

// HeaderSize is fixed so the index always begins at a known, 8-byte aligned
// offset. Alignment is load-bearing: the reader casts the index region
// directly to a slice of Entry, which requires the region to be aligned for
// the struct's widest field.
const HeaderSize = 64

// EntrySize must match unsafe.Sizeof(Entry{}) exactly. A test asserts this.
// If the struct grows and this constant does not, every tape written after
// that point is silently unreadable.
const EntrySize = 72

// MaxTapeBytes bounds a tape at 4 GiB because offsets are uint32. A single
// agent run producing four gigabytes of tool traffic is a bug worth failing
// on, not a case worth widening the index for: eight more bytes per entry to
// support a situation that should never occur is a bad trade.
const MaxTapeBytes = 1 << 32

// Header is the fixed prefix of every tape.
type Header struct {
	Magic        [8]byte
	Version      uint32
	Flags        uint32
	Count        uint32 // number of index entries
	VectorDim    uint32 // 0 when no vector section is present
	IndexOff     uint64
	StringsOff   uint64
	VectorsOff   uint64
	BlobsOff     uint64
	CreatedNanos int64
}

// Header flags.
const (
	// FlagComplete marks a tape that was closed cleanly. Its absence means
	// the recorder died mid-run, which is worth telling the user about
	// rather than replaying a truncated session as if it were whole.
	FlagComplete uint32 = 1 << 0
)

// MarshalBinary encodes the header. Explicit little-endian rather than an
// unsafe cast, because this is written once per tape and clarity is worth
// more than the nanoseconds.
func (h Header) MarshalBinary() ([]byte, error) {
	b := make([]byte, HeaderSize)
	copy(b[0:8], h.Magic[:])
	binary.LittleEndian.PutUint32(b[8:], h.Version)
	binary.LittleEndian.PutUint32(b[12:], h.Flags)
	binary.LittleEndian.PutUint32(b[16:], h.Count)
	binary.LittleEndian.PutUint32(b[20:], h.VectorDim)
	binary.LittleEndian.PutUint64(b[24:], h.IndexOff)
	binary.LittleEndian.PutUint64(b[32:], h.StringsOff)
	binary.LittleEndian.PutUint64(b[40:], h.VectorsOff)
	binary.LittleEndian.PutUint64(b[48:], h.BlobsOff)
	binary.LittleEndian.PutUint64(b[56:], uint64(h.CreatedNanos))
	return b, nil
}

// ErrBadMagic means the file is not a cassette tape, or was written by a
// machine with the opposite byte order.
var ErrBadMagic = errors.New("tape: bad magic, not a CAS1 tape")

// UnmarshalHeader decodes and validates a header.
func UnmarshalHeader(b []byte) (Header, error) {
	var h Header
	if len(b) < HeaderSize {
		return h, fmt.Errorf("tape: short header: %d bytes", len(b))
	}
	copy(h.Magic[:], b[0:8])
	if h.Magic != Magic {
		return h, ErrBadMagic
	}
	h.Version = binary.LittleEndian.Uint32(b[8:])
	if h.Version != Version {
		return h, fmt.Errorf("tape: unsupported version %d (this build reads %d)", h.Version, Version)
	}
	h.Flags = binary.LittleEndian.Uint32(b[12:])
	h.Count = binary.LittleEndian.Uint32(b[16:])
	h.VectorDim = binary.LittleEndian.Uint32(b[20:])
	h.IndexOff = binary.LittleEndian.Uint64(b[24:])
	h.StringsOff = binary.LittleEndian.Uint64(b[32:])
	h.VectorsOff = binary.LittleEndian.Uint64(b[40:])
	h.BlobsOff = binary.LittleEndian.Uint64(b[48:])
	h.CreatedNanos = int64(binary.LittleEndian.Uint64(b[56:]))
	return h, nil
}

// Complete reports whether the tape was closed cleanly.
func (h Header) Complete() bool { return h.Flags&FlagComplete != 0 }

// Entry is one recorded exchange: a message the agent sent and, when one
// came back, the server's reply.
//
// The field order is chosen so every 8-byte field sits at an 8-byte offset.
// That is what makes the unsafe cast in the reader legal. Reordering these
// fields for readability would break it on platforms with strict alignment,
// so the layout is deliberately not alphabetical or logical.
//
// Sizes are chosen for the same reason offsets are uint32: this struct is
// multiplied by every call in every run, and 72 bytes keeps a 10,000-call
// index at 720 KB, small enough to stay resident.
type Entry struct {
	StartedNanos int64  // wall clock, used to merge tapes into one trajectory
	DurationNs   int64  // request to response, for the waterfall view
	KeyHash      uint64 // hash of method plus canonical arguments
	NormHash     uint64 // hash of method plus normalized arguments
	Seq          uint32 // order within this tape
	Flags        uint32
	MethodID     uint32 // offset into the string table
	ToolID       uint32 // offset into the string table, 0 when not a tool call
	VecIdx       uint32 // index into the vector section
	ReqOff       uint32 // offset into the blob section
	ReqLen       uint32
	RespOff      uint32
	RespLen      uint32
	_            uint32 // pad to 72 and keep the struct 8-byte aligned
}

// Entry flags.
const (
	// EntryHasResponse distinguishes a request that was answered from a
	// notification, which by definition never is. Without it, a
	// notification and a request whose response was lost look identical,
	// and replay would hang waiting for one of them.
	EntryHasResponse uint32 = 1 << 0

	// EntryIsError marks a response that carried a JSON-RPC error. Errors
	// are recorded and replayed like any other response: an agent's
	// behavior when a tool fails is exactly the behavior worth testing.
	EntryIsError uint32 = 1 << 1

	// EntryIsToolCall marks a tools/call request, the only kind that
	// participates in fuzzy matching.
	EntryIsToolCall uint32 = 1 << 2

	// EntryTruncated marks an exchange recorded without its response
	// because the run ended first.
	EntryTruncated uint32 = 1 << 3

	// EntryServerInitiated marks a message the server sent on its own,
	// such as notifications/progress or notifications/message. These have
	// a response blob but no request, because nothing asked for them.
	// Replay has to emit them without being prompted, which is why they
	// are distinguishable rather than merged into the ordinary flow.
	EntryServerInitiated uint32 = 1 << 4
)

func (e Entry) HasResponse() bool { return e.Flags&EntryHasResponse != 0 }
func (e Entry) IsError() bool     { return e.Flags&EntryIsError != 0 }
func (e Entry) IsToolCall() bool  { return e.Flags&EntryIsToolCall != 0 }
func (e Entry) Truncated() bool   { return e.Flags&EntryTruncated != 0 }

// ServerInitiated reports a message the server sent unprompted.
func (e Entry) ServerInitiated() bool { return e.Flags&EntryServerInitiated != 0 }
