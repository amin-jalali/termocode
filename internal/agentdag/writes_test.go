package agentdag

import "testing"

// A step may change files unless its tools are none or read-only; no
// preset means all tools.
func TestWritesByPreset(t *testing.T) {
	for preset, want := range map[string]bool{"": true, PresetAll: true, PresetReadOnly: false, PresetNone: false} {
		n := Node{ID: "a"}
		n.Tools.Preset = preset
		if Writes(n) != want {
			t.Errorf("preset %q: writes %v", preset, !want)
		}
	}
}

// Validate reports the normalize error (a duplicate id) before the
// loop and limit checks.
func TestValidateReportsNormalizeError(t *testing.T) {
	if err := Validate(Run{Nodes: nodes("a", "a")}); err == nil {
		t.Error("duplicate ids accepted")
	}
}
