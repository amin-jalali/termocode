package app

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// wtRepo is a git repository with a linked worktree in <repo>.worktrees
// and one made outside termocode; it returns the main checkout.
func wtRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	base := t.TempDir()
	dir := filepath.Join(base, "app")
	run := func(d string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = d
		cmd.Env = append(cmd.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(base, "init", "-q", "-b", "main", dir)
	run(dir, "commit", "-q", "--allow-empty", "-m", "init")
	run(dir, "worktree", "add", "-q", "-b", "feat/x", filepath.Join(base, "app.worktrees", "x"))
	run(dir, "worktree", "add", "-q", "-b", "ext", filepath.Join(base, "ext"))
	return dir
}

func wtTestModel(t *testing.T, root string) *Model {
	t.Helper()
	m := &Model{services: newServicesState(), kube: newKubeState(), wt: newWTState(), w: 150, h: 42, explorerWidth: 30, hoverX: -1, hoverY: -1,
		ws: workspace.Workspace{ID: workspace.LocalID(root), Kind: workspace.KindLocal, Root: root}}
	return m
}

// wtRun runs the load command and applies it, then waits for the status
// batch it starts.
func wtRun(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no load command")
	}
	msg, ok := cmd().(wtLoadMsg)
	if !ok {
		t.Fatal("not a load result")
	}
	m.onWTLoad(msg)
	// The status batch the load started: wait for it as the program would.
	if c := m.wt.refreshStatus(); c != nil {
		t.Fatal("statuses read twice")
	}
	wtWaitFor(t, func() bool {
		for _, e := range m.wt.entries {
			if _, ok := m.wt.cache.Get(e.Path); !ok && !e.Gone { // a gone folder has no status
				return false
			}
		}
		return true
	})
	m.handleWTMsg(wtStatusMsg{gen: m.wt.gen})
}

func wtWaitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}

// Worktree: Show and Alt+Shift+W open the view under the Source Control
// icon and read every worktree of the repository.
func TestWorktreeShowLoadsTheList(t *testing.T) {
	root := wtRepo(t)
	m := wtTestModel(t, root)
	cmd, ok := m.dispatchWorktreePalette("worktree-view")
	if !ok || m.activity.Active() != activity.ViewWorktrees || m.focus != FocusExplorer || !m.showExp {
		t.Fatalf("not shown: %v %v %v", ok, m.activity.Active(), m.focus)
	}
	wtRun(t, m, cmd)
	st := m.wt
	if st.err != "" || len(st.entries) != 3 {
		t.Fatalf("err %q entries %+v", st.err, st.entries)
	}
	plain := stripANSI(m.renderWorktreesSidebar(29, 42))
	for _, want := range []string{"WORKTREES", "main", "feat/x", "EXTERNAL", "ext", "you are here", "clean"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q:\n%s", want, plain)
		}
	}
	// Shown again later: nothing new starts while it polls.
	if again := m.worktreesSync(); again != nil {
		t.Error("a second load started")
	}
	// Hidden: the chain ends; a late load of the old gen is dropped.
	m.activity.SetActive(activity.ViewFiles)
	m.worktreesSync()
	if st.polling {
		t.Error("still polling while hidden")
	}
	if c := m.onWTLoad(wtLoadMsg{gen: st.gen - 1}); c != nil {
		t.Error("an old load was applied")
	}
	if keys := strings.Join(keymap.Default().KeysFor(keymap.ActionWorktreeView), " "); !strings.Contains(keys, "alt+shift+w") || !strings.Contains(keys, "alt+W") {
		t.Errorf("Worktree: Show keys %q", keys)
	}
	if a := keymap.Default().Match(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("W"), Alt: true}); a != keymap.ActionWorktreeView {
		t.Errorf("Alt+Shift+W runs %v", a)
	}
	m.activity.SetActive(activity.ViewWorktrees)
	if got := m.sidebarHelpTopic(); got != "guides/worktrees.md#worktrees" { // WTX-17: the view has its own guide
		t.Errorf("guide section %q", got)
	}
}

func TestWorktreeShowOutsideGit(t *testing.T) {
	m := wtTestModel(t, t.TempDir())
	cmd := m.revealWorktrees()
	msg := cmd().(wtLoadMsg)
	m.onWTLoad(msg)
	if m.wt.err == "" || !strings.Contains(stripANSI(m.renderWorktreesSidebar(29, 30)), "Not a git repository") {
		t.Errorf("err %q", m.wt.err)
	}
}

// j / k / G / g move the cursor card by card; Esc gives the focus back.
func TestWorktreeKeysMove(t *testing.T) {
	st, tabs := wtFixture(t)
	_ = tabs
	m := &Model{wt: st, focus: FocusExplorer, explorerWidth: 30, h: 42}
	key := func(s string) {
		t.Helper()
		var k tea.KeyMsg
		switch s {
		case "esc":
			k = tea.KeyMsg{Type: tea.KeyEsc}
		default:
			k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
		}
		nm, _ := m.handleWTSidebarKey(k)
		*m = nm
	}
	items, rows := m.wtList()
	if got := items[rows[st.cursorIndex(items, rows)].item].e.Path; got != "/src/app.worktrees/wt-sidebar" {
		t.Fatalf("first card %q", got)
	}
	key("j")
	if st.cursorPath != "/src/app.worktrees/toast" {
		t.Errorf("j: %q", st.cursorPath)
	}
	key("G") // the archived row is last (WTX-14)
	if st.cursorPath != "/src/app.worktrees/old" {
		t.Errorf("G: %q", st.cursorPath)
	}
	key("k")
	if st.cursorPath != "/tmp/deleted" {
		t.Errorf("k: %q", st.cursorPath)
	}
	key("g")
	if st.cursorPath != "/src/app.worktrees/wt-sidebar" {
		t.Errorf("g: %q", st.cursorPath)
	}
	key("esc")
	if m.focus != FocusEditor {
		t.Error("esc kept the focus")
	}
}

// A click selects a card.
func TestWorktreeClickAndIcon(t *testing.T) {
	st, _ := wtFixture(t)
	m := &Model{wt: st, explorerWidth: 30, h: 42}
	m.activity.SetActive(activity.ViewWorktrees)
	var y int
	for _, h := range m.wtHits(29) {
		if h.ID == "wt:/elsewhere/api-v2" {
			y = h.Y
			break
		}
	}
	if y == 0 {
		t.Fatal("no hit target for api-v2")
	}
	nm, _ := m.handleWTSidebarMouse(3, y, tea.MouseLeft)
	if nm.wt.cursorPath != "/elsewhere/api-v2" || nm.focus != FocusExplorer {
		t.Errorf("click: %q %v", nm.wt.cursorPath, nm.focus)
	}
}

// W3 leftover: Worktree: Show run while the terminal has the keys moves
// them to the view (F, s, x … reach the list, not the shell).
func TestWorktreeShowFromTerminalTakesTheKeys(t *testing.T) {
	m := wtTestModel(t, t.TempDir())
	m.wt.off = true
	m.inTerminal, m.panelFocused, m.focus = true, true, FocusEditor
	if _, ok := m.dispatchWorktreePalette("worktree-view"); !ok {
		t.Fatal("worktree-view not handled")
	}
	if m.inTerminal || m.panelFocused || m.focus != FocusExplorer {
		t.Errorf("keys still go to the terminal: inTerminal %v panelFocused %v focus %v", m.inTerminal, m.panelFocused, m.focus)
	}
}
