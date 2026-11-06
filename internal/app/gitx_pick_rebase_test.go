package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
)

// gitxSideRepo: main has first and m2; branch side has s1 and s2.
func gitxSideRepo(t *testing.T) (*Model, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	covGit(t, dir, "checkout", "-q", "-b", "side")
	covWrite(t, dir, "s1.txt", "s1\n")
	covCommit(t, dir, "s1")
	covWrite(t, dir, "s2.txt", "s2\n")
	covCommit(t, dir, "s2")
	covGit(t, dir, "checkout", "-q", "main")
	covWrite(t, dir, "m.txt", "m\n")
	covCommit(t, dir, "m2")
	covRefresh(m)
	return m, dir
}

// gitxRowAt puts the GRAPH cursor on the commit with this subject.
func gitxRowAt(t *testing.T, m *Model, subject string) {
	t.Helper()
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowCommit && m.gitCommits[r.ci].Subject == subject })
}

func gitxMenuHas(id string) bool {
	for _, it := range gitxMenuItems() {
		if it.ID == id {
			return true
		}
	}
	return false
}

// GITX-16 (K2): Cherry-pick from Branch… lists the branch's commits on a
// Cancel-first card; Cherry-pick All applies them oldest first.
func TestGitxCherryPickFromBranch(t *testing.T) {
	m, dir := gitxSideRepo(t)
	m.dispatchPaletteAction("git-cherry-pick-branch")
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "branch-side" {
		t.Fatalf("branches %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "branch-side"})
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,pick" || !strings.Contains(m.confirm.View(), "s1") {
		t.Fatalf("card %q\n%s", gitxCardIDList(m), m.confirm.View())
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if covGit(t, dir, "log", "-1", "--format=%s") != "m2" {
		t.Fatal("Cancel picked")
	}
	m.dispatchPaletteAction("git-cherry-pick-branch")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "branch-side"})
	m.confirmOpen = false
	m.handleConfirmSelect("pick")
	if got := covGit(t, dir, "log", "--format=%s", "-3"); got != "s2\ns1\nm2" {
		t.Errorf("history\n%s", got)
	}
}

// GITX-16 (K5): Revert Merge… asks which parent to keep, then asks on a
// Cancel-first card.
func TestGitxRevertMerge(t *testing.T) {
	m, dir := gitxSideRepo(t)
	covGit(t, dir, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	covRefresh(m)
	if !gitxMenuHas("gitx-revert-merge") {
		t.Fatal("no menu row")
	}
	gitxRowAt(t, m, "merge side")
	m.handleGitMenuSelect("gitx-revert-merge")
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "parent-1,parent-2" {
		t.Fatalf("parents %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "parent-1"})
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,revert" {
		t.Fatalf("card %q", gitxCardIDList(m))
	}
	m.confirmOpen = false
	m.handleConfirmSelect("revert")
	if _, err := os.Stat(filepath.Join(dir, "s1.txt")); err == nil {
		t.Error("the side's file is still there")
	}
	// on a commit that is no merge
	gitxRowAt(t, m, "first")
	m.handleGitMenuSelect("gitx-revert-merge")
	if m.pickerOpen || !toastHas(m, "not a merge") {
		t.Error("no note for a normal commit")
	}
}

// GITX-16 (K3, K12): Cherry-pick a File from Here… and Restore a File
// from Here… pick one file of the commit and ask first.
func TestGitxPickAndRestoreFile(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "v2\n")
	covWrite(t, dir, "b.txt", "b\n")
	covCommit(t, dir, "v2")
	covWrite(t, dir, "a.txt", "dirty\n")
	covRefresh(m)
	gitxRowAt(t, m, "first")
	m.handleGitMenuSelect("gitx-restore-file")
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "file-a.txt" {
		t.Fatalf("files %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "file-a.txt"})
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,restore" || !strings.Contains(m.confirm.View(), "stash") {
		t.Fatalf("card %q\n%s", gitxCardIDList(m), m.confirm.View())
	}
	m.confirmOpen = false
	m.handleConfirmSelect("restore")
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "one\ntwo\nthree\n" || !toastHas(m, "stash@{0}") {
		t.Errorf("a.txt %q", b)
	}
	// pick b.txt's change of v2 onto a branch without it
	covGit(t, dir, "stash", "drop", "-q")
	covGit(t, dir, "checkout", "-q", "-f", "-b", "old", "HEAD~1")
	covRefresh(m)
	m.gitCommits = nil
	covRefresh(m)
	v2 := covGit(t, dir, "rev-parse", "main")
	m.gitxPickFileFrom(v2)
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "file-b.txt"})
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,apply" {
		t.Fatalf("card %q", gitxCardIDList(m))
	}
	m.confirmOpen = false
	m.handleConfirmSelect("apply")
	if b, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(b) != "b\n" {
		t.Errorf("b.txt %q", b)
	}
}

