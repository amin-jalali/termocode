package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// WTX-14: D on a dirty worktree whose branch has commits main lacks:
// card 1 says what would be lost (Cancel first); card 2 names the
// changed files (Cancel first); then the branch name must be typed. A
// wrong name deletes nothing. The right one removes the folder and the
// branch.
func TestWorktreeDeleteDirtyUnmerged(t *testing.T) {
	m, root, x, _ := wtRealModel(t)
	if err := os.WriteFile(filepath.Join(x, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wtGitClean(t, x, "add", "a.txt")
	wtGitClean(t, x, "commit", "-q", "-m", "work")
	for _, f := range []string{"b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(x, f), []byte("draft\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m.wt.cursorPath = x
	wtStep(t, m, wtKey(t, m, "D"))
	if !m.confirmOpen || m.confirmKind != confirmKindWTDelete || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card 1: open %v kind %d safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	if !strings.Contains(wtBox(m), "not merged into main: 1 commit would be lost") {
		t.Errorf("card 1:\n%s", wtBox(m))
	}
	m.confirmOpen = false
	m.handleAIConfirm("delete")
	if !m.confirmOpen || m.confirmKind != confirmKindWTDeleteDirty || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no second card for the changed files: open %v kind %d", m.confirmOpen, m.confirmKind)
	}
	if !strings.Contains(wtBox(m), "Delete anyway (loses 2 changed files)") {
		t.Errorf("card 2:\n%s", wtBox(m))
	}
	// Esc on card 2 deletes nothing.
	m.confirmOpen = false
	m.onAIConfirmClosed()
	if m.connectOpen || !git.BranchExists(root, "feat/x") {
		t.Fatal("Esc went on")
	}

	wtStep(t, m, wtKey(t, m, "D"))
	m.confirmOpen = false
	m.handleAIConfirm("delete")
	m.confirmOpen = false
	m.handleAIConfirm("discard")
	if !m.connectOpen || m.connect.Card().Title != wtDeleteTypeTitle {
		t.Fatal("no type-the-name card")
	}
	if b := m.connect.Card().Buttons; len(b) == 0 || b[0].ID != "cancel" {
		t.Fatalf("Cancel is not first: %+v", b)
	}
	m.connect.SetInputValue("name", "feat/y")
	if c, _ := m.handleWTMsg(connectcard.ButtonMsg{ID: "delete"}); c != nil {
		t.Fatal("a wrong name deleted")
	}
	if !m.connectOpen || !strings.Contains(ansi.Strip(m.connect.Box()), "Type feat/x") {
		t.Fatalf("wrong name: %s", ansi.Strip(m.connect.Box()))
	}
	m.connect.SetInputValue("name", "feat/x")
	cmd, _ := m.handleWTMsg(connectcard.ButtonMsg{ID: "delete"})
	wtStep(t, m, cmd)
	if _, err := os.Stat(x); !os.IsNotExist(err) || git.BranchExists(root, "feat/x") {
		t.Fatalf("not deleted: %v %v", err, git.BranchExists(root, "feat/x"))
	}
	if md, _ := worktrees.Load(m.wt.common); !md[x].Empty() {
		t.Errorf("meta left: %+v", md[x])
	}
	if _, ok := m.dispatchWorktreePalette(wtDeletePaletteID); !ok {
		t.Error("delete not in the palette")
	}
	if p, ok := aiActionPolicyFor(wtDeletePaletteID); !ok || p.Gate != aiGateConfirm {
		t.Errorf("delete policy %+v", p)
	}
}

// A clean, merged worktree deletes after one card: no second card and
// no name to type.
func TestWorktreeDeleteMerged(t *testing.T) {
	m, root, _, ext := wtRealModel(t)
	m.wt.cursorPath = ext
	wtStep(t, m, wtKey(t, m, "D"))
	if !strings.Contains(wtBox(m), "merged into main") || strings.Contains(wtBox(m), "would be lost") {
		t.Errorf("card:\n%s", wtBox(m))
	}
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm("delete")
	if m.confirmOpen || m.connectOpen {
		t.Fatal("asked again for a clean merged one")
	}
	wtStep(t, m, cmd)
	if _, err := os.Stat(ext); !os.IsNotExist(err) || git.BranchExists(root, "ext") {
		t.Fatal("not deleted")
	}
}
