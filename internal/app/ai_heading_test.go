package app

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Spec (UI review #11): AI answers use the same heading ladder as the
// Markdown preview: H1 bright over a rule, H2 accent over a thin rule, H3
// bright, H4 secondary.
func TestAIHeadingLadder(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	fg := func(c theme.Color256) string {
		r, _ := theme.ParseHex(theme.Hex(c))
		return "38;2;" + strconv.Itoa(int(r.R)) + ";" + strconv.Itoa(int(r.G)) + ";" + strconv.Itoa(int(r.B))
	}
	const w = 60
	lines := renderChatMarkdown("# One\n\n## Two\n\n### Three\n\n#### Four\n", w, aiIndent, 0, false)
	var rows []string
	for _, l := range lines {
		rows = append(rows, renderAILine(l, w))
	}
	idx := func(text string) int {
		for i, r := range rows {
			if strings.Contains(r, text) {
				return i
			}
		}
		t.Fatalf("missing %q", text)
		return -1
	}
	for _, c := range []struct {
		text string
		fg   theme.Color256
	}{{"One", theme.TextWhite}, {"Two", theme.AccentBlue}, {"Three", theme.TextWhite}, {"Four", theme.TextSecondary}} {
		if r := rows[idx(c.text)]; !strings.Contains(r, fg(c.fg)) {
			t.Errorf("%s: want %s in %q", c.text, fg(c.fg), r)
		}
	}
	if !strings.Contains(stripSGRForTest(rows[idx("One")+1]), "━") {
		t.Error("no rule under H1")
	}
	if !strings.Contains(stripSGRForTest(rows[idx("Two")+1]), "─") {
		t.Error("no rule under H2")
	}
}
