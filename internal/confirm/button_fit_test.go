package confirm

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Spec: Given a card with more buttons than fit at the usual padding
// (the CSV import card: five buttons in an 80-column window)
// When it is drawn
// Then every button sits whole on the button row inside the card, and
// the click targets match where the labels are drawn.
func TestManyButtonsFitInsideTheCard(t *testing.T) {
	buttons := []Button{
		{ID: "import", Title: "Import", Style: StylePrimary},
		{ID: "map", Title: "Map columns…"},
		{ID: "header", Title: "Header: on"},
		{ID: "bad-rows", Title: "Bad rows: stop"},
		{ID: "cancel", Title: "Cancel"},
	}
	m := New("Import 1200 rows", "a short message", buttons)
	m.SetSize(80, 24)
	bx, _, bw, _ := m.Bounds()
	rects := m.ButtonRects()
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	for i, r := range rects {
		if r.X <= bx || r.X+r.W >= bx+bw {
			t.Errorf("button %s at x %d..%d leaves the card %d..%d", r.ID, r.X, r.X+r.W, bx, bx+bw)
		}
		if r.Y >= len(rows) {
			t.Fatalf("button row %d past the view", r.Y)
		}
		got := ansi.Cut(rows[r.Y], r.X, r.X+r.W)
		if strings.TrimSpace(got) != buttons[i].Title {
			t.Errorf("button %s draws %q at its target, want %q", r.ID, got, buttons[i].Title)
		}
	}
}

// A card whose buttons fit keeps the roomy two-cell padding.
func TestFewButtonsKeepTheirPadding(t *testing.T) {
	m := New("Delete?", "msg", sampleButtons())
	m.SetSize(120, 40)
	for _, r := range m.ButtonRects() {
		if !strings.HasPrefix(r.Label, "  ") || !strings.HasSuffix(r.Label, "  ") {
			t.Errorf("button %s label %q lost its padding", r.ID, r.Label)
		}
	}
}
