package picker

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/anim"
	"github.com/amin-jalali/termocode/internal/theme"
)

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

// manyItems is n plain rows "item 0" … "item n-1".
func manyItems(n int) []Item {
	out := make([]Item, n)
	for i := range out {
		out[i] = Item{ID: fmt.Sprint(i), Title: fmt.Sprintf("item %d", i)}
	}
	return out
}

// grouped is two groups with a header each.
func grouped() []Item {
	return []Item{
		{Title: "Recent", Header: true, Group: "r"},
		{ID: "a", Title: "alpha", Group: "r"},
		{ID: "b", Title: "beta", Group: "r"},
		{Title: "Other", Header: true, Group: "o"},
		{ID: "c", Title: "gamma", Group: "o"},
		{ID: "d", Title: "delta", Group: "o"},
	}
}

func currentID(t *testing.T, m Model) string {
	t.Helper()
	it, ok := m.Current()
	if !ok {
		return ""
	}
	return it.ID
}

func TestPageKeysMoveAWholeScreen(t *testing.T) {
	m := NewItems(" P ", manyItems(100))
	m.SetSize(100, 30)
	_, _, avail := m.layoutDims()
	m, _ = m.Update(key(tea.KeyPgDown))
	if m.cursor != avail {
		t.Fatalf("PgDn: cursor %d, want %d", m.cursor, avail)
	}
	for i := 0; i < 10; i++ {
		m, _ = m.Update(key(tea.KeyPgDown))
	}
	if m.cursor != 99 {
		t.Errorf("PgDn past the end: cursor %d, want the last row 99", m.cursor)
	}
	if !strings.Contains(ansi.Strip(m.Box()), "item 99") {
		t.Error("the last row is not on screen after paging down")
	}
	m, _ = m.Update(key(tea.KeyPgUp))
	if m.cursor != 99-avail {
		t.Errorf("PgUp: cursor %d, want %d", m.cursor, 99-avail)
	}
	for i := 0; i < 10; i++ {
		m, _ = m.Update(key(tea.KeyPgUp))
	}
	if m.cursor != 0 {
		t.Errorf("PgUp past the top: cursor %d, want 0", m.cursor)
	}
}

func TestCtrlPAndCtrlNMoveLikeArrows(t *testing.T) {
	m := NewItems(" P ", manyItems(3))
	m.SetSize(100, 30)
	m, _ = m.Update(key(tea.KeyCtrlP))
	if m.cursor != 0 {
		t.Errorf("Ctrl+P at the top: cursor %d, want 0", m.cursor)
	}
	m, _ = m.Update(key(tea.KeyCtrlN))
	m, _ = m.Update(key(tea.KeyCtrlN))
	m, _ = m.Update(key(tea.KeyCtrlN))
	if m.cursor != 2 {
		t.Errorf("Ctrl+N past the end: cursor %d, want 2", m.cursor)
	}
	m, _ = m.Update(key(tea.KeyCtrlP))
	if m.cursor != 1 {
		t.Errorf("Ctrl+P: cursor %d, want 1", m.cursor)
	}
}

func TestPageKeysOnAnEmptyListStayPut(t *testing.T) {
	m := NewItems(" P ", nil)
	m.SetSize(100, 30)
	for _, k := range []tea.KeyType{tea.KeyPgDown, tea.KeyPgUp, tea.KeyDown, tea.KeyUp} {
		m, _ = m.Update(key(k))
	}
	if m.cursor != 0 {
		t.Errorf("cursor %d on an empty list", m.cursor)
	}
	if _, cmd := m.Update(key(tea.KeyEnter)); cmd != nil {
		t.Error("Enter on an empty list did something")
	}
	if _, ok := m.Current(); ok {
		t.Error("Current() found a row in an empty list")
	}
}

func TestCtrlCClosesThePicker(t *testing.T) {
	m := NewItems(" P ", manyItems(2))
	_, cmd := m.Update(key(tea.KeyCtrlC))
	if cmd == nil {
		t.Fatal("Ctrl+C did nothing")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Errorf("Ctrl+C sent %T, want CloseMsg", cmd())
	}
}