type gitxRebaseCall struct {
	what, hash, arg string
	keep, stash     bool
}

// gitxFakeRebases swaps the rebase runs (the test binary is no rebase
// helper) for a recorder.
func gitxFakeRebases(t *testing.T) *[]gitxRebaseCall {
	t.Helper()
	var calls []gitxRebaseCall
	old := gitxRebaseRuns
	t.Cleanup(func() { gitxRebaseRuns = old })
	gitxRebaseRuns = gitxRebaser{
		onto: func(dir, onto string, keep bool, opt git.RebaseOptions) (git.RebaseOutcome, error) {
			calls = append(calls, gitxRebaseCall{what: "onto", arg: onto, keep: keep, stash: opt.Autostash})
			return git.RebaseOutcome{}, nil
		},
		reword: func(dir, full, msg string, opt git.RebaseOptions) (git.RebaseOutcome, error) {
			calls = append(calls, gitxRebaseCall{what: "reword", hash: full, arg: msg, stash: opt.Autostash})
			return git.RebaseOutcome{}, nil
		},
		drop: func(dir, full string, opt git.RebaseOptions) (git.RebaseOutcome, error) {
			calls = append(calls, gitxRebaseCall{what: "drop", hash: full, stash: opt.Autostash})
			return git.RebaseOutcome{}, nil
		},
		squash: func(dir, full string, opt git.RebaseOptions) (git.RebaseOutcome, error) {
			calls = append(calls, gitxRebaseCall{what: "squash", hash: full, stash: opt.Autostash})
			return git.RebaseOutcome{}, nil
		},
		autosquash: func(dir string, opt git.RebaseOptions) (git.RebaseOutcome, error) {
			calls = append(calls, gitxRebaseCall{what: "autosquash", stash: opt.Autostash})
			return git.RebaseOutcome{}, nil
		},
	}
	return &calls
}

// gitxRunDone runs a rebase command and applies its rebaseDoneMsg.
func gitxRunDone(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	m.onRebaseMsg(covMsg[rebaseDoneMsg](t, cmd))
}

// GITX-17 (F9, J10, J11): Rebase Current Branch onto… picks a branch and
// asks on a Cancel-first card: Rebase, or Rebase (Keep Merges); a dirty
// tree stashes first.
func TestGitxRebaseOnto(t *testing.T) {
	calls := gitxFakeRebases(t)
	m, dir := gitxSideRepo(t)
	covGit(t, dir, "checkout", "-q", "side")
	covRefresh(m)
	m.dispatchPaletteAction("git-rebase-onto")
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "branch-main" {
		t.Fatalf("branches %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "branch-main"})
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,rebase,rebase-merges" || !strings.Contains(m.confirm.View(), "2 commits") {
		t.Fatalf("card %q\n%s", gitxCardIDList(m), m.confirm.View())
	}
	m.confirmOpen = false
	if m.handleConfirmSelect("cancel") != nil || len(*calls) != 0 {
		t.Fatal("Cancel rebased")
	}
	covWrite(t, dir, "s1.txt", "dirty\n")
	m.dispatchPaletteAction("git-rebase-onto")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "branch-main"})
	if !strings.Contains(m.confirm.View(), "uncommitted") {
		t.Errorf("no stash note:\n%s", m.confirm.View())
	}
	m.confirmOpen = false
	gitxRunDone(t, m, m.handleConfirmSelect("rebase-merges"))
	if len(*calls) != 1 || (*calls)[0] != (gitxRebaseCall{what: "onto", arg: "main", keep: true, stash: true}) {
		t.Errorf("calls %+v", *calls)
	}
}

