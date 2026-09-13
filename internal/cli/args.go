package cli

// SplitDashDash divides argv at the first bare "--".
//
// This matters more here than in a typical CLI. Cassette's commands wrap
// other processes, and those processes have their own flags:
//
//	cassette record fix-auth -- claude -p "fix the bug" --verbose
//
// The child's --verbose must reach claude, not be parsed by us, and a child
// argument that happens to collide with one of our flag names must not be
// silently swallowed. So the split happens before flag parsing, not after.
//
// The returned own slice is everything before the separator, child is
// everything after it, and found reports whether a separator was present at
// all. found distinguishes "no -- given" from "-- given with nothing after
// it", which are different mistakes and deserve different messages.
func SplitDashDash(argv []string) (own, child []string, found bool) {
	for i, a := range argv {
		if a == "--" {
			return argv[:i:i], argv[i+1:], true
		}
	}
	return argv, nil, false
}
