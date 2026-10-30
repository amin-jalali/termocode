package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/keymap"
)

// The start page cursor moves through the tile grid and the lists.
func TestCovCoreWelcomeStep(t *testing.T) {
	// 6 tiles in 3 columns, then 4 list rows: n = 10.
	cases := []struct {
		cur  int
		down bool
		want int
	}{
		{0, true, 3},  // down a tile row
		{4, true, 6},  // off the grid: the first list row
		{4, false, 1}, // up a tile row
		{1, false, 9}, // up from the first tile row: the last row
		{6, false, 3}, // up from the first list row: the last tile row
		{8, false, 7}, // up in the list
		{9, true, 0},  // down from the last row wraps
	}
	for _, c := range cases {
		if got := welcomeStep(c.cur, 10, c.down, 6, 3); got != c.want {
			t.Errorf("welcomeStep(%d, down=%v) = %d, want %d", c.cur, c.down, got, c.want)
		}
	}
	if welcomeStep(2, 5, false, 0, 1) != 1 || welcomeStep(4, 5, true, 0, 1) != 0 {
		t.Error("a plain list")
	}
}

// Start page keys and rows: a stale cursor resets, workspace rows open
// the picker, unknown rows and actions do nothing.
func TestCovCoreWelcomeRows(t *testing.T) {
	withConfigDir(t)
	m := covNew(t)
	m, _ = m.update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.welcomeFocus = 999
	m, _, handled := m.handleWelcomeKey(tea.KeyMsg{Type: tea.KeyRight})
	if !handled || m.welcomeFocus != 1 {
		t.Errorf("a stale cursor: handled %v focus %d", handled, m.welcomeFocus)
	}
	m, _ = m.activateWelcomeRow("workspace:" + welcomeSentinelBrowseAll)
	if !m.pickerOpen && !strings.Contains(toastText(m), "workspace") {
		t.Error("browse all workspaces opened nothing")
	}
	m.pickerOpen = false
	m, _ = m.activateWelcomeRow("workspace:" + t.TempDir() + "/missing")
	if m2, cmd := m.activateWelcomeRow("nonsense:x"); cmd != nil || m2.pickerOpen {
		t.Error("an unknown row did something")
	}
	if _, cmd := m.dispatchWelcomeAction(keymap.ActionSave); cmd != nil {
		t.Error("an action the start page does not have did something")
	}
	if k := synthKey("x"); k.String() != "x" {
		t.Errorf("synthKey(x) = %q", k.String())
	}
}

// Clicks on the bar's × of a panel tab close that tab.
func TestCovCorePanelTabCloseClick(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	m.showPanelTab(panelKindOutput)
	m.showPanelTab(panelKindProblems)
	first := m.panelTabs[0]
	x, y := covBarCell(t, m, panelHitTabClose, 0)
	m, _ = covClick(m, x, y)
	if len(m.panelTabs) != 1 || m.panelTabs[0] == first {
		t.Errorf("tabs after × on %v: %v", first, m.panelTabs)
	}
}
