package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/layout"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// layoutNvimModel is a Model on a real nvim inside a fresh project folder.
func layoutNvimModel(t *testing.T) (Model, string) {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}
	t.Setenv("SHELL", "/bin/sh")
	withConfigDir(t)
	root := layoutProject(t)
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(120, 40); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	return Model{w: 120, h: 40, nvim: c, terminalRows: 8, output: newOutputStore(), showExp: true, explorerWidth: defaultExplorerWidth}, root
}

// shellDir is the folder the shell of tab i runs in (Linux: /proc).
func shellDir(t *testing.T, m Model, i int) string {
	t.Helper()
	pid, err := m.nvim.EvalLuaString(`return tostring(vim.fn.jobpid(vim.b[` + intToLua(m.terminalTabs[i].BufID) + `].terminal_job_id))`)
	if err != nil {
		t.Fatal(err)
	}
	d, err := os.Readlink("/proc/" + strings.TrimSpace(pid) + "/cwd")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestOpenLayoutPanelOpensTabsAndShellsInTheirFolders(t *testing.T) {
	m, root := layoutNvimModel(t)
	if err := os.MkdirAll(filepath.Join(root, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.openLayout(layout.Layout{Panel: &layout.Panel{
		Open: true, Rows: 12, Active: "terminal", Tabs: []string{"problems"},
		Terminals: []layout.Terminal{{Name: "web", Cwd: "web"}, {Name: "gone", Cwd: "no/such/dir"}},
	}})
	if !m.termOpen || m.terminalRows != 12 {
		t.Fatalf("open=%v rows=%d", m.termOpen, m.terminalRows)
	}
	if len(m.terminalTabs) != 2 || len(m.panelTabs) != 1 || m.panelTabs[0] != panelKindProblems {
		t.Fatalf("terminals=%d tabs=%v", len(m.terminalTabs), m.panelTabs)
	}
	if m.panelActive != panelKindTerminal {
		t.Errorf("active = %v, want the terminal", m.panelActive)
	}
	if runtime.GOOS == "linux" {
		if got := shellDir(t, m, 0); got != filepath.Join(root, "web") {
			t.Errorf("first shell in %q", got)
		}
		if got := shellDir(t, m, 1); got != root { // a folder that does not exist: the project
			t.Errorf("second shell in %q, want %q", got, root)
		}
	}
	if s := loadSession(); s.TerminalRows != 12 {
		t.Errorf("rows not saved: %d", s.TerminalRows)
	}
}

func TestOpenLayoutPanelKeepsShellsThatAlreadyRunAndClosesTheRest(t *testing.T) {
	m, _ := layoutNvimModel(t)
	m.showPanelTab(panelKindTerminal)
	before := m.terminalTabs[0].BufID
	m.openLayout(layout.Layout{Panel: &layout.Panel{Open: true, Terminals: []layout.Terminal{{}, {}}}})
	if len(m.terminalTabs) != 2 || m.terminalTabs[0].BufID != before {
		t.Fatalf("tabs = %+v", m.terminalTabs)
	}
	m.openLayout(layout.Layout{Panel: &layout.Panel{Open: false}})
	if m.termOpen {
		t.Fatal("panel still open")
	}
	m.openLayout(layout.Layout{Panel: &layout.Panel{Open: true}}) // shells are back, none new
	if !m.termOpen || len(m.terminalTabs) != 2 {
		t.Fatalf("open=%v tabs=%d", m.termOpen, len(m.terminalTabs))
	}
}

func TestOpenLayoutPanelWithOnlyAnOutputTabHasNoShell(t *testing.T) {
	m, _ := layoutNvimModel(t)
	m.openLayout(layout.Layout{Panel: &layout.Panel{Open: true, Tabs: []string{"output"}, Active: "output"}})
	if !m.termOpen || m.panelActive != panelKindOutput {
		t.Fatalf("open=%v active=%v", m.termOpen, m.panelActive)
	}
}
