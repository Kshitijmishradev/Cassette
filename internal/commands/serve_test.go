package commands

import "testing"

func TestServeAcceptsOnlyLoopbackAddresses(t *testing.T) {
	for _, addr := range []string{"localhost:7070", "127.0.0.1:0", "[::1]:7070"} {
		if err := requireLoopback(addr); err != nil {
			t.Errorf("requireLoopback(%q): %v", addr, err)
		}
	}
	for _, addr := range []string{":7070", "0.0.0.0:7070", "example.com:7070", "broken"} {
		if err := requireLoopback(addr); err == nil {
			t.Errorf("requireLoopback(%q) succeeded", addr)
		}
	}
}
