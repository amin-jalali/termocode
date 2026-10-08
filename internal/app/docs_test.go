package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Docs: every Settings row needs a default and a description for
// docs/reference/settings.md.
func TestSettingsRowsDocumented(t *testing.T) {
	for _, s := range SettingsCatalog() {
		if s.Default == "" || s.Description == "" || s.Type == "" {
			t.Errorf("setting %q: missing type, default or description", s.Key)
		}
	}
}

// Docs: paletteActions must only name real palette IDs, and the docs
// command must be in the palette.
func TestPaletteCatalog(t *testing.T) {
	ids := map[string]bool{}
	for _, e := range PaletteCatalog() {
		if ids[e.ID] {
			t.Errorf("duplicate palette id %q", e.ID)
		}
		ids[e.ID] = true
	}
	for id := range paletteActions {
		if !ids[id] {
			t.Errorf("paletteActions has %q, which is not a palette id", id)
		}
	}
	if !ids["open-docs"] {
		t.Error(`"Help: Open Documentation" (open-docs) is missing from the palette`)
	}
}

// Palette hints come from the keymap. An action-backed item whose action
// has no key may only keep a static hint that names a Neovim-side key
// (EditorKeyCatalog) — so a stale "Ctrl+0" can not come back.
func TestPaletteHintsMatchKeymap(t *testing.T) {
	km := keymap.Default()
	var nvimKeys []string
	for _, k := range EditorKeyCatalog() {
		nvimKeys = append(nvimKeys, k.Key)
	}
	for _, e := range PaletteCatalog() {
		a, ok := paletteActions[e.ID]
		if !ok {
			continue
		}
		keys := km.KeysFor(a)
		if len(keys) > 0 {
			if e.Hint == "" {
				t.Errorf("%s: action is bound to %v but the hint is empty", e.ID, keys)
			}
			for _, part := range strings.Split(e.Hint, " / ") {
				found := false
				for _, k := range keys {
					if keymap.HumanKey(k) == part {
						found = true
					}
				}
				if !found {
					t.Errorf("%s: hint part %q is not a bound key (%v)", e.ID, part, keys)
				}
			}
			continue
		}
		if e.Hint == "" {
			continue
		}
		found := false
		for _, nk := range nvimKeys {
			if strings.Contains(nk, e.Hint) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: hint %q names no keymap key and no Neovim-side key", e.ID, e.Hint)
		}
	}
}

func TestWithKeyHints(t *testing.T) {
	items := []picker.Item{
		{ID: "git-stage-hunk", Title: "Git: Stage Hunk"},
		{ID: "focus-explorer", Title: "View: Focus Explorer"},
		{ID: "reopen-closed", Title: "View: Reopen Closed Editor", Hint: "Alt+Shift+T"},
		{ID: "not-an-action", Title: "X", Hint: "keep me"},
	}
	km := keymap.Default().MergeInto(map[string]keymap.Action{"ctrl+k": keymap.ActionFocusExplorer})
	got := withKeyHints(items, km)
	want := []string{"Alt+S", "Ctrl+K", "Alt+Shift+T / Ctrl+Shift+T", "keep me"}
	for i, w := range want {
		if got[i].Hint != w {
			t.Errorf("%s: hint %q, want %q", got[i].ID, got[i].Hint, w)
		}
	}
	if items[0].Hint != "" {
		t.Error("withKeyHints must not change its input")
	}
}
