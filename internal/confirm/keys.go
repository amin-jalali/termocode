package confirm

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// KeyRow is one line of a key card (WithKeys): a key and what it does.
// A row without a key is a small heading for the rows below it.
type KeyRow struct {
	Key  string
	What string
}

// Marks that tell Box how to paint a key-table line. They are in the line
// text only between layoutDims and Box; they have no width on screen.
const (
	keyHeadMark = "\x01" // a heading line
	keyKeyMark  = "\x02" // starts a key line: <mark>key<keyEndMark>rest
	keyEndMark  = "\x03"
)

// WithKeys makes this card a key card: a table of keys (the key, then what
// it does) under the title, scrollable with Up and Down. The buttons are the
// caller's (for example Open guide and Close). Esc closes it.
func (m Model) WithKeys(rows []KeyRow) Model {
	m.keyRows = append([]KeyRow(nil), rows...)
	m.scrollText = true
	return m
}

// keyColWidth is the width of the key column.
func (m Model) keyColWidth() int {
	w := 0
	for _, r := range m.keyRows {
		if r.Key != "" {
			w = max(w, lipgloss.Width(r.Key))
		}
	}
	return w
}

// keyTableWidth is how wide the table is when no line is wrapped.
func (m Model) keyTableWidth() int {
	kw, w := m.keyColWidth(), 0
	for _, r := range m.keyRows {
		if r.Key == "" {
			w = max(w, lipgloss.Width(r.What))
		} else {
			w = max(w, kw+2+lipgloss.Width(r.What))
		}
	}
	return w
}

// keyTableLines lays the table out in width cells: the key column, two
// spaces, then the text, wrapped under itself.
func (m Model) keyTableLines(width int) []string {
	kw := m.keyColWidth()
	var out []string
	for _, r := range m.keyRows {
		if r.Key == "" {
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, keyHeadMark+r.What)
			continue
		}
		parts := wrap(r.What, max(width-kw-2, 8))
		if len(parts) == 0 {
			parts = []string{""}
		}
		pad := strings.Repeat(" ", kw-lipgloss.Width(r.Key))
		out = append(out, keyKeyMark+r.Key+pad+keyEndMark+"  "+parts[0])
		for _, p := range parts[1:] {
			out = append(out, strings.Repeat(" ", kw+2)+p)
		}
	}
	return out
}
