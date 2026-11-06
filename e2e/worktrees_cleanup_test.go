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

// wtE2EGit runs git in dir for the W4 tests.
func wtE2EGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=E2E", "GIT_AUTHOR_EMAIL=e2e@example.com", "GIT_COMMITTER_NAME=E2E", "GIT_COMMITTER_EMAIL=e2e@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// wtE2EFilter shows the view filtered to one worktree.
func wtE2EFilter(tm *Term, text string) {
	tm.Palette("Worktree: Show")
	tm.WaitText("WORKTREES", uiTimeout)
	tm.Send("/")
	tm.Send("\x15") // Ctrl+U: an old filter goes
	tm.Type(text)
	tm.Send(KeyEnter)
	tm.settle()
}

// WTX-13 on the real binary: an agent started in a worktree (a fake CLI)
// runs in a terminal tab; s asks first (Keep running is lit: Enter keeps
// it); Sleep ends the tab's process and the worktree moves under ASLEEP;
// its files are untouched; s again wakes it.
func TestWorktreeSleepEndsTheAgentTab(t *testing.T) {
	ws := NewRepo(t)
	t.Cleanup(func() { _ = os.RemoveAll(ws + ".worktrees") })
	bin := t.TempDir()
	bye, cwd := filepath.Join(bin, "bye.txt"), filepath.Join(bin, "cwd.txt")
	script := "#!/bin/sh\ntrap 'echo bye > \"" + bye + "\"; exit 0' HUP TERM INT\npwd > '" + cwd + "'\necho \"CWD=$(pwd)\"\nprintf '  ? for shortcuts\\n'\nwhile :; do sleep 1; done\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	tm := Start(t, StartOpts{Workspace: ws, Cols: 150, Rows: 42, Env: []string{"PATH=" + bin + ":" + os.Getenv("PATH")}})
	tm.WaitText("README.md", uiTimeout)
	tm.Palette("Worktree: New")
	tm.WaitText("New worktree", uiTimeout)
	tm.WaitText("Claude Code", uiTimeout)
	tm.Type("feat/nap")
	tm.Send(keyTab + keyTab + keyTab + keyTab) // Issue, From, Branch → Agent
	tm.Send(keyRight)                          // none → Claude Code
	tm.Send(keyTab + keyTab)                   // Task → the buttons
	tm.Send(keyRight + keyRight + keyRight)    // → Create + Start
	tm.settle()
	tm.Send(KeyEnter)
	wt := filepath.Join(ws+".worktrees", "feat-nap")
	wtWaitCwd(t, tm, cwd, wt)
	if err := os.WriteFile(filepath.Join(wt, "keep.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Worktree: Show from the terminal takes the keys (W3 leftover).
	wtE2EFilter(tm, "nap")
	tm.Send("s")
	tm.WaitText("Put feat/nap to sleep?", uiTimeout)
	tm.WaitText("Claude Code", uiTimeout)
	tm.Send(KeyEnter) // Keep running is lit
	tm.WaitGone("Put feat/nap to sleep?", uiTimeout)
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(bye); err == nil {
		t.Fatal("Keep running ended the agent")
	}
	tm.Send("s")
	tm.WaitText("Put feat/nap to sleep?", uiTimeout)
	tm.Send(keyRight) // → Sleep
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("ASLEEP", uiTimeout)
	if b := wtWaitFile(t, tm, bye, 20*time.Second); strings.TrimSpace(string(b)) != "bye" {
		t.Errorf("the agent said %q", b)
	}
	if b, err := os.ReadFile(filepath.Join(wt, "keep.txt")); err != nil || string(b) != "mine\n" {
		t.Errorf("sleep touched the folder: %q %v", b, err)
	}
	tm.Send("s")
	tm.WaitText("Woke feat/nap", uiTimeout)
	tm.WaitGone("ASLEEP", uiTimeout)
	tm.Quit()
}

// WTX-14 on the real binary: x on a dirty worktree names the changed
// file; Enter (Cancel is lit) keeps everything. D on a merged clean one
// asks once and deletes the folder and the branch.
func TestWorktreeArchiveDirtyCancelDeleteMerged(t *testing.T) {
	ws := NewRepo(t)
	t.Cleanup(func() { _ = os.RemoveAll(ws + ".worktrees") })
	dirty, merged := filepath.Join(ws+".worktrees", "dirty"), filepath.Join(ws+".worktrees", "merged")
	wtE2EGit(t, ws, "worktree", "add", "-q", "-b", "feat/dirty", dirty)
	wtE2EGit(t, ws, "worktree", "add", "-q", "-b", "feat/merged", merged)
	if err := os.WriteFile(filepath.Join(dirty, "draft.txt"), []byte("draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm := Start(t, StartOpts{Workspace: ws, Cols: 150, Rows: 42})
	tm.WaitText("README.md", uiTimeout)

	wtE2EFilter(tm, "dirty")
	tm.Send("x")
	tm.WaitText("Archive anyway (loses 1 changed file)", uiTimeout)
	tm.Send(KeyEnter) // Cancel is lit
	tm.WaitGone("Archive anyway", uiTimeout)
	if _, err := os.Stat(filepath.Join(dirty, "draft.txt")); err != nil {
		t.Fatal("Cancel touched the folder")
	}

	wtE2EFilter(tm, "merged")
	tm.Send("D")
	tm.WaitText("Delete feat/merged and its branch?", uiTimeout)
	tm.WaitText("It is merged into", uiTimeout)
	tm.Send(keyRight) // → Delete…
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("Deleted feat/merged", uiTimeout)
	if _, err := os.Stat(merged); !os.IsNotExist(err) {
		t.Errorf("the folder is still there: %v", err)
	}
	if out := wtE2EGit(t, ws, "branch", "--list", "feat/merged"); strings.TrimSpace(out) != "" {
		t.Errorf("the branch is still there: %q", out)
	}
	tm.Quit()
}

// WTX-15 on the real binary: C ticks the merged worktree (not the dirty
// one); Enter on the first row shows the summary card; Archive archives
// it (its branch stays) and the row stays under ARCHIVED.
func TestWorktreeBulkCleanup(t *testing.T) {
	ws := NewRepo(t)
	t.Cleanup(func() { _ = os.RemoveAll(ws + ".worktrees") })
	done, dirty := filepath.Join(ws+".worktrees", "done"), filepath.Join(ws+".worktrees", "wip")
	wtE2EGit(t, ws, "worktree", "add", "-q", "-b", "feat/done", done)
	wtE2EGit(t, ws, "worktree", "add", "-q", "-b", "feat/wip", dirty)
	if err := os.WriteFile(filepath.Join(dirty, "draft.txt"), []byte("draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm := Start(t, StartOpts{Workspace: ws, Cols: 150, Rows: 42})
	tm.WaitText("README.md", uiTimeout)
	tm.Palette("Worktree: Show")
	tm.WaitText("feat/wip", uiTimeout)
	tm.Send("C")
	tm.WaitText("Clean up worktrees", uiTimeout)
	tm.WaitText("Archive the 1 ticked worktree", uiTimeout)
	tm.WaitText("[x] feat/done", uiTimeout)
	tm.WaitText("[ ] feat/wip", uiTimeout)
	tm.Send(KeyEnter) // the first row
	tm.WaitText("Archive 1 worktree?", uiTimeout)
	tm.Send(keyRight) // → Archive 1
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("Cleaned up", uiTimeout)
	tm.WaitText("ARCHIVED", uiTimeout)
	if _, err := os.Stat(done); !os.IsNotExist(err) {
		t.Errorf("not archived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dirty, "draft.txt")); err != nil {
		t.Error("the dirty one was archived")
	}
	if out := wtE2EGit(t, ws, "branch", "--list", "feat/done"); strings.TrimSpace(out) == "" {
		t.Error("the cleanup deleted the branch")
	}
	tm.Quit()
}
