package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/picker"
)

// gitxStashRepo is covGitRepo with a.txt changed and new.txt untracked.
func gitxStashRepo(t *testing.T) (*Model, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "ONE\ntwo\nthree\n")
	covWrite(t, dir, "new.txt", "n\n")
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

// GITX-10: Stash… picks what to stash, then asks a message.
func TestGitxStashOptions(t *testing.T) {
	m, dir := gitxStashRepo(t)
	m.dispatchPaletteAction("git-stash-options")
	if !m.pickerOpen || m.pickerKind != pickerKindStashMode {
		t.Fatal("no picker")
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "stash-untracked"})
	if !m.promptOpen || m.promptKind != promptKindStashMessage {
		t.Fatal("no message prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit("both files")
	if got := covGit(t, dir, "status", "--porcelain"); got != "" {
		t.Errorf("left %q", got)
	}
	if got := covGit(t, dir, "stash", "list", "--format=%gs"); got != "On main: both files" {
		t.Errorf("stash %q", got)
	}
	if !toastHas(m, "Stashed") {
		t.Error("no toast")
	}
}

// GITX-10: Only the current file stashes the editor's file.
func TestGitxStashCurrentFile(t *testing.T) {
	m, dir := gitxStashRepo(t)
	covWrite(t, dir, "b.txt", "b\n")
	covCommit(t, dir, "add b")
	covWrite(t, dir, "a.txt", "ONE\ntwo\nthree\n")
	covWrite(t, dir, "b.txt", "B\n")
	covRefresh(m)
	m.editor.SetMeta(filepath.Join(dir, "b.txt"), false, "text")
	m.dispatchPaletteAction("git-stash-options")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "stash-file"})
	m.promptOpen = false
	m.handlePromptSubmit("")
	if got := covGit(t, dir, "status", "--porcelain"); got != "M a.txt\n?? new.txt" {
		t.Errorf("status %q", got)
	}
}

// GITX-10: Stash Actions… shows a stash's changes, makes a branch from
// it, and drops it after a Cancel-first card that names the hash that
// brings it back.
func TestGitxStashActions(t *testing.T) {
	m, dir := gitxStashRepo(t)
	covGit(t, dir, "stash", "push", "-q", "-m", "first stash")
	hash := covGit(t, dir, "rev-parse", "stash@{0}")
	m.dispatchPaletteAction("git-stash-actions")
	if !m.pickerOpen || m.pickerKind != pickerKindStashPick {
		t.Fatal("no stash picker")
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "stash@{0}"})
	if !m.pickerOpen || m.pickerKind != pickerKindStashAction {
		t.Fatal("no action picker")
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "sa-diff"})
	if !strings.Contains(m.gitStashOps.page, "+ONE") {
		t.Errorf("diff page %q", m.gitStashOps.page)
	}
	m.pickerKind = pickerKindStashAction
	m.handlePickerSelect(picker.SelectMsg{ID: "sa-drop"})
	if !m.confirmOpen || m.confirmKind != confirmKindStashDrop || gitxButtons(m) != "cancel,drop" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %q", gitxButtons(m))
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if covGit(t, dir, "stash", "list") == "" {
		t.Fatal("Cancel dropped the stash")
	}
	m.pickerKind = pickerKindStashAction
	m.handlePickerSelect(picker.SelectMsg{ID: "sa-drop"})
	m.confirmOpen = false
	m.handleConfirmSelect("drop")
	if covGit(t, dir, "stash", "list") != "" || !toastHas(m, hash[:7]) {
		t.Fatal("not dropped, or the toast does not name the hash")
	}
	covGit(t, dir, "stash", "store", "-m", "first stash", hash)
	m.gitStashOps.ref = "stash@{0}"
	m.pickerKind = pickerKindStashAction
	m.handlePickerSelect(picker.SelectMsg{ID: "sa-branch"})
	if !m.promptOpen || m.promptKind != promptKindStashBranch {
		t.Fatal("no branch prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit("from-stash")
	if covGit(t, dir, "branch", "--show-current") != "from-stash" {
		t.Error("not on the new branch")
	}
}

// gitxResetRepo is covGitRepo with two more commits; the GRAPH cursor is
// on the first commit.
func gitxResetRepo(t *testing.T) (*Model, string, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "2\n")
	covCommit(t, dir, "second")
	covWrite(t, dir, "a.txt", "3\n")
	covCommit(t, dir, "third")
	covRefresh(m)
	first := covGit(t, dir, "rev-list", "--max-parents=0", "HEAD")
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowCommit && strings.HasPrefix(first, r.hash) })
	return m, dir, first
}

