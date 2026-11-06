package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/picker"
)

// GITX-26 (C11): on a merge commit, Diff Merge Commit Against Parent…
// lists its parents; each one opens the compare from that parent to the
// merge (parent 2 shows what the merge brought from main).
func TestGitxMergeParentDiff(t *testing.T) {
	m, dir := covGitRepo(t)
	covGit(t, dir, "checkout", "-q", "-b", "side")
	covWrite(t, dir, "side.txt", "side\n")
	covCommit(t, dir, "side work")
	covGit(t, dir, "checkout", "-q", "main")
	covWrite(t, dir, "main.txt", "main\n")
	covCommit(t, dir, "main work")
	covGit(t, dir, "merge", "-q", "--no-edit", "--no-ff", "side")
	covRefresh(m)
	head := covGit(t, dir, "rev-parse", "HEAD")

	m.onCommitViewReady(covMsg[commitViewReadyMsg](t, m.gitOpenCommitViewCmd(head)))
	m.dispatchPaletteAction("git-diff-merge-parent")
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "parent-1,parent-2" {
		t.Fatalf("parents %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	cmd := m.handlePickerSelect(picker.SelectMsg{ID: "parent-2"})
	msg := covMsg[commitViewReadyMsg](t, cmd)
	if msg.compare == nil || msg.err != nil || len(msg.info.Files) != 1 || msg.info.Files[0].Path != "main.txt" {
		t.Fatalf("against parent 2: %+v", msg.info.Files)
	}
	m.onCommitViewReady(msg)
	if !m.commitViewShown() || m.commitView.compare == nil {
		t.Fatal("the compare is not on show")
	}

	// Not a merge: a note, no picker.
	first := covGit(t, dir, "rev-list", "--max-parents=0", "HEAD")
	m.onCommitViewReady(covMsg[commitViewReadyMsg](t, m.gitOpenCommitViewCmd(first)))
	m.dispatchPaletteAction("git-diff-merge-parent")
	if m.pickerOpen || !toastHas(m, "not a merge") {
		t.Error("a commit with one parent offered parents")
	}
}

// GITX-26 (E30): Compare Active File with Clipboard opens a read-only diff
// card from the clipboard text to the file.
func TestGitxCompareWithClipboard(t *testing.T) {
	m, dir := covGitRepo(t)
	old := gitxClipboardRead
	gitxClipboardRead = func() string { return "one\nTWO\nthree\n" }
	t.Cleanup(func() { gitxClipboardRead = old })
	m.editor.SetMeta(filepath.Join(dir, "a.txt"), false, "")
	msg := covMsg[compareReadyMsg](t, m.dispatchPaletteAction("git-diff-clipboard"))
	if msg.file.Old != "one\nTWO\nthree\n" || msg.file.New != "one\ntwo\nthree\n" || !strings.Contains(msg.file.Name, "Clipboard") {
		t.Fatalf("compare %+v", msg.file)
	}
	gitxClipboardRead = func() string { return "" }
	if c := m.dispatchPaletteAction("git-diff-clipboard"); c == nil || !toastHas(m, "clipboard is empty") {
		t.Error("an empty clipboard opened a compare")
	}
	if _, ok := aiActionPolicies["git-diff-clipboard"]; !ok {
		t.Error("no AI policy")
	}
}
