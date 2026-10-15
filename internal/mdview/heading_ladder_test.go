package mdview

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

// fgSGR is the 24-bit foreground SGR of a theme colour.
func fgSGR(c theme.Color256) string {
	r, _ := theme.ParseHex(theme.Hex(c))
	return "38;2;" + strconv.Itoa(int(r.R)) + ";" + strconv.Itoa(int(r.G)) + ";" + strconv.Itoa(int(r.B))
}

// Spec (UI review #11): one heading ladder. H1 is the brightest text with
// an accent rule under it, H2 is accent text with a thin rule, H3 is bold
// bright text with no marker, H4 and below are bold secondary text.
func TestHeadingLadder(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	withMode(t, theme.IconModeUnicode)
	rows := Render("# One\n\n## Two\n\n### Three\n\n#### Four\n", Options{Width: 100})
	row := func(text string) string {
		for _, r := range rows {
			if strings.Contains(r, text) {
				return r
			}
		}
		t.Fatalf("missing %q", text)
		return ""
	}
	for _, c := range []struct {
		text string
		fg   theme.Color256
	}{{"One", theme.TextWhite}, {"Two", theme.AccentBlue}, {"Three", theme.TextWhite}, {"Four", theme.TextSecondary}} {
		if r := row(c.text); !strings.Contains(r, fgSGR(c.fg)) {
			t.Errorf("%s: want fg %s in %q", c.text, fgSGR(c.fg), r)
		}
	}
}
