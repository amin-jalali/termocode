package tabbar

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Spec (UI review #8): only the active tab shows ×; inactive tabs show
// their icon and name (a modified one keeps its ●). The close slot stays
// where it was, so clicking it still closes an inactive tab.
func TestCloseGlyphOnlyOnActiveTab(t *testing.T) {
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	defer func() { theme.CurrentIconMode = old }()
	m := New()
	m.SetBuffers([]nvim.BufferInfo{
		{ID: 1, Path: "/tmp/foo.go"},
		{ID: 2, Path: "/tmp/bar.go"},
		{ID: 3, Path: "/tmp/baz.go", Modified: true},
	}, 2)
	m.SetWidth(120)
	m.rebuildRegions()
	plain := ansi.Strip(m.View())
	if n := strings.Count(plain, "×"); n != 1 {
		t.Errorf("× shown %d times, want 1 (active tab only): %q", n, plain)
	}
	if !strings.Contains(plain, "●") {
		t.Error("modified tab lost its dot")
	}
	// The inactive foo.go tab's close slot still closes it.
	_, cmd := m.HandleMouse(m.regions[0].closeX, 0, 0, tea.MouseLeft)
	if cmd == nil {
		t.Fatal("click on the inactive tab's close slot did nothing")
	}
	if msg, ok := cmd().(CloseMsg); !ok || msg.ID != 1 {
		t.Fatalf("got %#v, want CloseMsg{ID: 1}", cmd())
	}
}
