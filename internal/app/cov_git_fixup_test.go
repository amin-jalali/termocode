package app

import (
	"strings"
	"testing"
)

// Fixup into an older commit: the card counts the staged files, warns
// when the commits are pushed already, and has Cancel first; Cancel
// runs nothing. A range with a merge commit is refused.
func TestCovGitFixupCard(t *testing.T) {
	m, dir := covThreeCommits(t)
	covGit(t, dir, "remote", "add", "origin", covBare(t))
	covGit(t, dir, "push", "-q", "-u", "origin", "main")
	covWrite(t, dir, "a.txt", "fix\n")
	covWrite(t, dir, "b.txt", "fix\n")
	covGit(t, dir, "add", "a.txt", "b.txt")
	covRefresh(m)
	if m.gitFixupIntoCmd("") != nil {
		t.Error("empty target gave a command")
	}
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowCommit && r.subject == "second" })
	cmd, _ := covKey(m, covRunes("F"))
	m.onRebaseMsg(covMsg[fixupCheckMsg](t, cmd))
	checkCardRule(t, "fixup", *m, true)
	body := m.confirm.View()
	for _, want := range []string{"2 files", "on origin/main already"} {
		if !strings.Contains(body, want) {
			t.Errorf("fixup card misses %q", want)
		}
	}
	m.confirmOpen = false
	if m.handleConfirmSelect("cancel") != nil || m.fixupTarget != "" {
		t.Error("Cancel ran the fixup")
	}

	// A merge commit in the range.
	covGit(t, dir, "reset", "-q", "--hard")
	covGit(t, dir, "checkout", "-q", "-b", "side", "HEAD~1")
	covWrite(t, dir, "s.txt", "s\n")
	covCommit(t, dir, "side")
	covGit(t, dir, "checkout", "-q", "main")
	covGit(t, dir, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	covWrite(t, dir, "a.txt", "again\n")
	covGit(t, dir, "add", "a.txt")
	covRefresh(m)
	m.onRebaseMsg(covMsg[fixupCheckMsg](t, m.gitFixupIntoCmd(covGit(t, dir, "rev-parse", "HEAD~2"))))
	if !toastHas(m, "Can't fix up into this commit") {
		t.Error("merge in range not refused")
	}
	if fileWord(1) != "file" {
		t.Error("fileWord")
	}
}
