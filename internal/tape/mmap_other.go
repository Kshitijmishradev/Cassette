//go:build !unix

package tape

import (
	"fmt"
	"io"
	"os"
)

// mapFile reads the file into memory on platforms without mmap here.
//
// Correct but not shared: every reader gets its own copy, so parallel suite
// runs cost memory linearly in the number of workers. No platform in the
// release matrix takes this path; it exists so the package still builds and
// behaves everywhere.
func mapFile(f *os.File, size int) ([]byte, func() error, error) {
	data := make([]byte, size)
	if _, err := io.ReadFull(f, data); err != nil {
		return nil, nil, fmt.Errorf("tape: read: %w", err)
	}
	return data, func() error { return nil }, nil
}
