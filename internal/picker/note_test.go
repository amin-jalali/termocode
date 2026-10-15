package picker

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// A picker can carry a short note above its list (the setup card's step
// chooser says why the step needs a choice). The note sits under the
// title, rows still line up with their hit areas, and the box keeps its
// width.
func TestNoteAboveTheList(t *testing.T) {
	m := NewItems(" Icons ", []Item{{ID: "a", Title: "First choice"}, {ID: "b", Title: "Second choice"}})
	m.SetSize(120, 40)
	plain := NewItems(" Icons ", []Item{{ID: "a", Title: "First choice"}, {ID: "b", Title: "Second choice"}})
	plain.SetSize(120, 40)
	m.SetNote([]string{"Nerd icons are on, but GNOME Terminal uses", "Ubuntu Mono 11, so icons may show as boxes."})

	box := ansi.Strip(m.Box())
	if !strings.Contains(box, "Ubuntu Mono 11, so icons may show as boxes.") {
		t.Fatalf("note not in the box:\n%s", box)
	}
	if strings.Index(box, "Nerd icons are on") > strings.Index(box, "First choice") {
		t.Error("note below the list")
	}
	w0, h0 := plain.BoxSize()
	w1, h1 := m.BoxSize()
	if w1 != w0 || h1 <= h0 {
		t.Errorf("box %dx%d with a note, %dx%d without", w1, h1, w0, h0)
	}
	if got := len(strings.Split(m.Box(), "\n")); got != h1 {
		t.Errorf("box draws %d rows, BoxSize says %d", got, h1)
	}
	// Every row hit area shows its own item.
	_, by, _, _ := m.boxRect()
	lines := strings.Split(box, "\n")
	for _, r := range m.RowRects() {
		want := m.items[m.matches[r.Index].Index].Title
		if got := lines[r.Y-by]; !strings.Contains(got, want) {
			t.Errorf("row rect %d at y=%d shows %q, want %q", r.Index, r.Y, got, want)
		}
	}
	for _, l := range strings.Split(m.Box(), "\n") {
		if lipgloss.Width(l) != w1 {
			t.Fatalf("row %q is %d cells, want %d", ansi.Strip(l), lipgloss.Width(l), w1)
		}
	}
}
