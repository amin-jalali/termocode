package app

import (
	"os/exec"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
)

func TestJobTabGlyphForServiceTabs(t *testing.T) {
	m := Model{terminalTabs: []terminalTab{{Service: "postgres", Running: true, LastExit: -1}, {Service: "api", LastExit: 1}}}
	if g := m.panelEntryGlyph(panelBarEntry{Kind: panelKindTerminal, Index: 0}); g != "▶" {
		t.Errorf("running service tab glyph = %q", g)
	}
	if g := m.panelEntryGlyph(panelBarEntry{Kind: panelKindTerminal, Index: 1}); g != "✘" {
		t.Errorf("failed service tab glyph = %q", g)
	}
	if !(terminalTab{Service: "x"}).isJob() || !(terminalTab{Task: "x"}).isJob() || (terminalTab{}).isJob() {
		t.Error("isJob")
	}
}

// spawnTerminalJob starts a job tab with a key; the same key again
// replaces the old tab, and the exit lands on the tab.
func TestSpawnTerminalJobNvim(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(100, 30); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	_ = c.ExecLua(`vim.o.shell = '/bin/sh'`)
	m := Model{w: 100, h: 30, nvim: c, terminalRows: 8, output: newOutputStore(),
		tasks: newTaskRunner(), problems: newProblemsPanel()}

	buf, err := m.spawnTerminalJob(terminalJob{Label: "logs: postgres", Cmdline: "echo hi", Dir: root, Service: "postgres"})
	if err != nil || buf <= 0 {
		t.Fatalf("spawn: %d %v", buf, err)
	}
	if len(m.terminalTabs) != 1 || m.terminalTabs[0].Service != "postgres" || m.terminalTabs[0].Name != "logs: postgres" ||
		!m.terminalTabs[0].Running || m.terminalTabs[0].Task != "" {
		t.Fatalf("tab = %+v", m.terminalTabs)
	}
	buf2, err := m.spawnTerminalJob(terminalJob{Label: "logs: postgres", Cmdline: "echo again", Dir: root, Service: "postgres"})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.terminalTabs) != 1 || m.terminalTabs[0].BufID != buf2 || buf2 == buf {
		t.Errorf("same key must replace: %+v", m.terminalTabs)
	}
	if _, err := m.spawnTerminalJob(terminalJob{Label: "logs: redis", Cmdline: "echo r", Dir: root, Service: "redis"}); err != nil {
		t.Fatal(err)
	}
	if len(m.terminalTabs) != 2 || m.terminalActiveTab != 1 {
		t.Errorf("a new key adds a tab: %+v active=%d", m.terminalTabs, m.terminalActiveTab)
	}
	runCmd(&m, m.onTaskExit(buf2, 2))
	if m.terminalTabs[0].Running || m.terminalTabs[0].LastExit != 2 {
		t.Errorf("exit not on tab: %+v", m.terminalTabs[0])
	}
}
