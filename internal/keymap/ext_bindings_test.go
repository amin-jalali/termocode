package keymap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExtBindings(t *testing.T) {
	p := filepath.Join(t.TempDir(), "keymap.json")
	body := `{"alt+h": "ext:hello.say", "ctrl+alt+r": "ext: rest-client.send ", "alt+s": "Save", "f9": "ext:", "": "ext:x"}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadExtBindings(p)
	if len(got) != 2 || got["alt+h"] != "hello.say" || got["ctrl+alt+r"] != "rest-client.send" {
		t.Fatalf("LoadExtBindings = %v", got)
	}
	// The Action loader must ignore ext entries and keep the real one.
	ov := LoadOverrides(p)
	if len(ov) != 1 || ov["alt+s"] != ActionSave {
		t.Errorf("LoadOverrides = %v", ov)
	}
	// Saving an Action override keeps the ext entries (read-modify-write).
	if err := SaveOverrides(p, map[string]Action{"ctrl+s": ActionSave}); err != nil {
		t.Fatal(err)
	}
	if got := LoadExtBindings(p); got["alt+h"] != "hello.say" {
		t.Errorf("ext binding lost after SaveOverrides: %v", got)
	}
}

func TestLoadExtBindingsBadFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "keymap.json")
	if LoadExtBindings(p) != nil {
		t.Error("missing file should give nil")
	}
	_ = os.WriteFile(p, []byte("{broken"), 0o644)
	if LoadExtBindings(p) != nil {
		t.Error("broken file should give nil")
	}
}
