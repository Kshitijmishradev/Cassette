package env

import "testing"

func TestParseMode(t *testing.T) {
	tests := []struct {
		in      string
		want    Mode
		wantErr bool
	}{
		// Unset must mean off. A shim left in an agent's config has to be
		// invisible during ordinary work.
		{in: "", want: ModeOff},
		{in: "off", want: ModeOff},
		{in: "record", want: ModeRecord},
		{in: "replay", want: ModeReplay},
		{in: "Record", wantErr: true},
		{in: "replay ", wantErr: true},
		{in: "nonsense", wantErr: true},
	}

	for _, tt := range tests {
		got, err := ParseMode(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseMode(%q): want error, got %v", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseMode(%q): unexpected error %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseMode(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestModeActive(t *testing.T) {
	if ModeOff.Active() {
		t.Error("ModeOff must not be active")
	}
	if !ModeRecord.Active() || !ModeReplay.Active() {
		t.Error("record and replay must both be active")
	}
}

// An invalid mode must fail closed, to off, so a typo degrades to
// passthrough rather than to some other behavior.
func TestParseModeFailsClosed(t *testing.T) {
	got, err := ParseMode("recrod")
	if err == nil {
		t.Fatal("want error for typo")
	}
	if got != ModeOff {
		t.Errorf("invalid mode returned %v, want ModeOff", got)
	}
}