func TestBackspaceAndSpaceEditTheSearch(t *testing.T) {
	m := NewItems(" P ", []Item{{ID: "1", Title: "open file"}, {ID: "2", Title: "سلام دنیا"}})
	m.SetSize(100, 30)
	m, _ = m.Update(key(tea.KeyBackspace)) // empty search: nothing to delete
	if m.input != "" || len(m.matches) != 2 {
		t.Fatalf("Backspace on empty search: input %q, %d rows", m.input, len(m.matches))
	}
	m = typeQuery(m, "سلا")
	m, _ = m.Update(key(tea.KeyBackspace))
	if m.input != "سل" {
		t.Errorf("Backspace removed bytes, not a letter: %q", m.input)
	}
	if currentID(t, m) != "2" {
		t.Errorf("RTL search picked %q, want 2", currentID(t, m))
	}
	m, _ = m.Update(key(tea.KeyBackspace))
	m, _ = m.Update(key(tea.KeyBackspace))
	m = typeQuery(m, "open")
	m, _ = m.Update(key(tea.KeySpace))
	m = typeQuery(m, "f")
	if m.input != "open f" {
		t.Errorf("input = %q, want %q", m.input, "open f")
	}
	if len(m.matches) != 1 || currentID(t, m) != "1" {
		t.Errorf("\"open f\" matched %d rows, current %q", len(m.matches), currentID(t, m))
	}
	if !strings.Contains(ansi.Strip(m.Box()), "open f") {
		t.Error("search text not shown")
	}
}

func TestAltBracketFromASplitMouseCodeIsDropped(t *testing.T) {
	m := NewItems(" P ", manyItems(2))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("["), Alt: true})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("<"), Alt: true})
	if m.input != "" {
		t.Errorf("Alt+[ from a split mouse code reached the search: %q", m.input)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true})
	if m.input != "x" {
		t.Errorf("Alt+x = %q, want x typed", m.input)
	}
}

func TestPastedEscapesNeverReachTheSearch(t *testing.T) {
	for in, want := range map[string]string{
		"a\x1b[31mb":     "ab",     // colour code
		"x\x1b[12":       "x",      // cut-off escape
		"p[<35;70;19Mq":  "pq",     // mouse code without ESC
		"[<12;3":         "[<12;3", // no final M: kept as typed
		"[<ab":           "[<ab",
		"tab\there\x7f!": "tabhere!",
	} {
		if got := sanitizeInput(in); got != want {
			t.Errorf("sanitizeInput(%q) = %q, want %q", in, got, want)
		}
	}
	m := NewItems(" P ", manyItems(2))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("it\x1b[2Jem")})
	if m.input != "item" {
		t.Errorf("pasted text with an escape = %q, want item", m.input)
	}
}

func TestRightArrowOnAnEmptyListDoesNothing(t *testing.T) {
	m := NewItems(" P ", []Item{{Title: "Only a header", Header: true}})
	m.SetRowAction("→", "details")
	if _, cmd := m.Update(key(tea.KeyRight)); cmd != nil {
		t.Errorf("Right on a header sent %#v", cmd())
	}
	m = NewItems(" P ", nil)
	m.SetRowAction("→", "details")
	if _, cmd := m.Update(key(tea.KeyRight)); cmd != nil {
		t.Error("Right on an empty list sent a message")
	}
}

