//go:build unix

package tape

import (
	"fmt"
	"os"
	"syscall"
)

// mapFile maps the whole file read-only.
//
// Mapping rather than reading matters at suite scale. `cassette test` runs
// many replays in parallel, each opening its own tapes, and a shared
// read-only mapping means the kernel keeps one copy of the pages in the page
// cache no matter how many workers touch it. Fifty concurrent workers over a
// 320 MB corpus cost 320 MB of memory, not 16 GB.
func mapFile(f *os.File, size int) ([]byte, func() error, error) {
	if size == 0 {
		return nil, func() error { return nil }, nil
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, nil, fmt.Errorf("tape: mmap: %w", err)
	}
	return data, func() error { return syscall.Munmap(data) }, nil
}
