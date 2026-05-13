package confirm

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleButtons() []Button {
	return []Button{
		{ID: "ok", Title: "OK", Style: StylePrimary},
		{ID: "cancel", Title: "Cancel", Style: StyleDefault},
		{ID: "delete", Title: "Delete", Style: StyleDestructive},
	}
}

func TestNewStoresFields(t *testing.T) {
	bs := sampleButtons()
	m := New("Title", "Message", bs)
	if m.title != "Title" || m.message != "Message" {
		t.Fatalf("New did not store title/message: %+v", m)
	}
	if len(m.buttons) != 3 {
		t.Fatalf("buttons not stored")
	}
	if m.cursor != 0 {
		t.Fatalf("cursor must default to 0")
	}
}

func TestUpdateCursorMovesAndClamps(t *testing.T) {
	m := New("", "", sampleButtons())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if m.cursor != 1 {
		t.Fatalf("Right: cursor want 1, got %d", m.cursor)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight}) // would be 3, clamped to 2
	if m.cursor != 2 {
		t.Fatalf("Right past end: cursor want 2, got %d", m.cursor)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if m.cursor != 1 {
		t.Fatalf("Left: cursor want 1, got %d", m.cursor)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if m.cursor != 0 {
		t.Fatalf("Left below 0: cursor want 0, got %d", m.cursor)
	}
}

func TestUpdateTabAdvances(t *testing.T) {
	m := New("", "", sampleButtons())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.cursor != 1 {
		t.Fatalf("Tab: cursor want 1, got %d", m.cursor)
	}
}

func TestUpdateEnterEmitsSelect(t *testing.T) {
	m := New("", "", sampleButtons())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter expected to emit cmd")
	}
	sel, ok := cmd().(SelectMsg)
	if !ok {
		t.Fatalf("want SelectMsg, got %T", cmd())
	}
	if sel.ID != "cancel" {
		t.Fatalf("selected ID want 'cancel', got %q", sel.ID)
	}
}

func TestUpdateEscEmitsClose(t *testing.T) {
	m := New("", "", sampleButtons())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc must emit close")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatalf("want CloseMsg, got %T", cmd())
	}
}

func TestUpdateNonKeyIsNoop(t *testing.T) {
	m := New("", "", sampleButtons())
	_, cmd := m.Update(struct{}{})
	if cmd != nil {
		t.Fatal("non-key msg must not emit cmd")
	}
}

func TestViewEmptyAtZeroSize(t *testing.T) {
	m := New("t", "m", sampleButtons())
	if v := m.View(); v != "" {
		t.Fatalf("zero size view want empty, got %q", v)
	}
}

func TestViewIncludesTitleMessageButtons(t *testing.T) {
	m := New("Quit?", "Discard changes and quit?", sampleButtons())
	m.SetSize(80, 20)
	out := m.View()
	for _, want := range []string{"Quit?", "Discard", "OK", "Cancel", "Delete"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q: %s", want, out)
		}
	}
}

func TestWrapBasic(t *testing.T) {
	got := wrap("the quick brown fox jumps", 10)
	if len(got) < 2 {
		t.Fatalf("wrap should produce >=2 lines for 25-wide text in 10 cols, got %v", got)
	}
	for _, line := range got {
		if len(line) > 10 {
			t.Fatalf("wrapped line %q exceeds width 10", line)
		}
	}
}

func TestWrapZeroWidthReturnsInput(t *testing.T) {
	got := wrap("foo", 0)
	if len(got) != 1 || got[0] != "foo" {
		t.Fatalf("wrap(0) want passthrough, got %v", got)
	}
}

