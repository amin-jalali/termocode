package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// paneShellDir is the folder the shell of pane i of tab ti runs in (Linux:
// /proc), "" elsewhere.
func paneShellDir(t *testing.T, m Model, ti, i int) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		return ""
	}
	buf := m.terminalTabs[ti].paneBuf(i)
	pid, err := m.nvim.EvalLuaString(`return tostring(vim.fn.jobpid(vim.b[` + intToLua(buf) + `].terminal_job_id))`)
	if err != nil {
		t.Fatal(err)
	}
	d, err := os.Readlink("/proc/" + strings.TrimSpace(pid) + "/cwd")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRestoreTerminalsBringsTabsSplitsAndFocusBack(t *testing.T) {
	m, root := layoutNvimModel(t)
	m.terminalRows = 14
	for _, d := range []string{"api", "web"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	saved := &savedTerminals{Open: true, Active: 0, Tabs: []savedTermTab{
		{Name: "api", Focus: 2, Panes: []savedTermPane{
			{Cwd: filepath.Join(root, "api")},
			{Cwd: filepath.Join(root, "web"), Split: "right"},
			{Cwd: root, Split: "down", Parent: 1},
		}},
		{Name: "two", Panes: []savedTermPane{{Cwd: root}}},
	}}
	m.restoreTerminals(saved, false)
	if !m.termOpen || len(m.terminalTabs) != 2 {
		t.Fatalf("open=%v tabs=%d err=%q", m.termOpen, len(m.terminalTabs), m.err)
	}
	if m.terminalActiveTab != 0 {
		t.Errorf("active tab %d, want 0", m.terminalActiveTab)
	}
	tab := m.terminalTabs[0]
	if tab.paneCount() != 3 || tab.Splits[0].Dir != paneRight || tab.Splits[1].Dir != paneDown || tab.Splits[1].Parent != 1 {
		t.Fatalf("first tab = %+v (err=%q)", tab, m.err)
	}
	if tab.PaneFocus != 2 {
		t.Errorf("focused pane %d, want 2", tab.PaneFocus)
	}
	if got := paneShellDir(t, m, 0, 0); got != "" && got != filepath.Join(root, "api") {
		t.Errorf("pane 0 in %q", got)
	}
	if got := paneShellDir(t, m, 0, 1); got != "" && got != filepath.Join(root, "web") {
		t.Errorf("pane 1 in %q", got)
	}
	if m.terminalTabs[1].Cwd != root || m.terminalTabs[1].paneCount() != 1 {
		t.Errorf("second tab = %+v", m.terminalTabs[1])
	}
	// Restored at start, the keyboard stays in the editor.
	cur, _ := m.nvim.EvalLuaString(`return vim.bo[vim.api.nvim_get_current_buf()].buftype`)
	if cur == "terminal" {
		t.Error("the editor should keep the keyboard after a start-up restore")
	}
	// And the next save writes the same shape back.
	if snap := m.terminalSnapshot(false); len(snap.Tabs) != 2 || len(snap.Tabs[0].Panes) != 3 || snap.Tabs[0].Focus != 2 || !snap.Open {
		t.Errorf("snapshot after restore = %+v", snap)
	}
}

func TestRestoreTerminalsAtStart(t *testing.T) {
	saved := func(open bool) SessionState {
		return SessionState{Terminals: &savedTerminals{Open: open, Tabs: []savedTermTab{{Panes: []savedTermPane{{Cwd: "/"}}}}}}
	}
	t.Run("open panel", func(t *testing.T) {
		m, _ := layoutNvimModel(t)
		m.restoreTerminalsAtStart(saved(true))
		if !m.termRestoreReady || !m.termOpen || len(m.terminalTabs) != 1 {
			t.Fatalf("ready=%v open=%v tabs=%d", m.termRestoreReady, m.termOpen, len(m.terminalTabs))
		}
	})
	t.Run("hidden panel waits for Ctrl+T", func(t *testing.T) {
		m, _ := layoutNvimModel(t)
		m.restoreTerminalsAtStart(saved(false))
		if !m.termRestoreReady || m.termOpen || m.termRestorePending == nil {
			t.Fatalf("ready=%v open=%v pending=%v", m.termRestoreReady, m.termOpen, m.termRestorePending)
		}
	})
	t.Run("setting off", func(t *testing.T) {
		m, _ := layoutNvimModel(t)
		writeTestConfig(t, os.Getenv("XDG_CONFIG_HOME"), `{"terminal_restore": false}`)
		m.restoreTerminalsAtStart(saved(true))
		if !m.termRestoreReady || !m.termRestoreOff || m.termOpen || m.termRestorePending != nil {
			t.Fatalf("ready=%v off=%v open=%v", m.termRestoreReady, m.termRestoreOff, m.termOpen)
		}
	})
}

// A panel that was hidden at quit comes back, with all its tabs, on the
// next Ctrl+T.
func TestCtrlTOpensTheTerminalsOfAHiddenPanel(t *testing.T) {
	m, root := layoutNvimModel(t)
	m.termRestorePending = &savedTerminals{Active: 1, Tabs: []savedTermTab{
		{Panes: []savedTermPane{{Cwd: root}}},
		{Panes: []savedTermPane{{Cwd: root}, {Cwd: root, Split: "right"}}},
	}}
	m.toggleTerminalPanel()
	if m.termRestorePending != nil || len(m.terminalTabs) != 2 || m.terminalActiveTab != 1 {
		t.Fatalf("pending=%v tabs=%d active=%d err=%q", m.termRestorePending, len(m.terminalTabs), m.terminalActiveTab, m.err)
	}
	if m.terminalTabs[1].paneCount() != 2 {
		t.Errorf("second tab has %d panes", m.terminalTabs[1].paneCount())
	}
	cur, _ := m.nvim.EvalLuaString(`return vim.bo[vim.api.nvim_get_current_buf()].buftype`)
	if cur != "terminal" {
		t.Errorf("Ctrl+T puts the keyboard in the terminal, current buftype %q", cur)
	}
}

// winbarOf is the title bar text of nvim window win.
func winbarOf(t *testing.T, m Model, win int) string {
	t.Helper()
	out, err := m.nvim.EvalLuaString(`return vim.wo[` + intToLua(win) + `].winbar`)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A folder that is gone: the shell starts in the project folder, and the
// pane says so until the user types in it.
func TestRestoreTerminalsGoneFolderStartsInTheProject(t *testing.T) {
	m, root := layoutNvimModel(t)
	gone := filepath.Join(root, "old-build")
	m.restoreTerminals(&savedTerminals{Open: true, Tabs: []savedTermTab{
		{Panes: []savedTermPane{{Cwd: gone}}},
	}}, true)
	if len(m.terminalTabs) != 1 || m.terminalTabs[0].Cwd != root {
		t.Fatalf("tabs = %+v err=%q", m.terminalTabs, m.err)
	}
	if got := paneShellDir(t, m, 0, 0); got != "" && got != root {
		t.Errorf("shell in %q, want the project folder", got)
	}
	bar := winbarOf(t, m, m.terminalWinID)
	if !strings.Contains(bar, "old-build is gone") || !strings.Contains(bar, "project folder") {
		t.Fatalf("title bar %q should say the folder is gone", bar)
	}
	m.inPaneWin, m.termOpen = true, true
	if m.restoreHintKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}) {
		t.Error("a typed key must still reach the shell")
	}
	if bar := winbarOf(t, m, m.terminalWinID); bar != "" {
		t.Errorf("after typing the note goes away, title bar %q", bar)
	}
}

