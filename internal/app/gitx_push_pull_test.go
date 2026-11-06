package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/picker"
)

// gitxCardIDList is the card's button ids, in order.
func gitxCardIDList(m *Model) string {
	var ids []string
	for _, b := range m.confirm.Buttons() {
		ids = append(ids, b.ID)
	}
	return strings.Join(ids, ",")
}

// gitxUpstreamRepo is covGitRepo pushed to a bare "origin".
func gitxUpstreamRepo(t *testing.T) (*Model, string, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	bare := t.TempDir()
	covGit(t, dir, "init", "-q", "--bare", "-b", "main", bare)
	covGit(t, dir, "remote", "add", "origin", bare)
	covGit(t, dir, "push", "-q", "-u", "origin", "main")
	covRefresh(m)
	return m, dir, bare
}

// gitxDone runs cmd and applies the gitRemoteDoneMsg it ends with.
func gitxDone(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	m.gitRemoteDone(covMsg[gitRemoteDoneMsg](t, cmd))
}

// GITX-12 (H10): Push… shows the outgoing commits on a Cancel-first card
// first; Cancel pushes nothing, Push sends them.
func TestGitxPushPreview(t *testing.T) {
	m, dir, bare := gitxUpstreamRepo(t)
	m.dispatchPaletteAction("git-push-preview")
	if m.confirmOpen || !toastHas(m, "Nothing to push") {
		t.Fatal("no note when nothing goes out")
	}
	covWrite(t, dir, "b.txt", "b\n")
	covCommit(t, dir, "second")
	m.dispatchPaletteAction("git-push-preview")
	if !m.confirmOpen || m.confirmKind != confirmKindGitx || gitxCardIDList(m) != "cancel,push" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %v %q", m.confirmOpen, gitxCardIDList(m))
	}
	if !strings.Contains(m.confirm.View(), "second") || !strings.Contains(m.confirm.View(), "origin/main") {
		t.Errorf("card does not list the commit:\n%s", m.confirm.View())
	}
	m.confirmOpen = false
	if cmd := m.handleConfirmSelect("cancel"); cmd != nil {
		t.Error("Cancel ran something")
	}
	if covGit(t, bare, "log", "-1", "--format=%s", "main") != "first" {
		t.Fatal("Cancel pushed")
	}
	m.dispatchPaletteAction("git-push-preview")
	m.confirmOpen = false
	gitxDone(t, m, m.handleConfirmSelect("push"))
	if covGit(t, bare, "log", "-1", "--format=%s", "main") != "second" {
		t.Error("Push did not push")
	}
}

// GITX-12 (H11): Push to… picks a remote, asks the branch name, shows the
// card, then pushes HEAD there.
func TestGitxPushTo(t *testing.T) {
	m, dir, bare := gitxUpstreamRepo(t)
	covWrite(t, dir, "b.txt", "b\n")
	covCommit(t, dir, "second")
	m.dispatchPaletteAction("git-push-to")
	if !m.pickerOpen || m.pickerKind != pickerKindGitx || strings.Join(gitxPickerIDs(m), ",") != "remote-origin" {
		t.Fatalf("remote picker %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "remote-origin"})
	if !m.promptOpen || m.promptKind != promptKindGitx || m.prompt.Value() != "main" {
		t.Fatalf("branch prompt %v %q", m.promptOpen, m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit("review")
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,push" {
		t.Fatalf("no card %q", gitxCardIDList(m))
	}
	m.confirmOpen = false
	gitxDone(t, m, m.handleConfirmSelect("push"))
	if covGit(t, bare, "log", "-1", "--format=%s", "review") != "second" {
		t.Error("review not pushed")
	}
}

// GITX-12 (H8, H9): Pull… offers merge, rebase and another branch; the
// rebase pull keeps the history straight.
func TestGitxPullOptions(t *testing.T) {
	m, dir, bare := gitxUpstreamRepo(t)
	other := t.TempDir()
	covGit(t, other, "clone", "-q", bare, ".")
	covWrite(t, other, "o.txt", "o\n")
	covCommit(t, other, "theirs")
	covGit(t, other, "push", "-q", "origin", "HEAD:main")
	covWrite(t, dir, "m.txt", "m\n")
	covCommit(t, dir, "mine")
	m.dispatchPaletteAction("git-pull-options")
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "pull-merge,pull-rebase,pull-from" {
		t.Fatalf("pull picker %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	gitxDone(t, m, m.handlePickerSelect(picker.SelectMsg{ID: "pull-rebase"}))
	if got := covGit(t, dir, "log", "--format=%s", "-3"); got != "mine\ntheirs\nfirst" {
		t.Errorf("history after a rebase pull:\n%s", got)
	}
	// Pull from another branch: remote, then the branch name.
	covGit(t, other, "checkout", "-q", "-b", "feature")
	covWrite(t, other, "f.txt", "f\n")
	covCommit(t, other, "feature work")
	covGit(t, other, "push", "-q", "origin", "feature")
	m.dispatchPaletteAction("git-pull-options")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "pull-from"})
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "remote-origin"})
	if !m.promptOpen {
		t.Fatal("no branch prompt")
	}
	m.promptOpen = false
	gitxDone(t, m, m.handlePromptSubmit("feature"))
	if covGit(t, dir, "log", "-1", "--format=%s", "--", "f.txt") != "feature work" {
		t.Error("feature not pulled")
	}
}

// GITX-12 (H7, H19): Fetch All Remotes then Show Incoming Commits… lists
// what a pull would bring; Show Outgoing Commits… what a push sends.
func TestGitxIncomingOutgoing(t *testing.T) {
	m, dir, bare := gitxUpstreamRepo(t)
	other := t.TempDir()
	covGit(t, other, "clone", "-q", bare, ".")
	covWrite(t, other, "o.txt", "o\n")
	covCommit(t, other, "theirs")
	covGit(t, other, "push", "-q", "origin", "HEAD:main")
	gitxDone(t, m, m.dispatchPaletteAction("git-fetch-all"))
	m.dispatchPaletteAction("git-incoming")
	theirs := covGit(t, dir, "rev-parse", "--short", "origin/main")
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "commit-"+theirs {
		t.Fatalf("incoming %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.dispatchPaletteAction("git-outgoing")
	if m.pickerOpen || !toastHas(m, "Nothing to push") {
		t.Error("outgoing is not empty")
	}
}

// GITX-12 (H6): Toggle Auto Fetch fetches on a timer while it is on; off
// by default.
func TestGitxAutoFetch(t *testing.T) {
	m, _, _ := gitxUpstreamRepo(t)
	if m.gitxAutoFetchOn() {
		t.Fatal("on by default")
	}
	cmd := m.dispatchPaletteAction("git-auto-fetch")
	if !m.gitxAutoFetchOn() || cmd == nil || !toastHas(m, "Auto fetch on") {
		t.Fatal("not on")
	}
	m.dispatchPaletteAction("git-auto-fetch")
	if m.gitxAutoFetchOn() || !toastHas(m, "Auto fetch off") {
		t.Error("not off")
	}
	// A tick of a stopped timer does nothing.
	if c := m.gitxAutoFetchTick(m.gitxAutoFetchGen() - 1); c != nil {
		t.Error("a stale tick fetched")
	}
}
