package app

import (
	"strings"
	"testing"
	"time"
)

// GITX-33 (P9): the Git Command Log lists the git commands termocode ran,
// newest first, with their time; background status reads are left out, a
// token in a URL is hidden and a long argument is cut. The log keeps the
// last gitxCmdLogMax commands.
func TestGitxCmdLog(t *testing.T) {
	gitxCmdLogReset()
	t.Cleanup(gitxCmdLogReset)
	at := time.Date(2026, 11, 6, 9, 30, 0, 0, time.Local)
	gitxCmdLogAdd(at, []string{"/usr/bin/git", "commit", "-m", "fix"})
	gitxCmdLogAdd(at, []string{"git", "--no-optional-locks", "status", "--porcelain=v1", "-z"})
	gitxCmdLogAdd(at, []string{"ls", "-la"})
	gitxCmdLogAdd(at.Add(time.Second), []string{"git", "clone", "https://ann:" + "sk-" + "test@github.com/o/r.git", strings.Repeat("x", 500)})
	page := gitxCmdLogPage()
	lines := strings.Split(strings.TrimSpace(page), "\n")
	var cmds []string
	for _, l := range lines {
		if strings.HasPrefix(l, "09:30:") {
			cmds = append(cmds, l)
		}
	}
	if len(cmds) != 2 || !strings.HasPrefix(cmds[0], "09:30:01  git clone https://ann:***@github.com/o/r.git x") || cmds[1] != "09:30:00  git commit -m fix" {
		t.Fatalf("page:\n%s", page)
	}
	if strings.Contains(page, "sk-"+"test") || strings.Contains(page, strings.Repeat("x", 300)) {
		t.Errorf("a token or a long argument shows:\n%s", page)
	}
	for i := 0; i < gitxCmdLogMax+10; i++ {
		gitxCmdLogAdd(at, []string{"git", "fetch"})
	}
	if n := strings.Count(gitxCmdLogPage(), "git fetch"); n != gitxCmdLogMax {
		t.Errorf("kept %d, want %d", n, gitxCmdLogMax)
	}
	m := &Model{w: 120, h: 40}
	m.dispatchPaletteAction("git-cmd-log")
	if _, ok := aiActionPolicies["git-cmd-log"]; !ok {
		t.Error("no AI policy")
	}
}