// GITX-15: Reset Current Branch to Here… picks a mode; a Cancel-first
// card names how many commits leave the branch and the hash that brings
// them back.
func TestGitxResetSoftFromGraph(t *testing.T) {
	m, dir, first := gitxResetRepo(t)
	old := covGit(t, dir, "rev-parse", "--short", "HEAD")
	var ids []string
	for _, it := range gitCommitMenuItems() {
		ids = append(ids, it.ID)
	}
	if !strings.Contains(strings.Join(ids, ","), gitMenuResetHere) {
		t.Fatalf("no Reset in the GRAPH menu: %v", ids)
	}
	m.handleGitMenuSelect(gitMenuResetHere)
	if !m.pickerOpen || m.pickerKind != pickerKindResetMode {
		t.Fatal("no mode picker")
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "reset-soft"})
	if !m.confirmOpen || m.confirmKind != confirmKindGitReset || gitxButtons(m) != "cancel,reset" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %q", gitxButtons(m))
	}
	if v := m.confirm.View(); !strings.Contains(v, "2 commits") || !strings.Contains(v, old) {
		t.Errorf("card text: %q", v)
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if covGit(t, dir, "rev-parse", "--short", "HEAD") != old {
		t.Fatal("Cancel reset")
	}
	m.pickerKind = pickerKindResetMode
	m.handlePickerSelect(picker.SelectMsg{ID: "reset-soft"})
	m.confirmOpen = false
	m.handleConfirmSelect("reset")
	if covGit(t, dir, "rev-parse", "HEAD") != first || covGit(t, dir, "status", "--porcelain") != "M  a.txt" {
		t.Error("not a soft reset to the first commit")
	}
}

// GITX-15: a hard reset wants the branch name typed, then saves the
// changes and HEAD first.
func TestGitxResetHardTyped(t *testing.T) {
	m, dir, first := gitxResetRepo(t)
	covWrite(t, dir, "a.txt", "unsaved\n")
	m.handleGitMenuSelect(gitMenuResetHere)
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "reset-hard"})
	m.confirmOpen = false
	m.handleConfirmSelect("reset")
	if !m.promptOpen || m.promptKind != promptKindResetHardTyped {
		t.Fatal("no typed prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit("mian")
	if covGit(t, dir, "rev-parse", "HEAD") == first || !toastHas(m, "did not match") {
		t.Fatal("reset on a wrong name")
	}
	m.handleGitMenuSelect(gitMenuResetHere)
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "reset-hard"})
	m.confirmOpen = false
	m.handleConfirmSelect("reset")
	m.promptOpen = false
	m.handlePromptSubmit("main")
	if covGit(t, dir, "rev-parse", "HEAD") != first {
		t.Fatal("not reset")
	}
	if !strings.Contains(covGit(t, dir, "stash", "list"), "before reset --hard") || !toastHas(m, "refs/termocode/backup/") {
		t.Error("the changes or HEAD were not saved first")
	}
}

// GITX-15: Uncommit Last Commit keeps the changes staged and names the
// hash that brings the commit back.
func TestGitxUncommit(t *testing.T) {
	m, dir, _ := gitxResetRepo(t)
	old := covGit(t, dir, "rev-parse", "--short", "HEAD")
	m.dispatchPaletteAction("git-uncommit")
	if covGit(t, dir, "log", "-1", "--format=%s") != "second" || covGit(t, dir, "status", "--porcelain") != "M  a.txt" {
		t.Error("not uncommitted")
	}
	if !toastHas(m, old) {
		t.Error("the toast does not name the old commit")
	}
}
