package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// f cycles All → Mine → Agents → Dirty → Asleep → External → All, the
// title says which, and / filters by name; both are remembered for the
// workspace (the next start shows the same list).
func TestWorktreeFilters(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, _ := wtFixture(t)
	m := &Model{wt: st, focus: FocusExplorer, explorerWidth: 30, h: 42}
	var kinds []string
	for i := 0; i < 6; i++ {
		wtKey(t, m, "f")
		kinds = append(kinds, st.kind.String())
	}
	if got := strings.Join(kinds, " "); got != "Mine Agents Dirty Asleep External All" {
		t.Errorf("f cycles %q", got)
	}
	wtKey(t, m, "f") // Mine
	if title := stripANSI(strings.Split(m.renderWorktreesSidebar(29, 42), "\n")[0]); !strings.Contains(title, "Mine") {
		t.Errorf("title %q", title)
	}
	wtKey(t, m, "/")
	for _, r := range "toast" {
		wtKey(t, m, string(r))
	}
	nm, _ := m.handleWTSidebarKey(tea.KeyMsg{Type: tea.KeyEnter}) // ends typing, keeps the text
	*m = nm
	items, rows := m.wtList()
	var shown []string
	for _, r := range rows {
		if r.item >= 0 {
			shown = append(shown, items[r.item].e.Path)
		}
	}
	if strings.Join(shown, ",") != "/src/app.worktrees/toast" || st.cursorPath != "/src/app.worktrees/toast" {
		t.Errorf("shown %v cursor %q", shown, st.cursorPath)
	}
	// A new window of the same workspace starts with the same filters.
	st2, _ := wtFixture(t)
	m2 := &Model{wt: st2}
	m2.worktreesLoadSession()
	if st2.kind != wtKindMine || st2.filter != "toast" {
		t.Errorf("restored %v %q", st2.kind, st2.filter)
	}
	if _, ok := m.dispatchWorktreePalette("worktree-filter"); !ok || st.kind != wtKindAgents {
		t.Errorf("worktree-filter: %v", st.kind)
	}
	if p, ok := aiActionPolicyFor("worktree-filter"); !ok || p.Gate != aiGateAuto {
		t.Errorf("worktree-filter policy %+v", p)
	}
}
