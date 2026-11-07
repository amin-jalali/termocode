package app

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/amin-jalali/termocode/internal/git"
)

// rcResetAnswer is the fake container repo for the reset commands: HEAD
// and HEAD~1 exist, two commits leave on a reset, one file changed.
func rcResetAnswer(argv []string) (string, int) {
	line := strings.Join(argv, " ")
	switch {
	case line == "git rev-parse --verify -q HEAD":
		return "2222222222222222222222222222222222222222\n", 0
	case line == "git rev-parse --verify -q HEAD~1":
		return "1111111111111111111111111111111111111111\n", 0
	case strings.HasPrefix(line, "git rev-list --count"):
		return "2\n", 0
	case line == "git log -1 --format=%s":
		return "box commit\n", 0
	case line == "git status --porcelain":
		return " M remote.go\n", 0
	case strings.HasPrefix(line, "git reset"), strings.HasPrefix(line, "git update-ref"):
		return "", 0
	}
	return (&rcStashRepo{}).answer(argv)
}

// rcResetWorkspace is a container workspace that answers the resets.
func rcResetWorkspace(t *testing.T) (*Model, *rcHost) {
	t.Helper()
	ws, h := newRCWorkspace(t, fstest.MapFS{"remote.go": {Data: []byte("package main\n")}})
	h.exec.answer = rcResetAnswer
	setProcWorkspace(t, ws)
	m := &Model{ws: ws}
	if !m.gitEnsureLoaded() {
		t.Fatal("the container repo was not seen")
	}
	m.gitStashOps.resetTo, m.gitStashOps.resetTag = "1111111111111111111111111111111111111111", "1111111"
	return m, h
}

// Spec: a soft, mixed or keep reset of a container workspace asks first
// on a Cancel-first card (with the commits that leave, counted in the
// container) and then runs `git reset` in the container.
func TestRCGitResetRunsInContainer(t *testing.T) {
	for _, mode := range []string{git.ResetSoft, git.ResetMixed, git.ResetKeep} {
		m, h := rcResetWorkspace(t)
		m.onResetModePicked("reset-" + mode)
		if !m.confirmOpen || m.confirmKind != confirmKindGitReset {
			t.Fatalf("%s: no reset card", mode)
		}
		if b := m.confirm.Buttons(); len(b) == 0 || b[0].ID != "cancel" {
			t.Errorf("%s: the card does not start with Cancel: %+v", mode, b)
		}
		if !rcRan(h, "git rev-list --count 1111111111111111111111111111111111111111..HEAD") {
			t.Errorf("%s: the commit count was not read in the container: %q", mode, h.exec.calls())
		}
		m.onGitOpsConfirm(confirmKindGitReset, "cancel")
		if rcRan(h, "git reset -q --"+mode+" 1111111111111111111111111111111111111111 --") {
			t.Fatalf("%s: Cancel reset the container", mode)
		}
		m.onGitOpsConfirm(confirmKindGitReset, "reset")
		if !rcRan(h, "git reset -q --"+mode+" 1111111111111111111111111111111111111111 --") {
			t.Errorf("%s: reset not run in the container: %q", mode, h.exec.calls())
		}
	}
}

// Spec: a hard reset of a container workspace asks on a Cancel-first
// card, then for the branch name typed; then it saves HEAD and the
// changes in the container and resets hard there.
func TestRCGitResetHardRunsInContainer(t *testing.T) {
	m, h := rcResetWorkspace(t)
	m.onResetModePicked("reset-hard")
	if !m.confirmOpen || m.confirm.Buttons()[0].ID != "cancel" {
		t.Fatal("no Cancel-first hard reset card")
	}
	m.onGitOpsConfirm(confirmKindGitReset, "reset")
	if !m.promptOpen || m.promptKind != promptKindResetHardTyped {
		t.Fatal("no typed name prompt")
	}
	label := gitResetBranchLabel(m)
	m.handleGitOpsPromptSubmit(label + "x")
	if rcRan(h, "git reset -q --hard 1111111111111111111111111111111111111111 --") {
		t.Fatal("a wrong name reset the container")
	}
	m.promptKind = promptKindResetHardTyped
	m.handleGitOpsPromptSubmit(label)
	var backup, stash bool
	for _, argv := range h.exec.calls() {
		l := strings.Join(argv, " ")
		backup = backup || strings.HasPrefix(l, "git update-ref refs/termocode/backup/")
		stash = stash || strings.HasPrefix(l, "git stash push -m termocode: before reset --hard")
	}
	if !backup || !stash || !rcRan(h, "git reset -q --hard 1111111111111111111111111111111111111111 --") {
		t.Errorf("hard reset not saved and run in the container: %q", h.exec.calls())
	}
}

// Spec: Git: Uncommit of a container workspace undoes the container's
// last commit (a soft reset to HEAD~1 there), naming its subject.
func TestRCGitUncommitRunsInContainer(t *testing.T) {
	m, h := rcResetWorkspace(t)
	m.gitUncommit()
	if !rcRan(h, "git reset -q --soft HEAD~1 --") {
		t.Errorf("uncommit not run in the container: %q", h.exec.calls())
	}
	if !strings.Contains(rcToastText(m), "box commit") {
		t.Errorf("toast %q, want the container commit's subject", rcToastText(m))
	}
}

// Spec: a container workspace whose host cannot run commands resets
// nothing (no card, a toast) and never runs git on the local disk.
func TestRCGitResetNoHostExec(t *testing.T) {
	ws, _ := newRCWorkspace(t, nil)
	ws.Host = nil
	setProcWorkspace(t, ws)
	m := &Model{ws: ws, gitIsRepo: true, gitLoaded: true}
	m.gitStashOps.resetTo, m.gitStashOps.resetTag = "1111111", "1111111"
	m.onResetModePicked("reset-mixed")
	if m.confirmOpen {
		t.Error("a reset card opened with no Host Exec")
	}
	m.gitUncommit()
	if !strings.Contains(rcToastText(m), "Uncommit failed") {
		t.Errorf("toast %q, want Uncommit failed", rcToastText(m))
	}
}

// rcToastText is the text of the toasts on show.
func rcToastText(m *Model) string {
	var b strings.Builder
	for _, t := range m.toast.Live() {
		b.WriteString(t.Title + " " + t.Detail + "\n")
	}
	return b.String()
}
