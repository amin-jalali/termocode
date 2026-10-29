package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
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
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
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

// G-UND: the card warns when commits leaving the branch are pushed
// already, and says what happens to local changes.
func TestUndoConfirmTextWarnings(t *testing.T) {
	p := git.UndoPlan{Entry: git.ReflogEntry{Kind: git.ReflogCommit, Message: "commit: c3", Subject: "c3"},
		Target: "abcdef0123", TargetSubject: "c2", Branch: "main", Upstream: "origin/main", Pushed: 2}
	body := undoConfirmText(p, false)
	for _, want := range []string{"2 commits leaving main are on origin/main already", "force-push", "termocode never force-pushes"} {
		if !strings.Contains(body, want) {
			t.Errorf("lacks %q:\n%s", want, body)
		}
	}
	p.Pushed = 0
	if body := undoConfirmText(p, false); strings.Contains(body, "force-push") {
		t.Errorf("warns without pushed commits:\n%s", body)
	}
	p.Staged = true
	if body := undoConfirmText(p, false); !strings.Contains(body, "Staged changes stay, but become unstaged.") {
		t.Errorf("no staged note:\n%s", body)
	}
	p.Blocked = []string{"a.txt", "b.txt"}
	if body := undoConfirmText(p, false); !strings.Contains(body, "Your local changes in a.txt, b.txt are in the way.") {
		t.Errorf("no blocked note:\n%s", body)
	}
}

// G-UND: with local changes in the way the card offers Stash and Undo
// (after Cancel). When the changes do not go back cleanly the undo is
// done, the stash stays, and the toast says how to get them back.
func TestGitUndoStashAndUndo(t *testing.T) {
	m, run := undoRepo(t, 3)
	c2 := run("rev-parse", "HEAD~1")
	if err := os.WriteFile("f3.txt", []byte("my edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	openUndoCard(t, m)
	b := m.confirm.Buttons()
	if len(b) != 2 || b[0].ID != "cancel" || b[1].ID != "undo-stash" || b[1].Title != "Stash and Undo" {
		t.Fatalf("buttons %+v", b)
	}
	if body := undoConfirmText(*m.gitUndo.plan, false); !strings.Contains(body, "Stash and Undo puts them aside") {
		t.Errorf("card:\n%s", body)
	}
	undoStep(t, m, m.handleConfirmSelect("undo-stash"))
	if run("rev-parse", "HEAD") != c2 {
		t.Errorf("HEAD not back at c2; toasts:\n%s", toastText(*m))
	}
	if !strings.Contains(run("stash", "list"), "before undo") {
		t.Errorf("stash dropped")
	}
	s := toastText(*m)
	for _, want := range []string{"Undone, but your changes did not go back cleanly", "stash@{0}"} {
		if !strings.Contains(s, want) {
			t.Errorf("toast lacks %q:\n%s", want, s)
		}
	}
}

// G-UND: during a stopped merge (or rebase) the undo refuses and says how
// to finish first; no card opens.
func TestGitUndoRefusesDuringMerge(t *testing.T) {
	m, run := undoRepo(t, 1)
	run("checkout", "-q", "-b", "side")
	_ = os.WriteFile("f1.txt", []byte("side\n"), 0o644)
	run("commit", "-q", "-am", "side")
	run("checkout", "-q", "main")
	_ = os.WriteFile("f1.txt", []byte("main\n"), 0o644)
	run("commit", "-q", "-am", "main")
	if err := exec.Command("git", "merge", "side").Run(); err == nil {
		t.Fatal("merge did not stop")
	}
	cmd, _ := m.dispatchUndoPalette("git-undo")
	undoStep(t, m, cmd)
	if m.confirmOpen {
		t.Fatal("card opened during a merge")
	}
	s := toastText(*m)
	for _, want := range []string{"A merge is in progress", "git merge --abort"} {
		if !strings.Contains(s, want) {
			t.Errorf("toast lacks %q:\n%s", want, s)
		}
	}
	run("merge", "--abort")
	if err := exec.Command("git", "rebase", "side").Run(); err == nil {
		t.Fatal("rebase did not stop")
	}
	cmd, _ = m.dispatchUndoPalette("git-undo")
	undoStep(t, m, cmd)
	if s := toastText(*m); m.confirmOpen || !strings.Contains(s, "banner in Source Control") {
		t.Errorf("rebase: card %v, toast:\n%s", m.confirmOpen, s)
	}
}

// undoToastClick is the click message of the newest clickable toast.
func undoToastClick(t *testing.T, m *Model) tea.Msg {
	t.Helper()
	cards := m.toast.Cards(120)
	for i := len(cards) - 1; i >= 0; i-- {
		if cards[i].Click != nil {
			return cards[i].Click
		}
	}
	t.Fatalf("no clickable toast:\n%s", toastText(*m))
	return nil
}

// G-UND: after an undo the toast offers Redo; a click (or the palette
// entry) puts HEAD back. Once HEAD moved again, Redo refuses.
func TestGitUndoRedo(t *testing.T) {
	m, run := undoRepo(t, 3)
	c3 := run("rev-parse", "HEAD")
	openUndoCard(t, m)
	undoStep(t, m, m.handleConfirmSelect("undo"))
	if s := toastText(*m); !strings.Contains(s, "Redo") {
		t.Errorf("toast does not offer Redo:\n%s", s)
	}
	click := undoToastClick(t, m)
	undoStep(t, m, m.onUndoMsg(click))
	if run("rev-parse", "HEAD") != c3 {
		t.Fatalf("not redone; toasts:\n%s", toastText(*m))
	}
	if s := toastText(*m); !strings.Contains(s, "Redone: commit 'c3'") {
		t.Errorf("toast:\n%s", s)
	}
	// Redo is spent: the palette entry has nothing left to redo.
	cmd, ok := m.dispatchUndoPalette("git-redo")
	if !ok || cmd == nil {
		t.Fatal("git-redo not handled")
	}
	if !strings.Contains(toastText(*m), "Nothing to redo") {
		t.Errorf("toast:\n%s", toastText(*m))
	}

	openUndoCard(t, m)
	undoStep(t, m, m.handleConfirmSelect("undo"))
	_ = os.WriteFile("f9.txt", []byte("9\n"), 0o644)
	run("add", "-A")
	run("commit", "-q", "-m", "c9")
	c9 := run("rev-parse", "HEAD")
	cmd, _ = m.dispatchUndoPalette("git-redo")
	undoStep(t, m, cmd)
	if run("rev-parse", "HEAD") != c9 || !strings.Contains(toastText(*m), "Can't redo") {
		t.Errorf("redo after a new commit; toasts:\n%s", toastText(*m))
	}
}
