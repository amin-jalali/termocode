package statusbar

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/theme"
)

// AH-9: the ⇄ glyph shows at any width. A bar too narrow for the centre
// keeps a short agents chip ("⇄ 2", ready: "⇄"); its span covers it, the
// other centre chips claim no cells, and the row is the window's width.
func TestAgentsChipAnyWidth(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	m := Model{}
	base := State{Line: 120, Col: 40, Branch: "feature/long-branch-name", Path: "/home/u/project/internal/app/file.go",
		Errors: 3, ShowServices: true, ServicesUp: 1, ServicesTotal: 2}
	for w := 30; w <= 200; w += 2 {
		m.SetWidth(w)
		for _, tc := range []struct {
			agents int
			ready  bool
			short  string
			full   string
		}{{2, false, "⇄ 2", "⇄ 2 agents"}, {0, true, "⇄", "⇄ ready"}} {
			s := base
			s.Agents, s.AgentsReady = tc.agents, tc.ready
			plain := []rune(stripCSI(m.View(s)))
			if len(plain) != w {
				t.Fatalf("width %d: row is %d cells: %q", w, len(plain), string(plain))
			}
			x0, x1, ok := m.AgentsChipSpan(s)
			if !ok {
				t.Fatalf("width %d: no agents span: %q", w, string(plain))
			}
			got := string(plain[x0:x1])
			if got != tc.short && got != tc.full {
				t.Fatalf("width %d: span covers %q", w, got)
			}
			if got == tc.short {
				if _, _, ok := m.DiagSpan(s); ok {
					t.Errorf("width %d: a problems span with only the agents chip", w)
				}
				if _, _, ok := m.ServicesChipSpan(s); ok {
					t.Errorf("width %d: a services span with only the agents chip", w)
				}
				for _, it := range m.ItemSpans(s) {
					if it.ID == "branch" {
						t.Errorf("width %d: a branch span with only the agents chip", w)
					}
				}
			}
		}
	}
	m.SetWidth(30)
	if strings.Contains(stripCSI(m.View(base)), "⇄") {
		t.Error("the glyph shows with no server and no agent")
	}
}
