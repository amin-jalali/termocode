package menu

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func sampleItems() []Item {
	return []Item{
		{ID: "open", Title: "Open"},
		{ID: "save", Title: "Save", Hint: "Ctrl+S"},
		{Sep: true},
		{ID: "quit", Title: "Quit"},
	}
}

func TestNewSkipsLeadingSeparators(t *testing.T) {
	items := []Item{{Sep: true}, {ID: "a", Title: "A"}}
	m := New(items, 0, 0)
	if m.cursor != 1 {
		t.Fatalf("cursor must skip leading separator: got %d", m.cursor)
	}
}

func TestUpdateDownSkipsSeparators(t *testing.T) {
	m := New(sampleItems(), 0, 0)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // should jump past sep
	if m.cursor != 3 {
		t.Fatalf("Down past sep: want cursor 3, got %d", m.cursor)
	}
}

func TestUpdateUpSkipsSeparators(t *testing.T) {
	m := New(sampleItems(), 0, 0)
	m.cursor = 3
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 1 {
		t.Fatalf("Up past sep: want cursor 1, got %d", m.cursor)
	}
}

func TestUpdateEnterEmitsSelect(t *testing.T) {
	m := New(sampleItems(), 0, 0)
	m.cursor = 1 // save
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter expected to emit cmd")
	}
	sel, ok := cmd().(SelectMsg)
	if !ok {
		t.Fatalf("want SelectMsg, got %T", cmd())
	}
	if sel.ID != "save" {
		t.Fatalf("selected ID want 'save', got %q", sel.ID)
	}
}

func TestUpdateEnterOnSeparatorNoop(t *testing.T) {
	m := New(sampleItems(), 0, 0)
	m.cursor = 2 // separator
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		// must not emit SelectMsg.
		if _, ok := cmd().(SelectMsg); ok {
			t.Fatal("Enter on separator must not select")
		}
	}
}

func TestUpdateEscEmitsClose(t *testing.T) {
	m := New(sampleItems(), 0, 0)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc must emit close")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatalf("want CloseMsg, got %T", cmd())
	}
}

func TestUpdateMouseInsideSelects(t *testing.T) {
	m := New(sampleItems(), 0, 0)
	m.SetScreenSize(80, 24)
	w, _ := m.size()
	x, y := m.clampedAnchor()
	// Click on first action row (Open). Row 0 is the top border, so
	// the first item lands at y+1.
	_, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x + 2, Y: y + 1})
	if cmd == nil {
		t.Fatal("inside click expected cmd")
	}
	sel, ok := cmd().(SelectMsg)
	if !ok {
		t.Fatalf("want SelectMsg, got %T", cmd())
	}
	if sel.ID != "open" {
		t.Fatalf("clicked open: want 'open', got %q", sel.ID)
	}
	_ = w
}

func TestUpdateMouseOnSeparatorIsNoop(t *testing.T) {
	m := New(sampleItems(), 0, 0)
	m.SetScreenSize(80, 24)
	x, y := m.clampedAnchor()
	// Layout (no header): border(0) + Open(1) + Save(2) + Sep(3) + Quit(4).
	// Click on the separator row (y+3).
	_, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x + 2, Y: y + 3})
	if cmd != nil {
		if _, ok := cmd().(SelectMsg); ok {
			t.Fatal("click on separator must not select")
		}
	}
}

func TestUpdateMouseOutsideClosesMenu(t *testing.T) {
	m := New(sampleItems(), 0, 0)
	m.SetScreenSize(80, 24)
	w, _ := m.size()
	x, y := m.clampedAnchor()
	_, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x + w + 5, Y: y})
	if cmd == nil {
		t.Fatal("outside click expected close cmd")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatalf("want CloseMsg, got %T", cmd())
	}
}

func TestClampedAnchorClampsToScreen(t *testing.T) {
	m := New(sampleItems(), 1000, 1000)
	m.SetScreenSize(80, 24)
	x, y := m.clampedAnchor()
	w, h := m.size()
	if x+w > 80 {
		t.Fatalf("clamped x+w must fit screen: x=%d w=%d", x, w)
	}
	if y+h > 24 {
		t.Fatalf("clamped y+h must fit screen: y=%d h=%d", y, h)
	}
}

// TestRenderWidthContract: every row of View() must be exactly innerW+2
// cells wide (the +2 accounts for the rounded border on each side). The
// host's glassyMenuOverlay relies on this — short rows leak black gaps.
func TestRenderWidthContract(t *testing.T) {
	items := []Item{
		{ID: "cut", Title: "Cut", Icon: "✂", Hint: "Ctrl+X"},
		{ID: "copy", Title: "Copy", Icon: "⎘", Hint: "Ctrl+C"},
		{Sep: true},
		{ID: "format", Title: "Format Document", Icon: "⚙", Hint: "Shift+Alt+F"},
		{ID: "def", Title: "Go to Definition", Icon: "▸", Hint: "F12 / Ctrl+Click"},
	}
	m := New(items, 0, 0)
	m.SetScreenSize(120, 40)
	_, _, want, _ := m.Bounds()
	for i, line := range strings.Split(m.View(), "\n") {
		if got := lipgloss.Width(line); got != want {
			t.Fatalf("row %d width=%d, want %d (line=%q)", i, got, want, line)
		}
	}
}

// TestRenderWidthContractNoIcons covers menus where no item has an icon
// (the icon column should collapse, but every row still must match width).
func TestRenderWidthContractNoIcons(t *testing.T) {
	items := []Item{
		{ID: "open", Title: "Open"},
		{ID: "save", Title: "Save", Hint: "Ctrl+S"},
		{Sep: true},
		{ID: "quit", Title: "Quit"},
	}
	m := New(items, 0, 0)
	m.SetScreenSize(80, 24)
	_, _, want, _ := m.Bounds()
	for i, line := range strings.Split(m.View(), "\n") {
		if got := lipgloss.Width(line); got != want {
			t.Fatalf("row %d width=%d, want %d", i, got, want)
		}
	}
}

func TestOverlayDrawsOnBase(t *testing.T) {
	m := New(sampleItems(), 0, 0)
	m.SetScreenSize(80, 24)
	base := strings.Repeat(strings.Repeat("·", 80)+"\n", 23) + strings.Repeat("·", 80)
	out := m.Overlay(base)
	if !strings.Contains(out, "Open") {
		t.Fatalf("overlay missing menu content")
	}
	// Output should preserve the same number of base lines.
	wantLines := strings.Count(base, "\n") + 1
	gotLines := strings.Count(out, "\n") + 1
	if gotLines != wantLines {
		t.Fatalf("overlay changed line count: want %d, got %d", wantLines, gotLines)
	}
}
