package theme

import (
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// withIconMode runs fn with CurrentIconMode set to mode, then restores it.
func withIconMode(t *testing.T, mode IconMode, fn func()) {
	t.Helper()
	old := CurrentIconMode
	CurrentIconMode = mode
	defer func() { CurrentIconMode = old }()
	fn()
}

// allFileIconDefs lists every table entry so the width tests cover them all.
func allFileIconDefs() map[string]fileIconDef {
	out := map[string]fileIconDef{
		"<default>":    fileIconDefault,
		"<dir closed>": dirClosedIcon,
		"<dir open>":   dirOpenIcon,
	}
	for k, d := range fileIconsByName {
		out["name:"+k] = d
	}
	for _, p := range fileIconsByPrefix {
		out["prefix:"+p.prefix] = p.def
	}
	for k, d := range fileIconsByExt {
		out["ext:"+k] = d
	}
	for k, d := range gitStatusIcons {
		out["git:"+k] = d
	}
	for k, sp := range dirIconsByName {
		out["dir:"+k] = fileIconDef{sp.nerd, uniDirC, asciiDirC, sp.color}
	}
	return out
}

func isPUA(r rune) bool {
	return (r >= 0xE000 && r <= 0xF8FF) || (r >= 0xF0000 && r <= 0xFFFFD)
}

func TestFileIconsAreOneCell(t *testing.T) {
	cond := runewidth.NewCondition()
	cond.EastAsianWidth = false
	condEA := runewidth.NewCondition()
	condEA.EastAsianWidth = true
	for key, d := range allFileIconDefs() {
		for tier, g := range map[string]string{"nerd": d.nerd, "unicode": d.uni, "ascii": d.ascii} {
			if g == "" {
				t.Errorf("%s %s: empty glyph", key, tier)
				continue
			}
			if w := lipgloss.Width(g); w != 1 {
				t.Errorf("%s %s %q: lipgloss.Width = %d, want 1", key, tier, g, w)
			}
			if w := cond.StringWidth(g); w != 1 {
				t.Errorf("%s %s %q: runewidth = %d, want 1", key, tier, g, w)
			}
			switch tier {
			case "nerd":
				r, n := utf8.DecodeRuneInString(g)
				if n != len(g) || !isPUA(r) {
					t.Errorf("%s nerd %q: want one Private Use Area rune", key, g)
				}
			case "ascii":
				if len(g) != 1 || g[0] < 0x21 || g[0] > 0x7e {
					t.Errorf("%s ascii %q: want one printable ASCII byte", key, g)
				}
			case "unicode":
				// File-icon plain glyphs must stay one cell even where
				// ambiguous-width glyphs render wide. (Git status glyphs
				// reuse existing ambiguous marks like ● and are exempt.)
				if len(key) < 4 || key[:4] != "git:" {
					if w := condEA.StringWidth(g); w != 1 {
						t.Errorf("%s unicode %q: width %d with EastAsianWidth, want 1", key, g, w)
					}
				}
			}
		}
		if d.color == nil {
			t.Errorf("%s: nil color token", key)
		}
	}
}

func TestPanelIconsAreOneCell(t *testing.T) {
	for _, mode := range []IconMode{IconModeNerdFont, IconModeUnicode, IconModeASCII} {
		withIconMode(t, mode, func() {
			for kind := range panelIcons {
				g := PanelIcon(kind)
				if g == "" {
					if mode != IconModeASCII {
						t.Errorf("mode %d %s: empty icon", mode, kind)
					}
					continue
				}
				if w := lipgloss.Width(g); w != 1 {
					t.Errorf("mode %d %s %q: width %d", mode, kind, g, w)
				}
			}
			if PanelIcon("nope") != "" {
				t.Errorf("unknown kind should be empty")
			}
		})
	}
}

func TestFileIconLookup(t *testing.T) {
	withIconMode(t, IconModeNerdFont, func() {
		cases := []struct {
			name string
			want fileIconDef
		}{
			{"main.go", fileIconsByExt["go"]},
			{"/a/b/MAIN.GO", fileIconsByExt["go"]},
			{"Photo.PNG", fileIconsByExt["png"]},
			{"go.sum", iconLock},            // full name beats .sum / nothing
			{"go.mod", iconGoMod},           // full name beats extension
			{"package-lock.json", iconLock}, // full name beats .json
			{"Dockerfile", iconDocker},
			{"Makefile", iconMake},
			{".gitignore", iconGitFile},
			{".env", iconEnv},
			{".env.local", iconEnv},
			{"LICENSE", iconLicense},
			{"LICENSE.md", iconLicense},
			{"README.md", iconReadme},
			{"unknown.xyz", fileIconDefault},
			{"noext", fileIconDefault},
			{"trailingdot.", fileIconDefault},
			{".bashrc", fileIconDefault},
		}
		for _, c := range cases {
			g, col := FileIconFor(c.name, false, false)
			if g != c.want.nerd || col != *c.want.color {
				t.Errorf("FileIconFor(%q) = %q/%d, want %q/%d", c.name, g, col, c.want.nerd, *c.want.color)
			}
		}
		if g, _ := FileIconFor("src", true, false); g != dirClosedIcon.nerd {
			t.Errorf("closed dir = %q", g)
		}
		if g, _ := FileIconFor("src", true, true); g != dirOpenIcon.nerd {
			t.Errorf("open dir = %q", g)
		}
		if g, _ := FileIconFor("/repo/.git", true, false); g != dirIconsByName[".git"].nerd {
			t.Errorf(".git dir = %q", g)
		}
	})
	withIconMode(t, IconModeASCII, func() {
		if g, _ := FileIconFor("x", true, false); g != ">" {
			t.Errorf("ascii closed dir = %q", g)
		}
		if g, _ := FileIconFor("x", true, true); g != "v" {
			t.Errorf("ascii open dir = %q", g)
		}
		if g, _ := FileIconFor(".git", true, true); g != "v" {
			t.Errorf("ascii special dir should keep chevron, got %q", g)
		}
	})
	withIconMode(t, IconModeUnicode, func() {
		if g, _ := FileIconFor("a.go", false, false); g != uniCode {
			t.Errorf("unicode go = %q", g)
		}
	})
}

func TestGitStatusGlyph(t *testing.T) {
	withIconMode(t, IconModeASCII, func() {
		want := map[string]string{"M": "M", "A": "A", "D": "D", "R": "R", "U": "U", "??": "U", "C": "!", "I": "I"}
		for code, g := range want {
			if got, _ := GitStatusGlyph(code); got != g {
				t.Errorf("GitStatusGlyph(%q) = %q, want %q", code, got, g)
			}
		}
		if got, c := GitStatusGlyph("X"); got != "X" || c != TextMuted {
			t.Errorf("unknown code = %q/%d", got, c)
		}
		if got, _ := GitStatusGlyph(""); got != " " {
			t.Errorf("empty code = %q", got)
		}
	})
	if _, c := GitStatusGlyph("M"); c != GitModified {
		t.Errorf("M color = %d, want GitModified %d", c, GitModified)
	}
}

// Colors are palette tokens read at call time: switching the palette must
// recolor icons without touching the table.
func TestFileIconColorFollowsPalette(t *testing.T) {
	saved := vscodeDarkPlusPalette()
	defer SetPalette(saved)

	SetPalette(vscodeDarkPlusPalette())
	_, before := FileIconFor("main.go", false, false)
	if before != AccentBlue {
		t.Fatalf("go color %d, want AccentBlue %d", before, AccentBlue)
	}
	p := vscodeDarkPlusPalette()
	p.AccentBlue = 99
	SetPalette(p)
	if _, after := FileIconFor("main.go", false, false); after != 99 {
		t.Errorf("after palette switch go color = %d, want 99", after)
	}
	if _, c := GitStatusGlyph("A"); c != GitAdded {
		t.Errorf("git A color %d != GitAdded %d", c, GitAdded)
	}
}
