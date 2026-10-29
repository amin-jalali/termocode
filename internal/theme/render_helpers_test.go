package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// withProfile renders with p for the rest of the test.
func withProfile(t *testing.T, p termenv.Profile) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(p)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

func TestHairlinesAreExactlyAsWideAsAsked(t *testing.T) {
	for _, p := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.Ascii} {
		withProfile(t, p)
		for _, w := range []int{-3, 0, 1, 2, 3, 7, 80} {
			want := w
			if want < 0 {
				want = 0
			}
			if got := ansi.StringWidth(Hairline(w, BgFloat)); got != want {
				t.Errorf("profile %v: Hairline(%d) is %d wide", p, w, got)
			}
			for _, focused := range []bool{true, false} {
				if got := ansi.StringWidth(PaneHairline(w, BgSidebar, focused)); got != want {
					t.Errorf("profile %v: PaneHairline(%d, %v) is %d wide", p, w, focused, got)
				}
			}
		}
	}
}

func TestGradientSpreadsAcrossTheStops(t *testing.T) {
	a, b, c := RGB{0, 0, 0}, RGB{100, 100, 100}, RGB{200, 200, 200}
	if got := Gradient(0, a, b); len(got) != 0 {
		t.Errorf("Gradient(0) = %v", got)
	}
	if got := Gradient(3); len(got) != 3 || got[0] != (RGB{}) {
		t.Errorf("Gradient with no stops = %v, want 3 zero colours", got)
	}
	for _, got := range Gradient(4, b) {
		if got != b {
			t.Errorf("one stop: got %v, want %v everywhere", got, b)
		}
	}
	if got := Gradient(1, a, c); got[0] != a {
		t.Errorf("one cell: got %v, want the first stop", got[0])
	}
	got := Gradient(5, a, b, c)
	if got[0] != a || got[2] != b || got[4] != c {
		t.Errorf("Gradient(5, a, b, c) = %v; want a, b, c at 0, 2, 4", got)
	}
}

func TestGradientTextKeepsEveryCharacter(t *testing.T) {
	stops := []RGB{{255, 0, 0}, {0, 0, 255}}
	withProfile(t, termenv.TrueColor)
	if got := GradientText("", BgEditor, false, stops...); got != "" {
		t.Errorf("empty text = %q", got)
	}
	out := GradientText("ab cd", BgEditor, true, stops...)
	if plain := ansi.Strip(out); plain != "ab cd" {
		t.Errorf("text = %q, want %q", plain, "ab cd")
	}
	if !strings.Contains(out, "38;2;255;0;0") || !strings.Contains(out, "38;2;0;0;255") {
		t.Errorf("truecolor gradient does not run from the first to the last stop: %q", out)
	}

	// Without truecolor the run is one flat colour (the middle stop).
	withProfile(t, termenv.ANSI256)
	flat := GradientText("abc", BgEditor, false, stops...)
	if ansi.Strip(flat) != "abc" || strings.Count(flat, "38;5;") != 1 {
		t.Errorf("256-colour gradient is not one flat run: %q", flat)
	}
	if got := GradientBar(0, "─", BgEditor, stops...); got != "" {
		t.Errorf("GradientBar(0) = %q", got)
	}
	if got := ansi.StringWidth(GradientBar(6, "─", BgEditor, stops...)); got != 6 {
		t.Errorf("GradientBar(6) is %d wide", got)
	}
}

