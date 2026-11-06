package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// GITX-10 (L18): the Source Control panel has a STASHES section when
// the repo has stashes. It starts folded (the count is on the header);
// Enter or a click on a stash row opens that stash's actions.
func TestGitStashPanelSection(t *testing.T) {
	m, dir := covGitRepo(t)
	hasSection := func() bool {
		for _, r := range m.gitPanelRows() {
			if r.kind == gitRowSection && r.section == gitSecStashes {
				return true
			}
		}
		return false
	}
	if hasSection() {
		t.Fatal("STASHES shown with no stash")
	}

	covWrite(t, dir, "a.txt", "changed\n")
	covGit(t, dir, "stash", "push", "-q", "-m", "half done")
	covWrite(t, dir, "a.txt", "changed again\n")
	covGit(t, dir, "stash", "push", "-q", "-m", "second try")
	covRefresh(m)
	if !hasSection() {
		t.Fatal("no STASHES section")
	}
	for _, r := range m.gitPanelRows() {
		if r.kind == gitRowStash {
			t.Fatal("STASHES is open at start; want folded")
		}
	}
	view := ansi.Strip(m.renderGitSidebar(60, 40))
	if !strings.Contains(view, "STASHES") || !strings.Contains(view, "2") {
		t.Errorf("header missing:\n%s", view)
	}

	// Enter on the header opens the section.
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowSection && r.section == gitSecStashes })
	covKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	var stashRows []gitPanelRow
	for _, r := range m.gitPanelRows() {
		if r.kind == gitRowStash {
			stashRows = append(stashRows, r)
		}
	}
	if len(stashRows) != 2 {
		t.Fatalf("stash rows %+v", stashRows)
	}
	view = ansi.Strip(m.renderGitSidebar(60, 40))
	if !strings.Contains(view, "second try") || !strings.Contains(view, "half done") {
		t.Errorf("stash messages missing:\n%s", view)
	}
	// The section sits above GRAPH.
	if strings.Index(view, "STASHES") > strings.Index(view, "GRAPH") {
		t.Errorf("STASHES below GRAPH:\n%s", view)
	}

	// Enter on a stash row: its actions.
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowStash && r.stash == 1 })
	covKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.pickerOpen || m.pickerKind != pickerKindStashAction || m.gitStashOps.ref != "stash@{1}" {
		t.Fatalf("Enter: picker %v kind %v ref %q", m.pickerOpen, m.pickerKind, m.gitStashOps.ref)
	}

	// A click on a stash row does the same.
	m.pickerOpen = false
	idx := -1
	for i, r := range m.gitPanelRows() {
		if r.kind == gitRowStash && r.stash == 0 {
			idx = i
		}
	}
	m.explorerWidth = 40
	top, _, _ := m.gitPanelLayout(m.h)
	m2, _ := m.handleGitSidebarMouse(2, m.gitPanelTopOffset()+idx-top, tea.MouseLeft)
	*m = m2
	if !m.pickerOpen || m.pickerKind != pickerKindStashAction || m.gitStashOps.ref != "stash@{0}" {
		t.Fatalf("click: picker %v kind %v ref %q", m.pickerOpen, m.pickerKind, m.gitStashOps.ref)
	}

	// Left on the header folds it again.
	m.pickerOpen = false
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowSection && r.section == gitSecStashes })
	covKey(m, tea.KeyMsg{Type: tea.KeyLeft})
	for _, r := range m.gitPanelRows() {
		if r.kind == gitRowStash {
			t.Fatal("Left did not fold STASHES")
		}
	}

	// All stashes gone: the section goes too.
	covGit(t, dir, "stash", "clear")
	covRefresh(m)
	if hasSection() {
		t.Error("STASHES stays after stash clear")
	}
}
