package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/tabbar"
)

// TestTabAccentFollowsKeys: the active tab is lit only while keys go to
// the editor; focus in the explorer, the terminal or the AI chat dims it.
func TestTabAccentFollowsKeys(t *testing.T) {
	tabs := tabbar.New()
	tabs.SetBuffers([]nvim.BufferInfo{{ID: 1, Path: "/tmp/a.go"}}, 1)
	tabs.SetWidth(60)
	m := Model{tabs: tabs, focus: FocusEditor}
	if !m.editorHasKeys() {
		t.Fatal("editor focus: want keys in the editor")
	}
	m.focus = FocusExplorer
	if m.editorHasKeys() {
		t.Error("explorer focus: editor still marked as focused")
	}
	m.focus, m.inTerminal = FocusEditor, true
	if m.editorHasKeys() {
		t.Error("terminal focus: editor still marked as focused")
	}
	m.inTerminal = false
	m.aiS().focused = true
	if m.actionsPanelVisible() && m.editorHasKeys() {
		t.Error("AI chat focus: editor still marked as focused")
	}
}

// TestAIPanelHasEdge: the AI panel starts with a thin edge line on every
// row, like the sidebar's right edge, so it reads as its own pane next to
// the editor. The edge only replaces the panel's blank first column.
func TestAIPanelHasEdge(t *testing.T) {
	m := Model{w: 150, h: 30}
	out := m.renderActionsPanel(50, 30)
	for i, row := range strings.Split(out, "\n") {
		if got := ansi.Strip(ansi.Cut(row, 0, 1)); got != "▏" {
			t.Fatalf("row %d: first cell %q, want the ▏ edge", i, got)
		}
		if w := ansi.StringWidth(row); w != 50 {
			t.Fatalf("row %d: width %d, want 50", i, w)
		}
	}
}