// TestHandleMouseClicksButton covers the new mouse hit-test path: a
// left-press on a button rect emits SelectMsg with that button's ID
// and parks the keyboard cursor on it; clicks elsewhere do not emit.
func TestHandleMouseClicksButton(t *testing.T) {
	m := New("Delete?", "Are you sure?", []Button{
		{ID: "delete", Title: "Delete", Style: StyleDestructive},
		{ID: "cancel", Title: "Cancel"},
	})
	m.SetSize(80, 24)

	// Recompute to learn the rects produced for this geometry; the
	// production path runs the same code.
	_, rects := m.computeRects()
	if len(rects) != 2 {
		t.Fatalf("expected 2 button rects, got %d", len(rects))
	}

	// Click the first button (Delete).
	m2, cmd, handled := m.HandleMouse(rects[0].x+1, rects[0].y, tea.MouseActionPress, tea.MouseButtonLeft)
	if !handled {
		t.Fatal("click on Delete button: handled=false")
	}
	if cmd == nil {
		t.Fatal("click on Delete button: nil cmd")
	}
	sel, ok := cmd().(SelectMsg)
	if !ok {
		t.Fatalf("want SelectMsg, got %T", cmd())
	}
	if sel.ID != "delete" {
		t.Fatalf("want ID=delete, got %q", sel.ID)
	}
	if m2.cursor != 0 {
		t.Fatalf("cursor want 0 after Delete click, got %d", m2.cursor)
	}

	// Click the second button (Cancel).
	_, cmd, handled = m.HandleMouse(rects[1].x+1, rects[1].y, tea.MouseActionPress, tea.MouseButtonLeft)
	if !handled || cmd == nil {
		t.Fatal("click on Cancel button: not handled")
	}
	sel = cmd().(SelectMsg)
	if sel.ID != "cancel" {
		t.Fatalf("want ID=cancel, got %q", sel.ID)
	}
}

// TestHandleMouseInsideButOffButton: a click inside the panel but not
// on a button is swallowed (handled=true) but emits no command —
// destructive prompts must not be dismissed by stray clicks.
func TestHandleMouseInsideButOffButton(t *testing.T) {
	m := New("Delete?", "msg", []Button{
		{ID: "ok", Title: "OK"},
		{ID: "cancel", Title: "Cancel"},
	})
	m.SetSize(80, 24)
	box, _ := m.computeRects()

	// Top-left corner of the panel — inside the rect, nowhere near a
	// button row.
	_, cmd, handled := m.HandleMouse(box.x+2, box.y+1, tea.MouseActionPress, tea.MouseButtonLeft)
	if !handled {
		t.Fatal("click inside panel must be reported as handled")
	}
	if cmd != nil {
		t.Fatal("click inside panel but not on a button must NOT emit a command")
	}
}

// TestHandleMouseOutsidePanel: a click outside the panel is reported
// as not-handled and emits nothing — the parent uses this to swallow
// the click rather than fall through to the editor.
func TestHandleMouseOutsidePanel(t *testing.T) {
	m := New("Delete?", "msg", []Button{
		{ID: "ok", Title: "OK"},
	})
	m.SetSize(80, 24)

	_, cmd, handled := m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonLeft)
	if handled {
		t.Fatal("click at (0,0) must be reported as not-inside")
	}
	if cmd != nil {
		t.Fatal("click outside panel must NOT emit a command")
	}
}

// TestBoundsTracksGeometry: Bounds() must return a non-zero rect once
// SetSize is called, and the returned rect must contain its own
// reported (x,y) corner.
func TestBoundsTracksGeometry(t *testing.T) {
	m := New("Delete?", "msg", []Button{{ID: "ok", Title: "OK"}})
	if x, y, w, h := m.Bounds(); x != 0 || y != 0 || w != 0 || h != 0 {
		t.Fatalf("zero-size Bounds want (0,0,0,0), got (%d,%d,%d,%d)", x, y, w, h)
	}
	m.SetSize(80, 24)
	x, y, w, h := m.Bounds()
	if w <= 0 || h <= 0 {
		t.Fatalf("after SetSize, Bounds must be non-zero, got w=%d h=%d", w, h)
	}
	if x < 0 || y < 0 {
		t.Fatalf("bounds origin must be non-negative, got (%d,%d)", x, y)
	}
	if x+w > 80 || y+h > 24 {
		t.Fatalf("bounds must fit screen: rect=(%d,%d,%d,%d) screen=80x24", x, y, w, h)
	}
}