// The note of a tab that is not on show appears when the user switches to it.
func TestRestoreNoteShowsOnTabSwitch(t *testing.T) {
	m, root := layoutNvimModel(t)
	m.restoreTerminals(&savedTerminals{Open: true, Active: 1, Tabs: []savedTermTab{
		{Panes: []savedTermPane{{Cwd: filepath.Join(root, "gone")}}},
		{Panes: []savedTermPane{{Cwd: root}}},
	}}, true)
	if bar := winbarOf(t, m, m.terminalWinID); bar != "" {
		t.Fatalf("tab 2 has no note, title bar %q", bar)
	}
	m.switchTerminalTab(0)
	if bar := winbarOf(t, m, m.terminalWinID); !strings.Contains(bar, "gone is gone") {
		t.Fatalf("tab 1's note is missing, title bar %q", bar)
	}
	m.switchTerminalTab(1)
	if bar := winbarOf(t, m, m.terminalWinID); bar != "" {
		t.Fatalf("back on tab 2, title bar %q", bar)
	}
}

func TestRestoredPaneSaysWhatWasRunning(t *testing.T) {
	m, root := layoutNvimModel(t)
	m.restoreTerminals(&savedTerminals{Open: true, Tabs: []savedTermTab{
		{Panes: []savedTermPane{{Cwd: root, Program: "npm"}}},
	}}, true)
	if bar := winbarOf(t, m, m.terminalWinID); !strings.Contains(bar, "was running: npm") {
		t.Fatalf("title bar %q", bar)
	}
}

// With terminal_restore_commands, Enter in the restored pane runs the last
// command again; any other key only drops the note.
func TestEnterRunsTheRestoredCommandAgain(t *testing.T) {
	m, root := layoutNvimModel(t)
	m.termRestoreCmds = true
	marker := filepath.Join(root, "ran.txt")
	m.restoreTerminals(&savedTerminals{Open: true, Tabs: []savedTermTab{
		{Panes: []savedTermPane{{Cwd: root, Program: "echo", Cmd: "echo again > " + shellQuote(marker)}}},
	}}, true)
	if bar := winbarOf(t, m, m.terminalWinID); !strings.Contains(bar, "Enter to run it again") {
		t.Fatalf("title bar %q", bar)
	}
	m.inPaneWin, m.termOpen = true, true
	if !m.restoreHintKey(tea.KeyMsg{Type: tea.KeyEnter}) {
		t.Fatal("Enter should run the command (and not reach the shell twice)")
	}
	if len(m.termHints) != 0 {
		t.Error("the note goes once the command runs")
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if b, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(b)) == "again" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the command did not run")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
