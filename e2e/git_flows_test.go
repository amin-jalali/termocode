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

// E2E-GIT: git flows on the real binary, driven from the palette and the
// cards (never the network).

// gitRepo is a fresh repo with commits c1..c4, one file each.
func gitRepo(t *testing.T) string {
	t.Helper()
	ws := NewRepo(t)
	for _, n := range []string{"1", "2", "3", "4"} {
		if err := os.WriteFile(filepath.Join(ws, "f"+n+".txt"), []byte("file "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitOut(t, ws, "add", "f"+n+".txt")
		gitOut(t, ws, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-q", "-m", "c"+n)
	}
	return ws
}

func startGit(t *testing.T, ws string) *Term {
	t.Helper()
	tm := startWindow(t, t.TempDir(), ws, nil)
	tm.settle()
	return tm
}

func subjects(t *testing.T, ws string) string {
	t.Helper()
	return strings.TrimSpace(gitOut(t, ws, "log", "--format=%s", "--", "."))
}

// Spec (G-RBS): Interactive Rebase from c2: d drops the row, Enter asks,
// Start Rebase runs it with termocode as git's editor; c2 is gone and the
// other commits stay.
func TestGitInteractiveRebaseDrop(t *testing.T) {
	ws := gitRepo(t)
	tm := startGit(t, ws)
	tm.Palette("Git: Interactive Rebase…")
	tm.WaitText("Rebase from which commit", uiTimeout)
	tm.Type("c2")
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("Interactive Rebase", uiTimeout)
	tm.settle()
	tm.Send("d") // the first row is c2
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("Start the rebase?", uiTimeout)
	tm.settle()
	tm.ClickText("Stash and Continue") // main.go of the fixture repo has changes
	waitUntil(t, "c2 dropped", 30*time.Second, func() bool {
		return !strings.Contains(gitOut(t, ws, "log", "--format=%s"), "c2")
	})
	got := gitOut(t, ws, "log", "--format=%s")
	for _, want := range []string{"c1", "c3", "c4"} {
		if !strings.Contains(got, want) {
			t.Errorf("history lost %s:\n%s", want, got)
		}
	}
	tm.Quit()
}

// Spec (G-UND): Reflog… lists where HEAD was; picking c2's entry asks
// "Move HEAD back to here?" (Cancel first) and Move HEAD does it.
func TestGitReflogMoveHeadBack(t *testing.T) {
	ws := gitRepo(t)
	c2 := strings.TrimSpace(gitOut(t, ws, "rev-parse", "HEAD~2"))
	head := strings.TrimSpace(gitOut(t, ws, "rev-parse", "HEAD"))
	tm := startGit(t, ws)
	tm.Palette("Git: Reflog…")
	tm.WaitText("Reflog: move HEAD back", uiTimeout)
	tm.Type("'c2'")
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("Move HEAD back to here?", uiTimeout)
	tm.settle()
	tm.Send(KeyEnter) // Cancel is focused
	tm.WaitGone("Move HEAD back to here?", uiTimeout)
	if got := strings.TrimSpace(gitOut(t, ws, "rev-parse", "HEAD")); got != head {
		t.Fatalf("Enter moved HEAD to %s", got)
	}
	tm.Palette("Git: Reflog…")
	tm.WaitText("Reflog: move HEAD back", uiTimeout)
	tm.Type("'c2'")
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("Move HEAD back to here?", uiTimeout)
	tm.settle()
	tm.ClickText("Move HEAD  ")
	waitUntil(t, "HEAD at c2", 20*time.Second, func() bool {
		return strings.TrimSpace(gitOut(t, ws, "rev-parse", "HEAD")) == c2
	})
	tm.Quit()
}

// Spec (G-RBS): a rebase that stopped on a conflict is found at start:
// the control card says where it stopped (the file) and Abort puts the
// branch back.
func TestGitStoppedRebaseAbort(t *testing.T) {
	ws := NewRepo(t)
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(msg string) {
		gitOut(t, ws, "add", "f.txt")
		gitOut(t, ws, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-q", "-m", msg)
	}
	gitOut(t, ws, "checkout", "-q", "--", ".") // the fixture repo has an edited main.go
	write("a\n")
	commit("base")
	gitOut(t, ws, "checkout", "-q", "-b", "topic")
	write("b\n")
	commit("topic-change")
	gitOut(t, ws, "checkout", "-q", "main")
	write("c\n")
	commit("main-change")
	gitOut(t, ws, "checkout", "-q", "topic")
	head := strings.TrimSpace(gitOut(t, ws, "rev-parse", "HEAD"))
	// The rebase stops on the conflict (non-zero exit is expected).
	out, _ := exec.Command("git", "-C", ws, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "rebase", "main").CombinedOutput()
	t.Logf("rebase: %s", out)
	if _, err := os.Stat(filepath.Join(ws, ".git", "rebase-merge")); err != nil {
		t.Fatalf("the rebase did not stop: %v", err)
	}

	tm := startGit(t, ws)
	tm.Palette("Git: Rebase in Progress (Continue / Skip / Abort)…")
	tm.WaitText("1 conflict: f.txt", uiTimeout)
	tm.settle()
	tm.Send(KeyEnter) // Close is focused
	tm.WaitGone("1 conflict: f.txt", uiTimeout)
	if _, err := os.Stat(filepath.Join(ws, ".git", "rebase-merge")); err != nil {
		t.Fatal("Enter ended the rebase")
	}
	tm.Palette("Git: Rebase in Progress (Continue / Skip / Abort)…")
	tm.WaitText("1 conflict: f.txt", uiTimeout)
	tm.settle()
	tm.ClickText("Abort   ")
	waitUntil(t, "rebase aborted", 20*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(ws, ".git", "rebase-merge"))
		return err != nil
	})
	if got := strings.TrimSpace(gitOut(t, ws, "rev-parse", "HEAD")); got != head {
		t.Errorf("HEAD %s, want %s", got, head)
	}
	tm.Quit()
}

// Spec (G-LNS): Stage Line puts only the cursor line in the index; the
// file on disk stays as it is.
func TestGitStageOnlyTheCursorLine(t *testing.T) {
	ws := NewRepo(t)
	base := "l1\nl2\nl3\nl4\nl5\nl6\n"
	if err := os.WriteFile(filepath.Join(ws, "lines.txt"), []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, ws, "add", "lines.txt")
	gitOut(t, ws, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-q", "-m", "lines")
	edited := "l1\nX2\nl3\nX4\nl5\nl6\n"
	if err := os.WriteFile(filepath.Join(ws, "lines.txt"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	tm := startGit(t, ws)
	tm.OpenFile("lines.txt")
	tm.WaitText("X4", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Send("4G")
	tm.settle()
	tm.Palette("Git: Stage Line (or Selected Lines)")
	waitUntil(t, "line 4 staged", 15*time.Second, func() bool {
		return strings.TrimSpace(gitOut(t, ws, "show", ":lines.txt")) == "l1\nl2\nl3\nX4\nl5\nl6"
	})
	if b, _ := os.ReadFile(filepath.Join(ws, "lines.txt")); string(b) != edited {
		t.Errorf("the file on disk changed: %q", b)
	}
	tm.Quit()
}
