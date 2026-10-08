package theme

import "testing"

// Chrome contrast for every built-in theme (WCAG ratios): body text must
// read on the editor and on floating cards, text on accent fills (status
// pills, buttons) and the dim text tiers must stay legible.
func TestThemeChromeContrast(t *testing.T) {
	defer ApplyTheme("vscode-dark-plus")
	for _, nt := range AvailableThemes() {
		ApplyTheme(nt.ID)
		fl := Float()
		_ = fl
		checks := []struct {
			name   string
			fg, bg Color256
			min    float64
		}{
			{"text on editor", TextPrimary, BgEditor, 4.5},
			{"text on sidebar", TextPrimary, BgSidebar, 4.5},
			{"text on float", TextPrimary, BgFloat, 4.5},
			{"muted on sidebar", TextMuted, BgSidebar, 2.5},
			{"on-accent on accent", OnAccent, BorderFocus, 3.0},
		}
		for _, c := range checks {
			if r := contrastRatio(ToRGB(c.fg), ToRGB(c.bg)); r < c.min {
				t.Errorf("%s: %s contrast %.2f < %.1f", nt.ID, c.name, r, c.min)
			}
		}
	}
}
