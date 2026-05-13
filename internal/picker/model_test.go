package picker

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func runeKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestNewItemsPopulatesMatches(t *testing.T) {
	items := []Item{
		{ID: "a", Title: "alpha"},
		{ID: "b", Title: "beta"},
	}
	m := NewItems("pick", items)
	if len(m.matches) != 2 {
		t.Fatalf("want 2 initial matches, got %d", len(m.matches))
	}
	if m.cursor != 0 {
		t.Fatalf("cursor want 0, got %d", m.cursor)
	}
}

func TestNewWithLoaderInitFiresLoad(t *testing.T) {
	called := false
	loader := func() []Item {
		called = true
		return []Item{{ID: "x", Title: "x"}}
	}
	m := NewWith("title", loader)
	if !m.loading {
		t.Fatalf("loading flag should be true before Init runs")
	}
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() must return a cmd when loader is set")
	}
	msg := cmd()
	if !called {
		t.Fatal("loader was not invoked")
	}
	loaded, ok := msg.(itemsLoadedMsg)
	if !ok {
		t.Fatalf("want itemsLoadedMsg, got %T", msg)
	}
	m, _ = m.Update(loaded)
	if m.loading {
		t.Fatal("loading flag still set after itemsLoadedMsg")
	}
	if len(m.items) != 1 {
		t.Fatalf("items not populated: %v", m.items)
	}
}

func TestUpdateNavigationAndFilter(t *testing.T) {
	items := []Item{
		{ID: "1", Title: "alpha"},
		{ID: "2", Title: "beta"},
		{ID: "3", Title: "gamma"},
	}
	m := NewItems("t", items)

	// Down moves the cursor.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("KeyDown: cursor want 1, got %d", m.cursor)
	}
	// Up brings it back.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("KeyUp: cursor want 0, got %d", m.cursor)
	}

	// Typing characters refilters.
	m, _ = m.Update(runeKey("g"))
	if m.input != "g" {
		t.Fatalf("input want 'g', got %q", m.input)
	}
	// Should now have a single match with index 2 (gamma).
	if len(m.matches) != 1 || m.matches[0].Index != 2 {
		t.Fatalf("filter to 'g' did not isolate gamma: %+v", m.matches)
	}

	// Backspace clears.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.input != "" {
		t.Fatalf("after backspace input want empty, got %q", m.input)
	}
	if len(m.matches) != 3 {
		t.Fatalf("after clear, want 3 matches, got %d", len(m.matches))
	}

	// Space concatenates.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if m.input != " " {
		t.Fatalf("space input want ' ', got %q", m.input)
	}
}

func TestUpdateEnterEmitsSelect(t *testing.T) {
	items := []Item{{ID: "id1", Title: "t1"}, {ID: "id2", Title: "t2"}}
	m := NewItems("", items)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected SelectMsg cmd from Enter")
	}
	msg := cmd()
	sel, ok := msg.(SelectMsg)
	if !ok {
		t.Fatalf("want SelectMsg, got %T", msg)
	}
	if sel.ID != "id2" {
		t.Fatalf("selected ID want id2, got %q", sel.ID)
	}
}

