package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSaveLoadRoundTrip verifies that SaveThemeID + LoadThemeID round-trip a
// theme ID through the on-disk config file. We point XDG_CONFIG_HOME at a
// t.TempDir() so the user's real ~/.config is left untouched.
func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if got := LoadThemeID(); got != "" {
		t.Fatalf("expected empty initial id, got %q", got)
	}
	if err := SaveThemeID("one-dark"); err != nil {
		t.Fatalf("SaveThemeID: %v", err)
	}
	if got := LoadThemeID(); got != "one-dark" {
		t.Errorf("LoadThemeID after save: got %q want %q", got, "one-dark")
	}
}

// TestLoadCorruptConfigFallsBack checks that a malformed config.json doesn't
// surface an error to the caller — we silently treat it as "no theme stored"
// so termocode can boot under any condition.
func TestLoadCorruptConfigFallsBack(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path := filepath.Join(dir, "termocode", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := LoadThemeID(); got != "" {
		t.Errorf("expected empty id on corrupt config, got %q", got)
	}
}

// TestLookupThemeNormalization confirms the user-facing labels resolve to the
// same theme regardless of casing / hyphenation. This matters because the
// stored value is whatever ApplyTheme returned, but a future release may want
// to look up by display name too.
func TestLookupThemeNormalization(t *testing.T) {
	cases := []string{
		"vscode-dark-plus",
		"VSCode Dark+",
		"VSCODE_DARK_PLUS",
	}
	for _, in := range cases {
		got, ok := LookupTheme(in)
		if !ok || got.ID != "vscode-dark-plus" {
			t.Errorf("LookupTheme(%q) = (%s, %v), want (vscode-dark-plus, true)",
				in, got.ID, ok)
		}
	}
	if _, ok := LookupTheme("definitely-not-a-theme"); ok {
		t.Errorf("LookupTheme of unknown id returned ok=true")
	}
}

// TestApplyThemeMutatesPalette guards against a regression where SetPalette
// stops actually updating the package-level vars: switching to One Dark
// should give a different BgEditor than VSCode Dark+ (otherwise users
// wouldn't see anything change on screen).
func TestApplyThemeMutatesPalette(t *testing.T) {
	// Restore default at end so we don't leak state into other tests.
	t.Cleanup(func() { ApplyTheme("vscode-dark-plus") })

	ApplyTheme("vscode-dark-plus")
	beforeBg := BgEditor

	ApplyTheme("one-dark")
	if BgEditor == beforeBg {
		t.Errorf("expected BgEditor to change after ApplyTheme(one-dark); both = %d", BgEditor)
	}
}

// Bug: SaveThemeID rewrote config.json with only "theme", so picking a
// theme dropped every other setting. Other keys must survive as they
// were, and a file that is not valid JSON is never overwritten.
func TestSaveThemeIDKeepsOtherKeys(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "termocode", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	in := `{"ai_provider": "openai", "tab_size": 2, "nested": {"a": [1, 2]}, "theme": "old"}`
	if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveThemeID("one-dark"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("not JSON: %s", b)
	}
	if got["theme"] != "one-dark" || got["ai_provider"] != "openai" || got["tab_size"] != float64(2) || got["nested"] == nil {
		t.Fatalf("config after save: %s", b)
	}

	bad := "{ not json"
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveThemeID("one-dark"); err == nil {
		t.Error("a broken config.json was overwritten without an error")
	}
	if b, _ := os.ReadFile(path); string(b) != bad {
		t.Fatalf("broken file changed: %s", b)
	}
}
