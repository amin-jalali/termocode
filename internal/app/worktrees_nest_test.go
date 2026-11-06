package app

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/worktreeform"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// WTX-11: children are drawn under their parent: one level indented with
// └, deeper ones flattened with └ …; a child whose parent is gone or
// unknown is a root of its group.
func TestWorktreeNestedRows(t *testing.T) {
	st, _ := wtFixture(t)
	parent := map[string]string{
		"/src/app.worktrees/toast":               "/src/app",
		"/src/app/.git/termocode/worktrees/r9-a": "/src/app.worktrees/toast",
		"/elsewhere/api-v2":                      "/tmp/deleted", // its parent is gone
	}
	for i := range st.entries {
		if p, ok := parent[st.entries[i].Path]; ok {
			st.entries[i].Meta.Parent = p
		}
	}
	m := &Model{wt: st, focus: FocusExplorer, explorerWidth: 40, h: 42}
	items, rows := m.wtList()
	var order []string
	for _, r := range rows {
		if r.item >= 0 && r.group == "ACTIVE" {
			order = append(order, filepath.Base(items[r.item].e.Path)+":"+string(rune('0'+r.depth)))
		}
	}
	if got := strings.Join(order, " "); got != "app:0 toast:1 r9-a:2" {
		t.Errorf("ACTIVE rows %s", got)
	}
	for _, r := range rows {
		if r.item >= 0 && items[r.item].e.Path == "/elsewhere/api-v2" && r.depth != 0 {
			t.Errorf("a child of a gone worktree is at depth %d", r.depth)
		}
	}
	plain := ansi.Strip(m.renderWorktreesSidebar(40, 42))
	if !strings.Contains(plain, "└ fix/toast-race") || !strings.Contains(plain, "└ … agent r9-a") {
		t.Errorf("no tree marks:\n%s", plain)
	}
}

// WTX-11: c makes a child of the selected worktree: Worktree: New… from
// its branch, and the new one is stored with its parent.
func TestWorktreeNewChild(t *testing.T) {
	withConfigDir(t)
	root := wtRepo(t)
	m := wtTestModel(t, root)
	cmd, _ := m.dispatchWorktreePalette("worktree-view")
	wtRun(t, m, cmd)
	var feat string
	for _, e := range m.wt.entries {
		if e.Branch == "feat/x" {
			feat = e.Path
		}
	}
	m.wt.cursorPath = feat
	var made []string
	old := wtCreateRun
	wtCreateRun = func(_ *wtForm, folder, branch, from string) ([]worktreeform.File, error) {
		made = append(made, branch+" from "+from)
		return nil, nil
	}
	defer func() { wtCreateRun = old }()
	wtDrive(t, m, wtKey(t, m, "c"))
	if !m.wtFormOpen() {
		t.Fatal("no New worktree card")
	}
	box := ansi.Strip(m.connect.Box())
	if !strings.Contains(box, "Child of") || !strings.Contains(box, "feat/x") {
		t.Errorf("card:\n%s", box)
	}
	if v := m.wtFormValues(); v.from != "feat/x" {
		t.Errorf("from %q", v.from)
	}
	m.connect.SetInputValue("name", "sub")
	wtDrive(t, m, func() tea.Msg { return connectcard.ButtonMsg{ID: "create"} })
	if len(made) != 1 || made[0] != "sub from feat/x" {
		t.Fatalf("made %v", made)
	}
	md, _ := worktrees.Load(m.wt.common)
	var child worktrees.Meta
	for p, v := range md {
		if filepath.Base(p) == "sub" {
			child = v
		}
	}
	if child.Parent != feat {
		t.Errorf("parent %q, want %q (%+v)", child.Parent, feat, md)
	}
	if p, ok := aiActionPolicyFor(wtChildPaletteID); !ok || p.Gate == aiGateAuto {
		t.Errorf("child policy %+v", p)
	}
}
