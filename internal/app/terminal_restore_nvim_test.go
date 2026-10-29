package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
