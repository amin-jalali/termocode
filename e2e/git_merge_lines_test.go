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

// E2E-GIT (rest): the three-way merge view (G-MRG) and line unstage /
// discard (G-LNS) on the real binary.

// commitAll commits every change in ws with msg.
func commitAll(t *testing.T, ws, msg string) {
	t.Helper()
	gitOut(t, ws, "add", "-A")
	gitOut(t, ws, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-q", "-m", msg)
}

// Spec (G-MRG): a merge that stopped on a conflict in f.txt; Git: Open
// Merge View shows base, ours and theirs; t takes theirs into the result
// and r saves and stages it: the file holds theirs and no conflict is
// left in the index.
func TestGitMergeViewTakeTheirsResolves(t *testing.T) {
	ws := NewRepo(t)
	gitOut(t, ws, "checkout", "-q", "--", ".") // the fixture repo has an edited main.go
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("top\nbase-line\nend\n")
	commitAll(t, ws, "base")
	gitOut(t, ws, "checkout", "-q", "-b", "topic")
	write("top\ntheirs-line\nend\n")
	commitAll(t, ws, "theirs")
	gitOut(t, ws, "checkout", "-q", "main")
	write("top\nours-line\nend\n")
	commitAll(t, ws, "ours")
	// The merge stops on the conflict (non-zero exit is expected).
	out, _ := exec.Command("git", "-C", ws, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "merge", "topic").CombinedOutput()
	t.Logf("merge: %s", out)
	if u := strings.TrimSpace(gitOut(t, ws, "diff", "--name-only", "--diff-filter=U")); u != "f.txt" {
		t.Fatalf("no conflict in f.txt: %q", u)
	}

	tm := startGit(t, ws)
	tm.Palette("Git: Open Merge View")
	tm.WaitFor("base, ours and theirs", uiTimeout, func(ls []string) bool {
		s := strings.Join(ls, "\n")
		return strings.Contains(s, "Base") && strings.Contains(s, "Ours") && strings.Contains(s, "Theirs") &&
			strings.Contains(s, "base-line") && strings.Contains(s, "ours-line") && strings.Contains(s, "theirs-line")
	})
	tm.settle()
	tm.Send("t")
	tm.WaitFor("theirs in the result, no markers", uiTimeout, func(ls []string) bool {
		return !strings.Contains(strings.Join(ls, "\n"), "<<<<<<<")
	})
	tm.settle()
	tm.Send("r")
	waitUntil(t, "f.txt resolved and staged", 20*time.Second, func() bool {
		return strings.TrimSpace(gitOut(t, ws, "diff", "--name-only", "--diff-filter=U")) == ""
	})
	if b, _ := os.ReadFile(filepath.Join(ws, "f.txt")); string(b) != "top\ntheirs-line\nend\n" {
		t.Errorf("f.txt on disk = %q, want theirs", b)
	}
	if got := gitOut(t, ws, "show", ":f.txt"); got != "top\ntheirs-line\nend\n" {
		t.Errorf("f.txt in the index = %q, want theirs", got)
	}
	tm.Quit()
}

// linesRepo commits lines.txt (l1..l6), then edits lines 2 and 4 on disk.
func linesRepo(t *testing.T) (ws, edited string) {
	t.Helper()
	ws = NewRepo(t)
	if err := os.WriteFile(filepath.Join(ws, "lines.txt"), []byte("l1\nl2\nl3\nl4\nl5\nl6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, ws, "add", "lines.txt")
	gitOut(t, ws, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-q", "-m", "lines")
	edited = "l1\nX2\nl3\nX4\nl5\nl6\n"
	if err := os.WriteFile(filepath.Join(ws, "lines.txt"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws, edited
}

// openAtLine4 opens lines.txt in normal mode with the cursor on line 4.
func (tm *Term) openAtLine4() {
	tm.t.Helper()
	tm.OpenFile("lines.txt")
	tm.WaitText("X4", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Send("4G")
	tm.settle()
}

// Spec (G-LNS): with both edits staged, Unstage Line takes only the
// cursor line out of the index; the file on disk stays as it is.
func TestGitUnstageOnlyTheCursorLine(t *testing.T) {
	ws, edited := linesRepo(t)
	gitOut(t, ws, "add", "lines.txt")
	tm := startGit(t, ws)
	tm.openAtLine4()
	tm.Palette("Git: Unstage Line (or Selected Lines)")
	waitUntil(t, "line 4 unstaged", 15*time.Second, func() bool {
		return strings.TrimSpace(gitOut(t, ws, "show", ":lines.txt")) == "l1\nX2\nl3\nl4\nl5\nl6"
	})
	if b, _ := os.ReadFile(filepath.Join(ws, "lines.txt")); string(b) != edited {
		t.Errorf("the file on disk changed: %q", b)
	}
	tm.Quit()
}

// Spec (G-LNS): Discard Line asks first (Cancel focused: Enter keeps the
// file); Discard reverts only the cursor line on disk.
func TestGitDiscardOnlyTheCursorLine(t *testing.T) {
	ws, edited := linesRepo(t)
	tm := startGit(t, ws)
	tm.openAtLine4()
	tm.Palette("Git: Discard Line (or Selected Lines)")
	tm.WaitText("Discard line 4?", uiTimeout)
	tm.settle()
	tm.Send(KeyEnter) // Cancel is focused
	tm.WaitGone("Discard line 4?", uiTimeout)
	if b, _ := os.ReadFile(filepath.Join(ws, "lines.txt")); string(b) != edited {
		t.Fatalf("Enter discarded: %q", b)
	}
	tm.Palette("Git: Discard Line (or Selected Lines)")
	tm.WaitText("Discard line 4?", uiTimeout)
	tm.settle()
	tm.ClickText("Discard ")
	waitUntil(t, "line 4 discarded on disk", 15*time.Second, func() bool {
		b, _ := os.ReadFile(filepath.Join(ws, "lines.txt"))
		return string(b) == "l1\nX2\nl3\nl4\nl5\nl6\n"
	})
	tm.WaitFor("the editor shows l4 again", uiTimeout, func(ls []string) bool {
		s := strings.Join(ls, "\n")
		return strings.Contains(s, "l4") && !strings.Contains(s, "X4")
	})
	if got := strings.TrimSpace(gitOut(t, ws, "show", ":lines.txt")); got != "l1\nl2\nl3\nl4\nl5\nl6" {
		t.Errorf("the index changed: %q", got)
	}
	tm.Quit()
}
