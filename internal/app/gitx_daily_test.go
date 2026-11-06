package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gitxFileBody(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		return ""
	}
	return string(b)
}

// gitxCardButtons is the open card's button ids in order.
func gitxCardButtons(m *Model) string {
	var ids []string
	for _, b := range m.confirm.Buttons() {
		ids = append(ids, b.ID)
	}
	return strings.Join(ids, ",")
}

// GITX-20: Unstage All takes every staged file out of the index.
func TestGitxUnstageAll(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "ONE\n")
	covWrite(t, dir, "n.txt", "new\n")
	covGit(t, dir, "add", ".")
	covRefresh(m)
	m.dispatchPaletteAction("git-unstage-all")
	if got := covGit(t, dir, "diff", "--cached", "--name-only"); got != "" {
		t.Errorf("still staged %q", got)
	}
	if !toastHas(m, "Unstaged all changes") {
		t.Error("no toast")
	}
}

// GITX-20: Discard All asks first on a Cancel-first card; Cancel (and
// Enter, which presses the focused Cancel) keeps every change. Discard
// puts the changes in a safety stash and keeps new files; the toast
// names the stash.
func TestGitxDiscardAllCard(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "ONE\n")
	covWrite(t, dir, "n.txt", "new\n")
	covRefresh(m)
	m.dispatchPaletteAction("git-discard-all")
	if !m.confirmOpen || m.confirmKind != confirmKindGitDiscardAll {
		t.Fatal("no card")
	}
	if got := gitxCardButtons(m); got != "cancel,discard,discard-new" || m.confirm.SafeID() != "cancel" {
		t.Errorf("buttons %q safe %q", got, m.confirm.SafeID())
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if gitxFileBody(t, dir, "a.txt") != "ONE\n" {
		t.Fatal("Cancel discarded")
	}

	m.dispatchPaletteAction("git-discard-all")
	m.confirmOpen = false
	m.handleConfirmSelect("discard")
	if gitxFileBody(t, dir, "a.txt") != "one\ntwo\nthree\n" || gitxFileBody(t, dir, "n.txt") != "new\n" {
		t.Error("discard did not keep new files / revert a.txt")
	}
	if !toastHas(m, "stash@{0}") {
		t.Error("toast does not name the safety copy")
	}

	covWrite(t, dir, "a.txt", "ONE\n")
	covRefresh(m)
	m.dispatchPaletteAction("git-discard-all")
	m.confirmOpen = false
	m.handleConfirmSelect("discard-new")
	if gitxFileBody(t, dir, "n.txt") != "" || gitxFileBody(t, dir, "a.txt") != "one\ntwo\nthree\n" {
		t.Error("discard with new files left changes")
	}
}

// GITX-20: with no new files the card has no "delete new files" button;
// with nothing changed no card opens.
func TestGitxDiscardAllCardShapes(t *testing.T) {
	m, dir := covGitRepo(t)
	m.dispatchPaletteAction("git-discard-all")
	if m.confirmOpen || !toastHas(m, "Nothing to discard") {
		t.Fatal("card on a clean tree")
	}
	covWrite(t, dir, "a.txt", "ONE\n")
	covRefresh(m)
	m.dispatchPaletteAction("git-discard-all")
	if got := gitxCardButtons(m); got != "cancel,discard" {
		t.Errorf("buttons %q", got)
	}
}

// GITX-20: Delete Untracked Files asks on a Cancel-first card naming the
// count, then moves only the new files into a safety stash.
func TestGitxDeleteUntrackedCard(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "ONE\n")
	covWrite(t, dir, "n.txt", "new\n")
	covWrite(t, dir, "d/m.txt", "new\n")
	covRefresh(m)
	m.dispatchPaletteAction("git-clean-untracked")
	if !m.confirmOpen || m.confirmKind != confirmKindGitCleanUntracked || m.confirm.SafeID() != "cancel" {
		t.Fatal("no Cancel-first card")
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if gitxFileBody(t, dir, "n.txt") == "" {
		t.Fatal("Cancel deleted")
	}
	m.dispatchPaletteAction("git-clean-untracked")
	m.confirmOpen = false
	m.handleConfirmSelect("delete")
	if gitxFileBody(t, dir, "n.txt") != "" || gitxFileBody(t, dir, "d/m.txt") != "" || gitxFileBody(t, dir, "a.txt") != "ONE\n" {
		t.Error("wrong files deleted")
	}
	if !toastHas(m, "stash@{0}") {
		t.Error("toast does not name the safety copy")
	}
	covRefresh(m)
	m.dispatchPaletteAction("git-clean-untracked")
	if m.confirmOpen || !toastHas(m, "No untracked files") {
		t.Error("card with no new files")
	}
}

