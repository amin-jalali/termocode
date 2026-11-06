package confirm

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// GITX-21: WithDiff shows the lines of a change in a frame below the
// message: added lines and removed lines in their own colours, a long
// line cut with … (never wrapped), a long change cut to a "more lines"
// row, and the buttons under it.
func TestWithDiff(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	lines := []string{" ctx", "-old line", "+new line", "+" + strings.Repeat("x", 300)}
	for i := 0; i < 40; i++ {
		lines = append(lines, "+more")
	}
	m := Risky("Change 1 of 2", "Lines 2-3 of a.txt", Button{ID: "close", Title: "Close"},
		Button{ID: "stage", Title: "Stage"}).WithDiff(lines)
	m.SetSize(100, 50)
	view := m.View()
	plain := ansiRe.ReplaceAllString(view, "")
	for _, want := range []string{"-old line", "+new line", " ctx", "…", "more lines"} {
		if !strings.Contains(plain, want) {
			t.Errorf("misses %q\n%s", want, plain)
		}
	}
	if strings.Contains(plain, strings.Repeat("x", 120)) {
		t.Error("a long line was not cut")
	}
	if strings.Index(plain, "Close") < strings.LastIndex(plain, "+new line") {
		t.Error("the buttons are not under the change")
	}
	// The added and the removed line are drawn in different colours.
	add, del := lineOf(view, "+new line"), lineOf(view, "-old line")
	if add == "" || del == "" || colourBefore(add, "+new line") == colourBefore(del, "-old line") {
		t.Errorf("added and removed lines share a colour:\n%q\n%q", add, del)
	}
	if x, y, w, h := m.Bounds(); w <= 0 || h <= 0 || x < 0 || y < 0 {
		t.Fatal("no bounds")
	}
}

func lineOf(view, s string) string {
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(ansiRe.ReplaceAllString(l, ""), s) {
			return l
		}
	}
	return ""
}

// colourBefore is the last escape code before s in the raw line.
func colourBefore(line, s string) string {
	i := strings.Index(line, s)
	if i < 0 {
		return ""
	}
	codes := ansiRe.FindAllString(line[:i], -1)
	if len(codes) == 0 {
		return ""
	}
	return codes[len(codes)-1]
}
