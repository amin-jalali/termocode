package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/layout"
)

func TestOpenLayoutSetsSidebarChatAndSavesThem(t *testing.T) {
	withConfigDir(t)
	layoutProject(t)
	m := panelTestModel()
	m.termOpen = false
	m.terminalTabs = nil
	m.showExp = false
	m.actionsOpen = false

	m.openLayout(layout.Layout{
		Sidebar: &layout.Sidebar{Open: true, View: "git", Width: 44},
		Chat:    &layout.Chat{Open: true, Width: 61},
		Focus:   "sidebar",
	})
	if !m.showExp || m.activity.Active() != activity.ViewGit || m.explorerWidth != 44 {
		t.Errorf("sidebar: show=%v view=%v width=%d", m.showExp, m.activity.Active(), m.explorerWidth)
	}
	if !m.actionsOpen || !m.actionsPinned || m.actionsWidth != 61 || !m.actionsWidthSet {
		t.Errorf("chat: open=%v pinned=%v width=%d", m.actionsOpen, m.actionsPinned, m.actionsWidth)
	}
	if m.focus != FocusExplorer {
		t.Errorf("focus = %v", m.focus)
	}
	s := loadSession() // saved like a manual change: the next start comes back the same
	if s.ExplorerWidth != 44 || s.ActionsOpen == nil || !*s.ActionsOpen || s.ActionsWidth != 61 {
		t.Errorf("session = %+v", s)
	}
}

func TestOpenLayoutHidesWhatItClosesAndLeavesTheRest(t *testing.T) {
	withConfigDir(t)
	layoutProject(t)
	m := panelTestModel()
	m.showExp = true
	m.explorerWidth = 31
	m.actionsOpen, m.actionsPinned = true, true

	m.openLayout(layout.Layout{Sidebar: &layout.Sidebar{Open: false}, Chat: &layout.Chat{Open: false}})
	if m.showExp || m.actionsOpen {
		t.Errorf("sidebar %v chat %v still shown", m.showExp, m.actionsOpen)
	}
	if m.explorerWidth != 31 || m.activity.Active() != activity.ViewFiles {
		t.Errorf("width %d view %v changed", m.explorerWidth, m.activity.Active())
	}
	if !m.termOpen || len(m.terminalTabs) != 2 { // no panel section: panel untouched
		t.Errorf("panel changed: open=%v tabs=%d", m.termOpen, len(m.terminalTabs))
	}
}

func TestOpenLayoutLeavesZenAndClampsWidth(t *testing.T) {
	t.Setenv("TERMOCODE_HEADLESS", "1") // zen would press F11 on the host
	withConfigDir(t)
	layoutProject(t)
	m := panelTestModel()
	m.zenMode = true
	m.showExp = false
	m.openLayout(layout.Layout{Sidebar: &layout.Sidebar{Open: true, Width: 5000}})
	if m.zenMode || !m.showExp {
		t.Errorf("zen=%v showExp=%v", m.zenMode, m.showExp)
	}
	if m.explorerWidth > explorerMaxWidth || m.explorerWidth < explorerMinWidth {
		t.Errorf("width %d outside %d..%d", m.explorerWidth, explorerMinWidth, explorerMaxWidth)
	}
}