// gitxRemoteRepo is covGitRepo with a bare origin the branch tracks.
func gitxRemoteRepo(t *testing.T) (*Model, string, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	remote := t.TempDir()
	covGit(t, remote, "init", "-q", "--bare", "-b", "main")
	covGit(t, dir, "remote", "add", "origin", remote)
	covGit(t, dir, "push", "-q", "-u", "origin", "main")
	covRefresh(m)
	return m, dir, remote
}

// GITX-7: Commit and Push commits the box's message and then pushes.
func TestGitxCommitAndPush(t *testing.T) {
	m, dir, remote := gitxRemoteRepo(t)
	covWrite(t, dir, "a.txt", "ONE\n")
	covGit(t, dir, "add", ".")
	covRefresh(m)
	m.gitCommitMsg = "change a"
	cmd := m.dispatchPaletteAction("git-commit-push")
	m.gitRemoteDone(covMsg[gitRemoteDoneMsg](t, cmd))
	if got := covGit(t, remote, "log", "-1", "--format=%s", "main"); got != "change a" {
		t.Errorf("remote head %q", got)
	}
	if m.gitCommitMsg != "" || !toastHas(m, "Pushed to origin") {
		t.Error("box not cleared or no push toast")
	}
}

// GITX-7: Commit and Sync with nothing staged asks to stage all first;
// after Stage All & Commit it pulls and pushes.
func TestGitxCommitAndSyncStageAll(t *testing.T) {
	m, dir, remote := gitxRemoteRepo(t)
	covWrite(t, dir, "a.txt", "ONE\n")
	covRefresh(m)
	m.gitCommitMsg = "sync a"
	if cmd := m.dispatchPaletteAction("git-commit-sync"); cmd != nil || !m.confirmOpen || m.confirmKind != confirmKindGitStageCommit {
		t.Fatal("no stage-all card")
	}
	m.confirmOpen = false
	cmd := m.handleConfirmSelect("stage")
	m.gitRemoteDone(covMsg[gitRemoteDoneMsg](t, cmd))
	if got := covGit(t, remote, "log", "-1", "--format=%s", "main"); got != "sync a" {
		t.Errorf("remote head %q", got)
	}
	if !toastHas(m, "Synced with origin") {
		t.Error("no sync toast")
	}
}

// GITX-7: a failed commit (empty message) neither commits nor pushes;
// a plain Commit afterwards does not push.
func TestGitxCommitAndPushNoMessage(t *testing.T) {
	m, dir, remote := gitxRemoteRepo(t)
	covWrite(t, dir, "a.txt", "ONE\n")
	covGit(t, dir, "add", ".")
	covRefresh(m)
	m.dispatchPaletteAction("git-commit-push")
	if m.gitRemoteBusy != "" || !m.gitCommitFocused {
		t.Fatal("pushed without a message")
	}
	m.gitCommitMsg = "plain"
	m.gitInlineCommit(false, false)
	if m.gitRemoteBusy != "" {
		t.Error("plain commit started a push")
	}
	if got := covGit(t, remote, "log", "-1", "--format=%s", "main"); got != "first" {
		t.Errorf("remote head %q", got)
	}
}

// GITX-7, GITX-20: the ⋯ menu offers the new commands.
func TestGitxActionsMenuItems(t *testing.T) {
	var ids []string
	for _, it := range gitActionsMenuItems() {
		ids = append(ids, it.ID)
	}
	got := strings.Join(ids, ",")
	for _, want := range []string{gitMenuCommitPush, gitMenuCommitSync, gitMenuUnstageAll, gitMenuDiscardAll, gitMenuCleanUntracked} {
		if !strings.Contains(got, want) {
			t.Errorf("menu misses %s: %s", want, got)
		}
	}
}