func TestUpdateEscEmitsClose(t *testing.T) {
	m := NewItems("", []Item{{ID: "x", Title: "x"}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc should emit CloseMsg cmd")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatalf("want CloseMsg, got %T", cmd())
	}
}

func TestViewEmptyDimensionsReturnsEmpty(t *testing.T) {
	m := NewItems("", []Item{{ID: "x", Title: "x"}})
	if v := m.View(); v != "" {
		t.Fatalf("View at zero size should be empty, got %q", v)
	}
}

func TestViewRendersTitleAndItems(t *testing.T) {
	m := NewItems(" Quick ", []Item{{ID: "1", Title: "alpha"}})
	m.SetSize(80, 20)
	out := m.View()
	if !strings.Contains(out, "Quick") {
		t.Fatalf("title not in output")
	}
	if !strings.Contains(out, "alpha") {
		t.Fatalf("item not rendered")
	}
}

// Mouse-tracking SGR codes (e.g. "[<35;70;19M") sometimes arrive as
// tea.KeyRunes events when mouse mode is mid-toggle. They can come as
// one big event or — more annoyingly — split across multiple events.
// The input filter must drop them entirely; nothing transient should
// flicker through m.input. Real keystrokes (single chars, normal text,
// even bracketed text like "[abc]") must still pass through.
func TestUpdateDropsMouseSGRSingleEvent(t *testing.T) {
	m := NewItems("", []Item{{ID: "x", Title: "x"}})
	m, _ = m.Update(runeKey("[<35;70;19M"))
	if m.input != "" {
		t.Fatalf("full mouse SGR fragment must be dropped, got %q", m.input)
	}
}

func TestUpdateDropsMouseSGRSplitAcrossEvents(t *testing.T) {
	m := NewItems("", []Item{{ID: "x", Title: "x"}})
	for _, chunk := range []string{"[<35", ";70", ";19M"} {
		m, _ = m.Update(runeKey(chunk))
	}
	if m.input != "" {
		t.Fatalf("split mouse SGR fragments must not accumulate, got %q", m.input)
	}
}

func TestUpdateAllowsBracketAlone(t *testing.T) {
	// A user actually pressing `[` (single-rune event) must still type.
	m := NewItems("", []Item{{ID: "x", Title: "x"}})
	m, _ = m.Update(runeKey("["))
	if m.input != "[" {
		t.Fatalf("single `[` should type as-is, got %q", m.input)
	}
}

func TestUpdateAllowsAbcTyping(t *testing.T) {
	m := NewItems("", []Item{{ID: "x", Title: "x"}})
	for _, c := range []string{"a", "b", "c"} {
		m, _ = m.Update(runeKey(c))
	}
	if m.input != "abc" {
		t.Fatalf("typing 'abc' want 'abc', got %q", m.input)
	}
}

func TestUpdateAllowsBracketedText(t *testing.T) {
	// "[abc]" contains non-mouse-charset chars so it must pass through
	// even when delivered as one paste-style multi-rune event.
	m := NewItems("", []Item{{ID: "x", Title: "x"}})
	m, _ = m.Update(runeKey("[abc]"))
	if m.input != "[abc]" {
		t.Fatalf("bracketed text want '[abc]', got %q", m.input)
	}
}

func TestUpdateAllowsPersianText(t *testing.T) {
	// Persian letters are well outside the mouse-code charset and must
	// pass through untouched even as multi-rune events.
	m := NewItems("", []Item{{ID: "x", Title: "x"}})
	m, _ = m.Update(runeKey("سلام"))
	if m.input != "سلام" {
		t.Fatalf("persian text want 'سلام', got %q", m.input)
	}
}

func TestIsMouseFragmentEventHeuristic(t *testing.T) {
	cases := []struct {
		name string
		in   string
		drop bool
	}{
		{"empty", "", false},
		{"single bracket", "[", false},
		{"single M", "M", false},
		{"sgr head", "[<35", true},
		{"sgr middle", ";70", true},
		{"sgr tail", ";19M", true},
		{"full sgr", "[<35;70;19M", true},
		{"bracketed letters", "[abc]", false},
		{"persian", "سلام", false},
		{"two letters", "ab", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isMouseFragmentEvent([]rune(tc.in))
			if got != tc.drop {
				t.Fatalf("isMouseFragmentEvent(%q) = %v, want %v", tc.in, got, tc.drop)
			}
		})
	}
}