func TestSGRFollowsTheColourProfile(t *testing.T) {
	red, blue := RGB{255, 0, 0}, RGB{0, 0, 255}
	withProfile(t, termenv.Ascii)
	if got := SGR(&red, &blue); got != "" {
		t.Errorf("no-colour terminal got %q", got)
	}
	if FgSeq(AccentBlue) != "" || BgSeq(BgEditor) != "" {
		t.Error("no-colour terminal got a colour sequence")
	}
	withProfile(t, termenv.TrueColor)
	if got := SGR(&red, &blue); got != "\x1b[38;2;255;0;0;48;2;0;0;255m" {
		t.Errorf("truecolor fg+bg = %q", got)
	}
	if got := SGR(nil, nil); got != "" {
		t.Errorf("no colours = %q", got)
	}
	withProfile(t, termenv.ANSI256)
	if got := FgSeq(AccentBlue); !strings.HasPrefix(got, "\x1b[38;5;") {
		t.Errorf("256-colour FgSeq = %q", got)
	}
	if got := BgSeq(BgEditor); !strings.HasPrefix(got, "\x1b[48;5;") {
		t.Errorf("256-colour BgSeq = %q", got)
	}
	withProfile(t, termenv.ANSI)
	if got := FgSeq(AccentBlue); strings.Contains(got, "38;5;") || strings.Contains(got, "38;2;") || got == "" {
		t.Errorf("16-colour FgSeq = %q, want a basic colour", got)
	}
}

func TestStyleHelpersPaintTheTokens(t *testing.T) {
	withProfile(t, termenv.TrueColor)
	if out := Fg(AccentBlue).Render("x"); !strings.Contains(out, fgOf(AccentBlue)) {
		t.Errorf("Fg: %q", out)
	}
	if out := Bg(BgEditor).Render("x"); !strings.Contains(out, "48;2;") {
		t.Errorf("Bg: %q", out)
	}
	out := FgBg(TextPrimary, BgEditor).Render("x")
	if !strings.Contains(out, fgOf(TextPrimary)) || !strings.Contains(out, "48;2;") {
		t.Errorf("FgBg: %q", out)
	}
}

func TestRenderKeyChangesWhenThePaletteChanges(t *testing.T) {
	restorePalette(t)
	before := CurrentRenderKey()
	ApplyTheme("one-dark")
	after := CurrentRenderKey()
	if after.Gen == before.Gen {
		t.Error("render key did not change after a theme switch")
	}
	if after.Icons != CurrentIconMode || after.Profile != lipgloss.ColorProfile() {
		t.Errorf("render key %+v does not match the live icon mode / profile", after)
	}
}

func TestPaletteWithoutAITokensBorrowsNeighbours(t *testing.T) {
	restorePalette(t)
	p := vscodeDarkPlusPalette()
	p.AIGhost, p.AIAccent = 0, 0
	SetPalette(p)
	if AIGhost != TextDim {
		t.Errorf("AIGhost = %d, want TextDim %d", AIGhost, TextDim)
	}
	if AIAccent != AccentLavender {
		t.Errorf("AIAccent = %d, want AccentLavender %d", AIAccent, AccentLavender)
	}
}

func TestReadableOnReachesTheContrast(t *testing.T) {
	restorePalette(t)
	bg := LG(BgEditor)
	got := ReadableOn(BgEditor, bg, 4.5) // text in the bg colour itself is unreadable
	gr, _ := ParseHex(string(got))
	if r := contrastRatio(gr, ToRGB(BgEditor)); r < 4.5 {
		t.Errorf("ReadableOn gave %s with contrast %.2f, want >= 4.5", got, r)
	}
	if got := readableOn("not-a-colour", "#000000", "#ffffff", 4.5); got != "not-a-colour" {
		t.Errorf("bad hex = %q, want it unchanged", got)
	}
	// A contrast no colour can reach ends on the body text colour.
	if got := readableOn("#000000", "#000000", "#ffffff", 50); got != "#ffffff" {
		t.Errorf("impossible contrast = %q, want the text colour", got)
	}
}

func TestBracketColoursCycle(t *testing.T) {
	restorePalette(t)
	saved := BracketPairs
	t.Cleanup(func() { BracketPairs = saved })
	BracketPairs = nil
	if got := bracket(3); got != AccentAmber {
		t.Errorf("no bracket colours: got %d, want AccentAmber", got)
	}
	BracketPairs = []Color256{1, 2, 3}
	if bracket(4) != 2 {
		t.Errorf("bracket(4) = %d, want the second colour", bracket(4))
	}
}

