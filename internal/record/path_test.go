package record

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNamesCannotEscapeOrSelectOtherPaths(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../other", "/tmp/other", `..\other`, "C:other", "a\nname", "*", "[ab]"} {
		if ValidateName(name) == nil {
			t.Errorf("accepted %q", name)
		}
	}
	if err := ValidateName("refund-session.01"); err != nil {
		t.Fatal(err)
	}
}

func TestManifestRejectsEscapingAndDuplicateTapes(t *testing.T) {
	for _, files := range [][]string{{"../secret.cas"}, {"srv.cas", "srv.cas"}, {"/secret.cas"}} {
		dir := t.TempDir()
		m := Manifest{}
		for _, file := range files {
			m.Tapes = append(m.Tapes, Tape{File: file})
		}
		b, _ := json.Marshal(m)
		if err := os.WriteFile(filepath.Join(dir, ManifestName), b, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadManifest(dir); err == nil {
			t.Errorf("accepted %v", files)
		}
	}
}

func TestChildPathRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Skip(err)
	}
	if _, err := ChildPath(dir, "escape"); err == nil {
		t.Fatal("accepted symlink")
	}
}
