package config

import "testing"

func TestLiveFallThroughRequiresExplicitOptIn(t *testing.T) {
	if (Replay{}).AllowFallThrough() {
		t.Fatal("default replay can start live server")
	}
	yes := true
	no := false
	if !(Replay{FallThrough: &yes}).AllowFallThrough() {
		t.Fatal("explicit opt-in ignored")
	}
	if (Replay{FallThrough: &no}).AllowFallThrough() {
		t.Fatal("explicit denial ignored")
	}
}
