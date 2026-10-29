package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/picker"
)

// covOpenFile makes rel (below dir) the editor's file, cursor on line.
func covOpenFile(m *Model, dir, rel string, line int) {
	m.editor.SetMeta(filepath.Join(dir, rel), false, "")
	m.cursorLine = line
}

// covStale makes the model believe dir is a repo after its .git went
// away (the user deleted it while termocode was open).
func covStale(t *testing.T, m *Model, dir string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	m.gitLoaded, m.gitIsRepo = true, true
}

// Stage / Unstage / Discard Hunk work on the hunk under the cursor of the
// open file; a line with no change is an error toast.
func TestCovGitHunkPalette(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "one\nTWO\nthree\n")
	covOpenFile(m, dir, "a.txt", 2)

	m.dispatchPaletteAction("git-stage-hunk")
	if got := covGit(t, dir, "diff", "--cached", "--name-only"); got != "a.txt" {
		t.Fatalf("staged = %q", got)
	}
	if !toastHas(m, "Staged hunk") {
		t.Error("no Staged hunk toast")
	}
	m.dispatchPaletteAction("git-unstage-hunk")
	if got := covGit(t, dir, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("after unstage = %q", got)
	}
	m.dispatchPaletteAction("git-discard-hunk")
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "one\ntwo\nthree\n" {
		t.Fatalf("discard hunk left %q", b)
	}
	if !toastHas(m, "Discarded hunk") {
		t.Error("no Discarded hunk toast")
	}
	// Clean file now: no hunk under the cursor.
	m.dispatchPaletteAction("git-stage-hunk")
	if !toastHas(m, "Git") {
		t.Error("no error toast for a line without a change")
	}
	// No open file: nothing happens.
	m.editor.SetMeta("", false, "")
	if m.dispatchPaletteAction("git-stage-hunk") != nil {
		t.Error("hunk op with no file did something")
	}
}

// Stage (open file), Stage All and Diff (open file) from the palette.
func TestCovGitStageAndDiffActiveFile(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "one\nTWO\nthree\n")
	covWrite(t, dir, "new.txt", "n\n")
	covOpenFile(m, dir, "a.txt", 1)

	cmd := m.dispatchPaletteAction("git-diff")
	pv := covMsg[PreviewMsg](t, cmd)
	if pv.Title != "diff · a.txt" || !strings.Contains(pv.Body, "+TWO") {
		t.Errorf("diff preview %q: %q", pv.Title, pv.Body)
	}
	m.dispatchPaletteAction("git-stage")
	if got := covGit(t, dir, "diff", "--cached", "--name-only"); got != "a.txt" {
		t.Fatalf("staged = %q", got)
	}
	m.dispatchPaletteAction("git-stage-all")
	if got := covGit(t, dir, "diff", "--cached", "--name-only"); got != "a.txt\nnew.txt" {
		t.Fatalf("staged all = %q", got)
	}
	if !toastHas(m, "Staged all changes") {
		t.Error("no Staged all toast")
	}

	// A file outside the project can not be staged.
	out := filepath.Join(t.TempDir(), "x.txt")
	covWrite(t, filepath.Dir(out), "x.txt", "x")
	m.editor.SetMeta(out, false, "")
	m.dispatchPaletteAction("git-stage")
	if !toastHas(m, "Stage failed") {
		t.Error("no Stage failed toast")
	}
	// The project folder itself is not a file.
	m.editor.SetMeta(dir, false, "")
	if m.dispatchPaletteAction("git-diff") != nil || m.dispatchPaletteAction("git-stage") != nil {
		t.Error("the project folder was taken as a file")
	}

	// The open file was deleted and git never knew it: an error.
	covOpenFile(m, dir, "gone.txt", 1)
	if covMsg[ErrMsg](t, m.dispatchPaletteAction("git-diff")).Err == nil {
		t.Error("diff of a lost file gave no error")
	}
	// The repo went away: stage all fails with a message.
	covStale(t, m, dir)
	m.dispatchPaletteAction("git-stage-all")
	if !toastHas(m, "Stage all failed") {
		t.Error("no Stage all failed toast")
	}
}

// Not a repo: the git palette actions do nothing.
func TestCovGitPaletteOutsideRepo(t *testing.T) {
	covGitEnv(t)
	dir := t.TempDir()
	m := covModelAt(t, dir)
	covWrite(t, dir, "a.txt", "x")
	covOpenFile(m, dir, "a.txt", 1)
	for _, id := range []string{"git-stage", "git-stage-all", "git-diff", "git-branch", "git-stash",
		"git-stash-pop", "git-stash-list", "git-blame", "git-history", "git-tags", "git-log", "git-push"} {
		if cmd := m.dispatchPaletteAction(id); cmd != nil {
			t.Errorf("%s outside a repo returned a command", id)
		}
	}
	m.openCommitPrompt()
	if m.promptOpen {
		t.Error("commit prompt opened outside a repo")
	}
	if m.gitInlineCommit(false, false) != nil || m.gitCreateBranchFromPrompt("x") != nil {
		t.Error("commit / branch outside a repo did something")
	}
}

