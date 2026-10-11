package toast

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The compact card is 3 rows, exactly the asked width, and shows only the
// newest toast with its detail on the title row.
func TestCompactCardIsOneRow(t *testing.T) {
	m := New()
	m, _ = m.PushDetail(Info, "Older", "x")
	m, _ = m.PushDetail(Info, "Saved", "internal/statusbar/model.go · 480 lines")
	for _, w := range []int{16, 24, 33, 40} {
		cards := m.CompactCards(w, 1)
		if len(cards) != 1 || len(cards[0].Lines) != 3 {
			t.Fatalf("w=%d: want one 3-row card, got %+v", w, cards)
		}
		c := cards[0]
		for _, l := range c.Lines {
			if lipgloss.Width(l) != w {
				t.Fatalf("w=%d: row %q is %d wide", w, ansi.Strip(l), lipgloss.Width(l))
			}
		}
		row := ansi.Strip(c.Lines[1])
		if !strings.Contains(row, "Saved") || strings.Contains(row, "Older") {
			t.Fatalf("w=%d: row %q", w, row)
		}
		if w >= 33 && !strings.Contains(row, "Saved · internal") {
			t.Fatalf("w=%d: detail should follow the title: %q", w, row)
		}
	}
	if n := len(m.CompactCards(30, 2)); n != 2 {
		t.Fatalf("CompactCards(30, 2) gave %d cards", n)
	}
}
