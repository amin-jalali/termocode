package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
)

// gitIdleRepo is a one-commit repo in a temp dir (the cwd) with a model
// that has read it once.
func gitIdleRepo(t *testing.T) (*Model, func(args ...string)) {
	t.Helper()
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
	_ = os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "sub", "a.txt"), []byte("a\n"), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "one")
	t.Chdir(dir)
	m := &Model{gitGate: &gitPollGate{}}
	m.applyGitSnapshot(fetchGitCmd()().(GitMsg))
	// A full poll records the fingerprint the idle wait compares with.
	m.applyGitSnapshot(m.pollGitCmd()().(GitMsg))
	return m, run
}

// idleSteps runs gitIdleWait with a fake clock; during step k (1-based)
// it calls at(k). It returns how many 2 s steps passed before the poll.
func idleSteps(t *testing.T, m *Model, at func(step int)) int {
	t.Helper()
	cwd, err := m.rootDir()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	msg := gitIdleWait(m.gitGate, cwd, func(d time.Duration) {
		if d != gitPollInterval {
			t.Fatalf("slept %v, want %v", d, gitPollInterval)
		}
		n++
		if n > 2*gitIdleEvery {
			t.Fatalf("still waiting after %d steps", n)
		}
		if at != nil {
			at(n)
		}
	})
	if _, ok := msg.(gitAutoRefreshMsg); !ok {
		t.Fatalf("wait ended with %#v", msg)
	}
	return n
}