func TestWheelScrollsThreeRowsAndSkipsHeaders(t *testing.T) {
	m := NewItems(" P ", manyItems(10))
	m.SetSize(100, 30)
	m, _ = m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelDown)
	if m.cursor != 3 {
		t.Errorf("wheel down: cursor %d, want 3", m.cursor)
	}
	for i := 0; i < 5; i++ {
		m, _ = m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelDown)
	}
	if m.cursor != 9 {
		t.Errorf("wheel down past the end: cursor %d, want 9", m.cursor)
	}
	m, _ = m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelUp)
	if m.cursor != 6 {
		t.Errorf("wheel up: cursor %d, want 6", m.cursor)
	}
	for i := 0; i < 5; i++ {
		m, _ = m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelUp)
	}
	if m.cursor != 0 {
		t.Errorf("wheel up past the top: cursor %d, want 0", m.cursor)
	}

	g := NewItems(" P ", grouped())
	g.SetSize(100, 30)
	g, _ = g.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelDown) // row 1 → 4 (gamma)
	g, _ = g.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelUp)   // 4 → 1 (alpha)
	if g.matchIsHeader(g.cursor) {
		t.Errorf("wheel left the cursor on a header (row %d)", g.cursor)
	}
	g.cursor = 3 // pretend: the "Other" header
	g, _ = g.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelDown)
	if g.matchIsHeader(g.cursor) || currentID(t, g) != "d" {
		t.Errorf("wheel down to the end: on %q", currentID(t, g))
	}
	g.cursor = 3
	g, _ = g.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelUp)
	if g.matchIsHeader(g.cursor) {
		t.Errorf("wheel up to the top header: cursor on a header (row %d)", g.cursor)
	}

	e := NewItems(" P ", nil)
	e.SetSize(100, 30)
	e, _ = e.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelDown)
	if e.cursor != 0 {
		t.Errorf("wheel on an empty list: cursor %d", e.cursor)
	}
}

