package theme

import (
	"strings"
	"testing"
)

func TestSpecThemesRegistered(t *testing.T) {
	for _, s := range SpecThemes {
		nt, ok := LookupTheme(s.ID)
		if !ok || nt.ID != s.ID {
			t.Fatalf("theme %s not registered", s.ID)
		}
		if nt.Light != s.Light {
			t.Errorf("%s: Light=%v want %v", s.ID, nt.Light, s.Light)
		}
	}
	// Names with spaces / accents resolve too.
	if nt, ok := LookupTheme("Catppuccin Mocha"); !ok || nt.ID != "catppuccin-mocha" {
		t.Errorf("lookup by display name failed: %v %v", nt.ID, ok)
	}
}

func TestFromSpecEveryTokenHasHex(t *testing.T) {
	defer ApplyTheme("vscode-dark-plus")
	for _, s := range SpecThemes {
		ApplyTheme(s.ID)
		for _, k := range PaletteKeys {
			h := CurrentPaletteHex(k)
			if _, ok := ParseHex(h); !ok {
				t.Errorf("%s: token %s has no truecolor hex (%q)", s.ID, k, h)
			}
		}
		if IsLight() != s.Light {
			t.Errorf("%s: IsLight()=%v", s.ID, IsLight())
		}
		// Text must stand out from the editor background.
		fg, bg := ToRGB(TextPrimary).Luma(), ToRGB(BgEditor).Luma()
		if d := fg - bg; d < 0.3 && d > -0.3 {
			t.Errorf("%s: low text contrast (%.2f vs %.2f)", s.ID, fg, bg)
		}
	}
}

func TestNvimColorsCoverLuaReferences(t *testing.T) {
	defer ApplyTheme("vscode-dark-plus")
	for _, id := range []string{"vscode-dark-plus", "github-dark", "catppuccin-latte", "rose-pine"} {
		ApplyTheme(id)
		c := NvimColors(id)
		for _, k := range []string{"bg", "fg", "diff_add", "diff_del_fg", "winbar", "ai_ghost", "br1", "inlay", "git_unt", "fg_sel", "match_paren"} {
			if _, ok := ParseHex(c[k]); !ok {
				t.Errorf("%s: nvim colour %q = %q", id, k, c[k])
			}
		}
	}
	// Dark+ keeps its historical editor background.
	ApplyTheme("vscode-dark-plus")
	if got := NvimColors("vscode-dark-plus")["bg"]; got != "#1c1c1c" {
		t.Errorf("dark+ bg = %s", got)
	}
}

func TestLuaTableSorted(t *testing.T) {
	out := LuaTable(map[string]string{"b": "#000002", "a": "#000001"})
	if strings.Index(out, "'a'") > strings.Index(out, "'b'") {
		t.Errorf("keys not sorted:\n%s", out)
	}
}

func TestGradient(t *testing.T) {
	g := Gradient(3, RGB{0, 0, 0}, RGB{200, 100, 0})
	if g[0] != (RGB{0, 0, 0}) || g[2] != (RGB{200, 100, 0}) || g[1] != (RGB{100, 50, 0}) {
		t.Errorf("gradient = %v", g)
	}
	if len(Gradient(0, RGB{})) != 0 {
		t.Error("n=0")
	}
}