func TestSpecWithMissingOrBadColours(t *testing.T) {
	s := catppuccinMocha
	s.Cursor, s.LineNr = "", ""
	s.Pink = "not-hex"
	p := FromSpec(s)
	if Hex(p.BgEditor) == "" || p.IndexHex[p.BgEditor] != strings.ToLower(s.Base) {
		t.Errorf("BgEditor = %q, want %s", p.IndexHex[p.BgEditor], s.Base)
	}
	if p.IndexHex[p.Cursor] != p.IndexHex[p.TextPrimary] {
		t.Errorf("Cursor without its own colour = %q, want the text colour %q", p.IndexHex[p.Cursor], p.IndexHex[p.TextPrimary])
	}
}

func TestLightSpecKeepsChromeDarkerThanTheEditor(t *testing.T) {
	s := catppuccinLatte
	s.Mantle, s.Crust = "#ffffff", "#ffffff" // upstream-style: brighter than Base
	got := lightReadable(s)
	lum := func(h string) float64 { c, _ := ParseHex(h); return relLuminance(c) }
	if lum(got.Mantle) > lum(got.Base) {
		t.Errorf("Mantle %s brighter than Base %s", got.Mantle, got.Base)
	}
	if lum(got.Crust) > lum(got.Mantle) {
		t.Errorf("Crust %s brighter than Mantle %s", got.Crust, got.Mantle)
	}
}

