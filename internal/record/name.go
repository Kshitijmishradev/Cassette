package record

import (
	"fmt"
	"hash/fnv"
	"path"
	"strings"
)

// runners launch a server rather than being one. For "npx -y @scope/srv" the
// interesting name is the package, not npx.
var runners = map[string]bool{
	"npx": true, "node": true, "bun": true, "deno": true,
	"python": true, "python3": true, "uv": true, "uvx": true, "pipx": true,
	"sh": true, "bash": true, "env": true, "docker": true, "podman": true,
	"cargo": true, "go": true,
}

// generic words carry no identity: subcommands of a runner, and transport
// selectors that servers commonly take as a trailing argument.
var generic = map[string]bool{
	"run": true, "exec": true, "start": true, "serve": true, "server": true,
	"stdio": true, "sse": true, "http": true, "streamablehttp": true,
	"-m": true, "main": true, "index": true,
}

// TapeName derives a stable filename for one MCP server's tape.
//
// A name is needed because several servers run under a single agent session,
// each in its own shim process writing its own tape. The outer record
// command cannot supply it, since it does not know which servers the agent
// will start.
//
// The result is a readable slug plus a short hash of the whole command. The
// slug is for whoever reads the directory listing; the hash is what makes it
// correct, keeping two servers that differ only in arguments off the same
// file. --name overrides it.
func TapeName(argv []string) string {
	h := fnv.New32a()
	for _, a := range argv {
		h.Write([]byte(a))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%s-%06x", pickSlug(argv), h.Sum32()&0xffffff)
}

// pickSlug chooses the argument that identifies the server.
//
// It scans forward, which is the whole trick. The obvious rule, take the last
// non-flag argument, is wrong in a common case: "npx -y
// @modelcontextprotocol/server-everything stdio" ends in a transport
// selector, and a reverse scan names the tape "stdio". Arguments *to* a
// server trail it, so the server itself is the first thing that is not a
// launcher, a flag, or a generic word.
//
//	npx -y @scope/server-github          -> server-github
//	node dist/mcp-server.js              -> mcp-server
//	python3 -m my_mcp_server             -> my-mcp-server
//	docker run -i ghcr.io/x/mcp-thing    -> mcp-thing
//	npx -y server-filesystem /home/me    -> server-filesystem
func pickSlug(argv []string) string {
	for _, a := range argv {
		base := strings.ToLower(path.Base(a))
		switch {
		case a == "" || strings.HasPrefix(a, "-"):
			continue
		case runners[base], generic[base], generic[strings.ToLower(a)]:
			continue
		}
		if s := slugify(a); s != "" && !generic[s] {
			return s
		}
	}
	return "server"
}

// slugify reduces an argument to a readable identifier: base name only, no
// directory, scope, or file extension.
func slugify(arg string) string {
	s := path.Base(arg)
	if i := strings.LastIndex(s, "."); i > 0 {
		s = s[:i] // drop .js, .py and friends
	}

	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		case r == '-' || r == '_' || r == '.':
			b.WriteByte('-')
		}
	}

	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-")
	}
	return out
}
