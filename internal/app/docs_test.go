package app

import "testing"

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
