package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// undoRepo is a temp repo with commits c1..cN (one file each) as the
// workspace and the process folder; it returns the model and a git runner.
func undoRepo(t *testing.T, n int) (*Model, func(args ...string) string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	for i := 1; i <= n; i++ {
		name := "f" + string(rune('0'+i)) + ".txt"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", "-A")
		run("commit", "-q", "-m", "c"+string(rune('0'+i)))
	}
	t.Chdir(dir)
	ws, err := localhost.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &Model{ws: ws, w: 120, h: 40}, run
}

// undoStep runs cmd (a plan or an undo) and gives its message to the model.
func undoStep(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	m.onUndoMsg(cmd())
}

// openUndoCard runs "Git: Undo Last Git Action…" up to the card.
func openUndoCard(t *testing.T, m *Model) {
	t.Helper()
	cmd, ok := m.dispatchUndoPalette("git-undo")
	if !ok {
		t.Fatal("git-undo not handled")
	}
	undoStep(t, m, cmd)
	if !m.confirmOpen || m.confirmKind != confirmKindGitUndo {
		t.Fatalf("no undo card; toasts:\n%s", toastText(*m))
	}
}

// G-UND: the palette entry asks first: the card names the last action and
// where HEAD goes; Cancel is first and focused and leaves HEAD alone; Undo
// moves HEAD back and says so.
func TestGitUndoLastCommit(t *testing.T) {
	m, run := undoRepo(t, 3)
	c2 := run("rev-parse", "HEAD~1")
	head := run("rev-parse", "HEAD")
	openUndoCard(t, m)
	b := m.confirm.Buttons()
	if b[0].ID != "cancel" || m.confirm.SafeID() != "cancel" || b[1].ID != "undo" {
		t.Errorf("buttons %+v", b)
	}
	body := undoConfirmText(*m.gitUndo.plan, false)
	for _, want := range []string{"Last action: commit 'c3' (", "HEAD (main) goes back to " + c2[:7] + " 'c2'.", "reflog for 90 days"} {
		if !strings.Contains(body, want) {
			t.Errorf("card lacks %q:\n%s", want, body)
		}
	}
	if cmd := m.handleConfirmSelect("cancel"); cmd != nil || run("rev-parse", "HEAD") != head {
		t.Fatal("Cancel ran something")
	}
	openUndoCard(t, m)
	undoStep(t, m, m.handleConfirmSelect("undo"))
	if run("rev-parse", "HEAD") != c2 {
		t.Errorf("HEAD not back at c2")
	}
	if s := toastText(*m); !strings.Contains(s, "Undone: commit 'c3'") {
		t.Errorf("toast:\n%s", s)
	}
}
