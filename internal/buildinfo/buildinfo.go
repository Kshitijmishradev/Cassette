// Package buildinfo exposes the identity of this binary.
//
// Version and Commit are set at link time by the release build. They matter
// more than usual here: a cassette records which build produced it, and a
// replay that silently used a different proxy version than the recording is
// a result you cannot trust. Stamping the binary is what makes that
// detectable later.
package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

var (
	// Version is the release version, injected via
	// -ldflags "-X .../buildinfo.Version=v0.1.0". It stays "dev" for local
	// builds, which is itself information worth recording on a cassette.
	Version = "dev"

	// Commit is the git SHA, injected the same way. When empty it falls back
	// to the VCS stamp the Go toolchain embeds automatically.
	Commit = ""
)

// Info is the full identity of this build.
type Info struct {
	Version string
	Commit  string
	Dirty   bool
	Go      string
	OS      string
	Arch    string
}

// Get assembles build identity, preferring link-time values and falling back
// to the toolchain's own VCS stamps for `go build` and `go run`.
func Get() Info {
	info := Info{
		Version: Version,
		Commit:  Commit,
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if info.Commit == "" {
				info.Commit = s.Value
			}
		case "vcs.modified":
			info.Dirty = s.Value == "true"
		}
	}
	return info
}

// Short is a one-line identifier suitable for stamping onto a cassette.
func (i Info) Short() string {
	commit := i.Commit
	if len(commit) > 12 {
		commit = commit[:12]
	}
	s := i.Version
	if commit != "" {
		s += "+" + commit
	}
	if i.Dirty {
		s += "-dirty"
	}
	return s
}

// String is the human-facing form printed by `cassette version`.
func (i Info) String() string {
	return fmt.Sprintf("cassette %s (%s %s/%s)", i.Short(), i.Go, i.OS, i.Arch)
}
