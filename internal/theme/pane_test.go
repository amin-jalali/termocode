package theme

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func fgOf(c Color256) string {
	r, _ := ParseHex(Hex(c))
	return "38;2;" + strconv.Itoa(int(r.R)) + ";" + strconv.Itoa(int(r.G)) + ";" + strconv.Itoa(int(r.B))
}

// Spec (UI review #6 + signature #1): the pane with the keyboard has its
// title in the accent and the lit hairline (accent fading out) under its
// header; other panes have a secondary title and a quiet hairline.
func TestPaneTitleAndHairlineFollowFocus(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	if PaneTitle(true) != LG(BorderFocus) || PaneTitle(false) != LG(TextSecondary) {
		t.Errorf("titles: %v / %v", PaneTitle(true), PaneTitle(false))
	}
	lit, quiet := PaneHairline(30, BgSidebar, true), PaneHairline(30, BgSidebar, false)
	for name, s := range map[string]string{"lit": lit, "quiet": quiet} {
		if w := ansi.StringWidth(s); w != 30 {
			t.Errorf("%s hairline is %d wide", name, w)
		}
	}
	if !strings.Contains(lit, fgOf(BorderFocus)) {
		t.Error("focused hairline is not lit with the accent")
	}
	if strings.Contains(quiet, fgOf(BorderFocus)) {
		t.Error("unfocused hairline uses the accent")
	}
}
