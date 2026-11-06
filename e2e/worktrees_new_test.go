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

const keyTab, keyRight = "\t", "\x1b[C"

// wtRepoWithConfig is a repository whose .termocode/worktrees.json copies
// .env (git ignores it) and runs a setup line that leaves a file.
func wtRepoWithConfig(t *testing.T) string {
	t.Helper()
	ws := NewRepo(t)
	t.Cleanup(func() { _ = os.RemoveAll(ws + ".worktrees") })
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(ws, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", ".env\n")
	write(".termocode/worktrees.json", `{
  // copied into each new worktree
  "include": [".env"],
  "setup": ["echo ran > setup-ran.txt"]
}`)
	cmd := exec.Command("sh", "-c", "git add -A && git commit -qm 'worktrees config'")
	cmd.Dir = ws
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+ws, "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	write(".env", "API_URL=http://localhost\n")
	return ws
}

// wtWaitCwd waits for the folder the fake agent wrote to path and checks
// it is wt (read from a file: a long macOS temp path wraps on the screen).
func wtWaitCwd(t *testing.T, tm *Term, path, wt string) {
	t.Helper()
	got := strings.TrimSpace(string(wtWaitFile(t, tm, path, 30*time.Second)))
	if real, _ := filepath.EvalSymlinks(wt); got != wt && got != real {
		t.Fatalf("the agent started in %q, want %q", got, wt)
	}
}

func wtWaitFile(t *testing.T, tm *Term, path string, timeout time.Duration) []byte {
	t.Helper()
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		if b, err := os.ReadFile(path); err == nil {
			return b
		}
		time.Sleep(100 * time.Millisecond)
	}
	tm.dump("no " + filepath.Base(path))
	t.Fatalf("%s did not appear", path)
	return nil
}

// Worktree: New… on the real binary (WTX-5/6/7): the form lists the
// copy and the setup line; Create makes <repo>.worktrees/<branch> on the
// new branch with .env copied; the setup runs only after the trust card
// (Not now is first, so the first Enter would not run it).
func TestWorktreeNewIncludeAndSetup(t *testing.T) {
	ws := wtRepoWithConfig(t)
	tm := Start(t, StartOpts{Workspace: ws, Cols: 150, Rows: 42})
	tm.WaitText("README.md", uiTimeout)
	tm.Palette("Worktree: New")
	tm.WaitText("New worktree", uiTimeout)
	tm.WaitText("copy .env (1 file)", uiTimeout)
	tm.WaitText("run setup: echo ran > setup-ran.txt", uiTimeout)
	tm.Type("feat/e2e")
	tm.WaitText("feat-e2e", uiTimeout) // the folder follows the name
	// Enter in a field moves on and makes nothing.
	tm.Send(KeyEnter)
	tm.settle()
	if _, err := os.Stat(ws + ".worktrees"); err == nil {
		t.Fatal("Enter in the name field made the worktree")
	}
	for range 5 { // Issue, From, Branch, Agent, Task → the buttons (Cancel lit)
		tm.Send(keyTab)
	}
	tm.Send(keyRight + keyRight) // Cancel → Start point… → Create
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("Run this repository's setup?", 20*time.Second)
	tm.WaitText("echo ran > setup-ran.txt", uiTimeout)
	wt := filepath.Join(ws+".worktrees", "feat-e2e")
	if b, err := os.ReadFile(filepath.Join(wt, ".env")); err != nil || string(b) != "API_URL=http://localhost\n" {
		t.Fatalf(".env in the worktree: %q %v", b, err)
	}
	out, _ := exec.Command("git", "-C", wt, "branch", "--show-current").Output()
	if strings.TrimSpace(string(out)) != "feat/e2e" {
		t.Errorf("branch %q", out)
	}
	if _, err := os.Stat(filepath.Join(wt, "setup-ran.txt")); err == nil {
		t.Fatal("the setup ran before the trust card was answered")
	}
	tm.Send(keyRight) // Not now → Trust and run
	tm.settle()
	tm.Send(KeyEnter)
	if b := wtWaitFile(t, tm, filepath.Join(wt, "setup-ran.txt"), 20*time.Second); strings.TrimSpace(string(b)) != "ran" {
		t.Errorf("setup wrote %q", b)
	}
	tm.WaitText("Setup done", uiTimeout)
	tm.Quit()
}

// Create + Start (WTX-8, AGX-2): the agent starts in the new worktree
// (the fake CLI prints its folder) and the task is typed once its input
// box shows.
func TestWorktreeNewStartsAgentWithTask(t *testing.T) {
	ws := NewRepo(t)
	t.Cleanup(func() { _ = os.RemoveAll(ws + ".worktrees") })
	bin := t.TempDir()
	rec, cwd := filepath.Join(bin, "got.txt"), filepath.Join(bin, "cwd.txt")
	script := "#!/bin/sh\npwd > '" + cwd + "'\necho \"CWD=$(pwd)\"\nprintf '  ? for shortcuts\\n'\nIFS= read -r line\nprintf '%s\\n' \"$line\" > '" + rec + "'\necho \"GOT: $line\"\nsleep 30\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	tm := Start(t, StartOpts{Workspace: ws, Cols: 150, Rows: 42, Env: []string{"PATH=" + bin + ":" + os.Getenv("PATH")}})
	tm.WaitText("README.md", uiTimeout)
	tm.Palette("Worktree: New")
	tm.WaitText("New worktree", uiTimeout)
	tm.WaitText("Claude Code", uiTimeout)
	tm.Type("feat/agent")
	tm.Send(keyTab + keyTab + keyTab + keyTab) // Issue, From, Branch → Agent
	tm.Send(keyRight)                          // none → Claude Code
	tm.Send(keyTab)                            // → Task
	tm.Type("say hello")
	tm.Send(keyTab)                         // → the buttons (Cancel lit)
	tm.Send(keyRight + keyRight + keyRight) // → Create + Start
	tm.settle()
	tm.Send(KeyEnter)
	wtWaitCwd(t, tm, cwd, filepath.Join(ws+".worktrees", "feat-agent"))
	if b := wtWaitFile(t, tm, rec, 30*time.Second); strings.TrimSpace(string(b)) != "say hello" {
		t.Errorf("the agent got %q", b)
	}
	tm.Quit()
}
