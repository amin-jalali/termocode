package app

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// covSidebarRepo is a repo with a staged file, a changed file, an
// untracked file in a folder and three commits, shown as a tree.
func covSidebarRepo(t *testing.T) (*Model, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	covWrite(t, dir, "b.txt", "b\n")
	covCommit(t, dir, "second")
	covWrite(t, dir, "c.txt", "c\n")
	covCommit(t, dir, "third")
	covWrite(t, dir, "a.txt", "one\nTWO\nthree\n")
	covWrite(t, dir, "staged.txt", "s\n")
	covGit(t, dir, "add", "staged.txt")
	covWrite(t, dir, "sub/new.txt", "n\n")
	m.gitViewTree = true
	covRefresh(m)
	return m, dir
}

// Arrow keys, Enter and j/k/g/G walk and fold the accordion: sections,
// folders and date groups fold with Left and open with Right or Enter.
func TestCovGitSidebarFoldAndMove(t *testing.T) {
	m, _ := covSidebarRepo(t)
	isSec := func(s gitSection) func(gitPanelRow) bool {
		return func(r gitPanelRow) bool { return r.kind == gitRowSection && r.section == s }
	}

	covCursorTo(t, m, isSec(gitSecChanges))
	covKey(m, tea.KeyMsg{Type: tea.KeyLeft})
	if !m.gitChangesCollapsed {
		t.Fatal("Left on CHANGES did not fold it")
	}
	covKey(m, tea.KeyMsg{Type: tea.KeyRight})
	if m.gitChangesCollapsed {
		t.Fatal("Right on CHANGES did not open it")
	}
	covKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.gitChangesCollapsed {
		t.Fatal("Enter on CHANGES did not fold it")
	}
	covKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	dir := covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowDir })
	covKey(m, tea.KeyMsg{Type: tea.KeyLeft})
	if !m.gitCollapsed[dir.dirPath] {
		t.Fatalf("Left did not fold folder %q", dir.dirPath)
	}
	covKey(m, tea.KeyMsg{Type: tea.KeyLeft}) // folded already: stays folded
	if !m.gitCollapsed[dir.dirPath] {
		t.Fatal("a second Left opened the folder")
	}
	covKey(m, tea.KeyMsg{Type: tea.KeyRight})
	if m.gitCollapsed[dir.dirPath] {
		t.Fatal("Right did not open the folder")
	}
	covKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.gitCollapsed[dir.dirPath] {
		t.Fatal("Enter did not fold the folder")
	}
	covKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	grp := covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowGroup })
	covKey(m, tea.KeyMsg{Type: tea.KeyLeft})
	if !m.gitGroupFolded[grp.group] {
		t.Fatal("Left did not fold the date group")
	}
	covKey(m, tea.KeyMsg{Type: tea.KeyRight})
	if m.gitGroupFolded[grp.group] {
		t.Fatal("Right did not open the date group")
	}
	covKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.gitGroupFolded[grp.group] {
		t.Fatal("Enter did not fold the date group")
	}
	covKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Moving: G to the end, g to the top, j/k and the arrows step.
	covKey(m, covRunes("G"))
	last := m.gitCursor
	if last == 0 {
		t.Fatal("G did not move")
	}
	covKey(m, covRunes("k"))
	covKey(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.gitCursor >= last {
		t.Fatalf("k/Up did not move up: %d", m.gitCursor)
	}
	covKey(m, covRunes("g"))
	if m.gitCursor != 0 {
		t.Fatalf("g did not go to the top: %d", m.gitCursor)
	}
	covKey(m, covRunes("j"))
	covKey(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.gitCursor == 0 {
		t.Fatal("j/Down did not move")
	}

	// Keys the panel does not own pass through.
	if _, ok := covKey(m, covRunes("jj")); ok {
		t.Error("a two-rune key was taken")
	}
	if _, ok := covKey(m, covRunes("q")); ok {
		t.Error("q was taken")
	}
	if cmd, ok := covKey(m, covRunes("r")); !ok || cmd == nil {
		t.Error("r did not refresh")
	}
	m.gitCursor = 9999
	if _, ok := covKey(m, tea.KeyMsg{Type: tea.KeyEnter}); !ok {
		t.Error("Enter with no row under the cursor was not taken")
	}
}

// Not a repo: the panel keys do nothing and pass through.
func TestCovGitSidebarNotARepo(t *testing.T) {
	covGitEnv(t)
	m := covModelAt(t, t.TempDir())
	if m.gitIsRepo {
		t.Fatal("an empty folder is a repo")
	}
	if _, ok := covKey(m, covRunes("s")); ok {
		t.Error("s was taken outside a repo")
	}
}

// On a commit, Enter and d open the commit; F, R, y and Space work on it.
// On a file the commit-only keys do nothing.
func TestCovGitSidebarCommitKeys(t *testing.T) {
	m, _ := covSidebarRepo(t)
	c := covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowCommit })
	if cmd, _ := covKey(m, tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Errorf("Enter on commit %s opened nothing", c.hash)
	}
	if cmd, _ := covKey(m, covRunes("d")); cmd == nil {
		t.Error("d on a commit opened nothing")
	}
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowFile })
	for _, k := range []string{"F", "R", "y"} {
		if cmd, ok := covKey(m, covRunes(k)); !ok || cmd != nil {
			t.Errorf("%s on a file: cmd=%v taken=%v", k, cmd != nil, ok)
		}
	}
}

