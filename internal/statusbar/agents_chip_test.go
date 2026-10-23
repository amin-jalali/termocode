package statusbar

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/theme"
)

// The agents chip "⇄ 1 agent" shows only while outside agents are
// connected to termocode's MCP server (M1 MS-3).
func TestAgentsChip(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	if renderAgentsChip(State{}) != "" {
		t.Error("a chip with no agent")
	}
	for n, want := range map[int]string{1: "⇄ 1 agent", 2: "⇄ 2 agents"} {
		if got := stripCSI(renderAgentsChip(State{Agents: n})); got != want {
			t.Errorf("%d agents: chip = %q, want %q", n, got, want)
		}
	}
	theme.CurrentIconMode = theme.IconModeASCII
	if got := stripCSI(renderAgentsChip(State{Agents: 1})); !strings.HasSuffix(got, " 1 agent") || strings.Contains(got, "⇄") {
		t.Errorf("ASCII chip = %q", got)
	}
}

// The bar shows the chip with agents and not without; the span covers it,
// before the services, database and test chips and never over them.
func TestAgentsChipSpan(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	m := Model{}
	m.SetWidth(160)
	s := State{Line: 1, Col: 1, Branch: "main", Agents: 1, ShowServices: true, ServicesUp: 3, ServicesTotal: 4,
		ShowDatabase: true, DatabaseName: "pg", ShowTests: true, TestsPassed: 2}
	x0, x1, ok := m.AgentsChipSpan(s)
	if !ok {
		t.Fatal("no span")
	}
	plain := []rune(stripCSI(m.View(s)))
	if got := string(plain[x0:x1]); got != "⇄ 1 agent" {
		t.Errorf("span covers %q", got)
	}
	if sx, _, ok := m.ServicesChipSpan(s); !ok || x1 > sx {
		t.Errorf("agents chip ends at %d, services chip starts at %d", x1, sx)
	}
	none := s
	none.Agents = 0
	if _, _, ok := m.AgentsChipSpan(none); ok {
		t.Error("a span with no agent")
	}
	if strings.Contains(stripCSI(m.View(none)), "agent") {
		t.Error("the bar shows the chip with no agent")
	}
}

// A narrow bar drops the centre whole (chip included) and stays one row
// of exactly the window's width.
func TestAgentsChipNarrow(t *testing.T) {
	m := Model{}
	for w := 20; w <= 120; w += 5 {
		m.SetWidth(w)
		out := stripCSI(m.View(State{Line: 1, Col: 1, Branch: "main", Agents: 2}))
		if got := len([]rune(out)); got != w {
			t.Fatalf("width %d: row is %d cells: %q", w, got, out)
		}
	}
}

// MCP-5: while the server listens and no agent is connected, a quiet chip
// "⇄ ready" shows (ASCII: "= ready"); a connected agent replaces it with
// the count; a server that does not listen shows nothing.
func TestAgentsChipReady(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	if got := stripCSI(renderAgentsChip(State{AgentsReady: true})); got != "⇄ ready" {
		t.Errorf("ready chip = %q", got)
	}
	if got := stripCSI(renderAgentsChip(State{AgentsReady: true, Agents: 2})); got != "⇄ 2 agents" {
		t.Errorf("with agents the chip = %q", got)
	}
	theme.CurrentIconMode = theme.IconModeASCII
	if got := stripCSI(renderAgentsChip(State{AgentsReady: true})); got != "= ready" {
		t.Errorf("ASCII ready chip = %q", got)
	}
	m := Model{}
	m.SetWidth(160)
	s := State{Line: 1, Col: 1, Branch: "main", AgentsReady: true}
	x0, x1, ok := m.AgentsChipSpan(s)
	if !ok {
		t.Fatal("no span for the ready chip")
	}
	if got := string([]rune(stripCSI(m.View(s)))[x0:x1]); got != "= ready" {
		t.Errorf("span covers %q", got)
	}
}
