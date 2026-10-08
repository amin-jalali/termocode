package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/nvim"
)

func TestCellToByte(t *testing.T) {
	s := "ab漢c"
	cases := map[int]int{0: 0, 1: 1, 2: 2, 3: 2, 4: 5, 5: -1}
	for cell, want := range cases {
		if got := cellToByte(s, cell); got != want {
			t.Errorf("cellToByte(%d) = %d, want %d", cell, got, want)
		}
	}
}

func TestTaskTabGlyph(t *testing.T) {
	if g := taskTabGlyph(terminalTab{Running: true, LastExit: -1}); g != "▶" {
		t.Errorf("running = %q", g)
	}
	if g := taskTabGlyph(terminalTab{LastExit: 0}); g != "✓" {
		t.Errorf("ok = %q", g)
	}
	if g := taskTabGlyph(terminalTab{LastExit: 2}); g != "✘" {
		t.Errorf("fail = %q", g)
	}
	m := Model{terminalTabs: []terminalTab{{Task: "build", LastExit: 1}, {Shell: "zsh", LastExit: -1}}}
	if g := m.panelEntryGlyph(panelBarEntry{Kind: panelKindTerminal, Index: 0}); g != "✘" {
		t.Errorf("task tab glyph = %q", g)
	}
	if g := m.panelEntryGlyph(panelBarEntry{Kind: panelKindTerminal, Index: 1}); g != "%" {
		t.Errorf("shell tab glyph = %q", g)
	}
}

func TestPathResolver(t *testing.T) {
	root := t.TempDir()
	must := func(p string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must(filepath.Join(root, "pkg", "calc", "calc_test.go"))
	must(filepath.Join(root, "main.go"))
	must(filepath.Join(root, "node_modules", "dep", "x.js"))
	r := newPathResolver(root)
	if got := r.resolve("./main.go", ""); got != filepath.Join(root, "main.go") {
		t.Errorf("root-relative = %q", got)
	}
	if got := r.resolve("calc_test.go", filepath.Join(root, "pkg", "calc")); got != filepath.Join(root, "pkg", "calc", "calc_test.go") {
		t.Errorf("dir-relative = %q", got)
	}
	// go test prints paths relative to the package: found by suffix.
	if got := r.resolve("calc/calc_test.go", root); got != filepath.Join(root, "pkg", "calc", "calc_test.go") {
		t.Errorf("suffix search = %q", got)
	}
	if got := r.resolve("x.js", root); got != "" {
		t.Errorf("node_modules must be skipped, got %q", got)
	}
	if got := r.resolve("/nope/a.go", root); got != "" {
		t.Errorf("missing abs = %q", got)
	}
}

func TestUserCommandTasks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[{"id":"a","title":"Lint","cmd":"make lint","cwd":"sub"},{"id":"b","title":"Empty","cmd":""}]`
	if err := os.WriteFile(filepath.Join(dir, "termocode", "commands.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := userCommandTasks()
	if len(got) != 1 || got[0].Label != "User: Lint" || got[0].Command != "make lint" || got[0].Cwd != "sub" || got[0].Source != "user" {
		t.Errorf("userCommandTasks = %+v", got)
	}
}

// runCmd executes a Cmd tree synchronously against m (Batch + applyMsg),
// standing in for the Bubble Tea loop.
func runCmd(m *Model, c tea.Cmd) {
	if c == nil {
		return
	}
	switch msg := c().(type) {
	case tea.BatchMsg:
		for _, sub := range msg {
			runCmd(m, sub)
		}
	case applyMsg:
		runCmd(m, msg(m))
	}
}

// TestRunTaskProblemMatcherNvim runs a failing task in a real nvim PTY tab
// and checks the exit status lands on the tab and the matched problem
// lands in vim.diagnostic (namespace termocode-tasks) and the Problems
// panel.
func TestRunTaskProblemMatcherNvim(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte("package x\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"tasks":[{"label":"fail","command":"printf 'x.go:3:6: error: boom\\n'; exit 3","problemMatcher":"$generic"}]}`
	if err := os.WriteFile(filepath.Join(root, ".termocode", "tasks.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

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

	runCmd(&m, m.runTaskByLabel("fail"))
	if len(m.terminalTabs) != 1 || m.terminalTabs[0].Task != "fail" || !m.terminalTabs[0].Running {
		t.Fatalf("task tab not created: %+v", m.terminalTabs)
	}
	buf := m.terminalTabs[0].BufID
	// Focus must stay out of the task terminal.
	if out, _ := c.EvalLuaString(`return vim.bo.buftype`); out == "terminal" {
		t.Error("running a task focused the terminal window")
	}

	// Wait for the job to end and its output to reach the buffer.
	var code int64 = -1
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_ = c.EvalLuaArgs(`
			local b = ...
			local ch = vim.bo[b].channel
			if not ch or ch <= 0 then return -2 end
			return vim.fn.jobwait({ ch }, 100)[1]
		`, &code, buf)
		var text string
		_ = c.EvalLuaArgs(`return table.concat(vim.api.nvim_buf_get_lines(..., 0, -1, false), '\n')`, &text, buf)
		if code != -1 && strings.Contains(text, "boom") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if code != 3 {
		t.Fatalf("job exit code = %d, want 3", code)
	}

	runCmd(&m, m.onTaskExit(buf, int(code)))
	if tab := m.terminalTabs[0]; tab.Running || tab.LastExit != 3 {
		t.Errorf("tab after exit: %+v", tab)
	}
	if m.activeTerminalLastExit() != 3 {
		t.Errorf("status dot exit = %d", m.activeTerminalLastExit())
	}

	out, err := c.EvalLuaString(problemsLua)
	if err != nil {
		t.Fatalf("problemsLua: %v", err)
	}
	diags := parseProblemsJSON(out)
	if len(diags) != 1 {
		t.Fatalf("diagnostics = %+v", diags)
	}
	d := diags[0]
	if filepath.Base(d.Path) != "x.go" || d.Line != 3 || d.Col != 6 || d.Severity != 1 || d.Message != "boom" || d.Source != "task: fail" {
		t.Errorf("diagnostic = %+v", d)
	}

	// Rerun replaces the tab instead of stacking a second one.
	runCmd(&m, m.rerunLastTask())
	if len(m.terminalTabs) != 1 || m.terminalTabs[0].BufID == buf {
		t.Errorf("rerun tabs = %+v", m.terminalTabs)
	}
	runCmd(&m, m.clearTaskProblems())
	if out, _ := c.EvalLuaString(problemsLua); out != "" {
		t.Errorf("clearTaskProblems left %q", out)
	}
}
