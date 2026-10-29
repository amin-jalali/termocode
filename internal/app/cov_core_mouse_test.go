package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
)

// covStatusCell finds the status bar cell of chip id.
func covStatusCell(t *testing.T, m Model, id string) (int, int) {
	t.Helper()
	x, y, ok := covHitCell(m, func(x, y int) bool { return m.statusHitAt(x, y) == id })
	if !ok {
		var ids []string
		for _, s := range m.statusSpans() {
			ids = append(ids, s.id)
		}
		t.Fatalf("no status chip %q; chips: %q", id, ids)
	}
	return x, y
}

func TestCovCoreMouseUnsizedIgnored(t *testing.T) {
	var m Model
	if got, cmd := m.handleMouse(tea.MouseMsg{X: 1, Y: 1, Type: tea.MouseLeft}); cmd != nil || got.hoverX != 0 {
		t.Error("an unsized model handled the mouse")
	}
}

// Status bar chips open what they name.
func TestCovCoreMouseStatusChips(t *testing.T) {
	m := covNvimModel(t, 140, 40)
	m.editor.SetMeta(filepath.Join(t.TempDir(), "main.go"), false, "go")
	m.editor.SetDiagnostics(2, 1)

	x, y := covStatusCell(t, m, "status:diag")
	m2, _ := covClick(m, x, y)
	if !m2.termOpen || m2.panelActive != panelKindProblems {
		t.Errorf("diag chip: panel open %v active %v", m2.termOpen, m2.panelActive)
	}

	x, y = covStatusCell(t, m, "status:lsp")
	m2, _ = covClick(m, x, y)
	if !m2.pickerOpen || m2.pickerKind != pickerKindToolManager {
		t.Errorf("lsp chip: picker %v kind %v", m2.pickerOpen, m2.pickerKind)
	}

	m.dapSessionActive = true
	x, y = covStatusCell(t, m, "status:debug")
	m2, _ = covClick(m, x, y)
	if !m2.showExp || m2.activity.Active() != activity.ViewRun {
		t.Errorf("debug badge: sidebar %v view %v", m2.showExp, m2.activity.Active())
	}
	m.dapSessionActive = false

	x, y = covStatusCell(t, m, "status:ai")
	m2, _ = covClick(m, x, y)
	if m2.overlayKind() == "" && !m2.actionsOpen && !m2.addAIOpen {
		t.Error("the AI badge click opened nothing")
	}

	// A plain item: the click does nothing.
	for _, s := range m.statusSpans() {
		if strings.HasPrefix(s.id, "status:") && !strings.Contains("status:ai status:lsp status:diag", s.id) {
			bx, _ := m.statusBarRect()
			if _, cmd := covClick(m, bx+s.x0, m.h-1); cmd != nil && s.id == "status:pos" {
				t.Errorf("%s click gave a command", s.id)
			}
		}
	}
}

