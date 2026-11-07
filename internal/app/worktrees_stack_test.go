package app

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/worktreeform"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// wtStackFixture is a repository whose ext worktree is a stacked child
// of feat/x (stored in worktrees.json), with the view loaded.
func wtStackFixture(t *testing.T) (m *Model, feat, ext string) {
	t.Helper()
	withConfigDir(t)
	root := wtRepo(t)
	list, common, err := git.ListWorktrees(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range list {
		switch w.Branch {
		case "feat/x":
			feat = w.Path
		case "ext":
			ext = w.Path
		}
	}
	if err := worktrees.Update(common, func(mm map[string]worktrees.Meta) {
		mm[ext] = worktrees.Meta{Parent: feat, Base: "feat/x"}
	}); err != nil {
		t.Fatal(err)
	}
	m = wtTestModel(t, root)
	cmd, _ := m.dispatchWorktreePalette("worktree-view")
	wtRun(t, m, cmd)
	return m, feat, ext
}

// CMP-17: a child made with c keeps its parent's branch as the base its
// pull request targets.
func TestWorktreeChildKeepsStackBase(t *testing.T) {
	withConfigDir(t)
	root := wtRepo(t)
	m := wtTestModel(t, root)
	cmd, _ := m.dispatchWorktreePalette("worktree-view")
	wtRun(t, m, cmd)
	for _, e := range m.wt.entries {
		if e.Branch == "feat/x" {
			m.wt.cursorPath = e.Path
		}
	}
	old := wtCreateRun
	wtCreateRun = func(*wtForm, string, string, string) ([]worktreeform.File, error) { return nil, nil }
	defer func() { wtCreateRun = old }()
	wtDrive(t, m, wtKey(t, m, "c"))
	if !m.wtFormOpen() {
		t.Fatal("no New worktree card")
	}
	m.connect.SetInputValue("name", "sub")
	wtDrive(t, m, func() tea.Msg { return connectcard.ButtonMsg{ID: "create"} })
	md, _ := worktrees.Load(m.wt.common)
	for p, v := range md {
		if filepath.Base(p) == "sub" && v.Base != "feat/x" {
			t.Errorf("base %q, want feat/x", v.Base)
		}
	}
}

// CMP-17: the PR base of a folder in a stacked child is its parent's
// branch; the main checkout and an unknown folder are not stacked (the
// default branch is used).
func TestStackBaseForDir(t *testing.T) {
	m, _, ext := wtStackFixture(t)
	if got := stackBaseFor(ext); got != "feat/x" {
		t.Errorf("child base %q, want feat/x", got)
	}
	if got := stackBaseFor(filepath.Join(ext, "sub", "dir")); got != "feat/x" {
		t.Errorf("a folder inside the child: %q", got)
	}
	if got := stackBaseFor(m.ws.Root); got != "" {
		t.Errorf("main checkout stacked on %q", got)
	}
	if got := stackBaseFor(t.TempDir()); got != "" {
		t.Errorf("a folder outside git: %q", got)
	}
}

// CMP-17: r on a stacked child asks first (Cancel is the safe button)
// and names both branches; Restack replays the child onto the parent's
// branch in the child's folder. r on a worktree with no parent only says
// why not.
func TestWorktreeRestack(t *testing.T) {
	m, feat, ext := wtStackFixture(t)
	var ran []string
	old := wtRestackRun
	wtRestackRun = func(dir, onto string) (git.RebaseOutcome, error) {
		ran = append(ran, dir+" onto "+onto)
		return git.RebaseOutcome{}, nil
	}
	defer func() { wtRestackRun = old }()

	m.wt.cursorPath = feat
	if cmd := wtKey(t, m, "e"); cmd != nil {
		wtDrive(t, m, cmd)
	}
	if m.confirmOpen {
		t.Fatal("a card for a worktree that is not stacked")
	}
	if got := wtLastToast(m); !strings.Contains(got, "not stacked") {
		t.Errorf("toast %q", got)
	}

	m.wt.cursorPath = ext
	wtKey(t, m, "e")
	if !m.confirmOpen || m.confirmKind != confirmKindWTRestack || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no safe-first card: open %v kind %d safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	box := wtBox(m)
	if !strings.Contains(box, "ext") || !strings.Contains(box, "feat/x") {
		t.Errorf("card:\n%s", box)
	}
	if cmd := m.answerWTRestack(""); cmd != nil || len(ran) != 0 {
		t.Fatalf("Esc restacked: %v", ran)
	}

	wtKey(t, m, "e")
	cmd := m.answerWTRestack("restack")
	if cmd == nil {
		t.Fatal("Restack did nothing")
	}
	msg := cmd()
	m.handleWTMsg(msg)
	if len(ran) != 1 || ran[0] != ext+" onto feat/x" {
		t.Fatalf("ran %v", ran)
	}
	if got := wtLastToast(m); !strings.Contains(got, "Restacked") {
		t.Errorf("toast %q", got)
	}

	// A conflict stops the rebase: the toast says where and what next.
	wtRestackRun = func(string, string) (git.RebaseOutcome, error) {
		return git.RebaseOutcome{Stopped: true, Conflicts: []string{"a.go"}}, nil
	}
	wtKey(t, m, "e")
	m.handleWTMsg(m.answerWTRestack("restack")())
	if got := wtLastToast(m); !strings.Contains(got, "conflict") || !strings.Contains(got, "a.go") {
		t.Errorf("conflict toast %q", got)
	}

	if p, ok := aiActionPolicyFor(wtRestackPaletteID); !ok || p.Gate == aiGateAuto {
		t.Errorf("restack policy %+v", p)
	}
	found := false
	for _, it := range worktreePaletteItems() {
		found = found || it.ID == wtRestackPaletteID
	}
	if !found {
		t.Error("no palette row")
	}
}

// CMP-17: Create Pull Request and the ship loop target the parent's
// branch from a stacked child (gh is not asked for the default branch);
// elsewhere they ask gh.
func TestPRBaseStacked(t *testing.T) {
	m, _, ext := wtStackFixture(t)
	asked := 0
	def := func() (string, error) { asked++; return "main", nil }
	base, stacked, err := prBaseFor(ext, def)
	if err != nil || base != "feat/x" || !stacked || asked != 0 {
		t.Errorf("child: %q stacked %v err %v asked %d", base, stacked, err, asked)
	}
	base, stacked, err = prBaseFor(m.ws.Root, def)
	if err != nil || base != "main" || stacked || asked != 1 {
		t.Errorf("main checkout: %q stacked %v err %v asked %d", base, stacked, err, asked)
	}
}
