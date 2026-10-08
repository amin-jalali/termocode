package activity

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestSetExtraItems(t *testing.T) {
	t.Cleanup(func() { SetExtraItems(nil) })
	SetExtraItems([]ExtraItem{{Label: "TODOs", Glyph: "✓"}, {Label: "rest", Glyph: "too wide"}})

	m := New()
	m.SetHeight(40)
	// First extension item sits right after the built-in top items.
	y := len(topItems) * itemStride
	v, ok := m.itemAt(y)
	if !ok || v != ViewExtBase {
		t.Fatalf("itemAt(%d) = %v, %v; want ViewExtBase", y, v, ok)
	}
	if i, ok := ExtIndex(ViewExtBase + 1); !ok || i != 1 {
		t.Errorf("ExtIndex(+1) = %d, %v", i, ok)
	}
	if _, ok := ExtIndex(ViewExtBase + 2); ok {
		t.Error("ExtIndex past the end should fail")
	}
	if _, ok := ExtIndex(ViewGit); ok {
		t.Error("built-in view is not an extension view")
	}
	// A click on it switches to it.
	_, cmd := m.HandleMouse(1, y, tea.MouseLeft)
	if cmd == nil {
		t.Fatal("click on extension item gave no command")
	}
	if sw, ok := cmd().(SwitchMsg); !ok || sw.View != ViewExtBase {
		t.Errorf("click msg = %#v", cmd())
	}
	// The bad glyph falls back to the label's first letter; rows stay
	// exactly Width wide.
	if g := extraItems[1].icon.ASCII; g != "R" {
		t.Errorf("fallback glyph = %q, want R", g)
	}
	for i, row := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(row); w != Width {
			t.Fatalf("row %d width %d, want %d", i, w, Width)
		}
	}

	SetExtraItems(nil)
	if _, ok := m.itemAt(y); ok {
		t.Error("item still present after SetExtraItems(nil)")
	}
}