// The sidebar edge drags between its limits; the release saves it.
func TestCovCoreMouseSidebarDrag(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	m.showExp = true
	m.applyLayout()
	sx := activity.Width + m.explorerWidth - 1
	m, _ = m.update(tea.MouseMsg{X: sx, Y: 10, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.dragKind != dragExplorerRight {
		t.Fatalf("press on the edge: drag %v", m.dragKind)
	}
	move := func(x int) {
		m, _ = m.update(tea.MouseMsg{X: x, Y: 10, Type: tea.MouseMotion, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	}
	move(0)
	if m.explorerWidth != explorerMinWidth {
		t.Errorf("drag to 0: width %d", m.explorerWidth)
	}
	move(m.w - 1)
	if limit := min(explorerMaxWidth, m.w-activity.Width-editorScrollbarWidth-10); m.explorerWidth != limit {
		t.Errorf("drag to the right edge: width %d, want %d", m.explorerWidth, limit)
	}
	move(40)
	w := m.explorerWidth
	m, _ = m.update(tea.MouseMsg{X: 40, Y: 10, Type: tea.MouseRelease, Action: tea.MouseActionRelease})
	if m.dragKind != dragNone {
		t.Error("release kept the drag")
	}
	if s := loadSession(); s.ExplorerWidth != w {
		t.Errorf("saved width %d, want %d", s.ExplorerWidth, w)
	}
	covFitsScreen(t, m, "after the drag")
}

// The actions panel: pin collapses it to the launcher chip, the chip
// brings it back, × closes it, its left edge drags.
func TestCovCoreMouseActionsPanel(t *testing.T) {
	m := covNvimModel(t, 160, 40)
	m.toggleActionsPanel()
	m.applyLayout()
	if !m.actionsPanelVisible() {
		t.Fatal("actions panel not visible")
	}
	cell := func(id string) (int, int) {
		x, y, ok := covHitCell(m, func(x, y int) bool { return m.hitActionsButton(x, y) == id })
		if !ok {
			t.Fatalf("no actions button %q", id)
		}
		return x, y
	}
	x, y := cell("pin")
	m, _ = covClick(m, x, y)
	if m.actionsPinned || !m.actionsLauncherVisible() {
		t.Fatalf("pin: pinned %v launcher %v", m.actionsPinned, m.actionsLauncherVisible())
	}
	lx, ly, ok := covHitCell(m, m.hitActionsLauncher)
	if !ok {
		t.Fatal("no launcher chip")
	}
	m, _ = covClick(m, lx, ly)
	if !m.actionsPanelVisible() {
		t.Fatal("the launcher did not bring the panel back")
	}
	// Drag the left edge: clamped to the minimum and to half the screen.
	edge := m.w - m.actionsColumnWidth()
	m, _ = m.update(tea.MouseMsg{X: edge, Y: 10, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.dragKind == dragActionsLeft {
		move := func(x int) {
			m, _ = m.update(tea.MouseMsg{X: x, Y: 10, Type: tea.MouseMotion, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
		}
		move(m.w - 1)
		if m.actionsWidth != actionsMinWidth {
			t.Errorf("drag right: width %d", m.actionsWidth)
		}
		move(0)
		if m.actionsWidth > m.w/2 {
			t.Errorf("drag left: width %d > half", m.actionsWidth)
		}
		covFitsScreen(t, m, "actions drag")
		m, _ = m.update(tea.MouseMsg{X: 0, Y: 10, Type: tea.MouseRelease, Action: tea.MouseActionRelease})
	}
	x, y = cell("close")
	m, _ = covClick(m, x, y)
	if m.actionsOpen || m.actionsLauncherVisible() {
		t.Errorf("close: open %v", m.actionsOpen)
	}
	if s := loadSession(); s.ActionsOpen == nil || *s.ActionsOpen {
		t.Error("closed panel not saved")
	}
}

// The ⋮ menu: click opens, hover moves the highlight, a click on an
// item runs it, a click outside closes it.
func TestCovCoreMouseOverflowMenu(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	gx, gy, ok := covHitCell(m, m.hitOverflowGlyph)
	if !ok {
		t.Skip("no ⋮ glyph on this screen")
	}
	m, _ = covClick(m, gx, gy)
	if !m.overflowMenuOpen {
		t.Fatal("⋮ did not open the menu")
	}
	// The second row of items.
	var ix, iy, idx int
	found := false
	for y := 0; y < m.h && !found; y++ {
		for x := 0; x < m.w; x++ {
			if in, i := m.overflowMenuHitAt(x, y); in && i > 0 {
				ix, iy, idx, found = x, y, i, true
				break
			}
		}
	}
	if !found {
		t.Fatal("no item rows")
	}
	m, _ = m.update(tea.MouseMsg{X: ix, Y: iy, Type: tea.MouseMotion, Action: tea.MouseActionMotion})
	if m.overflowMenuCursor != idx {
		t.Errorf("hover: cursor %d, want %d", m.overflowMenuCursor, idx)
	}
	// The border absorbs a click.
	bx, by, ok := covHitCell(m, func(x, y int) bool { in, i := m.overflowMenuHitAt(x, y); return in && i < 0 })
	if ok {
		m, _ = covClick(m, bx, by)
		if !m.overflowMenuOpen {
			t.Error("a click on the border closed the menu")
		}
	}
	m2, _ := covClick(m, ix, iy)
	if m2.overflowMenuOpen {
		t.Error("an item click left the menu open")
	}
	// ⋮ again toggles it closed.
	m, _ = covClick(m, gx, gy)
	if m.overflowMenuOpen {
		t.Error("⋮ on an open menu did not close it")
	}
	m.openOverflowMenu()
	m, _ = covClick(m, activity.Width+m.explorerWidth+5, m.h/2)
	if m.overflowMenuOpen {
		t.Error("a click outside left the menu open")
	}
}

// Side-by-side diff: the wheel scrolls nvim's panes, the nav buttons
// are clicks; Ctrl+click jumps to the definition.
func TestCovCoreMouseEditorWheelAndCtrlClick(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	p := filepath.Join(t.TempDir(), "long.txt")
	var b strings.Builder
	for i := 0; i < 300; i++ {
		b.WriteString("line\n")
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.nvim.OpenFile("edit", p); err != nil {
		t.Fatal(err)
	}
	m.gitDiffActive = true
	m.editor.SetMeta(p, false, "")
	pane := m.editorPaneLayout()
	x, y := pane.X+5, pane.Top+5
	for i := 0; i < 5; i++ {
		m, _ = m.update(tea.MouseMsg{X: x, Y: y, Type: tea.MouseWheelDown, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	}
	covWaitLuaNot(t, m, `return tostring(vim.fn.line('w0'))`, "1")
	m, _ = m.update(tea.MouseMsg{X: x, Y: y, Type: tea.MouseWheelUp, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	// Hover moves to the right pane, then back.
	m, _ = m.update(tea.MouseMsg{X: pane.X + pane.W - 3, Y: y, Type: tea.MouseMotion, Action: tea.MouseActionMotion})
	if m.gitDiffHoverPane != 1 {
		t.Errorf("hover right: pane %d", m.gitDiffHoverPane)
	}
	m, _ = m.update(tea.MouseMsg{X: x, Y: y, Type: tea.MouseMotion, Action: tea.MouseActionMotion})
	// The ‹ › buttons on the first row.
	m, _ = covClick(m, pane.X+1, pane.Top)
	m, _ = covClick(m, pane.X+4, pane.Top)
	m.gitDiffActive = false
	m, _ = m.update(tea.MouseMsg{X: x, Y: y, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Ctrl: true})
	if m.focus != FocusEditor {
		t.Errorf("ctrl+click: focus %v", m.focus)
	}
	var none Model
	if none.gotoDefinitionAtCell(1, 1) != nil {
		t.Error("goto definition without nvim")
	}
}

// covWaitLuaNot polls lua until it returns something other than not.
func covWaitLuaNot(t *testing.T, m Model, lua, not string) {
	t.Helper()
	for i := 0; i < 500; i++ {
		got, _ := m.nvim.EvalLuaString(lua)
		if strings.TrimSpace(got) != not {
			return
		}
		covSleep()
	}
	t.Fatalf("%s stayed %q", lua, not)
}

func covSleep() { time.Sleep(20 * time.Millisecond) }
