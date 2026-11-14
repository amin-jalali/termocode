package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// Coverage of problems_panel.go: the panel state, rendering, keys, mouse and
// the context menu, on a real nvim model with real diagnostics.

func problemsPanelModel(t *testing.T) (Model, string) {
	t.Helper()
	m, dir := goRepoModel(t)
	lib := filepath.Join(dir, "lib.go")
	openAt(t, &m, lib, 1)
	_ = m.nvim.ExecLua(`local ns = vim.api.nvim_create_namespace('pp')
		vim.diagnostic.set(ns, 0, {
			{ lnum = 0, col = 0, severity = 1, message = 'first error', source = 'vet' },
			{ lnum = 2, col = 2, severity = 2, message = 'warn\nsecond line' },
			{ lnum = 4, col = 0, severity = 3, message = 'info ' .. string.rep('long ', 40) },
			{ lnum = 4, col = 1, severity = 4, message = 'hint' },
		})`)
	m.problems = newProblemsPanel()
	out, err := m.nvim.EvalLuaString(problemsLua)
	if err != nil {
		t.Fatal(err)
	}
	m.problems.setItems(parseProblemsJSON(out))
	if len(m.problems.items) != 4 {
		t.Fatalf("items %+v", m.problems.items)
	}
	return m, lib
}

func TestCovProblemsPanelStateAndRender(t *testing.T) {
	m, _ := problemsPanelModel(t)
	p := m.problems
	if e, w, i := p.counts(); e != 1 || w != 1 || i != 2 {
		t.Errorf("counts %d %d %d", e, w, i)
	}
	(*problemsPanel)(nil).setItems(nil)
	if e, _, _ := (*problemsPanel)(nil).counts(); e != 0 || (*problemsPanel)(nil).rows() != nil {
		t.Error("nil panel")
	}
	for _, w := range []int{20, 40, 80, 140} {
		for sel := range p.rows() {
			p.sel = sel
			for _, focused := range []bool{true, false} {
				m.panelFocused = focused
				got := renderProblemsTab(&m, w, 12)
				if len(got) < 2 {
					t.Fatalf("w=%d sel=%d: %d rows", w, sel, len(got))
				}
			}
		}
	}
	_ = renderProblemsTab(&m, 80, 1)
	// every filter hidden / empty states
	p.hide = [4]bool{false, true, true, true}
	if got := renderProblemsTab(&m, 80, 10); !strings.Contains(strings.Join(got, "\n"), "hidden") {
		t.Error("all hidden")
	}
	p.hide = [4]bool{}
	p.setItems(nil)
	if got := renderProblemsTab(&m, 80, 10); !strings.Contains(strings.Join(got, "\n"), "No problems") {
		t.Error("empty")
	}
	m.problems = nil
	_ = renderProblemsTab(&m, 80, 5)
	// the title
	if problemsTitle(&m) != "PROBLEMS" {
		t.Error("title")
	}
	m, _ = problemsPanelModel(t)
	if problemsTitle(&m) != "PROBLEMS (4)" {
		t.Error("title count")
	}
	if problemsFixLabel(70) == "" || problemsFixLabel(40) == "" || problemsFixLabel(10) != "" {
		t.Error("fix label")
	}
	for _, w := range []int{10, 40, 100} {
		_ = problemsHeaderLayout(1, 2, 3, w)
	}
	_ = renderProblemsHeader(m.problems, 80)
	m.problems.hide[1] = true
	_ = renderProblemsHeader(m.problems, 80)
	if problemRelPath("/elsewhere/x.go") != "/elsewhere/x.go" {
		t.Error("outside path")
	}
	for _, s := range []string{"", "{bad", `[[1,2]]`, `[["p",1,1,1,"s","m"]]`} {
		_ = parseProblemsJSON(s)
	}
	_ = sortedAfterSet(p)
}