// Commit… asks for a message, counting the staged files; the toast shows
// the first line. With nothing staged git refuses and the toast says so.
func TestCovGitCommitPrompt(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "changed\n")
	covGit(t, dir, "add", "a.txt")
	covRefresh(m)
	m.dispatchPaletteAction("git-commit")
	if !m.promptOpen || m.promptKind != promptKindCommit || !strings.Contains(m.prompt.View(), "1 staged file") {
		t.Fatalf("no commit prompt: %q", m.prompt.View())
	}
	m.promptOpen = false
	m.handlePromptSubmit("fix: the title\n\nthe body")
	if got := covGit(t, dir, "log", "-1", "--format=%s"); got != "fix: the title" {
		t.Fatalf("HEAD subject %q", got)
	}
	if !toastHas(m, "fix: the title") || toastHas(m, "the body") {
		t.Error("toast does not show only the first line")
	}
	m.handlePromptSubmit("empty")
	if !toastHas(m, "Commit failed") {
		t.Error("no Commit failed toast")
	}
}

// covHook installs an executable git hook that fails.
func covHook(t *testing.T, gitDir, name string) {
	t.Helper()
	p := filepath.Join(gitDir, "hooks", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho refused by hook >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// The commit box: no message focuses it; nothing changed says so; only
// unstaged changes ask to stage all first (Cancel keeps all); a failing
// hook shows the error; amend with no text keeps the message.
func TestCovGitInlineCommit(t *testing.T) {
	m, dir := covGitRepo(t)
	if m.gitInlineCommit(false, false) == nil || !m.gitCommitFocused || !toastHas(m, "Enter a commit message") {
		t.Fatal("empty message did not focus the box")
	}
	m.gitCommitMsg = "feat: more"
	m.gitInlineCommit(false, false)
	if !toastHas(m, "Nothing to commit") {
		t.Error("no Nothing to commit toast")
	}

	covWrite(t, dir, "a.txt", "changed\n")
	covRefresh(m)
	m.gitInlineCommit(false, false)
	if !m.confirmOpen || m.confirmKind != confirmKindGitStageCommit {
		t.Fatal("no stage-all card")
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if covGit(t, dir, "diff", "--cached", "--name-only") != "" {
		t.Fatal("Cancel staged files")
	}

	// A pre-commit hook refuses: stage-all-and-commit stages, then fails.
	covHook(t, filepath.Join(dir, ".git"), "pre-commit")
	m.handleConfirmSelect("stage")
	if !toastHas(m, "Commit failed") || covGit(t, dir, "log", "--format=%s") != "first" {
		t.Fatal("the hook did not stop the commit")
	}
	covRefresh(m)
	m.gitInlineCommit(false, false) // staged now: commit, hook refuses
	if !toastHas(m, "refused by hook") {
		t.Error("the hook's error is not shown")
	}
	if m.gitCommitMsg != "feat: more" {
		t.Error("a failed commit cleared the box")
	}
	if err := os.Remove(filepath.Join(dir, ".git", "hooks", "pre-commit")); err != nil {
		t.Fatal(err)
	}
	m.gitInlineCommit(false, false)
	if covGit(t, dir, "log", "-1", "--format=%s") != "feat: more" || m.gitCommitMsg != "" {
		t.Fatal("commit did not land or the box was kept")
	}

	// Stage all & commit, the happy path.
	covWrite(t, dir, "a.txt", "again\n")
	m.gitCommitMsg = "fix: again"
	m.handleConfirmSelect("stage")
	if covGit(t, dir, "log", "-1", "--format=%s") != "fix: again" {
		t.Fatal("stage all & commit did not commit")
	}

	// Amend with an empty box keeps the message.
	m.gitInlineCommit(true, false)
	if !toastHas(m, "amended HEAD") || covGit(t, dir, "log", "-1", "--format=%s") != "fix: again" {
		t.Error("amend did not keep the message")
	}

	// The repo went away: stage all fails before the commit.
	covStale(t, m, dir)
	m.gitCommitMsg = "x"
	m.handleConfirmSelect("stage")
	if !toastHas(m, "Stage all failed") {
		t.Error("no Stage all failed toast")
	}
}

// Branches: the picker lists them with "current" and Create first; a new
// branch comes from a prompt; a name that exists or a branch that does
// not is an error toast.
func TestCovGitBranches(t *testing.T) {
	m, dir := covGitRepo(t)
	covGit(t, dir, "branch", "other")
	m.dispatchPaletteAction("git-branch")
	if !m.pickerOpen || m.pickerKind != pickerKindBranches {
		t.Fatal("no branch picker")
	}
	view := m.picker.View()
	for _, want := range []string{"Create new branch", "main", "current", "other"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker misses %q", want)
		}
	}
	m.handlePickerSelect(picker.SelectMsg{ID: "co-other"})
	if covGit(t, dir, "branch", "--show-current") != "other" || !toastHas(m, "Switched to") {
		t.Fatal("checkout other failed")
	}
	m.handlePickerSelect(picker.SelectMsg{ID: "co-nope"})
	if !toastHas(m, "Checkout failed") {
		t.Error("no Checkout failed toast")
	}
	m.handlePickerSelect(picker.SelectMsg{ID: "new"})
	if !m.promptOpen || m.promptKind != promptKindGitNewBranch {
		t.Fatal("no branch name prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit("feat/y")
	if covGit(t, dir, "branch", "--show-current") != "feat/y" || !toastHas(m, "Created branch") {
		t.Fatal("feat/y not created")
	}
	m.gitCreateBranchFromPrompt("main")
	if !toastHas(m, "Branch failed") {
		t.Error("no Branch failed toast")
	}
	covStale(t, m, dir)
	m.dispatchPaletteAction("git-branch")
	if !toastHas(m, "Branches failed") {
		t.Error("no Branches failed toast")
	}
}

// Stash, stash list (Enter applies, the stash stays) and stash pop.
func TestCovGitStash(t *testing.T) {
	m, dir := covGitRepo(t)
	m.dispatchPaletteAction("git-stash-list")
	if !toastHas(m, "No stashes") {
		t.Error("no No stashes toast")
	}
	m.dispatchPaletteAction("git-stash-pop")
	if !toastHas(m, "Stash pop failed") {
		t.Error("no Stash pop failed toast")
	}
	covWrite(t, dir, "a.txt", "wip\n")
	m.dispatchPaletteAction("git-stash")
	if !toastHas(m, "Stashed changes") || covGit(t, dir, "status", "--porcelain") != "" {
		t.Fatal("stash did not save the change")
	}
	m.dispatchPaletteAction("git-stash-list")
	if !m.pickerOpen || m.pickerKind != pickerKindStash || !strings.Contains(m.picker.View(), "stash@{0}") {
		t.Fatal("no stash picker")
	}
	m.handlePickerSelect(picker.SelectMsg{ID: "stash@{0}"})
	if covGit(t, dir, "status", "--porcelain") != "M a.txt" || covGit(t, dir, "stash", "list") == "" {
		t.Fatal("apply did not bring the change back or dropped the stash")
	}
	m.handlePickerSelect(picker.SelectMsg{ID: "stash@{0}"}) // same change on a dirty tree
	if !toastHas(m, "Stash apply failed") {
		t.Error("no Stash apply failed toast")
	}
	covGit(t, dir, "checkout", "--", "a.txt")
	m.dispatchPaletteAction("git-stash-pop")
	if !toastHas(m, "Popped latest stash") || covGit(t, dir, "stash", "list") != "" {
		t.Fatal("pop did not apply and drop")
	}
	covStale(t, m, dir)
	m.dispatchPaletteAction("git-stash")
	m.dispatchPaletteAction("git-stash-list")
	if !toastHas(m, "Stash failed") || !toastHas(m, "Stash list failed") {
		t.Error("lost repo: no stash errors")
	}
}

// Blame and File History of the open file.
func TestCovGitBlameAndHistory(t *testing.T) {
	m, dir := covGitRepo(t)
	m.dispatchPaletteAction("git-blame")
	m.dispatchPaletteAction("git-history")
	if !toastHas(m, "No file to blame") || !toastHas(m, "No file selected") {
		t.Fatal("no file: no warnings")
	}
	covOpenFile(m, dir, "a.txt", 0)
	m.dispatchPaletteAction("git-blame")
	if !toastHas(m, "Tess") || !toastHas(m, "first") {
		t.Error("blame toast misses author / subject")
	}
	m.dispatchPaletteAction("git-history")
	if !m.pickerOpen || m.pickerKind != pickerKindGitLog || !strings.Contains(m.picker.View(), "History · a.txt") {
		t.Fatalf("no history picker: %q", m.picker.View())
	}
	m.pickerOpen = false
	covWrite(t, dir, "new.txt", "n\n")
	covOpenFile(m, dir, "new.txt", 1)
	m.dispatchPaletteAction("git-blame")
	m.dispatchPaletteAction("git-history")
	if !toastHas(m, "Blame failed") || !toastHas(m, "No history") || m.pickerOpen {
		t.Error("untracked file: no blame error / no-history note")
	}
	covStale(t, m, dir)
	m.dispatchPaletteAction("git-history")
	if !toastHas(m, "History failed") {
		t.Error("no History failed toast")
	}
}

// Tags: none says so; a tag checks out as a detached HEAD.
func TestCovGitTags(t *testing.T) {
	m, dir := covGitRepo(t)
	m.dispatchPaletteAction("git-tags")
	if !toastHas(m, "No tags in this repo") {
		t.Error("no No tags toast")
	}
	covGit(t, dir, "tag", "v1.0")
	m.dispatchPaletteAction("git-tags")
	if !m.pickerOpen || m.pickerKind != pickerKindTags || !strings.Contains(m.picker.View(), "v1.0") {
		t.Fatal("no tag picker")
	}
	m.handlePickerSelect(picker.SelectMsg{ID: "tag-v1.0"})
	if covGit(t, dir, "branch", "--show-current") != "" || !toastHas(m, "detached HEAD") {
		t.Fatal("tag checkout is not detached")
	}
	m.handlePickerSelect(picker.SelectMsg{ID: "tag-v9"})
	if !toastHas(m, "Checkout failed") {
		t.Error("no Checkout failed toast")
	}
	covStale(t, m, dir)
	m.dispatchPaletteAction("git-tags")
	if !toastHas(m, "Tags failed") {
		t.Error("no Tags failed toast")
	}
}

// Show Log lists commits with their refs; Enter opens the commit's diff
// (a temp file, removed once shown). A repo with no commit says so.
func TestCovGitLogPicker(t *testing.T) {
	covGitEnv(t)
	empty := t.TempDir()
	covGit(t, empty, "init", "-q", "-b", "main")
	m := covModelAt(t, empty)
	m.dispatchPaletteAction("git-log")
	if !toastHas(m, "No commits yet") && !toastHas(m, "Log failed") {
		t.Error("empty repo: no note")
	}

	m, dir := covGitRepo(t)
	covGit(t, dir, "tag", "v1")
	m.dispatchPaletteAction("git-log")
	if !m.pickerOpen || m.pickerKind != pickerKindGitLog || !strings.Contains(m.picker.View(), "(HEAD -> ") {
		t.Fatalf("no log picker with refs: %q", m.picker.View())
	}
	hash := covGit(t, dir, "rev-parse", "--short", "HEAD")
	ready := covMsg[gitCommitDiffReadyMsg](t, m.handlePickerSelect(picker.SelectMsg{ID: "commit-" + hash}))
	body, _ := os.ReadFile(ready.path)
	if !strings.Contains(string(body), "+one") {
		t.Errorf("commit diff = %q", body)
	}
	m.openCommitDiffBuffer(ready)
	if _, err := os.Stat(ready.path); !os.IsNotExist(err) {
		t.Error("commit diff temp file left behind")
	}
	if covMsg[ErrMsg](t, m.showCommitFromPicker("commit-deadbeef")).Err == nil {
		t.Error("unknown commit gave no error")
	}
	if m.gitShowCommitCmd("") != nil {
		t.Error("empty hash gave a command")
	}
	covStale(t, m, dir)
	m.dispatchPaletteAction("git-log")
	if !toastHas(m, "Log failed") {
		t.Error("no Log failed toast")
	}
}

// Compare with Revision: a prompt (HEAD prefilled), then the diff against
// it; no change says so; an unknown revision is an error.
func TestCovGitCompareWithRevision(t *testing.T) {
	m, dir := covGitRepo(t)
	m.dispatchPaletteAction("git-compare-rev")
	if !m.promptOpen || m.promptKind != promptKindCompareRevision {
		t.Fatal("no revision prompt")
	}
	m.promptOpen = false
	pv := covMsg[PreviewMsg](t, m.handlePromptSubmit("HEAD"))
	if pv.Title != "diff · vs HEAD" || !strings.Contains(pv.Body, "(no differences)") {
		t.Errorf("clean compare: %q %q", pv.Title, pv.Body)
	}
	covWrite(t, dir, "a.txt", "zzz\n")
	pv = covMsg[PreviewMsg](t, m.runCompareWithRevision("HEAD"))
	if !strings.Contains(pv.Body, "+zzz") {
		t.Errorf("compare body %q", pv.Body)
	}
	if covMsg[ErrMsg](t, m.runCompareWithRevision("no-such-ref")).Err == nil {
		t.Error("unknown revision gave no error")
	}
	if m.runCompareWithRevision("") != nil {
		t.Error("empty revision gave a command")
	}
}
