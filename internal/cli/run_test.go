package cli

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

func testApp(capture *[]string, flagVal *string, boolVal *bool) *App {
	a := New("cassette", "test app")
	a.Register(&Command{
		Name:  "record",
		Usage: "record <name> -- <cmd>",
		Short: "record something",
		Flags: func(fs *flag.FlagSet) {
			fs.String("suite", "./default", "suite dir")
			fs.Bool("force", false, "force")
		},
		Run: func(ctx *Context) error {
			*capture = ctx.Args
			*flagVal = ctx.Flags.Lookup("suite").Value.String()
			*boolVal = ctx.Flags.Lookup("force").Value.String() == "true"
			return nil
		},
	})
	return a
}

// Flags must be honored wherever they appear. Go's flag package stops at the
// first positional, which would silently leave --suite at its default while
// the command otherwise looked like it worked.
func TestFlagsParseAroundPositionals(t *testing.T) {
	tests := []struct {
		name      string
		argv      []string
		wantArgs  []string
		wantSuite string
		wantForce bool
	}{
		{
			name:      "flags before positional",
			argv:      []string{"record", "--suite", "/tmp/s", "demo"},
			wantArgs:  []string{"demo"},
			wantSuite: "/tmp/s",
		},
		{
			name:      "flags after positional",
			argv:      []string{"record", "demo", "--suite", "/tmp/s"},
			wantArgs:  []string{"demo"},
			wantSuite: "/tmp/s",
		},
		{
			name:      "flags on both sides",
			argv:      []string{"record", "--force", "demo", "--suite", "/tmp/s"},
			wantArgs:  []string{"demo"},
			wantSuite: "/tmp/s",
			wantForce: true,
		},
		{
			name:      "no flags at all",
			argv:      []string{"record", "demo"},
			wantArgs:  []string{"demo"},
			wantSuite: "./default",
		},
		{
			name:      "several positionals with a flag between them",
			argv:      []string{"record", "a", "--suite", "/tmp/s", "b"},
			wantArgs:  []string{"a", "b"},
			wantSuite: "/tmp/s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var args []string
			var suite string
			var force bool

			code := testApp(&args, &suite, &force).Run(tt.argv, &bytes.Buffer{}, &bytes.Buffer{})
			if code != ExitOK {
				t.Fatalf("exit = %d, want 0", code)
			}
			if strings.Join(args, ",") != strings.Join(tt.wantArgs, ",") {
				t.Errorf("Args = %q, want %q", args, tt.wantArgs)
			}
			if suite != tt.wantSuite {
				t.Errorf("suite = %q, want %q", suite, tt.wantSuite)
			}
			if force != tt.wantForce {
				t.Errorf("force = %v, want %v", force, tt.wantForce)
			}
		})
	}
}

// Everything after -- belongs to the child, including things that look like
// our flags, and must not be parsed by us at any point.
func TestChildArgvSurvivesFlagParsing(t *testing.T) {
	var args []string
	var suite string
	var force bool

	a := New("cassette", "test app")
	var child []string
	a.Register(&Command{
		Name:  "record",
		Usage: "record <name> -- <cmd>",
		Short: "record",
		Flags: func(fs *flag.FlagSet) {
			fs.String("suite", "./default", "suite dir")
			fs.Bool("force", false, "force")
		},
		Run: func(ctx *Context) error {
			args, child = ctx.Args, ctx.Child
			suite = ctx.Flags.Lookup("suite").Value.String()
			force = ctx.Flags.Lookup("force").Value.String() == "true"
			return nil
		},
	})

	code := a.Run([]string{"record", "demo", "--suite", "/tmp/s", "--",
		"claude", "--force", "--suite", "theirs"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if suite != "/tmp/s" {
		t.Errorf("our --suite = %q", suite)
	}
	if force {
		t.Error("the child's --force was consumed by us")
	}
	if strings.Join(child, " ") != "claude --force --suite theirs" {
		t.Errorf("child argv = %q", child)
	}
	if strings.Join(args, ",") != "demo" {
		t.Errorf("Args = %q", args)
	}
}

func TestUnknownCommandExitsWithToolError(t *testing.T) {
	var a, b string
	var c bool
	var out, errb bytes.Buffer
	code := testApp(&[]string{}, &a, &c).Run([]string{"nope"}, &out, &errb)
	_ = b
	if code != ExitError {
		t.Errorf("exit = %d, want %d", code, ExitError)
	}
	if !strings.Contains(errb.String(), "unknown command") {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestExitCodeErrorPassesStatusThrough(t *testing.T) {
	a := New("cassette", "t")
	a.Register(&Command{
		Name: "wrap", Usage: "wrap", Short: "w",
		Run: func(*Context) error { return &ExitCodeError{Code: 7} },
	})
	if code := a.Run([]string{"wrap"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 7 {
		t.Errorf("exit = %d, want 7", code)
	}
}

func TestFailureErrorMapsToExitFailure(t *testing.T) {
	a := New("cassette", "t")
	a.Register(&Command{
		Name: "test", Usage: "test", Short: "t",
		Run: func(*Context) error { return Failuref("behavior changed") },
	})
	if code := a.Run([]string{"test"}, &bytes.Buffer{}, &bytes.Buffer{}); code != ExitFailure {
		t.Errorf("exit = %d, want %d", code, ExitFailure)
	}
}
