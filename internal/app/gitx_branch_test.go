package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/picker"
)

// gitxBranchRepo is covGitRepo (main) with a tag v1, a merged branch
// "done" and an unmerged branch "wip".
func gitxBranchRepo(t *testing.T) (*Model, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	covGit(t, dir, "tag", "v1")
	covGit(t, dir, "branch", "done")
	covGit(t, dir, "checkout", "-q", "-b", "wip")
	covWrite(t, dir, "w.txt", "w\n")
	covCommit(t, dir, "wip work")
	covGit(t, dir, "checkout", "-q", "main")
	covRefresh(m)
	return m, dir
}

func gitxIDs(m *Model) string {
	var ids []string
	for _, b := range m.confirm.Buttons() {
		ids = append(ids, b.ID)
	}
	return strings.Join(ids, ",")
}

// GITX-2: Rename Branch… asks for the new name (the old one filled in)
// and renames the current branch.
func TestGitxRenameBranch(t *testing.T) {
	m, dir := gitxBranchRepo(t)
	m.dispatchPaletteAction("git-branch-rename")
	if !m.promptOpen || m.promptKind != promptKindGitRenameBranch || m.prompt.Value() != "main" {
		t.Fatalf("no prompt with the name: %q", m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit("trunk")
	if got := covGit(t, dir, "branch", "--show-current"); got != "trunk" {
		t.Errorf("current %q", got)
	}
	if !toastHas(m, "Renamed main to trunk") {
		t.Error("no toast")
	}
}

// GITX-2: the delete picker lists the other branches, local and remote.
func TestGitxDeleteBranchItems(t *testing.T) {
	items := gitDeleteBranchItems([]string{"main", "wip"}, []string{"origin/wip"}, "main")
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	if got := strings.Join(ids, ","); got != "del-wip,delr-origin/wip" {
		t.Errorf("items %s", got)
	}
}

// GITX-2: deleting a merged branch asks on a Cancel-first card; an
// unmerged one then asks again before a force delete and names the tip
// that brings it back.
func TestGitxDeleteBranchSafeThenForce(t *testing.T) {
	m, dir := gitxBranchRepo(t)
	m.dispatchPaletteAction("git-branch-delete")
	if !m.pickerOpen || m.pickerKind != pickerKindDeleteBranch {
		t.Fatal("no picker")
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "del-done"})
	if !m.confirmOpen || m.confirmKind != confirmKindGitDeleteBranch || gitxIDs(m) != "cancel,delete" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %v %q", m.confirmOpen, gitxIDs(m))
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if !strings.Contains(covGit(t, dir, "branch"), "done") {
		t.Fatal("Cancel deleted")
	}
	m.pickerKind = pickerKindDeleteBranch
	m.handlePickerSelect(picker.SelectMsg{ID: "del-done"})
	m.confirmOpen = false
	m.handleConfirmSelect("delete")
	if strings.Contains(covGit(t, dir, "branch"), "done") || !toastHas(m, "Deleted branch done") {
		t.Fatal("not deleted")
	}

	tip := covGit(t, dir, "rev-parse", "--short", "wip")
	m.pickerKind = pickerKindDeleteBranch
	m.handlePickerSelect(picker.SelectMsg{ID: "del-wip"})
	m.confirmOpen = false
	m.handleConfirmSelect("delete")
	if !m.confirmOpen || m.confirmKind != confirmKindGitForceDeleteBranch || gitxIDs(m) != "cancel,force-delete" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no force card: %q", gitxIDs(m))
	}
	if v := m.confirm.View(); !strings.Contains(v, tip) {
		t.Errorf("force card does not name the tip %s: %q", tip, v)
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if !strings.Contains(covGit(t, dir, "branch"), "wip") {
		t.Fatal("Cancel force-deleted")
	}
	m.pickerKind = pickerKindDeleteBranch
	m.handlePickerSelect(picker.SelectMsg{ID: "del-wip"})
	m.confirmOpen = false
	m.handleConfirmSelect("delete")
	m.confirmOpen = false
	m.handleConfirmSelect("force-delete")
	if strings.Contains(covGit(t, dir, "branch"), "wip") || !toastHas(m, tip) {
		t.Error("force delete failed or toast misses the tip")
	}
}

// GITX-2: deleting a remote branch asks on a Cancel-first card, then for
// the typed name; a wrong name deletes nothing.
func TestGitxDeleteRemoteBranch(t *testing.T) {
	m, dir := gitxBranchRepo(t)
	remote := t.TempDir()
	covGit(t, remote, "init", "-q", "--bare")
	covGit(t, dir, "remote", "add", "origin", remote)
	covGit(t, dir, "push", "-q", "origin", "main", "wip")
	covGit(t, dir, "fetch", "-q", "origin")
	covRefresh(m)
	m.pickerKind = pickerKindDeleteBranch
	m.handlePickerSelect(picker.SelectMsg{ID: "delr-origin/wip"})
	if !m.confirmOpen || m.confirmKind != confirmKindGitDeleteRemote || gitxIDs(m) != "cancel,delete-remote" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %q", gitxIDs(m))
	}
	m.confirmOpen = false
	m.handleConfirmSelect("delete-remote")
	if !m.promptOpen || m.promptKind != promptKindGitDeleteRemoteTyped || m.prompt.Value() != "" {
		t.Fatal("no typed-confirm prompt")
	}
	m.promptOpen = false
	if cmd := m.handlePromptSubmit("wip"); cmd == nil || !toastHas(m, "did not match") {
		t.Error("a wrong name did not say so")
	}
	if !strings.Contains(covGit(t, remote, "branch"), "wip") {
		t.Fatal("deleted on a wrong name")
	}
	m.confirmOpen = false
	m.handleConfirmSelect("delete-remote")
	m.promptOpen = false
	cmd := m.handlePromptSubmit("origin/wip")
	m.gitRemoteDone(covMsg[gitRemoteDoneMsg](t, cmd))
	if strings.Contains(covGit(t, remote, "branch"), "wip") || !toastHas(m, "Deleted origin/wip") {
		t.Error("remote branch not deleted")
	}
}