// GITX-17 (J6, J7): E on a GRAPH commit edits its full message: the
// subject, then the body (\n starts a new line); a card asks first.
func TestGitxRewordFromGraph(t *testing.T) {
	calls := gitxFakeRebases(t)
	m, dir := covGitRepo(t)
	covWrite(t, dir, "b.txt", "b\n")
	covGit(t, dir, "add", "-A")
	covGit(t, dir, "commit", "-q", "-m", "second", "-m", "old body\nline 2")
	covWrite(t, dir, "c.txt", "c\n")
	covCommit(t, dir, "third")
	covRefresh(m)
	gitxRowAt(t, m, "second")
	if _, ok := covKey(m, covRunes("E")); !ok || !m.promptOpen || m.prompt.Value() != "second" {
		t.Fatalf("subject prompt %q", m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit("second, better")
	if !m.promptOpen || m.prompt.Value() != `old body\nline 2` {
		t.Fatalf("body prompt %q", m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit(`new body\nline two`)
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,reword" {
		t.Fatalf("card %q", gitxCardIDList(m))
	}
	m.confirmOpen = false
	gitxRunDone(t, m, m.handleConfirmSelect("reword"))
	full := covGit(t, dir, "rev-parse", "HEAD~1")
	if len(*calls) != 1 || (*calls)[0] != (gitxRebaseCall{what: "reword", hash: full, arg: "second, better\n\nnew body\nline two"}) {
		t.Errorf("calls %+v", *calls)
	}
}

// GITX-17 (J8, J9): D drops a commit and S squashes it into the one
// before, each after a Cancel-first card; Autosquash folds fixup!
// commits.
func TestGitxDropSquashAutosquash(t *testing.T) {
	calls := gitxFakeRebases(t)
	m, dir := covGitRepo(t)
	covWrite(t, dir, "b.txt", "b\n")
	covCommit(t, dir, "second")
	covRefresh(m)
	full := covGit(t, dir, "rev-parse", "HEAD")
	gitxRowAt(t, m, "second")
	covKey(m, covRunes("D"))
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,drop" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("drop card %q", gitxCardIDList(m))
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	covKey(m, covRunes("S"))
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,squash" {
		t.Fatalf("squash card %q", gitxCardIDList(m))
	}
	m.confirmOpen = false
	gitxRunDone(t, m, m.handleConfirmSelect("squash"))
	if len(*calls) != 1 || (*calls)[0] != (gitxRebaseCall{what: "squash", hash: full}) {
		t.Errorf("calls %+v", *calls)
	}
	m.dispatchPaletteAction("git-autosquash")
	if m.confirmOpen || !toastHas(m, "No fixup! commits") {
		t.Error("no note without fixups")
	}
	covWrite(t, dir, "a.txt", "fixed\n")
	covGit(t, dir, "add", "-A")
	covGit(t, dir, "commit", "-q", "--fixup", full)
	m.dispatchPaletteAction("git-autosquash")
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,autosquash" {
		t.Fatalf("autosquash card %q", gitxCardIDList(m))
	}
	m.confirmOpen = false
	gitxRunDone(t, m, m.handleConfirmSelect("autosquash"))
	if len(*calls) != 2 || (*calls)[1].what != "autosquash" {
		t.Errorf("calls %+v", *calls)
	}
}

// GITX-25: Copy as Patch puts the commit on the clipboard; Save Changes
// as Patch… writes the changes to a file (asking before it replaces
// one); Apply Patch from File… / from Clipboard check it and ask first.
func TestGitxPatches(t *testing.T) {
	m, dir := covGitRepo(t)
	var copied string
	sink := func(s string) { copied = s }
	clipboardSink.Store(&sink)
	t.Cleanup(func() { clipboardSink.Store(nil) })
	gitxRowAt(t, m, "first")
	m.handleGitMenuSelect("gitx-copy-patch")
	if !strings.Contains(copied, "Subject: [PATCH] first") {
		t.Fatalf("clipboard %q", copied)
	}
	covWrite(t, dir, "a.txt", "changed\n")
	m.dispatchPaletteAction("git-patch-save")
	if !m.promptOpen || m.prompt.Value() != "changes.patch" {
		t.Fatalf("prompt %q", m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit("changes.patch")
	saved, err := os.ReadFile(filepath.Join(dir, "changes.patch"))
	if err != nil || !strings.Contains(string(saved), "+changed") {
		t.Fatalf("saved %q %v", saved, err)
	}
	m.dispatchPaletteAction("git-patch-save")
	m.promptOpen = false
	m.handlePromptSubmit("changes.patch")
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,replace" {
		t.Fatalf("replace card %q", gitxCardIDList(m))
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	covGit(t, dir, "checkout", "-q", "--", "a.txt")
	m.dispatchPaletteAction("git-patch-apply-file")
	m.promptOpen = false
	m.handlePromptSubmit("changes.patch")
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,apply" || !strings.Contains(m.confirm.View(), "a.txt") {
		t.Fatalf("apply card %q\n%s", gitxCardIDList(m), m.confirm.View())
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) == "changed\n" {
		t.Fatal("Cancel applied")
	}
	old := gitxReadClipboard
	t.Cleanup(func() { gitxReadClipboard = old })
	gitxReadClipboard = func() string { return string(saved) }
	m.dispatchPaletteAction("git-patch-apply-clip")
	m.confirmOpen = false
	m.handleConfirmSelect("apply")
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "changed\n" {
		t.Errorf("a.txt %q", b)
	}
	gitxReadClipboard = func() string { return "hello" }
	m.dispatchPaletteAction("git-patch-apply-clip")
	if m.confirmOpen || !toastHas(m, "not a patch") {
		t.Error("text that is no patch opened a card")
	}
}
