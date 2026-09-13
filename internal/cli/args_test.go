package cli

import "testing"

func TestSplitDashDash(t *testing.T) {
	tests := []struct {
		name      string
		argv      []string
		wantOwn   []string
		wantChild []string
		wantFound bool
	}{
		{
			name:      "no separator",
			argv:      []string{"fix-auth", "--tag", "v1"},
			wantOwn:   []string{"fix-auth", "--tag", "v1"},
			wantChild: nil,
			wantFound: false,
		},
		{
			name:      "separator with child argv",
			argv:      []string{"fix-auth", "--", "claude", "-p", "go"},
			wantOwn:   []string{"fix-auth"},
			wantChild: []string{"claude", "-p", "go"},
			wantFound: true,
		},
		{
			name:      "separator with empty child",
			argv:      []string{"fix-auth", "--"},
			wantOwn:   []string{"fix-auth"},
			wantChild: []string{},
			wantFound: true,
		},
		{
			name:      "leading separator",
			argv:      []string{"--", "npx", "server"},
			wantOwn:   []string{},
			wantChild: []string{"npx", "server"},
			wantFound: true,
		},
		{
			// Only the first separator is ours. A later one belongs to the
			// child, which may itself be wrapping something.
			name:      "second separator belongs to the child",
			argv:      []string{"t", "--", "npm", "run", "x", "--", "-v"},
			wantOwn:   []string{"t"},
			wantChild: []string{"npm", "run", "x", "--", "-v"},
			wantFound: true,
		},
		{
			// A child flag colliding with one of our own flag names must
			// survive intact. This is the case that motivates splitting
			// before flag parsing rather than after.
			name:      "child flag colliding with ours",
			argv:      []string{"--tag", "a", "--", "claude", "--tag", "b"},
			wantOwn:   []string{"--tag", "a"},
			wantChild: []string{"claude", "--tag", "b"},
			wantFound: true,
		},
		{
			name:      "empty argv",
			argv:      []string{},
			wantOwn:   []string{},
			wantChild: nil,
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			own, child, found := SplitDashDash(tt.argv)
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
			if !equal(own, tt.wantOwn) {
				t.Errorf("own = %q, want %q", own, tt.wantOwn)
			}
			if !equal(child, tt.wantChild) {
				t.Errorf("child = %q, want %q", child, tt.wantChild)
			}
		})
	}
}

// TestSplitDashDashDoesNotAliasChild guards the three-index slice in
// SplitDashDash. Without the capped capacity, appending to own would write
// into the caller's backing array and corrupt child.
func TestSplitDashDashDoesNotAliasChild(t *testing.T) {
	argv := []string{"a", "--", "child"}
	own, child, _ := SplitDashDash(argv)

	own = append(own, "injected")

	if child[0] != "child" {
		t.Fatalf("append to own corrupted child: got %q, want %q", child[0], "child")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
