package recents

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func runeKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func sampleEntries(now time.Time) []Entry {
	return []Entry{
		{Path: "/home/user/proj/main.go", OpenedAt: now.Add(-5 * time.Minute)},
		{Path: "/home/user/proj/internal/app/view.go", OpenedAt: now.Add(-2 * time.Hour)},
		{Path: "/home/user/proj/internal/recents/model.go", OpenedAt: now.Add(-30 * time.Minute)},
		{Path: "/home/user/notes/todo.md", OpenedAt: now.Add(-3 * 24 * time.Hour)},
	}
}

func TestNewSetsEmptyFilterAndFirstSelected(t *testing.T) {
	now := time.Now()
	m := New(sampleEntries(now), now)
	if m.input != "" {
		t.Fatalf("input want empty, got %q", m.input)
	}
	if m.cursor != 0 {
		t.Fatalf("cursor want 0, got %d", m.cursor)
	}
	if got, want := len(m.filtered), 4; got != want {
		t.Fatalf("filtered len want %d, got %d", want, got)
	}
}

func TestTypingFiltersAndUpdatesCount(t *testing.T) {
	now := time.Now()
	m := New(sampleEntries(now), now)
	for _, c := range []string{"v", "i", "e", "w"} {
		m, _ = m.Update(runeKey(c))
	}
	if m.input != "view" {
		t.Fatalf("input want %q, got %q", "view", m.input)
	}
	if got, want := len(m.filtered), 1; got != want {
		t.Fatalf("filter to 'view' want %d match, got %d", want, got)
	}
	idx := m.filtered[0]
	if !strings.Contains(m.entries[idx].Path, "view.go") {
		t.Fatalf("expected view.go to be the survivor, got %q", m.entries[idx].Path)
	}
}

func TestFilterIsCaseInsensitive(t *testing.T) {
	now := time.Now()
	m := New(sampleEntries(now), now)
	m, _ = m.Update(runeKey("MAIN"))
	if got, want := len(m.filtered), 1; got != want {
		t.Fatalf("case-insensitive filter want %d, got %d", want, got)
	}
}

func TestUpDownMovesSelection(t *testing.T) {
	now := time.Now()
	m := New(sampleEntries(now), now)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("after Down: cursor want 1, got %d", m.cursor)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 1 {
		t.Fatalf("after Down/Down/Up: cursor want 1, got %d", m.cursor)
	}
	// Down past the end clamps.
	for i := 0; i < 10; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.cursor != len(m.filtered)-1 {
		t.Fatalf("cursor should clamp to last filtered idx, got %d", m.cursor)
	}
}

func TestEnterEmitsSelect(t *testing.T) {
	now := time.Now()
	entries := sampleEntries(now)
	m := New(entries, now)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // pick second entry
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should return a SelectMsg cmd")
	}
	msg := cmd()
	sel, ok := msg.(SelectMsg)
	if !ok {
		t.Fatalf("want SelectMsg, got %T", msg)
	}
	if sel.Path != entries[1].Path {
		t.Fatalf("selected path want %q, got %q", entries[1].Path, sel.Path)
	}
}

func TestEscEmitsClose(t *testing.T) {
	now := time.Now()
	m := New(sampleEntries(now), now)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc must emit a CloseMsg cmd")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatalf("want CloseMsg, got %T", cmd())
	}
}

func TestEnterOnEmptyDoesNothing(t *testing.T) {
	now := time.Now()
	m := New(nil, now)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("Enter with no entries must not emit a SelectMsg, got %T", cmd())
	}
}

func TestBackspaceRefilters(t *testing.T) {
	now := time.Now()
	m := New(sampleEntries(now), now)
	m, _ = m.Update(runeKey("v"))
	m, _ = m.Update(runeKey("i"))
	if got, want := len(m.filtered), 1; got != want {
		t.Fatalf("filter to 'vi' want %d, got %d", want, got)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.input != "" {
		t.Fatalf("input want empty after 2 backspaces, got %q", m.input)
	}
	if len(m.filtered) != len(m.entries) {
		t.Fatalf("filtered len want %d, got %d", len(m.entries), len(m.filtered))
	}
}

func TestViewRendersHeaderSearchListFooter(t *testing.T) {
	now := time.Now()
	m := New(sampleEntries(now), now)
	m.SetSize(80, 24)
	out := m.View()

	checks := []string{
		"Open Recent File", // header title
		"matches",          // header pill copy ("4 matches")
		"recent files",     // search placeholder ("Search recent files…")
		"main.go",          // first list row basename
		"view.go",          // second row basename
		"Navigate",         // footer keycap label
		"Select",
		"Close",
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("expected %q in rendered modal, missing\n%s", c, out)
		}
	}
}

func TestViewEmptyDimensionsReturnsEmpty(t *testing.T) {
	now := time.Now()
	m := New(sampleEntries(now), now)
	if v := m.View(); v != "" {
		t.Fatalf("View at zero size must be empty, got %q", v)
	}
}

func TestViewEmptyEntriesShowsHint(t *testing.T) {
	now := time.Now()
	m := New(nil, now)
	m.SetSize(80, 24)
	out := m.View()
	if !strings.Contains(out, "No recent files found") {
		t.Fatalf("expected empty-state hint, got\n%s", out)
	}
}

func TestMouseSGRFragmentDropped(t *testing.T) {
	now := time.Now()
	m := New(sampleEntries(now), now)
	m, _ = m.Update(runeKey("[<35;70;19M"))
	if m.input != "" {
		t.Fatalf("mouse SGR fragment must not leak into search, got %q", m.input)
	}
}

func TestBoxFillsExactInnerWidth(t *testing.T) {
	// Render-width contract: every visible row of Box() must occupy the
	// same number of cells (the rounded border auto-fits to the longest
	// inner row, so a short row would leak terminal default bg through
	// the modal_overlay glass blend).
	now := time.Now()
	m := New(sampleEntries(now), now)
	m.SetSize(80, 24)
	box := m.Box()
	if box == "" {
		t.Fatal("Box() returned empty string")
	}
	lines := strings.Split(box, "\n")
	if len(lines) < 4 {
		t.Fatalf("expected multi-line box, got %d lines", len(lines))
	}
	w0 := lipglossWidth(lines[0])
	for i, line := range lines {
		if w := lipglossWidth(line); w != w0 {
			t.Fatalf("row %d width %d != row 0 width %d", i, w, w0)
		}
	}
}

func lipglossWidth(s string) int { return lipgloss.Width(s) }