// d on a changed file opens a side-by-side diff with HEAD; its HEAD copy
// is a temp file that goes away once shown.
func TestCovGitSidebarDiffFile(t *testing.T) {
	m, _ := covSidebarRepo(t)
	covCursorTo(t, m, func(r gitPanelRow) bool {
		return r.kind == gitRowFile && r.section == gitSecChanges && m.gitFiles[r.fileIndex].Path == "a.txt"
	})
	cmd, _ := covKey(m, covRunes("d"))
	if cmd == nil {
		t.Fatal("d on a file opened nothing")
	}
	ready, ok := cmd().(gitDiffReadyMsg)
	if !ok {
		t.Fatalf("got %T", cmd())
	}
	if len(ready.hunkStarts) != 1 || ready.hunkStarts[0] != 1 || ready.totalLines != 4 {
		t.Errorf("hunks %v total %d", ready.hunkStarts, ready.totalLines)
	}
	head, _ := os.ReadFile(ready.headPath)
	if string(head) != "one\ntwo\nthree\n" {
		t.Errorf("HEAD copy = %q", head)
	}
	m.openSideBySideDiff(ready)
	if _, err := os.Stat(ready.headPath); !os.IsNotExist(err) {
		t.Error("the HEAD temp file was left behind")
	}
}

// s stages a changed file and unstages a staged one, with a toast; on a
// file that is gone git fails and the toast says so.
func TestCovGitSidebarStageToggle(t *testing.T) {
	m, dir := covSidebarRepo(t)
	pick := func(sec gitSection, path string) {
		covCursorTo(t, m, func(r gitPanelRow) bool {
			return r.kind == gitRowFile && r.section == sec && m.gitFiles[r.fileIndex].Path == path
		})
	}
	pick(gitSecChanges, "a.txt")
	covKey(m, covRunes("s"))
	if got := covGit(t, dir, "diff", "--cached", "--name-only"); got != "a.txt\nstaged.txt" {
		t.Fatalf("staged = %q", got)
	}
	if !toastHas(m, "Staged") {
		t.Error("no Staged toast")
	}
	covRefresh(m)
	pick(gitSecStaged, "staged.txt")
	covKey(m, covRunes("s"))
	if got := covGit(t, dir, "diff", "--cached", "--name-only"); got != "a.txt" {
		t.Fatalf("after unstage = %q", got)
	}
	if !toastHas(m, "Unstaged") {
		t.Error("no Unstaged toast")
	}

	// The list is stale: the changed file under the cursor was deleted
	// since, so staging it fails.
	covWrite(t, dir, "late.txt", "x\n")
	covRefresh(m)
	pick(gitSecChanges, "late.txt")
	if err := os.Remove(dir + "/late.txt"); err != nil {
		t.Fatal(err)
	}
	covKey(m, covRunes("s"))
	if !toastHas(m, "Git error") || m.err == "" {
		t.Errorf("no error shown: err=%q", m.err)
	}
	// No file under the cursor: s does nothing.
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowSection })
	if cmd, _ := covKey(m, covRunes("s")); cmd != nil {
		t.Error("s on a section header did something")
	}
}
