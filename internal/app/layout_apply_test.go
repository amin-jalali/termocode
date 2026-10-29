package app

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/layout"
	"github.com/amin-jalali/termocode/internal/statusbar"
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

// statusLine is the status bar text of m.
func statusLine(m Model) string {
	bar := statusbar.New()
	bar.SetWidth(200)
	return ansi.Strip(bar.View(m.statusState()))
}

func TestOpenLayoutShowsItsNameInTheStatusBarForTwoSeconds(t *testing.T) {
	withConfigDir(t)
	layoutProject(t)
	m := panelTestModel()
	if layoutHintFor != 2*time.Second {
		t.Fatalf("hint time = %v", layoutHintFor)
	}
	if strings.Contains(statusLine(m), "Layout:") {
		t.Fatal("hint before any layout")
	}
	cmd := m.openLayout(layout.Layout{Name: "debug", Sidebar: &layout.Sidebar{Open: true}})
	if cmd == nil || !strings.Contains(statusLine(m), "Layout: debug") {
		t.Fatalf("no hint: %q", statusLine(m))
	}
	// A second layout replaces the hint; the older timer must not clear it.
	oldGen := m.layoutHintGen
	m.openLayout(layout.Layout{Name: "review", Sidebar: &layout.Sidebar{Open: true}})
	m, _ = m.update(layoutHintClearMsg{gen: oldGen})
	if !strings.Contains(statusLine(m), "Layout: review") {
		t.Fatalf("old timer cleared the new hint: %q", statusLine(m))
	}
	m, _ = m.update(layoutHintClearMsg{gen: m.layoutHintGen})
	if strings.Contains(statusLine(m), "Layout:") {
		t.Fatalf("hint stays: %q", statusLine(m))
	}
}

func TestOpenLayoutWithoutANameShowsNoHint(t *testing.T) {
	withConfigDir(t)
	layoutProject(t)
	m := panelTestModel()
	m.openLayout(layout.Layout{Sidebar: &layout.Sidebar{Open: true}})
	if m.layoutHint != "" {
		t.Errorf("hint %q", m.layoutHint)
	}
}

func TestResetLayoutIsTheFirstStartLook(t *testing.T) {
	withConfigDir(t)
	layoutProject(t)
	t.Setenv("TERMOCODE_HEADLESS", "1")
	m := panelTestModel()
	m.activity.SetActive(activity.ViewGit)
	m.explorerWidth, m.terminalRows = 50, 20
	m.actionsOpen, m.actionsPinned, m.actionsWidth, m.actionsWidthSet = true, true, 70, true
	m.zenMode = true

	m.dispatchPaletteAction("layout-reset")
	if m.zenMode || !m.showExp || m.activity.Active() != activity.ViewFiles {
		t.Errorf("zen=%v show=%v view=%v", m.zenMode, m.showExp, m.activity.Active())
	}
	if m.explorerWidth != defaultExplorerWidth || m.terminalRows != terminalRowsDefault {
		t.Errorf("sizes: sidebar %d, rows %d", m.explorerWidth, m.terminalRows)
	}
	if m.actionsOpen || m.actionsWidthSet {
		t.Errorf("chat open=%v widthSet=%v", m.actionsOpen, m.actionsWidthSet)
	}
	s := loadSession() // saved: the next start keeps the reset
	if s.ExplorerWidth != defaultExplorerWidth || s.ActionsWidth != 0 || s.ActionsOpen == nil || *s.ActionsOpen {
		t.Errorf("session = %+v", s)
	}
	if !strings.Contains(statusLine(m), "Layout: default") {
		t.Errorf("status bar: %q", statusLine(m))
	}
}