func TestKeyLabelNamesEveryKey(t *testing.T) {
	for in, want := range map[string]string{
		"ctrl++":      "Ctrl++",
		"backspace":   "Backspace",
		"pgup":        "PgUp",
		"pgdown":      "PgDn",
		"home":        "Home",
		"end":         "End",
		"f5":          "F5",
		"shift+F12":   "Shift+F12",
		"alt+space":   "Alt+Space",
		"ctrl+return": "Ctrl+Enter",
		"⇧⇥":          "Shift+Tab",
		"fn":          "fn",
	} {
		if got := KeyLabel(in); got != want {
			t.Errorf("KeyLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPillWidthMatchesWhatPillDraws(t *testing.T) {
	saved := CurrentIconMode
	t.Cleanup(func() { CurrentIconMode = saved })
	withProfile(t, termenv.TrueColor)
	for _, mode := range []IconMode{IconModeNerdFont, IconModeUnicode, IconModeASCII} {
		CurrentIconMode = mode
		for _, text := range []string{"main", "", "سلام", "日本"} {
			out := Pill(text, LG(TextWhite), LG(AccentBlue), LG(BgStatusBar), true)
			if got, want := ansi.StringWidth(out), PillWidth(text); got != want {
				t.Errorf("mode %v: Pill(%q) is %d wide, PillWidth says %d", mode, text, got, want)
			}
			if !strings.Contains(ansi.Strip(out), text) {
				t.Errorf("mode %v: Pill(%q) lost the text: %q", mode, text, ansi.Strip(out))
			}
		}
	}
}

func TestHintSpansMatchTheDrawnBar(t *testing.T) {
	withProfile(t, termenv.TrueColor)
	pairs := []string{"↵", "open", "esc", "close", "ctrl+r", "rename"}
	bar := ansi.Strip(HintBar(LG(BgFloat), pairs...))
	spans := HintSpans(pairs...)
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3", len(spans))
	}
	for i, sp := range spans {
		if sp.Key != pairs[2*i] {
			t.Errorf("span %d key = %q, want %q", i, sp.Key, pairs[2*i])
		}
		text := ansi.Cut(bar, sp.X0, sp.X1)
		if !strings.Contains(text, KeyLabel(pairs[2*i])) || !strings.HasSuffix(text, pairs[2*i+1]) {
			t.Errorf("span %d covers %q, want the %q hint", i, text, pairs[2*i])
		}
	}
	if last := spans[len(spans)-1]; last.X1 != ansi.StringWidth(bar) {
		t.Errorf("last span ends at %d, bar is %d wide", last.X1, ansi.StringWidth(bar))
	}
	if got := HintSpans("lonely"); len(got) != 0 {
		t.Errorf("odd pair list gave %v", got)
	}
}

func TestFloatBoxIsAsWideAsAsked(t *testing.T) {
	withProfile(t, termenv.TrueColor)
	out := FloatBox("hello", 20)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("FloatBox has %d lines, want 3", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 20 {
			t.Errorf("line %d is %d wide, want 20", i, w)
		}
	}
	if !strings.Contains(lines[1], "hello") {
		t.Errorf("content row = %q", ansi.Strip(lines[1]))
	}
}

func TestResolveIconModeReadsTheEnvironment(t *testing.T) {
	t.Setenv("TERMOCODE_ICON_MODE", "nerd_font")
	t.Setenv("TERMOCODE_NERD_FONT", "")
	if got := ResolveIconMode("ascii", false, nil, nil); got != IconModeNerdFont {
		t.Errorf("TERMOCODE_ICON_MODE=nerd_font gave %v", got)
	}
	t.Setenv("TERMOCODE_ICON_MODE", "Unicode")
	if got := ResolveIconMode("nerd", false, nil, nil); got != IconModeUnicode {
		t.Errorf("TERMOCODE_ICON_MODE=Unicode gave %v", got)
	}
	t.Setenv("TERMOCODE_ICON_MODE", "sparkles") // unknown: the setting decides
	if got := ResolveIconMode("ascii", false, nil, nil); got != IconModeASCII {
		t.Errorf("unknown TERMOCODE_ICON_MODE gave %v, want the setting (ascii)", got)
	}
}

func TestDetectProfileReadsTheEnvironment(t *testing.T) {
	t.Setenv("TERMOCODE_COLORS", "ansi256")
	if got := DetectProfile(nil); got != termenv.ANSI256 {
		t.Errorf("TERMOCODE_COLORS=ansi256 gave %v", got)
	}
}

func TestLoadAndApplyUsesTheSavedTheme(t *testing.T) {
	restorePalette(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got := LoadAndApply(); got.ID != "vscode-dark-plus" {
		t.Errorf("no config: theme %s, want the default", got.ID)
	}
	if err := SaveThemeID("one-dark"); err != nil {
		t.Fatal(err)
	}
	got := LoadAndApply()
	if got.ID != "one-dark" || BgEditor != oneDarkPalette().BgEditor {
		t.Errorf("saved one-dark: theme %s, BgEditor %d", got.ID, BgEditor)
	}
	if err := SaveThemeID("gone-theme"); err != nil {
		t.Fatal(err)
	}
	if got := LoadAndApply(); got.ID != "vscode-dark-plus" {
		t.Errorf("unknown saved theme: %s, want the default", got.ID)
	}
}

func TestThemeConfigWithoutAHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	if p, err := ConfigPath(); err != nil || p != filepath.Join(home, ".config", "termocode", "config.json") {
		t.Errorf("ConfigPath = %q, %v", p, err)
	}
	t.Setenv("HOME", "")
	if _, err := ConfigPath(); err == nil {
		t.Skip("this platform finds a home folder without $HOME")
	}
	if got := LoadThemeID(); got != "" {
		t.Errorf("LoadThemeID = %q, want empty", got)
	}
	if err := SaveThemeID("one-dark"); err == nil {
		t.Error("SaveThemeID without a home: want an error")
	}
}

func TestThemeConfigThatIsAFolderLoadsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "termocode", "config.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := LoadThemeID(); got != "" {
		t.Errorf("LoadThemeID on a folder = %q", got)
	}
}

func TestThemeIDWithRepeatedSeparators(t *testing.T) {
	for _, in := range []string{"  VSCode  Dark + ", "-one--dark-", "Solarized__Dark"} {
		if _, ok := LookupTheme(in); !ok {
			t.Errorf("LookupTheme(%q) missed", in)
		}
	}
}
