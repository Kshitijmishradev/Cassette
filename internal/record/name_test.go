package record

import (
	"strings"
	"testing"
)

func TestTapeNamePrefersThePackageOverTrailingModeArguments(t *testing.T) {
	tests := []struct {
		name     string
		argv     []string
		wantSlug string
	}{
		{
			// The motivating case. Taking the last non-flag argument picks
			// "stdio", the transport selector, not the server.
			name:     "npx package with a trailing transport argument",
			argv:     []string{"npx", "-y", "@modelcontextprotocol/server-everything", "stdio"},
			wantSlug: "server-everything",
		},
		{
			name:     "npx package with no trailing argument",
			argv:     []string{"npx", "-y", "@modelcontextprotocol/server-github"},
			wantSlug: "server-github",
		},
		{
			name:     "node script path",
			argv:     []string{"node", "dist/mcp-server.js"},
			wantSlug: "mcp-server",
		},
		{
			name:     "docker subcommand is skipped",
			argv:     []string{"docker", "run", "-i", "--rm", "ghcr.io/acme/mcp-thing"},
			wantSlug: "mcp-thing",
		},
		{
			// A directory argument trails the server and must not win.
			name:     "package followed by a path argument",
			argv:     []string{"npx", "-y", "server-filesystem", "/home/me"},
			wantSlug: "server-filesystem",
		},
		{
			name:     "python module",
			argv:     []string{"python3", "-m", "my_mcp_server"},
			wantSlug: "my-mcp-server",
		},
		{
			// Nothing package-shaped, so the relaxed pass takes the bare
			// binary name rather than falling through to "server".
			name:     "plain binary",
			argv:     []string{"myserver"},
			wantSlug: "myserver",
		},
		{
			name:     "flags only",
			argv:     []string{"npx", "-y"},
			wantSlug: "server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TapeName(tt.argv)
			if !strings.HasPrefix(got, tt.wantSlug+"-") {
				t.Errorf("TapeName(%q) = %q, want slug %q", tt.argv, got, tt.wantSlug)
			}
		})
	}
}

// Two servers differing only in arguments must not collide on one file, or
// one run's recording would silently overwrite the other's.
func TestTapeNameDisambiguatesByFullCommand(t *testing.T) {
	a := TapeName([]string{"npx", "-y", "server-filesystem", "/home"})
	b := TapeName([]string{"npx", "-y", "server-filesystem", "/tmp"})
	if a == b {
		t.Errorf("both commands named %q; one would overwrite the other", a)
	}
	if !strings.HasPrefix(a, "server-filesystem-") || !strings.HasPrefix(b, "server-filesystem-") {
		t.Errorf("slugs differ: %q vs %q", a, b)
	}
}

// The name goes into a filename that gets committed to git, so it has to be
// deterministic across processes and machines.
func TestTapeNameIsStable(t *testing.T) {
	argv := []string{"npx", "-y", "@scope/server-x", "stdio"}
	first := TapeName(argv)
	for range 5 {
		if got := TapeName(argv); got != first {
			t.Fatalf("TapeName is not deterministic: %q then %q", first, got)
		}
	}
}

func TestSlugifyStripsPathScopeAndExtension(t *testing.T) {
	cases := map[string]string{
		"@modelcontextprotocol/server-github": "server-github",
		"dist/index.js":                       "index",
		"/usr/local/bin/my_server":            "my-server",
		"Server.Name.py":                      "server-name",
		"---":                                 "",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
