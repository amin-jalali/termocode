package confirm

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// Four buttons (the AI card for services) fit on one row: the card
// widens to its button row, still inside the screen.
func TestBoxWidensForButtons(t *testing.T) {
	btns := []Button{{ID: "allow", Title: "Allow"}, {ID: "always", Title: "Always allow services"},
		{ID: "project", Title: "Always in project"}, {ID: "deny", Title: "Deny"}}
	for _, w := range []int{80, 160} {
		m := New("AI wants to: services_restart", "Restart service api", btns)
		m.SetSize(w, 30)
		for i, line := range strings.Split(m.Box(), "\n") {
			if lw := lipgloss.Width(line); lw > w-4 {
				t.Errorf("w=%d line %d is %d wide", w, i, lw)
			}
		}
		_, rects := m.computeRects()
		box, _ := m.computeRects()
		last := rects[len(rects)-1]
		if last.x+last.w > box.x+box.w-1 || last.y != rects[0].y {
			t.Errorf("w=%d: buttons leave the card or wrap: %+v in %+v", w, rects, box)
		}
	}
}

// A wide card keeps a long SQL line whole when the window has room, and
// wraps it to the window when it does not.
func TestWideTextKeepsLongLinesWhole(t *testing.T) {
	line := "UPDATE `app`.`users` SET `email` = 'd@example.com' WHERE `id` = 2 AND `email` = 'x';"
	m := New("Submit", line+"\n\nnote", []Button{{ID: "ok", Title: "OK"}}).WithScrollText().WithWideText()
	m.SetSize(150, 42)
	if !strings.Contains(m.View(), line) {
		t.Fatalf("line was cut:\n%s", m.View())
	}
	m.SetSize(80, 24)
	for _, l := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(l); w > 80 {
			t.Fatalf("line wider than the window (%d):\n%s", w, m.View())
		}
	}
	plain := New("Submit", line, []Button{{ID: "ok", Title: "OK"}}).WithScrollText()
	plain.SetSize(150, 42)
	if strings.Contains(plain.View(), line) {
		t.Fatal("a card without WithWideText grew")
	}
}

func TestWideTextBreaksBetweenWords(t *testing.T) {
	m := New("Submit", "aaaa bbbb cccc dddd eeee ffff gggg hhhh iiii jjjj kkkk llll mmmm nnnn oooo pppp qqqq rrrr", []Button{{ID: "ok", Title: "OK"}}).WithScrollText().WithWideText()
	m.SetSize(50, 24)
	for _, l := range m.fullTextLines() {
		for _, w := range strings.Fields(l) {
			if len(w) != 4 {
				t.Fatalf("a word was cut: %q in %q", w, m.fullTextLines())
			}
		}
	}
}

// CT-6: a card with one button is an info card: its footer says
// "Enter OK · Esc close", not "move / select / cancel". Cards with
// several buttons keep the full footer.
func TestFooterOfOneButtonCard(t *testing.T) {
	footer := func(m Model) string {
		m.SetSize(100, 30)
		lines := strings.Split(ansi.Strip(m.Box()), "\n")
		return lines[len(lines)-2]
	}
	one := footer(New("Image is in use", "It stays.", []Button{{ID: "cancel", Title: "Close"}}))
	if !strings.Contains(one, "Enter") || !strings.Contains(one, "OK") || !strings.Contains(one, "close") ||
		strings.Contains(one, "move") || strings.Contains(one, "select") || strings.Contains(one, "cancel") {
		t.Errorf("one-button footer: %q", one)
	}
	two := footer(New("Remove?", "Sure?", []Button{{ID: "cancel", Title: "Cancel"}, {ID: "ok", Title: "Remove"}}))
	if !strings.Contains(two, "move") || !strings.Contains(two, "select") || !strings.Contains(two, "cancel") {
		t.Errorf("two-button footer: %q", two)
	}
}

// The footer hints of a one-button card are clickable too.
func TestFooterHintsOfOneButtonCardClick(t *testing.T) {
	m := New("In use", "It stays.", []Button{{ID: "cancel", Title: "Close"}})
	m.SetSize(100, 30)
	bx, by, bw, bh := m.Bounds()
	_ = bw
	y := by + bh - 2
	x := bx + 1 + 2 + 1 // inside the Enter key
	_, cmd, _ := m.HandleMouse(x, y, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd == nil {
		t.Fatal("no command from clicking Enter")
	}
	if got, ok := cmd().(SelectMsg); !ok || got.ID != "cancel" {
		t.Errorf("got %#v", cmd())
	}
}

// X-1: a key card shows the keys in one column and what they do in
// another, wraps long text under itself, scrolls, and Esc closes it.
func TestKeyCardTable(t *testing.T) {
	rows := []KeyRow{
		{What: "Move"},
		{Key: "j / k", What: "move"},
		{Key: "Enter", What: "connect, expand or fold the row under the cursor and then go on with a very long text that must wrap"},
		{What: "Tables"},
		{Key: "D", What: "show the DDL"},
	}
	m := New("Keys", "", []Button{{ID: "guide", Title: "Open guide", Style: StylePrimary}, {ID: "close", Title: "Close"}}).WithKeys(rows)
	m.SetSize(70, 30)
	lines := strings.Split(ansi.Strip(m.Box()), "\n")
	keyCol, whatCol, moveCol := -1, -1, -1
	for _, l := range lines {
		if strings.Contains(l, "Enter  connect") {
			keyCol = strings.Index(l, "Enter")
			whatCol = strings.Index(l, "connect")
		}
		if i := strings.Index(l, "j / k"); i >= 0 {
			moveCol = i + len("j / k") + strings.Index(l[i+len("j / k"):], "move")
		}
	}
	if keyCol < 0 || whatCol <= keyCol {
		t.Fatalf("no two columns:\n%s", strings.Join(lines, "\n"))
	}
	if moveCol != whatCol {
		t.Errorf("the text column is not aligned: %d vs %d\n%s", moveCol, whatCol, strings.Join(lines, "\n"))
	}
	text := strings.Join(lines, "\n")
	for _, want := range []string{"Move", "Tables", "j / k", "show the DDL", "Open guide", "Close", "very long text"} {
		if !strings.Contains(text, want) {
			t.Errorf("card lacks %q:\n%s", want, text)
		}
	}
	for _, l := range lines {
		if strings.ContainsAny(l, "\x01\x02\x03") {
			t.Errorf("a mark leaked into the screen: %q", l)
		}
		if lipgloss.Width(l) > 70 {
			t.Errorf("line wider than the window: %d %q", lipgloss.Width(l), l)
		}
	}
	// A short window scrolls the table with Down instead of cutting it.
	m.SetSize(70, 16)
	if got := ansi.Strip(m.Box()); strings.Contains(got, "show the DDL") || !strings.Contains(got, "scroll") {
		t.Errorf("a short window must scroll:\n%s", got)
	}
	for i := 0; i < 6; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if got := ansi.Strip(m.Box()); !strings.Contains(got, "show the DDL") {
		t.Errorf("Down did not scroll to the end:\n%s", got)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if _, ok := cmd().(CloseMsg); !ok {
		t.Error("Esc does not close the key card")
	}
}
