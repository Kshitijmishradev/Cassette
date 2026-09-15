package tape

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestRejectWrappedIndexOffset(t *testing.T) {
	h := Header{Magic: Magic, Version: Version, Count: 1, IndexOff: math.MaxUint64 - EntrySize + 1, StringsOff: HeaderSize, VectorsOff: HeaderSize, BlobsOff: HeaderSize}
	data, _ := h.MarshalBinary()
	path := filepath.Join(t.TempDir(), "bad.cas")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if r, err := Open(path); err == nil {
		r.Close()
		t.Fatal("wrapped index offset accepted")
	}
}

func TestStringLengthOverflowCannotEscapeMapping(t *testing.T) {
	data := make([]byte, 32)
	binary.PutUvarint(data[1:], math.MaxUint64)
	r := Reader{data: data, stringsOff: 0, header: Header{VectorsOff: 32}}
	if got := r.str(1); got != "" {
		t.Fatal("overflowing string length accepted")
	}
}

func TestTapeFinalizationDoesNotFollowPredictableSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "keep")
	if err := os.WriteFile(victim, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "recording.cas")
	if err := os.Symlink(victim, path+".tmp"); err != nil {
		t.Skip(err)
	}
	w, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(victim)
	if err != nil || string(data) != "untouched" {
		t.Fatal("temporary-file symlink overwrote another file")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("tape readable by other users: %o", info.Mode().Perm())
	}
}

func FuzzReaderBounds(f *testing.F) {
	h := Header{Magic: Magic, Version: Version, IndexOff: HeaderSize, StringsOff: HeaderSize, VectorsOff: HeaderSize, BlobsOff: HeaderSize}
	seed, _ := h.MarshalBinary()
	f.Add(seed)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < HeaderSize || len(data) > 1<<20 {
			return
		}
		r := Reader{data: data}
		if err := r.load(); err != nil {
			return
		}
		for i := 0; i < r.Len(); i++ {
			_ = r.Method(i)
			_ = r.ToolName(i)
			_ = r.Request(i)
			_ = r.Response(i)
		}
	})
}