func TestHeaderRowsAreSkippedByNavigation(t *testing.T) {
	items := []Item{
		{ID: "h1", Title: "Quick Fix", Header: true, Group: "Quick Fix"},
		{ID: "1", Title: "add import \"fmt\"", Group: "Quick Fix"},
		{ID: "h2", Title: "Refactor", Header: true, Group: "Refactor"},
		{ID: "2", Title: "extract function", Group: "Refactor"},
		{ID: "3", Title: "inline variable", Group: "Refactor"},
	}
	m := NewItems("Code Actions", items)
	// Cursor must land on the first non-header item.
	if m.cursor < 0 || m.cursor >= len(m.matches) || m.matchIsHeader(m.cursor) {
		t.Fatalf("initial cursor landed on header: matches=%d cursor=%d", len(m.matches), m.cursor)
	}
	first := m.cursor
	// Down from "add import" should skip the "Refactor" header and land on "extract function".
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor == first {
		t.Fatalf("cursor did not advance")
	}
	if m.matchIsHeader(m.cursor) {
		t.Fatalf("cursor landed on header at idx=%d", m.cursor)
	}
	idx := m.matches[m.cursor].Index
	if items[idx].Title != "extract function" {
		t.Fatalf("expected to land on extract function, got %q", items[idx].Title)
	}
	// Up from extract function should hop back over the header to "add import".
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.matchIsHeader(m.cursor) {
		t.Fatalf("cursor landed on header after Up at idx=%d", m.cursor)
	}
	idx = m.matches[m.cursor].Index
	if items[idx].Title != "add import \"fmt\"" {
		t.Fatalf("expected to return to add import, got %q", items[idx].Title)
	}
}

func TestHeaderRowsRefilterReinjects(t *testing.T) {
	items := []Item{
		{ID: "h1", Title: "Quick Fix", Header: true, Group: "Quick Fix"},
		{ID: "1", Title: "add import \"fmt\"", Group: "Quick Fix"},
		{ID: "h2", Title: "Refactor", Header: true, Group: "Refactor"},
		{ID: "2", Title: "extract function", Group: "Refactor"},
	}
	m := NewItems("", items)
	// Filter to "import" — only the import item passes; its header should re-appear.
	for _, c := range []string{"i", "m", "p", "o", "r", "t"} {
		m, _ = m.Update(runeKey(c))
	}
	if m.input != "import" {
		t.Fatalf("input not accumulated: %q", m.input)
	}
	// We expect: header "Quick Fix" + the matching item.
	if len(m.matches) != 2 {
		t.Fatalf("filtered matches: want 2 (header+item), got %d: %+v", len(m.matches), m.matches)
	}
	if !m.matchIsHeader(0) {
		t.Fatalf("first match should be the Quick Fix header")
	}
	if items[m.matches[1].Index].Title != "add import \"fmt\"" {
		t.Fatalf("second match should be add import, got %q", items[m.matches[1].Index].Title)
	}
	if m.matchIsHeader(m.cursor) {
		t.Fatalf("cursor should auto-skip header, sits at %d (header=%v)", m.cursor, m.matchIsHeader(m.cursor))
	}
}

func TestHeaderEnterIsNoOp(t *testing.T) {
	items := []Item{
		{ID: "h1", Title: "Quick Fix", Header: true, Group: "Quick Fix"},
		{ID: "1", Title: "add import", Group: "Quick Fix"},
	}
	m := NewItems("", items)
	// Force-cursor onto the header to exercise the no-op path on Enter.
	m.cursor = 0
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		// Header rows must not emit SelectMsg.
		msg := cmd()
		if _, ok := msg.(SelectMsg); ok {
			t.Fatalf("Enter on header emitted SelectMsg: %+v", msg)
		}
	}
}

func TestViewLoadingAndEmptyStates(t *testing.T) {
	t.Run("loading", func(t *testing.T) {
		m := NewWith("L", func() []Item { return nil })
		m.SetSize(60, 12)
		out := m.View()
		if !strings.Contains(out, "loading") {
			t.Fatalf("loading hint missing: %s", out)
		}
	})
	t.Run("no results", func(t *testing.T) {
		m := NewItems("", nil)
		m.SetSize(60, 12)
		out := m.View()
		if !strings.Contains(out, "no results") {
			t.Fatalf("no-results hint missing: %s", out)
		}
	})
}
