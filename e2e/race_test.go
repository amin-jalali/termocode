//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// raceBin is a PATH folder with git, nvim, sh and the fake agent as
// "opencode" and "aider" only: no real agent CLI can be found or run.
func raceBin(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	for _, p := range []string{"git", "nvim", "sh"} {
		full, err := exec.LookPath(p)
		if err != nil {
			t.Skip(p + " not found")
		}
		if err := os.Symlink(full, filepath.Join(bin, p)); err != nil {
			t.Fatal(err)
		}
	}
	fake, err := filepath.Abs("testdata/fake-race-agent.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"opencode", "aider"} {
		if err := os.Symlink(fake, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

func lineWith(ls []string, words ...string) bool {
	for _, l := range ls {
		ok := true
		for _, w := range words {
			ok = ok && strings.Contains(l, w)
		}
		if ok {
			return true
		}
	}
	return false
}

// Spec (AGX-4, AGX-5): Agent: Race… in the real binary with a fake agent
// CLI in a temp repo. Two lanes start in their own worktrees, each gets
// the task typed once its input box shows, edits files differently; the
// compare page shows both done, v runs the verify command per lane, m
// merges the first lane into the workspace and Keep (the first, focused
// answer) leaves the other lane's worktree.
func TestAgentRace(t *testing.T) {
	bin := raceBin(t)
	tm := Start(t, StartOpts{Cols: 150, Rows: 42, Env: []string{"PATH=" + bin + string(os.PathListSeparator) + "/usr/bin:/bin"}})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()

	tm.Palette("Agent: Race…")
	tm.WaitText("one task, several agents", uiTimeout)
	if tm.Has("Claude Code") || tm.Has("Codex") {
		t.Fatalf("a real agent is listed:\n%s", tm.Screen())
	}
	tm.Type("write the lane note")
	tm.Send(KeyEnter) // to the first lane (OpenCode)
	tm.Send(" ")
	tm.Send(KeyDown) // Aider
	tm.Send(" ")
	tm.Send(KeyDown) // the verify field
	tm.Type("test -f lane-note.txt")
	tm.Send(KeyEnter) // to Cancel
	tm.Send("\t")     // to Start
	tm.WaitText("Start 2 lanes", uiTimeout)
	tm.step("race card filled")
	tm.Send(KeyEnter)

	tm.WaitText("RACE race/write-the-lane-note", uiTimeout)
	tm.WaitFor("both lanes done", 40*time.Second, func(ls []string) bool {
		return lineWith(ls, "OpenCode", "done") && lineWith(ls, "Aider", "done")
	})
	tm.WaitFor("files read", uiTimeout, func(ls []string) bool { return lineWith(ls, "lane-note.txt", "(new)") })
	tm.step("race done")

	dir := filepath.Join(tm.ws, ".git", "termocode", "worktrees")
	one, two := filepath.Join(dir, "race-1-1-opencode"), filepath.Join(dir, "race-1-2-aider")
	if b, err := os.ReadFile(filepath.Join(one, "lane-note.txt")); err != nil || !strings.Contains(string(b), "write the lane note") {
		t.Fatalf("lane 1 did not get the task: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(two, "other.txt")); err != nil {
		t.Fatalf("lane 2 did not work: %v", err)
	}

	tm.Send("v")
	tm.WaitFor("verify results", 20*time.Second, func(ls []string) bool {
		return lineWith(ls, "OpenCode", "passed") && lineWith(ls, "Aider", "exit 1")
	})
	tm.step("verified")

	tm.Send("m") // the first lane is selected
	tm.WaitText("Keep the other lanes", uiTimeout)
	tm.step("other lanes card")
	tm.Send(KeyEnter) // Keep: the safe, focused answer
	tm.WaitGone("Keep the other lanes", uiTimeout)
	tm.WaitFor("merged", uiTimeout, func(ls []string) bool { return lineWith(ls, "OpenCode", "merged") })
	if b, err := os.ReadFile(filepath.Join(tm.ws, "lane-note.txt")); err != nil || !strings.Contains(string(b), "lane one") {
		t.Errorf("the winner is not in the workspace: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(tm.ws, "other.txt")); err == nil {
		t.Error("the other lane's file reached the workspace")
	}
	if _, err := os.Stat(two); err != nil {
		t.Errorf("Keep removed the other lane: %v", err)
	}
	if _, err := os.Stat(one); err == nil {
		t.Error("the merged lane's worktree is still there")
	}
	tm.Send(KeyEsc)
	tm.WaitGone("RACE race/", uiTimeout)
	tm.Quit()
}
