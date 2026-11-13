package keymap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// With XDG_CONFIG_HOME set, the overrides file lives under it.
func TestOverridesPathHonorsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x/cfg")
	got, err := OverridesPath()
	if err != nil || got != filepath.Join("/x/cfg", "termocode", "keymap.json") {
		t.Fatalf("got %q, %v", got, err)
	}
}

// Without XDG_CONFIG_HOME it falls back to ~/.config.
func TestOverridesPathFallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	got, err := OverridesPath()
	if err != nil || got != filepath.Join(home, ".config", "termocode", "keymap.json") {
		t.Fatalf("got %q, %v", got, err)
	}
}

// With no home at all, there is no path: loaders give nothing and the
// saver reports the error; startup never breaks.
func TestOverridesWithoutAnyHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := OverridesPath(); err == nil {
		t.Skip("this platform finds a home anyway")
	}
	if m, p := LoadOverridesReport(""); m != nil || p != nil {
		t.Fatalf("got %v %v", m, p)
	}
	if LoadExtBindings("") != nil {
		t.Fatal("ext bindings from nowhere")
	}
	if SaveOverrides("", map[string]Action{"ctrl+k": ActionNone}) == nil {
		t.Fatal("save without a path must fail")
	}
}

// Empty path means the default file under XDG_CONFIG_HOME, for load,
// ext load and save alike.
func TestOverridesDefaultPathRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	act := AllActions()[0]
	if err := SaveOverrides("", map[string]Action{"ctrl+k": act}); err != nil {
		t.Fatal(err)
	}
	m, problems := LoadOverridesReport("")
	if len(problems) != 0 || m["ctrl+k"] != act {
		t.Fatalf("got %v %v", m, problems)
	}
	p, _ := OverridesPath()
	if err := os.WriteFile(p, []byte(`{"ctrl+e":"ext:hello"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadExtBindings(""); got["ctrl+e"] != "hello" {
		t.Fatalf("ext bindings %v", got)
	}
}

// A keymap.json that cannot be read (here: a directory) is reported, not
// swallowed.
func TestLoadOverridesReportUnreadableFile(t *testing.T) {
	m, problems := LoadOverridesReport(t.TempDir())
	if m != nil || len(problems) != 1 {
		t.Fatalf("got %v %v", m, problems)
	}
}

// Saving fails clearly when the folder cannot be made or the file cannot
// be written.
func TestSaveOverridesErrors(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if SaveOverrides(filepath.Join(blocker, "sub", "keymap.json"), nil) == nil {
		t.Fatal("a file in the way of the folder must fail")
	}
	if SaveOverrides(dir, nil) == nil {
		t.Fatal("a directory as the target file must fail")
	}
}

// MergeInto layers overrides on a copy and leaves the receiver alone.
func TestMergeIntoDoesNotTouchReceiver(t *testing.T) {
	base := Default()
	act := AllActions()[0]
	merged := base.MergeInto(map[string]Action{"ctrl+k": act})
	if got := merged.KeysFor(act); len(got) == 0 {
		t.Fatalf("override missing in %v", got)
	}
	for _, k := range base.KeysFor(act) {
		if k == "ctrl+k" {
			t.Fatal("receiver was changed")
		}
	}
	if len(merged.bindings) < len(base.bindings) {
		t.Fatal("merged lost default bindings")
	}
}

// KeysFor lists every key for an action, sorted.
func TestKeysForSorted(t *testing.T) {
	k := KeyMap{bindings: map[string]Action{"b": 5, "a": 5, "c": 6}}
	if got := strings.Join(k.KeysFor(5), ","); got != "a,b" {
		t.Fatalf("got %q", got)
	}
	if len(k.KeysFor(7)) != 0 {
		t.Fatal("unbound action has keys")
	}
}

// A control character is not a printable key; an unknown action has no name.
func TestValidateKeyControlCharAndUnknownActionName(t *testing.T) {
	err := ValidateKey("ctrl+\x01")
	if err == nil || !strings.Contains(err.Error(), "not a printable key") {
		t.Fatalf("got %v", err)
	}
	if ActionName(Action(99999)) != "" {
		t.Fatal("unknown action got a name")
	}
}
