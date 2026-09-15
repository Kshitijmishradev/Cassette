// Package safety decides whether an unmatched call may reach the real world.
//
// This is the most consequential decision in the tool, and it is worth being
// blunt about why. During replay the tape answers everything it recognizes.
// When it does not recognize a call there are only two options: refuse, or
// let the call through to a live server.
//
// Letting it through is often right. The agent grepped for a slightly
// different string, or read a file the recording never touched; running that
// for real costs nothing and keeps the replay moving.
//
// Letting it through is sometimes catastrophic. The same fall-through
// applied to create_pull_request opens a real pull request. Applied to a
// refund tool it refunds a real customer. A replay that was supposed to be a
// free, side-effect-free experiment silently becomes a production action.
//
// So the classification is not a performance hint or a convenience. It is
// the boundary between a test and an accident, and it fails closed: anything
// not known to be read-only is treated as a write.
package safety

import "strings"

// Class is what a call is permitted to do.
type Class uint8

const (
	// ClassWrite is the default for anything unrecognized. A tool nobody
	// has classified might send email, and the cost of being wrong in that
	// direction is unbounded while the cost of being wrong the other way is
	// a replay that stops and asks a human.
	ClassWrite Class = iota

	// ClassRead is known to have no side effects. Unmatched read calls may
	// fall through to a live server and extend the tape.
	ClassRead
)

func (c Class) String() string {
	if c == ClassRead {
		return "read"
	}
	return "write"
}

// protocolMethods are MCP's own machinery rather than tools. They are
// read-only by definition: they describe what a server offers and never
// change anything. Classifying them here means a tape that is missing one
// can still fall through and complete the handshake.
var protocolMethods = map[string]bool{
	"initialize":               true,
	"server/discover":          true,
	"ping":                     true,
	"tools/list":               true,
	"resources/list":           true,
	"resources/read":           true,
	"resources/templates/list": true,
	"prompts/list":             true,
	"prompts/get":              true,
	"completion/complete":      true,
	"logging/setLevel":         true,
}

// readPrefixes and readWords are a heuristic, applied only when a tool has
// not been classified explicitly.
//
// A heuristic is uncomfortable here, and it is deliberately one-directional:
// a name is not proof of safety. Enable it only for trusted servers. Anything it does not recognize stays a write. The names below
// are conventional across MCP servers and none of them describes an action
// that changes state.
var readPrefixes = []string{
	"read_", "get_", "list_", "search_", "find_", "query_",
	"fetch_", "show_", "view_", "describe_", "inspect_", "grep",
}

var readWords = map[string]bool{
	"read": true, "get": true, "list": true, "search": true, "find": true,
	"query": true, "fetch": true, "grep": true, "glob": true, "ls": true,
	"cat": true, "head": true, "tail": true, "stat": true, "diff": true,
}

// Classifier decides the class of a call.
type Classifier struct {
	read  map[string]bool
	write map[string]bool

	// heuristic enables name-based inference for tools that appear in
	// neither list. Off makes the classifier strictly deny-by-default,
	// which is the right setting when the tools being replayed are
	// dangerous enough that a wrong guess is unacceptable.
	heuristic bool
}

// Config is the user-supplied part of classification.
type Config struct {
	// Read names tools that are safe to call for real on a replay miss.
	Read []string `json:"read,omitempty"`

	// Write names tools that must never be called on a miss. Listing a tool
	// here is redundant with the default, and that is the point: it lets
	// someone write down the dangerous ones explicitly so a future reader
	// can see the decision was made rather than defaulted into.
	Write []string `json:"write,omitempty"`

	// Heuristic allows name-based inference for unlisted tools. Defaults to
	// false; explicitly opt in only for trusted servers with reviewed names.
	Heuristic *bool `json:"heuristic,omitempty"`
}

// New builds a classifier.
func New(cfg Config) *Classifier {
	c := &Classifier{
		read:      make(map[string]bool, len(cfg.Read)),
		write:     make(map[string]bool, len(cfg.Write)),
		heuristic: cfg.Heuristic != nil && *cfg.Heuristic,
	}
	for _, t := range cfg.Read {
		c.read[t] = true
	}
	for _, t := range cfg.Write {
		c.write[strings.ToLower(t)] = true
	}
	return c
}

// Classify decides what a call is allowed to do.
//
// Order matters: an explicit write listing beats an explicit read listing,
// which beats the protocol table, which beats the heuristic. When two
// sources disagree, the more restrictive one wins.
func (c *Classifier) Classify(method, tool string) Class {
	if tool == "" {
		if protocolMethods[method] {
			return ClassRead
		}
		return ClassWrite
	}

	name := strings.ToLower(tool)
	switch {
	case c.write[name]:
		return ClassWrite
	case c.read[tool]:
		return ClassRead
	case !c.heuristic:
		return ClassWrite
	}

	if readWords[name] {
		return ClassRead
	}
	for _, p := range readPrefixes {
		if strings.HasPrefix(name, p) {
			return ClassRead
		}
	}
	return ClassWrite
}
