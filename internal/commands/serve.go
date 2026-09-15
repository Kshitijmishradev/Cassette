package commands

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	"github.com/Kshitijmishradev/cassette/internal/api"
	"github.com/Kshitijmishradev/cassette/internal/cli"
	"github.com/Kshitijmishradev/cassette/internal/config"
	"github.com/Kshitijmishradev/cassette/internal/httpui"
	webui "github.com/Kshitijmishradev/cassette/web"
)

func serveCmd() *cli.Command {
	return &cli.Command{
		Name:  "serve",
		Usage: "serve [--addr <host:port>] [--suite <dir>]",
		Short: "Serve the local web UI for exploring runs and diffs",
		Long: `Serve starts a local web UI for browsing recorded runs, inspecting
trajectories, and reading diffs.

    cassette serve --addr localhost:7070

Four screens: the run list, a per-run waterfall showing which match tier
served each call, the trajectory diff, and the suite grid. There is
deliberately no metrics dashboard; that space is well served already and is
not what this tool is for.

The UI is compiled into the binary, so this needs no node runtime and no
network. It binds to localhost by design. Cassettes contain production
payloads and credentials, which is the same reason this tool is not a hosted
service.`,
		Flags: func(fs *flag.FlagSet) {
			fs.String("addr", "localhost:7070", "address to listen on")
			fs.String("suite", "./cassettes", "directory of recorded runs")
			fs.Bool("open", false, "open a browser once listening")
		},
		Run: runServe,
	}
}

func runServe(ctx *cli.Context) error {
	addr := ctx.Flags.Lookup("addr").Value.String()
	if err := requireLoopback(addr); err != nil {
		return err
	}

	cfg, _, err := config.Load(".")
	if err != nil {
		return err
	}
	handler := &httpui.Handler{
		Builder: &api.Builder{
			SuiteDir:   ctx.Flags.Lookup("suite").Value.String(),
			Classifier: cfg.Classifier(),
			Live:       true,
		},
		Assets: webui.Assets(),
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	serverURL := url.URL{Scheme: "http", Host: listener.Addr().String(), Path: "/", Fragment: "/runs"}
	if host, port, splitErr := net.SplitHostPort(listener.Addr().String()); splitErr == nil && net.ParseIP(host).IsLoopback() {
		serverURL.Host = net.JoinHostPort("localhost", port)
	}
	fmt.Fprintf(ctx.Err, "cassette: exploring %s at %s\n", ctx.Flags.Lookup("suite").Value.String(), serverURL.String())
	if ctx.Flags.Lookup("open").Value.String() == "true" {
		if err := openBrowser(serverURL.String()); err != nil {
			fmt.Fprintf(ctx.Err, "cassette: opening browser: %v\n", err)
		}
	}

	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	err = server.Serve(listener)
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid --addr %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("refusing non-loopback --addr %q; cassettes may contain credentials", addr)
	}
	return nil
}

func openBrowser(target string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{target}
	case "linux":
		command, args = "xdg-open", []string{target}
	default:
		return fmt.Errorf("open %s manually", target)
	}
	return exec.Command(command, args...).Start()
}
