package picker

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeQuery(m Model, q string) Model {
	for _, r := range q {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// Spec: Given a flat list where a longer title also fuzzy-matches,
// when the user types a full title (or a close query),
// then the exact / best match is the selected row.
func TestFilterSelectsBestMatch(t *testing.T) {
	items := []Item{
		{ID: "sym", Title: "Go to Symbol in File..."},
		{ID: "line", Title: "Go to Line..."},
		{ID: "start", Title: "Debug: Start / Continue"},
		{ID: "cont", Title: "Debug: Continue"},
	}
	for q, want := range map[string]string{
		"Go to Line...":   "line",
		"go to line":      "line",
		"Debug: Continue": "cont",
	} {
		m := typeQuery(NewItems("t", items), q)
		if it, ok := m.Current(); !ok || it.ID != want {
			t.Errorf("query %q selects %q, want %q", q, it.ID, want)
		}
	}
}

// Spec: Given a grouped list (headers),
// when the query matches rows in two groups,
// then the rows keep their group order and the cursor is on the best one.
func TestFilterGroupedKeepsOrderSelectsBest(t *testing.T) {
	items := []Item{
		{Title: "A", Header: true, Group: "a"},
		{ID: "long", Title: "Fix all lint issues", Group: "a"},
		{Title: "B", Header: true, Group: "b"},
		{ID: "exact", Title: "Fix", Group: "b"},
	}
	m := typeQuery(NewItems("t", items), "Fix")
	if it, ok := m.Current(); !ok || it.ID != "exact" {
		t.Errorf("selected %q, want exact", it.ID)
	}
	if got := m.items[m.matches[1].Index].ID; got != "long" {
		t.Errorf("group order changed: second row is %q", got)
	}
}
