package theme

import (
	"os"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestEnvOverrideMode(t *testing.T) {
	saved := os.Getenv("TERMOCODE_ICON_MODE")
	savedNF := os.Getenv("TERMOCODE_NERD_FONT")
	defer func() {
		_ = os.Setenv("TERMOCODE_ICON_MODE", saved)
		_ = os.Setenv("TERMOCODE_NERD_FONT", savedNF)
	}()

	cases := []struct {
		env     string
		nerdEnv string
		want    IconMode
		wantOK  bool
	}{
		{"nerd_font", "", IconModeNerdFont, true},
		{"NerdFont", "", IconModeNerdFont, true},
		{"unicode", "", IconModeUnicode, true},
		{"ascii", "", IconModeASCII, true},
		{"", "1", IconModeNerdFont, true},
		{"", "", IconModeASCII, false},
		{"bogus", "", IconModeASCII, false},
	}
	for _, tc := range cases {
		t.Run(tc.env+"/"+tc.nerdEnv, func(t *testing.T) {
			_ = os.Setenv("TERMOCODE_ICON_MODE", tc.env)
			_ = os.Setenv("TERMOCODE_NERD_FONT", tc.nerdEnv)
			got, ok := envOverrideMode()
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("got (%v,%v) want (%v,%v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestDetectIconModeDefaultASCII(t *testing.T) {
	// Without env overrides the early pick is the ASCII placeholder;
	// ResolveIconMode makes the real choice once settings are read.
	saved := os.Getenv("TERMOCODE_ICON_MODE")
	savedNF := os.Getenv("TERMOCODE_NERD_FONT")
	savedNC := os.Getenv("NO_COLOR")
	defer func() {
		_ = os.Setenv("TERMOCODE_ICON_MODE", saved)
		_ = os.Setenv("TERMOCODE_NERD_FONT", savedNF)
		_ = os.Setenv("NO_COLOR", savedNC)
	}()
	_ = os.Unsetenv("TERMOCODE_ICON_MODE")
	_ = os.Unsetenv("TERMOCODE_NERD_FONT")
	_ = os.Unsetenv("NO_COLOR")
	if got := DetectIconMode(); got != IconModeASCII {
		t.Fatalf("DetectIconMode() = %v want %v", got, IconModeASCII)
	}
}

func TestDetectIconModeNoColor(t *testing.T) {
	saved := os.Getenv("NO_COLOR")
	defer func() { _ = os.Setenv("NO_COLOR", saved) }()
	_ = os.Setenv("NO_COLOR", "1")
	if got := DetectIconMode(); got != IconModeASCII {
		t.Fatalf("NO_COLOR DetectIconMode() = %v want ASCII", got)
	}
}

func TestDetectIconModeEnvOverrideWins(t *testing.T) {
	saved := os.Getenv("TERMOCODE_ICON_MODE")
	defer func() { _ = os.Setenv("TERMOCODE_ICON_MODE", saved) }()

	for _, env := range []struct {
		v    string
		want IconMode
	}{
		{"nerd_font", IconModeNerdFont},
		{"unicode", IconModeUnicode},
		{"ascii", IconModeASCII},
	} {
		_ = os.Setenv("TERMOCODE_ICON_MODE", env.v)
		if got := DetectIconMode(); got != env.want {
			t.Fatalf("DetectIconMode with TERMOCODE_ICON_MODE=%s = %v want %v", env.v, got, env.want)
		}
	}
}

// TestActivityBarBoldIconsAreTwoByTwo asserts every activity-bar icon
// ships a Bold field where each row is exactly 2 visible cells. The
// activity bar's 2×2 slot relies on this contract — a wider or narrower
// row would break the Width-cell render contract and leak terminal
// default bg through the modal-overlay splice.
func TestActivityBarBoldIconsAreTwoByTwo(t *testing.T) {
	saved := CurrentIconMode
	defer func() { CurrentIconMode = saved }()
	CurrentIconMode = IconModeUnicode
	cases := []struct {
		name string
		ic   Icon
	}{
		{"IconFiles", IconFiles},
		{"IconSearch", IconSearch},
		{"IconGit", IconGit},
		{"IconOutline", IconOutline},
		{"IconRun", IconRun},
		{"IconTests", IconTests}, // Group E
		{"IconServices", IconServices},
		{"IconContainers", IconContainers},
		{"IconDebug", IconDebug},
		{"IconExtensions", IconExtensions},
		{"IconTerminal", IconTerminal},
		{"IconAccount", IconAccount},
		{"IconSettings", IconSettings},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.ic.BoldRender()
			for i, row := range b {
				if w := runewidth.StringWidth(row); w != 2 {
					t.Fatalf("Bold row %d width = %d (%q), want 2", i, w, row)
				}
			}
			if tc.ic.Bold[0] == "" && tc.ic.Bold[1] == "" {
				t.Fatalf("expected hand-crafted Bold artwork, got empty fallback")
			}
		})
	}
}

// TestActivityBarBoldUsesQuadrantBlocks asserts every Bold row of every
// activity-bar icon is built strictly from the U+2580–U+259F block-
// element set (single quadrants, half blocks, three-quadrants,
// diagonals, full block) plus regular ASCII space. Mixing in geometric
// or box-drawing glyphs (◯, ●, ❯, ─, ═, …) would break the puzzle-fit
// constraint — the four cells are meant to lock together into a
// coherent silhouette using one Unicode block only.
func TestActivityBarBoldUsesQuadrantBlocks(t *testing.T) {
	// BoldRender now respects CurrentIconMode (ASCII default returns the
	// letter fallback). The contract this test enforces is on the
	// Bold-mode artwork specifically, so flip to Unicode for the duration.
	saved := CurrentIconMode
	defer func() { CurrentIconMode = saved }()
	CurrentIconMode = IconModeUnicode
	allowed := map[rune]struct{}{
		' ': {},
		'▀': {}, '▁': {}, '▂': {}, '▃': {}, '▄': {},
		'▅': {}, '▆': {}, '▇': {}, '█': {}, '▉': {},
		'▊': {}, '▋': {}, '▌': {}, '▍': {}, '▎': {},
		'▏': {}, '▐': {}, '░': {}, '▒': {}, '▓': {},
		'▔': {}, '▕': {}, '▖': {}, '▗': {}, '▘': {},
		'▙': {}, '▚': {}, '▛': {}, '▜': {}, '▝': {},
		'▞': {}, '▟': {},
	}
	cases := []struct {
		name string
		ic   Icon
	}{
		{"IconFiles", IconFiles},
		{"IconSearch", IconSearch},
		{"IconGit", IconGit},
		{"IconOutline", IconOutline},
		{"IconRun", IconRun},
		{"IconTests", IconTests}, // Group E
		{"IconServices", IconServices},
		{"IconDebug", IconDebug},
		{"IconExtensions", IconExtensions},
		{"IconTerminal", IconTerminal},
		{"IconAccount", IconAccount},
		{"IconSettings", IconSettings},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.ic.BoldRender()
			for i, row := range b {
				for _, r := range row {
					if _, ok := allowed[r]; !ok {
						t.Fatalf("Bold row %d (%q) contains disallowed rune %q (U+%04X) — only U+2580..U+259F block-element runes and space are permitted", i, row, r, r)
					}
				}
			}
		})
	}
}

// TestBoldRenderFallback exercises the path where an Icon has no Bold
// artwork: the single-cell glyph must end up in the lower row, padded
// to 2 cells, with a blank top row, both rows still 2 cells wide.
func TestBoldRenderFallback(t *testing.T) {
	saved := CurrentIconMode
	defer func() { CurrentIconMode = saved }()
	CurrentIconMode = IconModeUnicode

	ic := Icon{Unicode: "★"}
	b := ic.BoldRender()
	for i, row := range b {
		if w := runewidth.StringWidth(row); w != 2 {
			t.Fatalf("fallback row %d width = %d (%q), want 2", i, w, row)
		}
	}
	if b[0] != "  " {
		t.Fatalf("fallback top row want %q, got %q", "  ", b[0])
	}
	if b[1] != "★ " {
		t.Fatalf("fallback bottom row want %q, got %q", "★ ", b[1])
	}
}

// TestBoldRenderFallbackEmptyGlyph covers the corner where the icon
// has no Bold artwork *and* the active String() returns "" (e.g.
// NerdFont mode with an empty NerdFont field). Both rows must collapse
// to a 2-space placeholder so the slot stays exactly 2×2.
func TestBoldRenderFallbackEmptyGlyph(t *testing.T) {
	saved := CurrentIconMode
	defer func() { CurrentIconMode = saved }()
	CurrentIconMode = IconModeNerdFont

	ic := Icon{NerdFont: "", Unicode: "★", ASCII: "X"}
	b := ic.BoldRender()
	if b[0] != "  " || b[1] != "  " {
		t.Fatalf("empty-glyph fallback want two blank rows, got %q,%q", b[0], b[1])
	}
}
