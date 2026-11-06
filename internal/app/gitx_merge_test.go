package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/picker"
)

// gitxMergeRepo is covGitRepo with a branch "side" one commit ahead
// (b.txt); conflict also makes main and side change a.txt's first line.
func gitxMergeRepo(t *testing.T, conflict bool) (*Model, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	covGit(t, dir, "checkout", "-q", "-b", "side")
	covWrite(t, dir, "b.txt", "b\n")
	if conflict {
		covWrite(t, dir, "a.txt", "SIDE\ntwo\nthree\n")
	}
	covCommit(t, dir, "side work")
	covGit(t, dir, "checkout", "-q", "main")
	if conflict {
		covWrite(t, dir, "a.txt", "MAIN\ntwo\nthree\n")
		covCommit(t, dir, "main work")
	}
	covRefresh(m)
	return m, dir
}

func gitxButtons(m *Model) string {
	var ids []string
	for _, b := range m.confirm.Buttons() {
		ids = append(ids, b.ID)
	}
	return strings.Join(ids, ",")
}

// GITX-1: the picker lists every branch but the current one, remote ones
// too.
func TestGitxMergeBranchItems(t *testing.T) {
	items := gitMergeBranchItems([]string{"main", "side", "feat/x"}, []string{"origin/main", "origin/side"}, "main")
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	if got := strings.Join(ids, ","); got != "mg-side,mg-feat/x,mg-origin/main,mg-origin/side" {
		t.Errorf("items %s", got)
	}
}

// GITX-1: Merge Branch… → pick → a Cancel-first card counting the
// commits that come in; Cancel merges nothing; Merge fast-forwards.
func TestGitxMergeBranchFlow(t *testing.T) {
	m, dir := gitxMergeRepo(t, false)
	m.dispatchPaletteAction("git-merge-branch")
	if !m.pickerOpen || m.pickerKind != pickerKindMergeBranch {
		t.Fatal("no branch picker")
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "mg-side"})
	if !m.confirmOpen || m.confirmKind != confirmKindGitMergeBranch {
		t.Fatal("no card")
	}
	if got := gitxButtons(m); got != "cancel,merge,merge-no-ff" || m.confirm.SafeID() != "cancel" {
		t.Errorf("buttons %q", got)
	}
	if v := m.confirm.View(); !strings.Contains(v, "1 commit") || !strings.Contains(v, "side") {
		t.Errorf("card text %q", v)
	}
	m.confirmOpen = false
	if cmd := m.handleConfirmSelect("cancel"); cmd != nil {
		t.Error("Cancel ran something")
	}
	head := covGit(t, dir, "rev-parse", "HEAD")

	m.handlePickerSelect(picker.SelectMsg{ID: "mg-side"})
	m.confirmOpen = false
	cmd := m.handleConfirmSelect("merge")
	m.onGitMergeDone(covMsg[gitMergeDoneMsg](t, cmd))
	if covGit(t, dir, "rev-parse", "HEAD") == head || covGit(t, dir, "rev-parse", "HEAD") != covGit(t, dir, "rev-parse", "side") {
		t.Error("not merged")
	}
	if !toastHas(m, "Merged side into main") {
		t.Error("no toast")
	}
	// Merged already: a note, no card.
	m.handlePickerSelect(picker.SelectMsg{ID: "mg-side"})
	if m.confirmOpen || !toastHas(m, "Already up to date") {
		t.Error("card for a merged branch")
	}
}

// GITX-1: Merge (no fast-forward) makes a merge commit.
func TestGitxMergeBranchNoFF(t *testing.T) {
	m, dir := gitxMergeRepo(t, false)
	m.pickerKind = pickerKindMergeBranch
	m.handlePickerSelect(picker.SelectMsg{ID: "mg-side"})
	m.confirmOpen = false
	m.onGitMergeDone(covMsg[gitMergeDoneMsg](t, m.handleConfirmSelect("merge-no-ff")))
	if n := len(strings.Fields(covGit(t, dir, "log", "-1", "--format=%P"))); n != 2 {
		t.Errorf("parents %d", n)
	}
}

// GITX-1 (I11): a conflicting merge stops; the panel shows a banner whose
// card (Close first) aborts or continues the merge.
func TestGitxMergeConflictBanner(t *testing.T) {
	m, dir := gitxMergeRepo(t, true)
	m.pickerKind = pickerKindMergeBranch
	m.handlePickerSelect(picker.SelectMsg{ID: "mg-side"})
	m.confirmOpen = false
	m.onGitMergeDone(covMsg[gitMergeDoneMsg](t, m.handleConfirmSelect("merge")))
	if !toastHas(m, "Merge stopped") {
		t.Error("no stop toast")
	}
	covRefresh(m)
	if m.gitStopped != "merge" {
		t.Fatalf("stopped %q", m.gitStopped)
	}
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowStopped })
	covKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.confirmOpen || m.confirmKind != confirmKindGitOpControl {
		t.Fatal("no control card")
	}
	if got := gitxButtons(m); got != "op-close,op-abort,op-continue" || m.confirm.SafeID() != "op-close" {
		t.Errorf("buttons %q", got)
	}
	if v := m.confirm.View(); !strings.Contains(v, "a.txt") {
		t.Errorf("card does not name the conflict: %q", v)
	}
	m.confirmOpen = false
	if cmd := m.handleConfirmSelect("op-close"); cmd != nil {
		t.Error("Close ran something")
	}

	// Continue with markers left: an error, the merge stays.
	m.onGitOpDone(covMsg[gitOpDoneMsg](t, m.onGitOpControl("op-continue")))
	if !toastHas(m, "a.txt") {
		t.Error("continue did not name the file left")
	}
	covWrite(t, dir, "a.txt", "BOTH\ntwo\nthree\n")
	m.onGitOpDone(covMsg[gitOpDoneMsg](t, m.onGitOpControl("op-continue")))
	covRefresh(m)
	if m.gitStopped != "" || len(strings.Fields(covGit(t, dir, "log", "-1", "--format=%P"))) != 2 {
		t.Error("merge not finished")
	}
	for _, r := range m.gitPanelRows() {
		if r.kind == gitRowStopped {
			t.Error("banner still shown")
		}
	}
}

// GITX-1: Abort from the palette card puts the branch back.
func TestGitxMergeAbortFromPalette(t *testing.T) {
	m, dir := gitxMergeRepo(t, true)
	head := covGit(t, dir, "rev-parse", "HEAD")
	m.dispatchPaletteAction("git-merge-status")
	if m.confirmOpen || !toastHas(m, "No merge in progress") {
		t.Fatal("card with nothing stopped")
	}
	_, _ = covGitCmd(dir, "merge", "-q", "side").CombinedOutput()
	covRefresh(m)
	m.dispatchPaletteAction("git-merge-status")
	if !m.confirmOpen || m.confirmKind != confirmKindGitOpControl {
		t.Fatal("no card")
	}
	m.confirmOpen = false
	m.onGitOpDone(covMsg[gitOpDoneMsg](t, m.handleConfirmSelect("op-abort")))
	covRefresh(m)
	if m.gitStopped != "" || covGit(t, dir, "rev-parse", "HEAD") != head || !toastHas(m, "Merge aborted") {
		t.Error("abort did not put the branch back")
	}
}
