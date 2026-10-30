package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCovCorePanelTitles(t *testing.T) {
	if panelKindTitle(panelKindTerminal) != "Terminal" || panelKindTitle(panelKind(9999)) != "Panel" {
		t.Error("titles")
	}
	if panelKindIcon(panelKind(9999)) != "≡" {
		t.Error("unknown kind icon")
	}
	var m Model
	if m.renderPanelContent(0, 5) != nil {
		t.Error("zero width content")
	}
}

// Closing panel tabs: the neighbour takes over, then the terminal, and
// with nothing left the panel closes.
func TestCovCorePanelTabsClose(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	m.showPanelTab(panelKind(9999)) // unknown: nothing
	if m.termOpen {
		t.Fatal("an unknown tab opened the panel")
	}
	m.showPanelTab(panelKindOutput)
	m.showPanelTab(panelKindProblems)
	m.showPanelTab(panelKindTests)
	if !m.termOpen || len(m.panelTabs) != 3 {
		t.Fatalf("tabs %v", m.panelTabs)
	}
	// Close a tab that is not the shown one: the shown one stays.
	m.closePanelTab(panelKindOutput)
	if m.panelActive != panelKindTests {
		t.Errorf("active %v after closing a hidden tab", m.panelActive)
	}
	m.closePanelTab(panelKindOutput) // already closed: nothing
	// Close the shown middle tab: the next one shows.
	m.activateNonTerminalTab(panelKindProblems)
	m.closePanelTab(panelKindProblems)
	if m.panelActive != panelKindTests {
		t.Errorf("after closing the middle: %v", m.panelActive)
	}
	// With a shell open, closing the last shown tab shows the shell.
	m.showPanelTab(panelKindTerminal)
	m.activateNonTerminalTab(panelKindTests)
	m.closeActivePanelTab()
	if m.panelActive != panelKindTerminal {
		t.Errorf("after closing the last tab: %v", m.panelActive)
	}
	// Two tabs, the last one shown, no shells: the one before shows.
	m.closeActivePanelTab() // the shell
	m.showPanelTab(panelKindOutput)
	m.showPanelTab(panelKindTests)
	m.closeActivePanelTab()
	if !m.termOpen || m.panelActive != panelKindOutput {
		t.Errorf("after closing the last of two: open %v active %v", m.termOpen, m.panelActive)
	}
	m.closeActivePanelTab()
	if m.termOpen {
		t.Error("the panel stayed open with no tabs")
	}
	m.closeActivePanelTab() // closed: nothing
}

// Panel content takes the mouse and keys; Esc gives the keys back.
func TestCovCorePanelInput(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	m.showPanelTab(panelKindProblems)
	x, y, w, h, ok := m.panelContentRect()
	if !ok {
		t.Fatal("no content rect")
	}
	cx, cy := x+w/2, y+h/2
	m, _ = m.update(tea.MouseMsg{X: cx, Y: cy, Type: tea.MouseRight, Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
	if !m.panelFocused {
		t.Error("right click did not focus the panel")
	}
	m, _ = m.update(tea.MouseMsg{X: cx, Y: cy, Type: tea.MouseWheelDown, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	m.menuOpen = false
	m.panelFocused = true
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.panelFocused {
		t.Error("esc kept the panel focus")
	}
	// With the shell shown, keys are not the panel's.
	m.showPanelTab(panelKindTerminal)
	m.panelFocused = true
	if _, ok := m.routePanelKey(tea.KeyMsg{Type: tea.KeyDown}); ok || m.panelFocused {
		t.Error("the shell tab took a panel key")
	}
	// A window too short for content.
	m, _ = m.update(tea.WindowSizeMsg{Width: 60, Height: 4})
	m.showPanelTab(panelKindOutput)
	covFitsScreen(t, m, "panel in a short window")
}
