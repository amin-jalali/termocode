package statusbar

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Spec (old 5.3, WEB-10): the web chip is hidden while the panel is off,
// "◉ web on" with no browser, "◉ web connected" / "◉ web 2 connected"
// with browsers; its span sits before the agents chip.
func TestWebChip(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	if renderWebChip(State{WebBrowsers: 2}) != "" {
		t.Error("a chip with the panel off")
	}
	for n, want := range map[int]string{0: "◉ web on", 1: "◉ web connected", 2: "◉ web 2 connected"} {
		if got := stripCSI(renderWebChip(State{WebOn: true, WebBrowsers: n})); got != want {
			t.Errorf("%d browsers: chip = %q, want %q", n, got, want)
		}
	}
	m := Model{}
	m.SetWidth(160)
	s := State{Line: 1, Col: 1, Branch: "main", WebOn: true, WebBrowsers: 1, Agents: 1, ShowTests: true, TestsPassed: 2}
	x0, x1, ok := m.WebChipSpan(s)
	if !ok {
		t.Fatal("no span")
	}
	plain := []rune(stripCSI(m.View(s)))
	if got := string(plain[x0:x1]); got != "◉ web connected" {
		t.Errorf("span covers %q", got)
	}
	if ax, _, ok := m.AgentsChipSpan(s); !ok || x1 > ax {
		t.Errorf("web chip ends at %d, agents chip starts at %d", x1, ax)
	}
	off := s
	off.WebOn = false
	if _, _, ok := m.WebChipSpan(off); ok || strings.Contains(stripCSI(m.View(off)), "web") {
		t.Error("a web chip with the panel off")
	}
}
