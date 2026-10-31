package findbar

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func send(m Model, keys ...tea.KeyMsg) Model {
	for _, k := range keys {
		m, _ = m.Update(k)
	}
	return m
}

func typeIn(m Model, s string) Model {
	return send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

// replaceField is a bar with "find" in Find and the focus in Replace.
func replaceField() Model {
	m := New()
	m.SetWidth(80)
	m.SetQuery("find")
	m.EnableReplace(true)
	return m
}

func TestCaretKeysInTheFindField(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m = typeIn(m, "abcd")
	m = send(m, key(tea.KeyLeft), key(tea.KeyLeft))
	if m.cursorPos != 2 {
		t.Fatalf("Left×2: caret %d, want 2", m.cursorPos)
	}
	m = typeIn(m, "X")
	if m.Query() != "abXcd" {
		t.Errorf("typing mid-text = %q", m.Query())
	}
	m = send(m, key(tea.KeyHome), key(tea.KeyLeft))
	if m.cursorPos != 0 {
		t.Errorf("Left at the start: caret %d", m.cursorPos)
	}
	m = send(m, key(tea.KeyEnd), key(tea.KeyRight))
	if m.cursorPos != 5 {
		t.Errorf("Right at the end: caret %d, want 5", m.cursorPos)
	}
	m = send(m, key(tea.KeyCtrlA))
	if m.cursorPos != 0 {
		t.Errorf("Ctrl+A: caret %d", m.cursorPos)
	}
	m = send(m, key(tea.KeyCtrlE))
	if m.cursorPos != 5 {
		t.Errorf("Ctrl+E: caret %d", m.cursorPos)
	}
	// Delete at the end does nothing; in the middle it removes and searches.
	m2, cmd := m.Update(key(tea.KeyDelete))
	if cmd != nil || m2.Query() != "abXcd" {
		t.Errorf("Delete at the end: %q, cmd %v", m2.Query(), cmd != nil)
	}
	m = send(m, key(tea.KeyHome))
	m, cmd = m.Update(key(tea.KeyDelete))
	if m.Query() != "bXcd" || cmd == nil {
		t.Fatalf("Delete at the start: %q, cmd %v", m.Query(), cmd != nil)
	}
	if s, ok := cmd().(SearchMsg); !ok || s.Query != "bXcd" {
		t.Errorf("Delete sent %#v", cmd())
	}
}

func TestUpDownFindMatchesInTheFindField(t *testing.T) {
	m := New()
	_, cmd := m.Update(key(tea.KeyUp))
	if _, ok := cmd().(FindPrevMsg); !ok {
		t.Errorf("Up sent %T", cmd())
	}
	_, cmd = m.Update(key(tea.KeyDown))
	if _, ok := cmd().(FindNextMsg); !ok {
		t.Errorf("Down sent %T", cmd())
	}
}

func TestEditingTheReplaceField(t *testing.T) {
	m := replaceField()
	m = typeIn(m, "new")
	if m.ReplaceValue() != "new" || m.Query() != "find" {
		t.Fatalf("replace %q, find %q", m.ReplaceValue(), m.Query())
	}
	// Edits in Replace never start a search.
	for _, k := range []tea.KeyMsg{key(tea.KeySpace), key(tea.KeyBackspace), key(tea.KeyLeft), key(tea.KeyDelete)} {
		var cmd tea.Cmd
		m, cmd = m.Update(k)
		if cmd != nil {
			t.Errorf("%v in Replace sent %T", k, cmd())
		}
	}
	if m.ReplaceValue() != "ne" {
		t.Errorf("after space, backspace, left, delete: %q, want %q", m.ReplaceValue(), "ne")
	}
	m = send(m, key(tea.KeyHome))
	if m.replaceCursorPos != 0 || m.cursorPos != 4 {
		t.Errorf("Home in Replace moved the wrong caret: replace %d find %d", m.replaceCursorPos, m.cursorPos)
	}
	m = send(m, key(tea.KeyRight), key(tea.KeyEnd))
	if m.replaceCursorPos != 2 {
		t.Errorf("End in Replace: caret %d", m.replaceCursorPos)
	}
	if !strings.Contains(ansi.Strip(m.View()), "ne") {
		t.Error("replace text not shown")
	}
}

func TestArrowsAndShiftTabLeaveTheReplaceField(t *testing.T) {
	for _, k := range []tea.KeyType{tea.KeyUp, tea.KeyDown, tea.KeyShiftTab} {
		m, cmd := replaceField().Update(key(k))
		if cmd != nil {
			t.Errorf("%v in Replace sent %T, want only a focus move", k, cmd())
		}
		if m.focus != fieldFind {
			t.Errorf("%v in Replace: focus stays in Replace", k)
		}
	}
}

func TestTabDoesNothingWhenCollapsed(t *testing.T) {
	m := New()
	m, cmd := m.Update(key(tea.KeyTab))
	if cmd != nil || m.focus != fieldFind {
		t.Errorf("Tab without Replace: focus %v, cmd %v", m.focus, cmd != nil)
	}
}

func TestBlinkTogglesTheCaretAndTypingShowsIt(t *testing.T) {
	m := New()
	m.SetWidth(80)
	if m.Init() == nil {
		t.Fatal("Init does not start the blink")
	}
	m, cmd := m.Update(blinkMsg{})
	if m.cursorVisible || cmd == nil {
		t.Fatalf("blink: visible %v, next tick %v", m.cursorVisible, cmd != nil)
	}
	if strings.Contains(m.View(), "▎") {
		t.Error("hidden caret still drawn")
	}
	m, _ = m.Update(tea.MouseMsg{})
	if !m.cursorVisible {
		t.Error("any event should show the caret again")
	}
	if !strings.Contains(m.View(), "▎") {
		t.Error("caret not drawn")
	}
}

func TestLongQueryKeepsTheCaretInView(t *testing.T) {
	m := New()
	m.SetWidth(80)
	long := strings.Repeat("abcdefghij", 10) + "END"
	m = typeIn(m, long)
	row := ansi.Strip(strings.Split(m.View(), "\n")[1])
	if !strings.Contains(row, "END▎") {
		t.Errorf("caret at the end not in view: %q", row)
	}
	m = send(m, key(tea.KeyHome))
	row = ansi.Strip(strings.Split(m.View(), "\n")[1])
	if !strings.Contains(row, "▎abcdefghij") || strings.Contains(row, "END") {
		t.Errorf("after Home: %q", row)
	}
	// Wide (CJK) and RTL text keeps the row exactly PanelWidth.
	for _, q := range []string{strings.Repeat("日本語", 20), strings.Repeat("سلام ", 20)} {
		w := New()
		w.SetWidth(80)
		w = typeIn(w, q)
		for i, l := range strings.Split(w.View(), "\n") {
			if got := ansi.StringWidth(l); got != w.PanelWidth() {
				t.Errorf("query %q: row %d is %d cells, want %d", q[:6], i, got, w.PanelWidth())
			}
		}
	}
}

func TestVisibleWindowEdges(t *testing.T) {
	r := []rune("ab日c")
	if s, e := visibleWindow(r, 2, 0); s != 2 || e != 2 {
		t.Errorf("width 0: %d..%d", s, e)
	}
	if s, e := visibleWindow(r, 4, 3); s != 2 || e != 4 {
		t.Errorf("caret at end, width 3: %d..%d, want 2..4", s, e)
	}
	if s, e := visibleWindow(r, 0, 3); s != 0 || e != 2 {
		t.Errorf("caret at start, width 3: %d..%d, want 0..2", s, e)
	}
	zw := []rune("a​b") // zero-width space counts as one cell
	if s, e := visibleWindow(zw, 3, 2); s != 1 || e != 3 {
		t.Errorf("zero-width rune: %d..%d, want 1..3", s, e)
	}
	if s, e := visibleWindow(zw, 0, 2); s != 0 || e != 2 {
		t.Errorf("zero-width rune forward: %d..%d, want 0..2", s, e)
	}
	if got := insertAt("abc", -3, "X"); got != "Xabc" {
		t.Errorf("insertAt(-3) = %q", got)
	}
	if got := insertAt("abc", 99, "X"); got != "abcX" {
		t.Errorf("insertAt(99) = %q", got)
	}
}

func TestMatchCountSlot(t *testing.T) {
	for _, c := range []struct{ count, shows string }{
		{"3/12", "3/12"},
		{"123/4567", "123/4"}, // cut to the slot
		{"0/0", "0/0"},
	} {
		m := New()
		m.SetWidth(80)
		m.SetMatchCount(c.count)
		got := m.renderCount()
		if w := ansi.StringWidth(got); w != fbCountSlotW {
			t.Errorf("%q: slot is %d cells", c.count, w)
		}
		if !strings.Contains(ansi.Strip(got), c.shows) {
			t.Errorf("%q: slot shows %q, want %q", c.count, ansi.Strip(got), c.shows)
		}
	}
	m := New()
	m.SetMatchCount("0")
	if m.renderCount() == func() string { n := New(); n.SetMatchCount("1"); return n.renderCount() }() {
		t.Error("no matches looks like a match")
	}
}

func TestPanelWidthLimits(t *testing.T) {
	for avail, want := range map[int]int{0: 64, -5: 64, 20: 20, 37: 36, 50: 48, 200: 64} {
		if got := panelWidth(avail); got != want {
			t.Errorf("panelWidth(%d) = %d, want %d", avail, got, want)
		}
	}
	m := New()
	m.SetSize(80, 999)
	if m.PanelWidth() != 64 {
		t.Errorf("SetSize: PanelWidth %d", m.PanelWidth())
	}
}

func TestNoButtonsOnATinyPanel(t *testing.T) {
	m := New()
	m.SetWidth(10)
	m.EnableReplace(true)
	if m.View() != "" {
		t.Error("a 10-cell panel was drawn")
	}
	for _, b := range m.Buttons() {
		t.Errorf("button %s on a 10-cell panel", b.ID)
	}
	if got, _ := m.buttonColumnsReplace(); got != -1 {
		t.Errorf("replace icon column %d on a tiny panel", got)
	}
}

func TestMouseOnTheInputsMovesFocus(t *testing.T) {
	m := replaceField()
	m.SetAnchor(10, 4)
	// Click the find input (row 1), then the replace input (row 3 on screen: by+2).
	m, cmd := m.HandleMouse(10+8, 4+1, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd != nil || m.focus != fieldFind {
		t.Fatalf("click on Find: focus %v, cmd %v", m.focus, cmd != nil)
	}
	m, _ = m.HandleMouse(10+8, 4+3, tea.MouseActionPress, tea.MouseButtonLeft)
	if m.focus != fieldReplace {
		t.Errorf("click on Replace row: focus %v", m.focus)
	}
	for _, b := range m.Buttons() {
		if b.ID == "menu" {
			t.Fatal("find bar exposes an unimplemented menu")
		}
	}
	// Collapsing with the chevron puts the focus back in Find.
	chev := m.Buttons()[0]
	m, _ = m.HandleMouse(chev.X, chev.Y, tea.MouseActionPress, tea.MouseButtonLeft)
	if m.ReplaceMode() || m.focus != fieldFind {
		t.Errorf("chevron collapse: replace %v focus %v", m.ReplaceMode(), m.focus)
	}
	var none Model
	if _, cmd := none.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Error("a zero Model handled a click")
	}
}

func TestPasteStripsEscapesInBothFields(t *testing.T) {
	for _, replace := range []bool{false, true} {
		m := New()
		m.SetQuery("ab")
		m.EnableReplace(replace)
		if replace {
			m.replace = "ab"
			m.replaceCursorPos = 1
		} else {
			m.cursorPos = 1
		}
		m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("\x1b[31mé\x1b[0m\x1b]0;title\a\x00\n")})
		if got := string(m.activeRunes()); got != "aéb" {
			t.Fatalf("replace %v: %q", replace, got)
		}
		if m.activeCursor() != 2 {
			t.Fatalf("cursor = %d", m.activeCursor())
		}
		if !replace && (cmd == nil || cmd().(SearchMsg).Query != "aéb") {
			t.Fatal("search did not receive clean text")
		}
		if replace && cmd != nil {
			t.Fatal("replacement triggered search")
		}
	}
}
