// Package env defines the environment-variable contract between the outer
// cassette command and the proxy shims it spawns indirectly.
//
// Why environment variables and not flags:
//
// The proxy is never launched by us. It is launched by the agent, out of the
// agent's own MCP configuration, which looks like this and is edited once and
// then left alone:
//
//	{"mcpServers": {"github": {"command": "cassette", "args": ["wrap", "--", "npx", "-y", "server-github"]}}}
//
// That config is static. We cannot rewrite a user's .mcp.json on every run to
// flip record into replay, and we should not want to. So the outer command
// (`cassette record`, `cassette replay`) sets these variables on the agent
// process, the agent inherits them to every MCP server it spawns, and each
// shim reads its mode from the environment it woke up in.
//
// The consequence worth stating: one static line in the agent's config
// supports recording, replay, and passthrough forever.
package env

import "fmt"

// Variable names. These are user-visible contract, so treat them as fixed.
const (
	// ModeVar selects proxy behavior: off, record, or replay.
	ModeVar = "CASSETTE_MODE"

	// TapeVar is the path to the cassette file to write or read.
	TapeVar = "CASSETTE_TAPE"

	// RunIDVar identifies one execution. Every shim under a single agent run
	// shares it, which is what lets calls from several different MCP servers
	// be stitched back into one ordered trajectory.
	RunIDVar = "CASSETTE_RUN_ID"

	// ConfigVar points at cassette.json. Resolved by the outer command and
	// passed down so the shims do not each re-walk the directory tree.
	ConfigVar = "CASSETTE_CONFIG"
)

// Mode is how a proxy shim behaves for the run it finds itself in.
type Mode string

const (
	// ModeOff forwards every frame untouched and records nothing. This is
	// the default when the variable is unset, which is deliberate: a shim
	// left in an agent's config must behave exactly like no shim at all
	// during ordinary use. Anything else makes the tool a liability.
	ModeOff Mode = "off"

	// ModeRecord forwards to the real server and tees both directions.
	ModeRecord Mode = "record"

	// ModeReplay serves matched calls from the tape. Whether an unmatched
	// call may fall through to the real server depends on the tool's safety
	// class, not on this mode.
	ModeReplay Mode = "replay"
)

// ParseMode converts a raw environment value into a Mode. An empty value is
// ModeOff, not an error: the common case is a shim sitting in a config during
// normal work, with nothing set.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case "", ModeOff:
		return ModeOff, nil
	case ModeRecord:
		return ModeRecord, nil
	case ModeReplay:
		return ModeReplay, nil
	default:
		return ModeOff, fmt.Errorf("invalid %s %q: want one of off, record, replay", ModeVar, s)
	}
}

// Active reports whether the mode does anything beyond pass frames through.
func (m Mode) Active() bool { return m == ModeRecord || m == ModeReplay }

func (m Mode) String() string { return string(m) }
