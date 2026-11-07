package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/picker"
)

// GITX-39 (J13): Git: Move Last Commit to Another Branch… picks a new or
// an existing branch, asks on a Cancel-first card, then moves the commit
// there; the current branch drops it and Cancel changes nothing.
func TestGitxMoveCommit(t *testing.T) {
	m, dir := covGitRepo(t)
	m.dispatchPaletteAction("git-move-commit")
	if m.pickerOpen || !toastHas(m, "first commit") {
		t.Fatal("the first commit is offered")
	}
	covGit(t, dir, "branch", "other")
	covWrite(t, dir, "b.txt", "b\n")
	covCommit(t, dir, "feat: b")
	head := covGit(t, dir, "rev-parse", "HEAD")
	covRefresh(m)
	m.dispatchPaletteAction("git-move-commit")
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "move-new,move-to:other" {
		t.Fatalf("picker %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "move-to:other"})
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,move" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %q", gitxCardIDList(m))
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if covGit(t, dir, "rev-parse", "HEAD") != head || covGit(t, dir, "log", "--format=%s", "other") != "first" {
		t.Fatal("Cancel moved the commit")
	}
	m.dispatchPaletteAction("git-move-commit")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "move-to:other"})
	m.confirmOpen = false
	m.handleConfirmSelect("move")
	if got := covGit(t, dir, "log", "--format=%s", "other"); got != "feat: b\nfirst" {
		t.Errorf("other %q", got)
	}
	if got := covGit(t, dir, "log", "--format=%s", "main"); got != "first" {
		t.Errorf("main %q", got)
	}
	if !toastHas(m, "other") {
		t.Error("no note of the move")
	}

	covWrite(t, dir, "c.txt", "c\n")
	covCommit(t, dir, "feat: c")
	covRefresh(m)
	m.dispatchPaletteAction("git-move-commit")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "move-new"})
	if !m.promptOpen {
		t.Fatal("no branch name prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit("feat/c")
	if !m.confirmOpen || m.confirm.SafeID() != "cancel" {
		t.Fatal("no card for a new branch")
	}
	m.confirmOpen = false
	m.handleConfirmSelect("move")
	if covGit(t, dir, "log", "-1", "--format=%s", "feat/c") != "feat: c" || covGit(t, dir, "log", "--format=%s", "main") != "first" {
		t.Error("not moved to the new branch")
	}
	if p := aiActionPolicies["git-move-commit"]; p.Gate != aiGateNever {
		t.Error("the AI may rewrite the user's branches")
	}
}

// A pushed commit is only copied: the card says so and offers Copy, and
// the current branch keeps it.
func TestGitxMoveCommitPushedIsCopied(t *testing.T) {
	m, dir, _ := gitxUpstreamRepo(t)
	covWrite(t, dir, "b.txt", "b\n")
	covCommit(t, dir, "feat: b")
	covGit(t, dir, "push", "-q")
	head := covGit(t, dir, "rev-parse", "HEAD")
	covRefresh(m)
	m.dispatchPaletteAction("git-move-commit")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "move-new"})
	m.promptOpen = false
	m.handlePromptSubmit("keep-b")
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,copy" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %q", gitxCardIDList(m))
	}
	if v := m.confirm.View(); !strings.Contains(v, "pushed") {
		t.Errorf("the card does not say the commit is pushed:\n%s", v)
	}
	m.confirmOpen = false
	m.handleConfirmSelect("copy")
	if covGit(t, dir, "rev-parse", "HEAD") != head || covGit(t, dir, "rev-parse", "keep-b") != head {
		t.Error("a pushed commit was removed from its branch")
	}
}
