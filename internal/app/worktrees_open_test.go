package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/worktrees"
)

// wtSeams records what Enter and a asked for instead of re-executing or
// changing the explorer.
func wtSeams(t *testing.T) (opened, added *[]string) {
	t.Helper()
	var o, a []string
	oldOpen, oldAdd := wtOpenFolder, wtAddFolder
	wtOpenFolder = func(_ *Model, p string) tea.Cmd { o = append(o, p); return nil }
	wtAddFolder = func(_ *Model, p string) tea.Cmd { a = append(a, p); return nil }
	t.Cleanup(func() { wtOpenFolder, wtAddFolder = oldOpen, oldAdd })
	return &o, &a
}

func wtKey(t *testing.T, m *Model, s string) tea.Cmd {
	t.Helper()
	var k tea.KeyMsg
	switch s {
	case "enter":
		k = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		k = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	nm, cmd := m.handleWTSidebarKey(k)
	*m = nm
	return cmd
}

func wtLastToast(m *Model) string {
	live := m.toast.Live()
	if len(live) == 0 {
		return ""
	}
	return live[len(live)-1].Title + " " + live[len(live)-1].Detail
}

// Enter opens the cursor's worktree the Open Recent Workspace way and
// records when it was opened; the worktree you are in, and one whose
// folder is gone, only say why not.
func TestWorktreeEnterOpens(t *testing.T) {
	opened, _ := wtSeams(t)
	st, _ := wtFixture(t)
	st.common = t.TempDir()
	m := &Model{wt: st, focus: FocusExplorer, explorerWidth: 30, h: 42}
	st.cursorPath = "/src/app.worktrees/toast"
	wtKey(t, m, "enter")
	if strings.Join(*opened, ",") != "/src/app.worktrees/toast" {
		t.Fatalf("opened %v", *opened)
	}
	metas, err := worktrees.Load(st.common)
	if err != nil || metas["/src/app.worktrees/toast"].LastSeen.IsZero() {
		t.Errorf("last seen not stored: %v %+v", err, metas)
	}
	st.cursorPath = "/src/app"
	wtKey(t, m, "enter")
	st.cursorPath = "/tmp/deleted"
	wtKey(t, m, "enter")
	if len(*opened) != 1 {
		t.Errorf("opened %v", *opened)
	}
	if !strings.Contains(wtLastToast(m), "gone") {
		t.Errorf("toast %q", wtLastToast(m))
	}
}

// In the compact layout Enter shows the card first; Enter there opens,
// Esc goes back to the list.
func TestWorktreeCompactEnterShowsTheCard(t *testing.T) {
	opened, _ := wtSeams(t)
	st, _ := wtFixture(t)
	st.off = true // a demo: nothing is written
	m := &Model{wt: st, focus: FocusExplorer, explorerWidth: 30, h: 24}
	st.cursorPath = "/elsewhere/api-v2"
	wtKey(t, m, "enter")
	if !st.detail || len(*opened) != 0 {
		t.Fatalf("detail %v opened %v", st.detail, *opened)
	}
	wtKey(t, m, "esc")
	if st.detail || m.focus != FocusExplorer {
		t.Fatalf("esc: detail %v focus %v", st.detail, m.focus)
	}
	wtKey(t, m, "enter")
	wtKey(t, m, "enter")
	if strings.Join(*opened, ",") != "/elsewhere/api-v2" {
		t.Errorf("opened %v", *opened)
	}
}

// a adds the worktree as a folder of this window (not a gone one); the
// palette rows do what the keys do on the cursor's worktree.
func TestWorktreeAddFolderAndPalette(t *testing.T) {
	opened, added := wtSeams(t)
	st, _ := wtFixture(t)
	st.off = true
	m := &Model{wt: st, focus: FocusExplorer, explorerWidth: 30, h: 42}
	st.cursorPath = "/src/app.worktrees/toast"
	wtKey(t, m, "a")
	st.cursorPath = "/tmp/deleted"
	wtKey(t, m, "a")
	if strings.Join(*added, ",") != "/src/app.worktrees/toast" {
		t.Errorf("added %v", *added)
	}
	st.cursorPath = "/elsewhere/api-v2"
	if _, ok := m.dispatchWorktreePalette("worktree-add-folder"); !ok {
		t.Fatal("worktree-add-folder not handled")
	}
	if _, ok := m.dispatchWorktreePalette("worktree-open"); !ok {
		t.Fatal("worktree-open not handled")
	}
	if strings.Join(*added, ",") != "/src/app.worktrees/toast,/elsewhere/api-v2" || strings.Join(*opened, ",") != "/elsewhere/api-v2" {
		t.Errorf("palette: added %v opened %v", *added, *opened)
	}
	if p, ok := aiActionPolicyFor("worktree-open"); !ok || p.Gate != aiGateNever || p.Reason != aiNoSession {
		t.Errorf("worktree-open policy %+v", p)
	}
}
