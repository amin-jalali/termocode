package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// wtStep runs one background step of the view (a prep or a run) and
// gives its message to the view; the follow-up (reload, toast) is not
// run.
func wtStep(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	msg := cmd()
	if _, ok := m.handleWTMsg(msg); !ok {
		t.Fatalf("%T is not a WORKTREES message", msg)
	}
}

// wtRealModel is the view on a real repository (wtRepo): main, feat/x
// in app.worktrees/x and ext made by hand.
func wtRealModel(t *testing.T) (m *Model, root, x, ext string) {
	t.Helper()
	root = wtRepo(t)
	m = wtTestModel(t, root)
	cmd, _ := m.dispatchWorktreePalette("worktree-view")
	wtRun(t, m, cmd)
	base := filepath.Dir(root)
	x, ext = filepath.Join(base, "app.worktrees", "x"), filepath.Join(base, "ext")
	if real, err := filepath.EvalSymlinks(base); err == nil {
		x, ext = filepath.Join(real, "app.worktrees", "x"), filepath.Join(real, "ext")
	}
	return m, root, x, ext
}

func wtBox(m *Model) string { return ansi.Strip(m.confirm.Box()) }

func wtItemAt(m *Model, path string) (wtItem, bool) {
	items, rows := m.wtList()
	for _, r := range rows {
		if r.item >= 0 && items[r.item].e.Path == path {
			return items[r.item], true
		}
	}
	return wtItem{}, false
}

// WTX-14: x on a dirty worktree asks with Cancel first and names the
// changed files; Cancel and Esc keep everything. "Archive anyway (loses
// 1 changed file)" removes the folder; the branch stays and the row stays
// under ARCHIVED; x on it restores the folder from the branch.
func TestWorktreeArchiveDirtyAndRestore(t *testing.T) {
	m, root, x, _ := wtRealModel(t)
	if err := os.WriteFile(filepath.Join(x, "draft.txt"), []byte("not saved anywhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.wt.cursorPath = x
	wtStep(t, m, wtKey(t, m, "x"))
	if !m.confirmOpen || m.confirmKind != confirmKindWTArchive || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no safe-first card: open %v kind %d safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	for _, want := range []string{"1 changed file", "Archive anyway (loses 1 changed file)", "feat/x stays"} {
		if !strings.Contains(wtBox(m), want) {
			t.Errorf("card lacks %q:\n%s", want, wtBox(m))
		}
	}
	m.confirmOpen = false
	if c := m.onAIConfirmClosed(); c != nil { // Esc
		t.Error("Esc started work")
	}
	wtStep(t, m, wtKey(t, m, "x"))
	m.confirmOpen = false
	if c, _ := m.handleAIConfirm("cancel"); c != nil {
		t.Error("Cancel started work")
	}
	if _, err := os.Stat(filepath.Join(x, "draft.txt")); err != nil {
		t.Fatal("Cancel touched the folder")
	}

	wtStep(t, m, wtKey(t, m, "x"))
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm("archive")
	wtStep(t, m, cmd)
	if _, err := os.Stat(x); !os.IsNotExist(err) {
		t.Fatal("the folder is still there")
	}
	if !git.BranchExists(root, "feat/x") {
		t.Fatal("archive deleted the branch")
	}
	wtRun(t, m, m.worktreesReload())
	it, ok := wtItemAt(m, x)
	if !ok || it.group != "ARCHIVED" || it.state != wtStateArchived {
		t.Fatalf("archived row: %v %+v", ok, it)
	}
	if c := wtCardText(m, x, 40); !strings.Contains(c, "feat/x") || !strings.Contains(c, "x restores") {
		t.Errorf("archived card:\n%s", c)
	}

	// x restores it.
	m.wt.cursorPath = x
	wtStep(t, m, wtKey(t, m, "x"))
	if m.confirmOpen {
		t.Fatal("restore asked")
	}
	if br, _ := git.GetBranch(x); br.Name != "feat/x" {
		t.Fatalf("restored on %q", br.Name)
	}
	wtRun(t, m, m.worktreesReload())
	if it, _ := wtItemAt(m, x); it.group != "ACTIVE" || it.e.Gone {
		t.Errorf("restored row in %s gone %v", it.group, it.e.Gone)
	}
	if md, _ := worktrees.Load(m.wt.common); md[x].State != "" {
		t.Errorf("still archived in the meta: %+v", md[x])
	}
}

// A clean worktree archives after a plain card; the main checkout, the
// one you are in and a locked one are refused without a card.
func TestWorktreeArchiveCleanAndRefusals(t *testing.T) {
	m, root, x, ext := wtRealModel(t)
	m.wt.cursorPath = ext
	wtStep(t, m, wtKey(t, m, "x"))
	if !strings.Contains(wtBox(m), "not made by termocode") || strings.Contains(wtBox(m), "anyway") {
		t.Errorf("external clean card:\n%s", wtBox(m))
	}
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm("archive")
	wtStep(t, m, cmd)
	if _, err := os.Stat(ext); !os.IsNotExist(err) {
		t.Fatal("not archived")
	}

	m.wt.cursorPath = root
	if c := wtKey(t, m, "x"); m.confirmOpen || !strings.Contains(wtLastToast(m), "main checkout") {
		t.Errorf("main: card %v toast %q cmd %v", m.confirmOpen, wtLastToast(m), c != nil)
	}
	wtGitClean(t, root, "worktree", "lock", "--reason", "usb disk", x)
	wtRun(t, m, m.worktreesReload())
	m.wt.cursorPath = x
	wtKey(t, m, "x")
	if m.confirmOpen || !strings.Contains(wtLastToast(m), "usb disk") {
		t.Errorf("locked: card %v toast %q", m.confirmOpen, wtLastToast(m))
	}
	for _, id := range []string{wtArchivePaletteID} {
		if _, ok := m.dispatchWorktreePalette(id); !ok {
			t.Errorf("%s not in the palette", id)
		}
		if p, ok := aiActionPolicyFor(id); !ok || p.Gate != aiGateConfirm {
			t.Errorf("%s policy %+v", id, p)
		}
	}
}

// wtGitClean runs git without the user's own config (no signing).
func wtGitClean(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
