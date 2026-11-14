package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Coverage of debug_view.go: the Run view keys, mouse, activation of every
// row kind, the toolbar buttons and rendering, on a real nvim model that
// carries the paused-session fixture of debugTestModel.

func runViewModel(t *testing.T) Model {
	t.Helper()
	fx := debugTestModel(t) // chdirs to its own temp workspace with a go.mod
	m := newTestModel(t)
	if m.nvim == nil {
		t.Skip("no nvim")
	}
	m.w, m.h, m.explorerWidth, m.showExp = 120, 40, 41, true
	m.debug = fx.debug
	m.dapSessionActive = true
	m.debug.bps = map[string][]storedBreakpoint{"/ws/main.go": {{Line: 8, Text: "s := a + b"}, {Line: 20, Condition: "a > 1"}, {Line: 30, LogMessage: "hi"}}}
	m.debug.open["s:Locals/p"] = true
	m.debug.children["s:Locals/p"] = []dbgVar{{Name: "X", Value: "1"}}
	_ = m.nvim.ExecLua(`_G._termocode_dap = { variables = function() end, evaluate = function() end, select_frame = function() end, remove = function() end, set_stored = function() end, breakpoints = function() return '' end }`)
	return m
}

func TestCovRunViewKeys(t *testing.T) {
	m := runViewModel(t)
	d := m.debug
	keys := []string{"down", "j", "up", "k", "end", "G", "home", "g", "right", "l", "left", "h", "enter", " ", "x", "delete", "backspace", "a", "r", "esc"}
	for round := 0; round < 3; round++ {
		for _, k := range keys {
			m.handleRunSidebarKey(keyOf(k))
		}
		// walk the whole list with Enter / expand / remove on every row
		for i := range m.runRows() {
			d.cursor = i
			m.handleRunSidebarKey(keyOf("right"))
			m.handleRunSidebarKey(keyOf("enter"))
			m.handleRunSidebarKey(keyOf("left"))
			m.handleRunSidebarKey(keyOf("r"))
			m.dapSessionActive = true
			m.promptOpen = false
		}
		d.collapsed = map[runSection]bool{}
		d.watches = []dbgWatch{{Expr: "a+1", Value: "2"}, {Expr: "bad", Value: "undefined", Err: true}}
		d.bps = map[string][]storedBreakpoint{"/ws/main.go": {{Line: 8}, {Line: 20}}}
	}
	// activating each kind
	for _, r := range []runRow{
		{kind: runRowSection, section: runSecStack},
		{kind: runRowScope, key: "s:Locals", expandable: true},
		{kind: runRowVar, key: "s:Locals/a"},
		{kind: runRowConfig, key: "Go: Test"},
		{kind: runRowWatchAdd},
		{kind: runRowFrame, frameID: 12, path: "/ws/main.go", line: 14},
		{kind: runRowFrame, frameID: 12},
		{kind: runRowBreakpoint, path: "/ws/main.go", line: 8},
		{kind: runRowAction, action: "install"},
		{kind: runRowAction, action: "launch"},
		{kind: runRowAction, action: "other"},
		{kind: runRowNote},
	} {
		for _, mouse := range []bool{false, true} {
			m.runActivate(r, mouse)
			m.pickerOpen, m.promptOpen, m.confirmOpen = false, false, false
		}
	}
	// the section the Run view can focus
	for _, sec := range []runSection{runSecVariables, runSecWatch, runSecStack, runSecBreakpoints, runSecConfigs} {
		m.openRunView(sec, true)
		m.openRunView(sec, false)
	}
	for _, r := range []runRow{{}, {kind: runRowSection}} {
		_ = r.selectable()
	}
	if runNearestSelectable([]runRow{{kind: runRowNote}, {kind: runRowNote}}, 1) != 0 {
		t.Error("no selectable rows")
	}
}

func TestCovRunViewMouseToolbarAndRender(t *testing.T) {
	m := runViewModel(t)
	d := m.debug
	m.handleRunSidebarMouse(3, 5, tea.MouseWheelUp)
	m.handleRunSidebarMouse(3, 5, tea.MouseWheelDown)
	m.handleRunSidebarMouse(3, 5, tea.MouseMotion)
	// every toolbar button, in each session state
	for _, state := range []struct{ active, stopped bool }{{false, false}, {true, false}, {true, true}} {
		m.dapSessionActive, d.snap.Stopped = state.active, state.stopped
		for _, b := range runToolbarLayout(state.active, state.stopped, "Go: Debug package", m.explorerWidth-1) {
			m.handleRunSidebarMouse(b.start, runToolbarRow, tea.MouseLeft)
			m.pickerOpen, m.promptOpen = false, false
			m.dapSessionActive, d.snap.Stopped = state.active, state.stopped
		}
		m.handleRunSidebarMouse(0, runToolbarRow, tea.MouseLeft)
		_ = m.renderRunToolbar(40)
		_ = m.renderRunToolbar(12)
		_ = m.renderRunToolbar(120)
	}
	for _, w := range []int{6, 14, 30, 60} {
		for _, name := range []string{"", "a very long configuration name that must be cut"} {
			_ = runToolbarLayout(false, false, name, w)
			_ = runToolbarLayout(true, true, name, w)
		}
	}
	m.handleRunSidebarMouse(3, 0, tea.MouseLeft) // header
	m.handleRunSidebarMouse(3, 90, tea.MouseLeft)
	m.dapSessionActive, d.snap.Stopped = true, true
	for y := runHeaderRows; y < 30; y++ {
		m.handleRunSidebarMouse(4, y, tea.MouseLeft)
		m.pickerOpen, m.promptOpen, m.confirmOpen = false, false, false
		m.dapSessionActive = true
	}
	// rendering at several sizes and states
	for _, h := range []int{6, 12, 40} {
		for _, w := range []int{20, 41, 80} {
			for _, active := range []bool{true, false} {
				m.dapSessionActive = active
				for cur := range m.runRows() {
					d.cursor = cur
					if out := m.renderRunSidebar(w, h); out == "" {
						t.Fatal("empty sidebar")
					}
				}
			}
		}
	}
	d.hint, d.hintTool = "DAP: dlv missing — Install Adapter…", "dlv"
	d.configs.err = "launch.json: bad"
	d.snap = dbgSnapshot{}
	d.watches = nil
	d.bps = nil
	m.dapSessionActive = false
	rows := m.runRows()
	if len(rows) == 0 {
		t.Fatal("rows")
	}
	if !strings.Contains(m.renderRunSidebar(60, 20), "RUN") {
		t.Error("title")
	}
	_ = m.hitStatusDebugBadge(0, 0)
	m.runMoveCursor(1)
	m.debugClampCursor()
}