func sortedAfterSet(p *problemsPanel) bool {
	p.setItems([]nvim.Diagnostic{{Path: "b", Severity: 2, Line: 3}, {Path: "a", Severity: 2, Line: 2}, {Path: "a", Severity: 2, Line: 2, Col: 1}, {Path: "a", Severity: 1, Line: 9}})
	p.sel = 1
	p.setItems(append([]nvim.Diagnostic(nil), p.items...)) // keeps the selection
	return true
}

func TestCovProblemsPanelKeysAndMouse(t *testing.T) {
	m, _ := problemsPanelModel(t)
	p := m.problems
	p.viewH = 3
	for _, k := range []string{"down", "j", "up", "k", "pgdown", "pgup", "end", "G", "home", "g", " ", " ", "left", "h", "right", "l", "e", "e", "w", "w", "i", "i", "c", "c", "enter", "o", "r", "a", "zzz"} {
		cmd, handled := problemsKey(&m, keyOf(k))
		_ = cmd
		if k == "zzz" && handled {
			t.Error("unknown key must not be handled")
		}
		m.panelFocused = true
	}
	if _, ok := problemsKey(&Model{}, keyOf("j")); ok {
		t.Error("no panel")
	}
	// selection: header toggles, row jumps
	p.hide, p.collapsed, p.scroll = [4]bool{}, map[string]bool{}, 0
	p.sel = 0
	m.openSelectedProblem()
	p.collapsed = map[string]bool{}
	p.sel = 1
	m.focus = FocusExplorer
	m.openSelectedProblem()
	if m.focus != FocusEditor {
		t.Error("opening a problem focuses the editor")
	}
	p.sel = 99
	m.openSelectedProblem()
	// mouse: header chips and rows
	p.sel = 0
	for col := 0; col < 40; col++ {
		problemsClick(&m, 0, col)
	}
	p.hide = [4]bool{}
	for row := 1; row <= 5; row++ {
		problemsClick(&m, row, 3)
		problemsClick(&m, row, m.editorPaneWidth()-2) // the ✦ chip of the selected row
	}
	problemsClick(&m, 50, 3)
	problemsClick(&Model{}, 1, 1)
	// right click + menu
	p.hide, p.collapsed, p.scroll = [4]bool{}, map[string]bool{}, 0
	p.sel = 0
	if problemsRightClick(&m, 99, 0, 5, 5); m.menuOpen {
		t.Error("out of range")
	}
	problemsRightClick(&m, 1, 0, 5, 5) // a header: nothing
	problemsRightClick(&m, 2, 0, 5, 5)
	if !m.menuOpen || m.menuKind != menuKindProblems {
		t.Fatal("menu")
	}
	for _, id := range []string{"problem-open", "problem-fix-ai", "problem-copy", "other"} {
		m.handleProblemsMenuSelect(id)
	}
	p.sel = 0 // a header has no diagnostic
	if m.handleProblemsMenuSelect("problem-open") != nil || m.fixSelectedProblemWithAI() != nil {
		t.Error("header row")
	}
	if (&Model{}).handleProblemsMenuSelect("x") != nil || (&Model{}).fixSelectedProblemWithAI() != nil {
		t.Error("no panel")
	}
	// scroll callback
	spec := panelKinds[panelKindProblems]
	spec.Scroll(&m, 2)
	spec.Scroll(&m, -9)
	spec.Scroll(&Model{}, 1)
	_ = spec.TitleFn(&m)
	// the refresh command and the panel show
	if cmd := m.refreshProblemsCmd(); cmd != nil {
		if am, ok := cmd().(applyMsg); ok {
			am(&m)
		}
	}
	if (Model{}).refreshProblemsCmd() != nil {
		t.Error("no editor")
	}
	m.showProblemsPanel()
	m.jumpToDiagnostic("", 1, 1)
	m.jumpToDiagnostic(filepath.Join(filepath.Dir(m.editor.Path()), "main_test.go"), 0, 0)
	(&Model{}).jumpToDiagnostic("x", 1, 1)
	_ = os.Remove("x")
}
