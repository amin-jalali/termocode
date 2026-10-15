package statusbar

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Spec (UI review #7 + signature #3): the project pill is a soft tint, not
// a solid accent block, and its tint says what is going on: accent when
// idle, amber with unsaved changes, the AI colour while the AI works, red
// in a debug session.
func TestProjectPillTintFollowsState(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	m := New()
	base := State{Project: "proj", Path: "/p/a.go", Line: 1, Col: 1}
	cases := []struct {
		name string
		mod  func(*State)
		tok  theme.Color256
	}{
		{"idle", func(*State) {}, theme.BorderFocus},
		{"dirty", func(s *State) { s.Dirty = true }, theme.GitModified},
		{"ai", func(s *State) { s.AIState = AIStreaming; s.AI = "m" }, theme.AIAccent},
		{"debug", func(s *State) { s.Debug = true; s.Dirty = true }, theme.DiagError},
	}
	for _, c := range cases {
		s := base
		c.mod(&s)
		out := m.renderLeft(s)
		want := string(projectPillBg(s))
		r, g, b := hexRGB(t, want)
		if !hasBgNear(out, r, g, b) {
			t.Errorf("%s: pill bg %s not in %q", c.name, want, out)
		}
		if want == string(theme.LG(theme.BorderFocus)) {
			t.Errorf("%s: pill is a solid accent block", c.name)
		}
		if got := projectPillTint(s); got != c.tok {
			t.Errorf("%s: tint %v, want %v", c.name, got, c.tok)
		}
	}
}

// Spec (UI review #7): with the terminal focused the right side shows the
// shell and its folder instead of editor facts (indent, encoding,
// language, cursor), and there is no separate "● TERM" item any more.
func TestStatusRightWhenTerminalFocused(t *testing.T) {
	m := New()
	s := State{Project: "p", Path: "/p/a.go", Line: 3, Col: 4, Indent: "Spaces: 4", Lang: "Go", Term: true,
		TermFocused: true, TermLabel: "zsh · ~/proj"}
	out := ansi.Strip(m.renderRight(s))
	if !strings.Contains(out, "zsh · ~/proj") {
		t.Errorf("terminal label missing: %q", out)
	}
	for _, bad := range []string{"Spaces: 4", "UTF-8", "Ln 3", "TERM"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q shown while the terminal has focus: %q", bad, out)
		}
	}
	s.TermFocused = false
	out = ansi.Strip(m.renderRight(s))
	if !strings.Contains(out, "Ln 3, Col 4") || strings.Contains(out, "TERM") {
		t.Errorf("editor focused: %q", out)
	}
}

func hexRGB(t *testing.T, h string) (int, int, int) {
	t.Helper()
	c, ok := theme.ParseHex(h)
	if !ok {
		t.Fatalf("bad hex %q", h)
	}
	return int(c.R), int(c.G), int(c.B)
}

// hasBgNear reports a 24-bit background SGR within ±2 of r, g, b (colour
// conversion rounds).
func hasBgNear(out string, r, g, b int) bool {
	for _, m := range bgRe.FindAllStringSubmatch(out, -1) {
		x, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		z, _ := strconv.Atoi(m[3])
		if abs(x-r) <= 2 && abs(y-g) <= 2 && abs(z-b) <= 2 {
			return true
		}
	}
	return false
}

var bgRe = regexp.MustCompile(`48;2;(\d+);(\d+);(\d+)`)

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
