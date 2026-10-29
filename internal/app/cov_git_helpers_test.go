package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// covGitEnv gives git a fixed identity and no user or system config, so
// the tests run the same on every machine.
func covGitEnv(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME":     "Tess",
		"GIT_AUTHOR_EMAIL":    "tess@example.com",
		"GIT_COMMITTER_NAME":  "Tess",
		"GIT_COMMITTER_EMAIL": "tess@example.com",
		"GIT_TERMINAL_PROMPT": "0",
	} {
		t.Setenv(k, v)
	}
}

// covGit runs git in dir and returns its trimmed output.
func covGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// covWrite writes a file below dir.
func covWrite(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// covCommit stages everything and commits it.
func covCommit(t *testing.T, dir, msg string) {
	t.Helper()
	covGit(t, dir, "add", "-A")
	covGit(t, dir, "commit", "-q", "-m", msg)
}

// covGitRepo is a model on a fresh temp repo (branch main, a.txt
// committed) with the git state loaded.
func covGitRepo(t *testing.T) (*Model, string) {
	t.Helper()
	covGitEnv(t)
	dir := t.TempDir()
	covGit(t, dir, "init", "-q", "-b", "main")
	covGit(t, dir, "config", "commit.gpgsign", "false")
	covWrite(t, dir, "a.txt", "one\ntwo\nthree\n")
	covCommit(t, dir, "first")
	return covModelAt(t, dir), dir
}

// covModelAt is a model whose workspace is dir, git state loaded.
func covModelAt(t *testing.T, dir string) *Model {
	t.Helper()
	ws, err := localhost.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The app serves one workspace: the process one and the model's are
	// the same folder (New copies it), so git refreshes read this repo.
	old := processWS.Load()
	SetWorkspace(ws)
	t.Cleanup(func() { processWS.Store(old) })
	m := &Model{w: 160, h: 40, ws: ws}
	covRefresh(m)
	return m
}

// covRefresh reads the git state of the model's folder again.
func covRefresh(m *Model) {
	m.gitCommits = nil
	if msg, ok := m.pollGitCmd()().(GitMsg); ok {
		m.applyGitSnapshot(msg)
	}
}

// covCursorTo puts the Source Control cursor on the first row ok accepts.
func covCursorTo(t *testing.T, m *Model, ok func(gitPanelRow) bool) gitPanelRow {
	t.Helper()
	for i, r := range m.gitPanelRows() {
		if ok(r) {
			m.gitCursor = i
			return r
		}
	}
	t.Fatalf("no such row in %+v", m.gitPanelRows())
	return gitPanelRow{}
}

// covKey sends one key to the Source Control panel.
func covKey(m *Model, k tea.KeyMsg) (tea.Cmd, bool) {
	m2, cmd, ok := m.handleGitSidebarKey(k)
	*m = m2
	return cmd, ok
}

func covRunes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// covMsg runs cmd (and every command of a batch, each on its own
// goroutine, since a toast's timer blocks) and returns the first message
// of type T. It fails the test when none comes within a minute.
func covMsg[T any](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	got := make(chan T, 64)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			switch msg := c().(type) {
			case tea.BatchMsg:
				for _, s := range msg {
					run(s)
				}
			case T:
				got <- msg
			}
		}()
	}
	run(cmd)
	select {
	case msg := <-got:
		return msg
	case <-time.After(time.Minute):
		var zero T
		t.Fatalf("no %T came", zero)
		return zero
	}
}
