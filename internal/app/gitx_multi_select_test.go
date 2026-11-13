//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// gitSelRepo: a.txt edited, b.txt and c.txt new; the panel in flat view.
func gitSelRepo(t *testing.T) (*Model, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "edited\n")
	covWrite(t, dir, "b.txt", "b\n")
	covWrite(t, dir, "c.txt", "c\n")
	covRefresh(m)
	m.explorerWidth = 40
	return m, dir
}

// gitSelCursor puts the cursor on path's row in section sec.
func gitSelCursor(t *testing.T, m *Model, sec gitSection, path string) {
	t.Helper()
	covCursorTo(t, m, func(r gitPanelRow) bool {
		return r.kind == gitRowFile && r.section == sec && m.gitFiles[r.fileIndex].Path == path
	})
}

func gitSelStatus(t *testing.T, dir string) string {
	t.Helper()
	return covGit(t, dir, "status", "--porcelain")
}

// GITX-20 (A22): Space marks file rows; s then stages every marked row
// of CHANGES (and unstages every marked row of STAGED); the marks go
// after the action.
func TestGitMultiSelectStage(t *testing.T) {
	m, dir := gitSelRepo(t)
	gitSelCursor(t, m, gitSecChanges, "a.txt")
	covKey(m, tea.KeyMsg{Type: tea.KeySpace})
	gitSelCursor(t, m, gitSecChanges, "b.txt")
	covKey(m, tea.KeyMsg{Type: tea.KeySpace})
	if n := m.gitSelCount(); n != 2 {
		t.Fatalf("marked %d rows, want 2", n)
	}
	// Space again unmarks; once more marks.
	covKey(m, tea.KeyMsg{Type: tea.KeySpace})
	if n := m.gitSelCount(); n != 1 {
		t.Fatalf("Space did not unmark: %d", n)
	}
	covKey(m, tea.KeyMsg{Type: tea.KeySpace})
	// The header of the panel says how many are marked.
	if v := ansi.Strip(m.renderGitSidebar(40, 40)); !strings.Contains(v, "2 selected") {
		t.Errorf("no \"2 selected\" in the panel:\n%s", v)
	}

	covKey(m, covRunes("s"))
	covRefresh(m)
	if got := gitSelStatus(t, dir); got != "M  a.txt\nA  b.txt\n?? c.txt" {
		t.Fatalf("after s: %q", got)
	}
	if n := m.gitSelCount(); n != 0 {
		t.Errorf("marks stay after the action: %d", n)
	}

	// Shift+Down marks a range in STAGED; s unstages both.
	gitSelCursor(t, m, gitSecStaged, "a.txt")
	covKey(m, tea.KeyMsg{Type: tea.KeyShiftDown})
	if n := m.gitSelCount(); n != 2 {
		t.Fatalf("Shift+Down marked %d rows, want 2", n)
	}
	covKey(m, covRunes("s"))
	covRefresh(m)
	if got := gitSelStatus(t, dir); got != "M a.txt\n?? b.txt\n?? c.txt" {
		t.Fatalf("after unstage: %q", got)
	}

	// Esc drops the marks.
	gitSelCursor(t, m, gitSecChanges, "a.txt")
	covKey(m, tea.KeyMsg{Type: tea.KeySpace})
	covKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if n := m.gitSelCount(); n != 0 {
		t.Errorf("Esc kept %d marks", n)
	}
}

// GITX-20: x with marked rows asks once on a Cancel-first card; Cancel
// keeps everything; Discard takes the marked files back (new ones are
// deleted) and keeps a safety copy in the stash list.
func TestGitMultiSelectDiscard(t *testing.T) {
	m, dir := gitSelRepo(t)
	gitSelCursor(t, m, gitSecChanges, "a.txt")
	covKey(m, tea.KeyMsg{Type: tea.KeySpace})
	gitSelCursor(t, m, gitSecChanges, "c.txt")
	covKey(m, tea.KeyMsg{Type: tea.KeySpace})

	covKey(m, covRunes("x"))
	checkCardRule(t, "discard marked", *m, true)
	if box := ansi.Strip(m.confirm.Box()); !strings.Contains(box, "2 files") {
		t.Errorf("card does not say how many:\n%s", box)
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if got := gitSelStatus(t, dir); got != "M a.txt\n?? b.txt\n?? c.txt" {
		t.Fatalf("Cancel changed files: %q", got)
	}

	covKey(m, covRunes("x"))
	m.confirmOpen = false
	m.handleConfirmSelect("discard")
	if got := gitSelStatus(t, dir); got != "?? b.txt" {
		t.Fatalf("after Discard: %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "one\ntwo\nthree\n" {
		t.Errorf("a.txt %q", b)
	}
	if got := covGit(t, dir, "stash", "list"); !strings.Contains(got, "safety copy") {
		t.Errorf("no safety copy: %q", got)
	}
	if n := m.gitSelCount(); n != 0 {
		t.Errorf("marks stay after discard: %d", n)
	}
}

// GITX-20: Ctrl+click marks or unmarks a file row, Shift+click marks
// the rows from the last one; a plain click drops the marks.
func TestGitMultiSelectMouse(t *testing.T) {
	m, _ := gitSelRepo(t)
	yOf := func(sec gitSection, path string) int {
		top, _, _ := m.gitPanelLayout(m.h)
		for i, r := range m.gitPanelRows() {
			if r.kind == gitRowFile && r.section == sec && m.gitFiles[r.fileIndex].Path == path {
				return m.gitPanelTopOffset() + i - top
			}
		}
		t.Fatalf("no row %s", path)
		return 0
	}
	click := func(path string, ctrl, shift bool) {
		m2, _ := m.handleGitSidebarMouseMod(2, yOf(gitSecChanges, path), tea.MouseLeft, ctrl, shift)
		*m = m2
	}
	click("a.txt", true, false)
	if n := m.gitSelCount(); n != 1 {
		t.Fatalf("Ctrl+click: %d marks", n)
	}
	click("c.txt", false, true)
	if n := m.gitSelCount(); n != 3 {
		t.Fatalf("Shift+click: %d marks, want 3", n)
	}
	click("b.txt", true, false)
	if n := m.gitSelCount(); n != 2 {
		t.Fatalf("Ctrl+click on a marked row: %d marks, want 2", n)
	}
	click("b.txt", false, false)
	if n := m.gitSelCount(); n != 0 {
		t.Errorf("plain click kept %d marks", n)
	}
}