func TestMouseOnRows(t *testing.T) {
	m := NewItems(" P ", grouped())
	m.SetSize(100, 30)
	rows := m.RowRects()
	if len(rows) != 6 {
		t.Fatalf("%d row rects, want 6", len(rows))
	}
	// Hover moves the highlight, but never onto a header.
	m, _ = m.HandleMouse(rows[4].X+5, rows[4].Y, tea.MouseActionMotion, tea.MouseButtonNone)
	if currentID(t, m) != "c" {
		t.Errorf("hover on gamma: current %q", currentID(t, m))
	}
	m, _ = m.HandleMouse(rows[3].X+5, rows[3].Y, tea.MouseActionMotion, tea.MouseButtonNone)
	if currentID(t, m) != "c" {
		t.Errorf("hover on a header moved the highlight to %q", currentID(t, m))
	}
	// A click on a header moves to the row below it and selects nothing.
	m, cmd := m.HandleMouse(rows[0].X+5, rows[0].Y, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd != nil || currentID(t, m) != "a" {
		t.Errorf("click on header: cmd %v, current %q", cmd != nil, currentID(t, m))
	}
	// A click on a row selects it.
	m, cmd = m.HandleMouse(rows[5].X+5, rows[5].Y, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd == nil {
		t.Fatal("click on delta did nothing")
	}
	if sel, ok := cmd().(SelectMsg); !ok || sel.ID != "d" {
		t.Errorf("click on delta sent %#v", cmd())
	}
	// Right click and release do nothing.
	if _, cmd := m.HandleMouse(rows[1].X+5, rows[1].Y, tea.MouseActionPress, tea.MouseButtonRight); cmd != nil {
		t.Error("right click did something")
	}
	if _, cmd := m.HandleMouse(rows[1].X+5, rows[1].Y, tea.MouseActionRelease, tea.MouseButtonLeft); cmd != nil {
		t.Error("button release did something")
	}
	// A click inside the box but not on a row or hint does nothing.
	bx, by, _, _ := m.boxRect()
	if _, cmd := m.HandleMouse(bx+3, by+1, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Errorf("click on the title sent %#v", cmd())
	}
	// A click outside the box closes.
	_, cmd = m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd == nil {
		t.Fatal("click outside did nothing")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Errorf("click outside sent %T", cmd())
	}
}

func TestMouseBeforeTheFirstSizeIsIgnored(t *testing.T) {
	m := NewItems(" P ", manyItems(3))
	if _, cmd := m.HandleMouse(1, 1, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Error("click before SetSize did something")
	}
	if m.RowRects() != nil {
		t.Error("row rects before SetSize")
	}
}

// The footer "→ details" hint is a button: a click reports the highlighted row.
func TestFooterActionHintIsClickable(t *testing.T) {
	m := NewItems(" P ", []Item{{ID: "a", Title: "First"}, {ID: "b", Title: "Second"}})
	m.SetSize(120, 40)
	m.SetRowAction("→", "details")
	bx, by, _, bh := m.boxRect()
	y := by + bh - 2
	line := ansi.Strip(strings.Split(m.Box(), "\n")[bh-2])
	i := strings.Index(line, "details")
	if i < 0 {
		t.Fatalf("no details hint in %q", line)
	}
	_, cmd := m.HandleMouse(bx+ansi.StringWidth(line[:i]), y, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd == nil {
		t.Fatal("click on the details hint did nothing")
	}
	if got, ok := cmd().(ActionMsg); !ok || got.ID != "a" {
		t.Errorf("details hint sent %#v", cmd())
	}
	// A click on the footer gap, between hints, does nothing.
	if _, cmd := m.HandleMouse(bx+ansi.StringWidth(line)-2, y, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Errorf("click on the footer gap sent %#v", cmd())
	}
}

func TestFooterEnterOnAHeaderOnlyListDoesNothing(t *testing.T) {
	m := NewItems(" P ", []Item{{Title: "Just a header", Header: true}})
	m.SetSize(120, 40)
	bx, by, _, bh := m.boxRect()
	line := ansi.Strip(strings.Split(m.Box(), "\n")[bh-2])
	i := strings.Index(line, "Enter")
	if _, cmd := m.HandleMouse(bx+ansi.StringWidth(line[:i]), by+bh-2, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Errorf("Enter hint on a header-only list sent %#v", cmd())
	}
	e := NewItems(" P ", nil)
	e.SetSize(120, 40)
	bx, by, _, bh = e.boxRect()
	line = ansi.Strip(strings.Split(e.Box(), "\n")[bh-2])
	i = strings.Index(line, "Enter")
	if _, cmd := e.HandleMouse(bx+ansi.StringWidth(line[:i]), by+bh-2, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Errorf("Enter hint on an empty list sent %#v", cmd())
	}
}

func TestFooterDropsHintsThatDoNotFit(t *testing.T) {
	m := NewItems(" P ", manyItems(3))
	m.SetRowAction("→", "show the details")
	for _, inner := range []int{200, 60, 44, 34} {
		pairs := m.footerPairs(inner)
		sp := theme.HintSpans(pairs...)
		if inner >= 44 && sp[len(sp)-1].X1+2 > inner {
			t.Errorf("inner %d: footer %v is %d wide", inner, pairs, sp[len(sp)-1].X1+2)
		}
		if pairs[len(pairs)-2] != "esc" {
			t.Errorf("inner %d: footer %v lost Esc", inner, pairs)
		}
	}
	plain := NewItems(" P ", manyItems(3))
	if got := plain.footerPairs(20); got[0] != "enter" {
		t.Errorf("narrow footer = %v, want it to start at enter", got)
	}
}

func TestBoxNeverWiderThanAsked(t *testing.T) {
	long := []Item{
		{ID: "1", Title: strings.Repeat("very-long-folder/", 8) + "file.go", File: true, Hint: "a hint that is long too"},
		{ID: "2", Title: "日本語のファイル名がとても長い場合のテスト", Hint: "日本"},
		{ID: "3", Title: "متن فارسی خیلی طولانی برای آزمایش عرض", Hint: "راهنما"},
		{Title: strings.Repeat("Long header ", 10), Header: true, Group: "g"},
		{ID: "4", Title: "short", Group: "g"},
	}
	for _, w := range []int{40, 60, 80, 200} {
		m := NewItems(" Open ", long)
		m.SetSize(w, 30)
		m.SetWidth(90)
		boxW, boxH := m.BoxSize()
		lines := strings.Split(m.Box(), "\n")
		if len(lines) != boxH {
			t.Errorf("width %d: %d lines, BoxSize says %d", w, len(lines), boxH)
		}
		for i, l := range lines {
			if lw := ansi.StringWidth(l); lw != boxW {
				t.Errorf("width %d: line %d is %d wide, box is %d: %q", w, i, lw, boxW, ansi.Strip(l))
			}
		}
		if boxW > w {
			t.Errorf("width %d: box is %d wide", w, boxW)
		}
	}
}

func TestOriginStaysOnScreen(t *testing.T) {
	m := NewItems(" P ", manyItems(3))
	m.SetSize(100, 6) // shorter than the box
	_, boxH := m.BoxSize()
	if x, y := m.Origin(60, boxH); x != 20 || y != 0 {
		t.Errorf("Origin on a short screen = %d,%d; want 20,0", x, y)
	}
	m.SetSize(30, 40)
	if x, _ := m.Origin(60, 10); x != 0 {
		t.Errorf("Origin on a narrow screen: x = %d, want 0", x)
	}
}

func TestTitleAndSearchIconsFollowTheIconMode(t *testing.T) {
	saved := theme.CurrentIconMode
	t.Cleanup(func() { theme.CurrentIconMode = saved })
	m := NewItems("", manyItems(1))
	m.SetSize(100, 30)
	for mode, want := range map[theme.IconMode][2]string{
		theme.IconModeNerdFont: {"", ""},
		theme.IconModeUnicode:  {"❯", "⌕"},
		theme.IconModeASCII:    {">", "/"},
	} {
		theme.CurrentIconMode = mode
		box := ansi.Strip(m.Box())
		if !strings.Contains(box, want[0]+" Picker") {
			t.Errorf("mode %v: title icon %q missing (empty title shows Picker)", mode, want[0])
		}
		if !strings.Contains(box, want[1]) {
			t.Errorf("mode %v: search icon %q missing", mode, want[1])
		}
	}
}

func TestMatchCountSubtitle(t *testing.T) {
	for n, want := range map[int]string{0: "no matches", 1: "1 match", 12: "12 matches", 1005: "1005 matches"} {
		if got := pickerSubtitle(n, false); got != want {
			t.Errorf("pickerSubtitle(%d) = %q, want %q", n, got, want)
		}
	}
	if got := pickerSubtitle(5, true); got != "loading…" {
		t.Errorf("loading subtitle = %q", got)
	}
	if itoa(-4) != "0" {
		t.Errorf("itoa(-4) = %q", itoa(-4))
	}
}

func TestSetCursorIDAndGlideScrolling(t *testing.T) {
	saved := anim.Enabled
	anim.Enabled = true
	t.Cleanup(func() { anim.Enabled = saved })
	m := NewItems(" P ", manyItems(100))
	m.SetSize(100, 30)
	m.SetCursorID("80")
	if currentID(t, m) != "80" {
		t.Fatalf("SetCursorID(80): current %q", currentID(t, m))
	}
	if !m.Animating() {
		t.Error("a long jump should glide")
	}
	for i := 0; i < 500 && m.Animating(); i++ {
		m.AnimStep()
	}
	if m.Animating() {
		t.Fatal("scroll never settled")
	}
	if !strings.Contains(ansi.Strip(m.Box()), "item 80") {
		t.Error("row 80 not on screen after the glide")
	}
	m.SetCursorID("no-such-id")
	if currentID(t, m) != "80" {
		t.Errorf("unknown id moved the cursor to %q", currentID(t, m))
	}
}

func TestFirstSelectableFallsBackToARowAbove(t *testing.T) {
	m := NewItems(" P ", []Item{{ID: "a", Title: "alpha"}, {Title: "H", Header: true}})
	if got := m.firstSelectable(1); got != 0 {
		t.Errorf("firstSelectable(1) = %d, want 0 (the row above the header)", got)
	}
	if got := m.firstSelectable(-5); got != 0 {
		t.Errorf("firstSelectable(-5) = %d, want 0", got)
	}
	h := NewItems(" P ", []Item{{Title: "H", Header: true}})
	if got := h.firstSelectable(0); got != 0 {
		t.Errorf("header-only list: firstSelectable = %d, want 0", got)
	}
	if m.matchIsHeader(99) || m.matchIsHeader(-1) {
		t.Error("out-of-range rows count as headers")
	}
	if got := m.nextSelectable(0, 0); got != 0 {
		t.Errorf("nextSelectable with no direction = %d", got)
	}
}
