//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// WORKTREES view (WTX-2/3/4) on the real binary: the list shows the
// repository's worktrees; p pins one and the pin is still there after a
// restart; Enter on it re-execs termocode into that worktree.
func TestWorktreesPinSurvivesRestartAndEnterOpens(t *testing.T) {
	ws := NewRepo(t)
	wt := ws + ".worktrees/wt"
	t.Cleanup(func() { _ = os.RemoveAll(ws + ".worktrees") })
	cmd := exec.Command("git", "worktree", "add", "-q", "-b", "feat/wt", wt)
	cmd.Dir = ws
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+ws)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	home := t.TempDir()
	show := func(tm *Term) {
		t.Helper()
		tm.Palette("Worktree: Show")
		tm.WaitText("WORKTREES", uiTimeout)
		tm.WaitText("feat/wt", uiTimeout)
		tm.WaitText("you are here", uiTimeout)
	}
	tm := Start(t, StartOpts{Home: home, Workspace: ws, Cols: 150, Rows: 42})
	tm.WaitText("README.md", uiTimeout)
	show(tm)
	if tm.Has("PINNED") {
		t.Fatal("PINNED before any pin")
	}
	tm.Send("j") // main (you are here) is first; the worktree is next
	tm.settle()
	tm.Send("p")
	tm.WaitText("PINNED", uiTimeout)
	tm.Quit()

	tm = Start(t, StartOpts{Home: home, Workspace: ws, Cols: 150, Rows: 42})
	tm.WaitText("README.md", uiTimeout)
	show(tm)
	tm.WaitText("PINNED", uiTimeout) // the pin survived the restart

	// The cursor starts on the first card: the pinned worktree. Enter
	// re-execs into it; there "you are here" moves onto its card.
	tm.Send(KeyEnter)
	tm.WaitFor("termocode in the worktree", 20*time.Second, func(ls []string) bool {
		return !strings.Contains(strings.Join(ls, "\n"), "WORKTREES")
	})
	tm.settle()
	show(tm)
	tm.WaitFor("you are here on feat/wt", uiTimeout, func(ls []string) bool {
		for _, l := range ls {
			if strings.Contains(l, "feat/wt") && strings.Contains(l, "you are here") {
				return true
			}
		}
		return false
	})
	tm.Quit()
}