// GITX-2: Create Branch From… picks a branch or tag, asks the name and
// switches to the new branch there.
func TestGitxCreateBranchFrom(t *testing.T) {
	m, dir := gitxBranchRepo(t)
	m.dispatchPaletteAction("git-branch-from")
	if !m.pickerOpen || m.pickerKind != pickerKindBranchFrom {
		t.Fatal("no picker")
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "from-wip"})
	if !m.promptOpen || m.promptKind != promptKindGitBranchFrom {
		t.Fatal("no name prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit("hot")
	if covGit(t, dir, "branch", "--show-current") != "hot" || covGit(t, dir, "rev-parse", "HEAD") != covGit(t, dir, "rev-parse", "wip") {
		t.Error("not on hot at wip")
	}
	if !toastHas(m, "Created branch hot") {
		t.Error("no toast")
	}
}

// GITX-2 (F3, K11): a GRAPH commit's menu creates a branch there or
// checks the commit out on its own.
func TestGitxGraphRowBranchAndDetach(t *testing.T) {
	m, dir := gitxBranchRepo(t)
	var ids []string
	for _, it := range gitCommitMenuItems() {
		ids = append(ids, it.ID)
	}
	if got := strings.Join(ids, ","); !strings.Contains(got, gitMenuBranchHere) || !strings.Contains(got, gitMenuCheckoutCommit) {
		t.Fatalf("menu %s", got)
	}
	first := covGit(t, dir, "rev-list", "--max-parents=0", "HEAD")
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowCommit && strings.HasPrefix(first, r.hash) })
	m.handleGitMenuSelect(gitMenuBranchHere)
	if !m.promptOpen || m.promptKind != promptKindGitBranchFrom {
		t.Fatal("no prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit("old")
	if covGit(t, dir, "rev-parse", "HEAD") != first || covGit(t, dir, "branch", "--show-current") != "old" {
		t.Error("branch not at the row's commit")
	}
	covRefresh(m)
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowCommit && strings.HasPrefix(first, r.hash) })
	m.handleGitMenuSelect(gitMenuCheckoutCommit)
	if covGit(t, dir, "rev-parse", "HEAD") != first || covGit(t, dir, "branch", "--show-current") != "" {
		t.Error("not detached at the commit")
	}
	if !toastHas(m, "Detached HEAD") {
		t.Error("no toast")
	}
}
