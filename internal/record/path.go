package record

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateName accepts a single portable path component, never a path.
func ValidateName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\:*?[]") || strings.HasPrefix(name, ".") {
		return fmt.Errorf("invalid cassette name %q: use a single non-hidden name", name)
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return fmt.Errorf("invalid control character in cassette name")
		}
	}
	return nil
}

// ChildPath rejects path traversal and links outside a recording directory.
// The suite must not be concurrently modified by untrusted local processes.
func ChildPath(dir, name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return path, nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing symlink %q", name)
	}
	return path, nil
}
