package proxy

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// The tests need a real MCP server subprocess, because most of what is being
// verified is process behavior: pipe lifecycle, exit status, signals. An
// in-process fake would test none of that.
//
// So the test binary doubles as the server. TestMain checks for a mode in the
// environment and, if present, becomes the server and exits before the test
// framework ever starts.

const serverModeVar = "CASSETTE_TEST_SERVER_MODE"

func TestMain(m *testing.M) {
	if mode := os.Getenv(serverModeVar); mode != "" {
		os.Exit(runFakeServer(mode))
	}
	os.Exit(m.Run())
}

// helperCommand returns a command and environment that launches this test
// binary as a fake MCP server in the given mode.
func helperCommand(mode string) (cmd []string, env []string) {
	return []string{os.Args[0]}, append(os.Environ(), serverModeVar+"="+mode)
}

func runFakeServer(mode string) int {
	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	out := bufio.NewWriterSize(os.Stdout, 1<<20)
	defer out.Flush()

	switch mode {
	case "exit3":
		// Exits immediately with a distinctive status.
		return 3

	case "stderr":
		fmt.Fprintln(os.Stderr, "server starting up")
		fmt.Fprintln(os.Stderr, "this is not an error, per the spec")
		return 0

	case "ignore-stdin":
		// Never exits on its own. Used to exercise SIGTERM escalation.
		// Signals are left at their default disposition so SIGTERM kills it.
		time.Sleep(60 * time.Second)
		return 0

	case "big":
		// Emits a response far larger than the read window.
		line, err := in.ReadString('\n')
		if err != nil {
			return 1
		}
		_ = line
		fmt.Fprintf(out, `{"jsonrpc":"2.0","id":1,"result":{"text":"%s"}}`+"\n", strings.Repeat("x", 1<<20))
		out.Flush()
		return 0

	case "garbage":
		// Writes something that is not valid JSON. The proxy must forward it
		// rather than swallow it.
		fmt.Fprintln(out, `this is not json at all`)
		fmt.Fprintln(out, `{"jsonrpc":"2.0","id":1,"result":{}}`)
		out.Flush()
		return 0

	case "echo":
		// Echoes every line back verbatim. This is what proves the proxy is
		// byte-transparent: whatever the client sent comes back unchanged,
		// having crossed the proxy twice.
		for {
			line, err := in.ReadString('\n')
			if len(line) > 0 {
				out.WriteString(line)
				if !strings.HasSuffix(line, "\n") {
					out.WriteByte('\n')
				}
				out.Flush()
			}
			if err != nil {
				return 0
			}
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown fake server mode %q\n", mode)
		return 64
	}
}
