package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// WTX-15: the cleanup rules: a merged branch or a merged PR, or no
// activity for 14 days on a clean worktree, pre-ticks it; a dirty one is
// never pre-ticked; each row says why.
func TestWorktreeCleanupRules(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		f    worktrees.CleanupFacts
		why  string
		tick bool
	}{
		{worktrees.CleanupFacts{Base: "main", Merged: true, LastActive: now}, "merged into main", true},
		{worktrees.CleanupFacts{Base: "main", PR: 12, PRMerged: true, LastActive: now}, "PR #12 merged", true},
		{worktrees.CleanupFacts{Base: "main", LastActive: now.Add(-20 * 24 * time.Hour)}, "no activity for 20 days", true},
		{worktrees.CleanupFacts{Base: "main", LastActive: now.Add(-2 * 24 * time.Hour)}, "kept: active 2 days ago", false},
		{worktrees.CleanupFacts{Base: "main", Merged: true, Dirty: 3, LastActive: now}, "merged into main · 3 changed files", false},
		{worktrees.CleanupFacts{Base: "main", Dirty: 1, LastActive: now.Add(-30 * 24 * time.Hour)}, "kept: 1 changed file", false},
	} {
		why, tick := worktrees.CleanupReason(c.f, now)
		if why != c.why || tick != c.tick {
			t.Errorf("%+v: got %q %v, want %q %v", c.f, why, tick, c.why, c.tick)
		}
	}
}

// WTX-15: C opens a checkbox list of the worktrees that can go, pre-ticked
// by the rules, plus git's records of gone folders; Space ticks; the
// summary card lists every worktree that will be archived (Cancel first);
// a ticked dirty one asks again on its own (Keep it first); then the
// ticked ones are archived and the gone records pruned.
func TestWorktreeBulkCleanup(t *testing.T) {
	m, root, x, ext := wtRealModel(t)
	base := filepath.Dir(x)
	busy := filepath.Join(base, "busy")
	wtGitClean(t, root, "worktree", "add", "-q", "-b", "busy", busy)
	if err := os.WriteFile(filepath.Join(busy, "w.txt"), []byte("w\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wtGitClean(t, busy, "add", "w.txt")
	wtGitClean(t, busy, "commit", "-q", "-m", "work")
	gone := filepath.Join(base, "gone")
	wtGitClean(t, root, "worktree", "add", "-q", "-b", "gone", gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "draft.txt"), []byte("d\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wtRun(t, m, m.worktreesReload())

	wtStep(t, m, wtKey(t, m, "C"))
	if !m.pickerOpen || m.pickerKind != pickerKindWTCleanup {
		t.Fatalf("no cleanup list: open %v kind %d", m.pickerOpen, m.pickerKind)
	}
	cl := m.wt.cleanup
	ticked := func() string {
		var out []string
		for _, r := range cl.rows {
			if r.tick {
				out = append(out, filepath.Base(r.path))
			}
		}
		return strings.Join(out, ",")
	}
	if got := ticked(); got != "x" || !cl.prune {
		t.Fatalf("pre-ticked %q prune %v", got, cl.prune)
	}
	box := ansi.Strip(m.picker.Box())
	for _, want := range []string{"[x]", "merged into main", "1 changed file", "kept: active", "Forget 1 gone folder"} {
		if !strings.Contains(box, want) {
			t.Errorf("list lacks %q:\n%s", want, box)
		}
	}
	m.onWTCleanupToggle("wt:" + ext) // Space
	if got := ticked(); got != "x,ext" {
		t.Fatalf("after Space: %q", got)
	}

	// Enter on the first row: the summary card, Cancel first.
	m.pickerOpen = false
	m.onWTCleanupPicked(wtCleanupGoID)
	if !m.confirmOpen || m.confirmKind != confirmKindWTCleanup || m.confirm.SafeID() != "cancel" {
		t.Fatalf("summary: open %v kind %d safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	sum := wtBox(m)
	for _, want := range []string{"Archive 2 worktrees", "feat/x", "ext", "1 changed file", "gone folder"} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary lacks %q:\n%s", want, sum)
		}
	}
	m.confirmOpen = false
	m.onAIConfirmClosed() // Esc: nothing happens
	if _, err := os.Stat(x); err != nil {
		t.Fatal("Esc archived")
	}

	wtStep(t, m, wtKey(t, m, "C"))
	m.onWTCleanupToggle("wt:" + ext)
	m.pickerOpen = false
	m.onWTCleanupPicked(wtCleanupGoID)
	m.confirmOpen = false
	m.handleAIConfirm("archive")
	if !m.confirmOpen || m.confirmKind != confirmKindWTCleanupDirty || m.confirm.SafeID() != "keep" {
		t.Fatalf("no own card for the dirty one: open %v kind %d safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	if !strings.Contains(wtBox(m), "Archive anyway (loses 1 changed file)") {
		t.Errorf("dirty card:\n%s", wtBox(m))
	}
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm("keep")
	wtStep(t, m, cmd)
	if _, err := os.Stat(x); !os.IsNotExist(err) {
		t.Error("x not archived")
	}
	if _, err := os.Stat(filepath.Join(ext, "draft.txt")); err != nil {
		t.Error("the kept dirty one was archived")
	}
	if _, err := os.Stat(busy); err != nil {
		t.Error("an unticked one was archived")
	}
	if !git.BranchExists(root, "feat/x") {
		t.Error("cleanup deleted a branch")
	}
	list, _, _ := git.ListWorktrees(root)
	for _, w := range list {
		if w.Prunable {
			t.Errorf("not pruned: %s", w.Path)
		}
	}
	if md, _ := worktrees.Load(m.wt.common); md[x].State != worktrees.StateArchived || md[x].Branch != "feat/x" {
		t.Errorf("x meta %+v", md[x])
	}
	if p, ok := aiActionPolicyFor(wtCleanupPaletteID); !ok || p.Gate != aiGateConfirm {
		t.Errorf("cleanup policy %+v", p)
	}
}
