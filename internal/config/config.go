// Package config loads cassette.json.
//
// JSON rather than YAML, for two reasons. There is no YAML parser in the
// standard library and this project has no dependencies, and the agent
// ecosystem this plugs into already speaks JSON config (.mcp.json,
// claude_desktop_config.json), so it is the format users are already editing.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Kshitijmishradev/cassette/internal/safety"
)

// FileName is the config file cassette looks for.
const FileName = "cassette.json"

// Config is the whole file.
type Config struct {
	Tools  safety.Config `json:"tools,omitempty"`
	Replay Replay        `json:"replay,omitempty"`
}

// Replay holds replay-time behavior.
type Replay struct {
	// FallThrough allows an unmatched read-class call to reach a live
	// server. Defaults to false. Setting it false makes MCP replay entirely
	// hermetic: nothing leaves the process, and any miss stops the run.
	//
	// Hermetic is the stricter and more honest mode for CI, where a replay
	// that quietly talked to the network is not the experiment anyone
	// thought they were running.
	FallThrough *bool `json:"fallThrough,omitempty"`
}

// AllowFallThrough reports the effective setting.
func (r Replay) AllowFallThrough() bool {
	return r.FallThrough != nil && *r.FallThrough
}

// Classifier builds a tool classifier from this config.
func (c Config) Classifier() *safety.Classifier { return safety.New(c.Tools) }

// Load reads the config nearest to dir, walking up toward the filesystem
// root the way git finds its own config. Returns defaults when no file
// exists, since the tool has to work before anyone has configured it.
func Load(dir string) (Config, string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Config{}, "", err
	}

	for {
		path := filepath.Join(abs, FileName)
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			var c Config
			if err := json.Unmarshal(b, &c); err != nil {
				return Config{}, path, fmt.Errorf("parsing %s: %w", path, err)
			}
			return c, path, nil
		case !errors.Is(err, fs.ErrNotExist):
			return Config{}, path, err
		}

		parent := filepath.Dir(abs)
		if parent == abs {
			return Config{}, "", nil
		}
		abs = parent
	}
}

// LoadFile reads a specific config file.
func LoadFile(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("parsing %s: %w", path, err)
	}
	return c, nil
}
