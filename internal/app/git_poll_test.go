package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/git"
)

// The 2-second git poll used to start five git processes (rev-parse,
// branch, rev-list, status, log --graph) even when nothing changed. Now
// one `git status --porcelain=v2 --branch` decides; the graph is read
// again only when HEAD, the branch or ahead/behind moved, or it is old.
func TestGitPollKeepsGraphUntilHeadMoves(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@e.x")
	run("config", "user.name", "T")
	run("config", "commit.gpgsign", "false")
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "one")
	t.Chdir(dir)

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	defer clock.Set(func() time.Time { return now })()

	m := &Model{}
	m.applyGitSnapshot(fetchGitCmd()().(GitMsg))
	if len(m.gitCommits) == 0 {
		t.Fatal("no graph after the first full fetch")
	}
	poll := func() GitMsg {
		t.Helper()
		msg := m.pollGitCmd()().(GitMsg)
		m.applyGitSnapshot(msg)
		return msg
	}

	// Nothing changed: the graph is kept, the file list still updates.
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("b\n"), 0o644)
	if msg := poll(); !msg.KeepGraph {
		t.Error("graph was read again though HEAD did not move")
	}
	if len(m.gitFiles) != 1 || m.gitFiles[0].Code != " M" {
		t.Errorf("files = %+v, want a.txt modified", m.gitFiles)
	}
	if len(m.gitCommits) == 0 {
		t.Fatal("kept graph is empty")
	}

	// A commit moves HEAD: the graph is read again.
	run("commit", "-q", "-am", "two")
	if msg := poll(); msg.KeepGraph {
		t.Error("graph kept after a new commit")
	}
	if m.gitCommits[0].Subject != "two" {
		t.Errorf("graph head = %+v, want the new commit", m.gitCommits[0])
	}

	// Tags and other refs do not move HEAD; an old graph is read again.
	if msg := poll(); !msg.KeepGraph {
		t.Error("graph read again right after a fresh read")
	}
	now = now.Add(gitGraphMaxAge + time.Second)
	if msg := poll(); msg.KeepGraph {
		t.Error("old graph was kept")
	}
}

// In the background (BlurMsg) only every gitBlurredEvery-th git tick
// polls; focus back polls at once.
func TestGitPollSlowerInBackground(t *testing.T) {
	// Mouse mode switches on focus (mouse_hover.go) would add their own
	// command to the batch this test counts.
	defer func(v bool) { mouseHover = v }(mouseHover)
	mouseHover = false
	m := Model{gitLoaded: true}
	polls := func(n int) int {
		got := 0
		for i := 0; i < n; i++ {
			var cmd = func() bool {
				nm, cmd, _ := m.updateGitTickForTest()
				m = nm
				return cmd
			}()
			if cmd {
				got++
			}
		}
		return got
	}
	if n := polls(5); n != 5 {
		t.Fatalf("focused: %d polls in 5 ticks, want 5", n)
	}
	m.onTerminalFocus(tea.BlurMsg{})
	if n := polls(10); n != 10/gitBlurredEvery {
		t.Errorf("blurred: %d polls in 10 ticks, want %d", n, 10/gitBlurredEvery)
	}
	if cmd := m.onTerminalFocus(tea.FocusMsg{}); cmd == nil {
		t.Error("focus back gave no git poll")
	}
	if n := polls(3); n != 3 {
		t.Errorf("focused again: %d polls in 3 ticks, want 3", n)
	}
}

// updateGitTickForTest feeds one gitAutoRefreshMsg and reports whether
// that tick polled git (a batch of poll + next tick) or only rescheduled.
func (m Model) updateGitTickForTest() (Model, bool, bool) {
	nm, cmd := m.update(gitAutoRefreshMsg{})
	if cmd == nil {
		return nm, false, false
	}
	// A batch (poll + next tick) returns at once; a lone tea.Tick
	// would sleep for the poll interval, so it is not waited for.
	got := make(chan bool, 1)
	go func() { _, ok := cmd().(tea.BatchMsg); got <- ok }()
	select {
	case ok := <-got:
		return nm, ok, true
	case <-time.After(200 * time.Millisecond):
		return nm, false, true
	}
}

// Spec: the full Source Control refresh (after a git action) reads the
// branch and the changed files with one `git status` and the graph at
// the same time, instead of six git processes one after another.
func TestGitFullRefreshGitProcesses(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("needs git and a shell")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(realGit, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@e.x")
	run("config", "user.name", "T")
	run("config", "commit.gpgsign", "false")
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "one")
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("b\n"), 0o644)
	t.Chdir(dir)
	git.RebaseStatus(dir) // its rev-parse is cached per folder

	// A git on PATH that logs each run, then runs the real one.
	bin, log := t.TempDir(), filepath.Join(t.TempDir(), "runs")
	script := "#!/bin/sh\necho \"$1\" >> '" + log + "'\nexec '" + realGit + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	msg := fetchGitCmd()().(GitMsg)
	if !msg.IsRepo || msg.Branch.Name != "main" || len(msg.Files) != 1 || len(msg.Graph) != 1 {
		t.Fatalf("snapshot = %+v", msg)
	}
	runs, _ := os.ReadFile(log)
	if n := strings.Count(string(runs), "\n"); n > 3 {
		t.Fatalf("%d git processes, want at most 3:\n%s", n, runs)
	}
}
