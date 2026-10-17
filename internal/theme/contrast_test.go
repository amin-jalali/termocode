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
			// Dim tiers (breadcrumb folders, hints, placeholders, the
			// version line) must stay legible. Light themes get the full
			// limits; dark ones a floor (dim grey on dark is the norm).
			{"dim on editor", TextDim, BgEditor, lightOr(3.0, 2.4)},
			{"dim on sidebar", TextDim, BgSidebar, lightOr(3.0, 2.4)},
			{"dim on panel", TextDim, BgPanel, lightOr(3.0, 2.4)},
			{"quaternary on editor", TextQuaternary, BgEditor, lightOr(2.2, 1.5)},
			{"muted on panel", TextMuted, BgPanel, 3.0},
			{"divider on sidebar", BorderSubtle, BgSidebar, lightOr(1.15, 1.0)},
		}
		for _, c := range checks {
			if r := contrastRatio(ToRGB(c.fg), ToRGB(c.bg)); r < c.min {
				t.Errorf("%s: %s contrast %.2f < %.2f", nt.ID, c.name, r, c.min)
			}
		}
	}
}

// Light themes: the sidebar (Mantle) must not be brighter than the editor
// (Base), or the panes read inside out (Rosé Pine Dawn).
func TestLightThemeSidebarNotBrighterThanEditor(t *testing.T) {
	defer ApplyTheme("vscode-dark-plus")
	for _, nt := range AvailableThemes() {
		ApplyTheme(nt.ID)
		if !IsLight() {
			continue
		}
		if relLuminance(ToRGB(BgSidebar)) > relLuminance(ToRGB(BgEditor)) {
			t.Errorf("%s: sidebar %s is brighter than the editor %s", nt.ID, Hex(BgSidebar), Hex(BgEditor))
		}
	}
}

func lightOr(light, dark float64) float64 {
	if IsLight() {
		return light
	}
	return dark
}

// The Find Results page: body text on its cursor row and white text on
// its match wash read on every theme.
func TestFindResultsContrast(t *testing.T) {
	defer ApplyTheme("vscode-dark-plus")
	for _, nt := range AvailableThemes() {
		ApplyTheme(nt.ID)
		c := NvimColors(nt.ID)
		for _, p := range [][3]string{{"fg", "find_cursor", "text on cursor row"}, {"fg_sel", "bg_match", "white on match"}} {
			fg, _ := ParseHex(c[p[0]])
			bg, _ := ParseHex(c[p[1]])
			if r := contrastRatio(fg, bg); r < 4.5 {
				t.Errorf("%s: %s %.2f < 4.5", nt.ID, p[2], r)
			}
		}
	}
}

// Source Control GRAPH pills (branch, tag) read on every theme.
func TestGraphPillContrast(t *testing.T) {
	defer ApplyTheme("vscode-dark-plus")
	for _, nt := range AvailableThemes() {
		ApplyTheme(nt.ID)
		for name, acc := range map[string]Color256{"branch": AccentBlue, "tag": AccentAmber} {
			fg, bg := TintPill(acc, BgSidebar)
			f, _ := ParseHex(string(fg))
			b, _ := ParseHex(string(bg))
			if r := contrastRatio(f, b); r < 4.5 {
				t.Errorf("%s: %s pill %.2f < 4.5", nt.ID, name, r)
			}
		}
	}
}

// The start page's directory / time column and its version line use
// Readable(TextDim, BgEditor, 3): at least 3:1 in every theme, without
// changing TextDim elsewhere.
func TestStartPageDimContrast(t *testing.T) {
	defer ApplyTheme("vscode-dark-plus")
	for _, nt := range AvailableThemes() {
		ApplyTheme(nt.ID)
		c, _ := ParseHex(string(Readable(TextDim, BgEditor, 3.0)))
		if r := contrastRatio(c, ToRGB(BgEditor)); r < 3.0 {
			t.Errorf("%s: start page dim text contrast %.2f < 3.00", nt.ID, r)
		}
		if contrastRatio(ToRGB(TextDim), ToRGB(BgEditor)) >= 3.0 && string(Readable(TextDim, BgEditor, 3.0)) != Hex(TextDim) {
			t.Errorf("%s: a passing TextDim was changed", nt.ID)
		}
	}
}

// The Services view's state glyphs (GitAdded running, GitModified
// starting, DiagError exited) must read on the sidebar in every theme.
func TestServicesStateContrast(t *testing.T) {
	defer ApplyTheme("vscode-dark-plus")
	for _, nt := range AvailableThemes() {
		ApplyTheme(nt.ID)
		for name, c := range map[string]Color256{"running": GitAdded, "starting": GitModified, "exited": DiagError} {
			if r := contrastRatio(ToRGB(c), ToRGB(BgSidebar)); r < 2.0 {
				t.Errorf("%s: %s glyph contrast %.2f < 2.0 on the sidebar", nt.ID, name, r)
			}
		}
	}
}