// While nobody uses termocode the 2 s git poll does not wake the UI or
// start git when nothing changed: it polls every gitIdleEvery steps
// (10 s). A commit from another terminal (.git changes) or a new file
// at the root is seen on the next step, and so is any key.
func TestGitPollSleepsWhileIdle(t *testing.T) {
	m, run := gitIdleRepo(t)

	if n := idleSteps(t, m, nil); n != gitIdleEvery {
		t.Fatalf("idle: polled after %d steps, want %d", n, gitIdleEvery)
	}
	// An edit inside a file another program made (FIX-33): polled after
	// that step, not on the 10 s safety step.
	if n := idleSteps(t, m, func(k int) {
		if k == 1 {
			_ = os.WriteFile(filepath.Join("sub", "a.txt"), []byte("b\n"), 0o644)
			waitTreeSeen(t, m)
		}
	}); n != 1 {
		t.Fatalf("edit: polled after %d steps, want 1", n)
	}
	m.applyGitSnapshot(m.pollGitCmd()().(GitMsg))
	if len(m.gitFiles) != 1 {
		t.Fatalf("files after the edit = %+v", m.gitFiles)
	}

	// A commit made elsewhere during step 2: polled after step 2.
	if n := idleSteps(t, m, func(k int) {
		if k == 2 {
			run("commit", "-q", "-am", "two")
		}
	}); n != 2 {
		t.Fatalf("commit: polled after %d steps, want 2", n)
	}
	m.applyGitSnapshot(m.pollGitCmd()().(GitMsg))
	if m.gitCommits[0].Subject != "two" || len(m.gitFiles) != 0 {
		t.Fatalf("after the commit: head %q, files %+v", m.gitCommits[0].Subject, m.gitFiles)
	}

	// A new file at the root: polled after that step.
	if n := idleSteps(t, m, func(k int) {
		if k == 3 {
			_ = os.WriteFile("new.txt", []byte("n\n"), 0o644)
			waitTreeSeen(t, m) // its file event too, or it would wake a later wait
		}
	}); n != 3 {
		t.Fatalf("new file: polled after %d steps, want 3", n)
	}
	m.applyGitSnapshot(m.pollGitCmd()().(GitMsg))

	// A key during step 1: polled after step 1. A tick alone is no activity.
	if n := idleSteps(t, m, func(k int) {
		if k == 1 {
			m.noteGitActivity(gitAutoRefreshMsg{})
			m.noteGitActivity(GitMsg{})
		}
	}); n != gitIdleEvery {
		t.Fatalf("ticks only: polled after %d steps, want %d", n, gitIdleEvery)
	}
	if n := idleSteps(t, m, func(k int) {
		if k == 1 {
			m.noteGitActivity(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
		}
	}); n != 1 {
		t.Fatalf("key: polled after %d steps, want 1", n)
	}
}

// In the background the idle wait alone slows the poll: the blurred
// tick skips (TestGitPollSlowerInBackground) do not stack on top of it,
// or a background window would poll only every 50 s.
func TestGitPollIdleWaitNotSkippedWhenBlurred(t *testing.T) {
	defer func(v bool) { mouseHover = v }(mouseHover)
	mouseHover = false
	pm, _ := gitIdleRepo(t)
	m := *pm
	m.onTerminalFocus(tea.BlurMsg{})
	for i := 0; i < 3; i++ {
		nm, polled, _ := m.updateGitTickForTest()
		m = nm
		if !polled {
			t.Fatalf("blurred tick %d did not poll", i)
		}
	}
}

// FIX-40: the poll's status takes no lock, so it never settles a touched
// file in the index; about once a minute the poll refreshes the index
// (git diff-files, which never refreshes, is the witness).
func TestGitPollRefreshesTheIndexAboutOnceAMinute(t *testing.T) {
	now := time.Now()
	defer clock.Set(func() time.Time { return now })()
	m, _ := gitIdleRepo(t)
	dirty := func() string {
		t.Helper()
		out, err := exec.Command("git", "diff-files", "--name-only").CombinedOutput()
		if err != nil {
			t.Fatalf("diff-files: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	later := now.Add(time.Hour)
	if err := os.Chtimes(filepath.Join("sub", "a.txt"), later, later); err != nil {
		t.Fatal(err)
	}
	m.applyGitSnapshot(m.pollGitCmd()().(GitMsg))
	if dirty() == "" {
		t.Fatal("refreshed again within the minute (the setup poll just did)")
	}
	now = now.Add(gitIndexRefreshEvery + time.Second)
	m.applyGitSnapshot(m.pollGitCmd()().(GitMsg))
	if got := dirty(); got != "" {
		t.Errorf("a minute on, the poll did not refresh the index: %q", got)
	}
	if len(m.gitFiles) != 0 {
		t.Errorf("a touched file shows as changed: %+v", m.gitFiles)
	}
}

// waitTreeSeen waits until the work tree watch has seen a change (file
// events come a moment after the write); the stat scan sees it at once.
func waitTreeSeen(t *testing.T, m *Model) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !m.gitGate.treeSeen() {
		if time.Now().After(deadline) {
			t.Fatal("the work tree watch never saw the change")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// FIX-33: edits by another program inside existing files, in a new file
// in a folder and in an already changed file, each wake the idle poll on
// the next 2 s step, with file events and with the stat scan used where
// events are missing (network folders, other systems).
func TestGitPollSeesEditsInsideTheTree(t *testing.T) {
	for _, scan := range []bool{false, true} {
		name := "events"
		if scan {
			name = "scan"
		}
		t.Run(name, func(t *testing.T) {
			defer func(v bool) { gitTreeNoWatch = v }(gitTreeNoWatch)
			gitTreeNoWatch = scan
			m, _ := gitIdleRepo(t)
			want := gitTreeScan
			if !scan && gitTreeWatchSupported {
				want = gitTreeWatch
			}
			if got := m.gitGate.treeBackend(); got != want {
				t.Fatalf("backend = %v, want %v", got, want)
			}
			if n := idleSteps(t, m, nil); n != gitIdleEvery {
				t.Fatalf("idle: polled after %d steps, want %d", n, gitIdleEvery)
			}
			for _, c := range []struct {
				what string
				do   func()
			}{
				{"edit a clean file", func() { _ = os.WriteFile(filepath.Join("sub", "a.txt"), []byte("b\n"), 0o644) }},
				{"edit it again", func() { _ = os.WriteFile(filepath.Join("sub", "a.txt"), []byte("c\n"), 0o644) }},
				{"new file in a folder", func() { _ = os.WriteFile(filepath.Join("sub", "n.txt"), []byte("n\n"), 0o644) }},
			} {
				if n := idleSteps(t, m, func(k int) {
					if k == 2 {
						c.do()
						waitTreeSeen(t, m)
					}
				}); n != 2 {
					t.Fatalf("%s: polled after %d steps, want 2", c.what, n)
				}
				m.applyGitSnapshot(m.pollGitCmd()().(GitMsg))
			}
			if len(m.gitFiles) != 2 {
				t.Errorf("files = %+v", m.gitFiles)
			}
		})
	}
}
