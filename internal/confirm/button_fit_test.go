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

// Spec: a word longer than the card is wide (a long path, as macOS temp
// folders are) is cut over lines inside the card: it never runs over the
// border, and the buttons are drawn where a click finds them.
func TestLongWordStaysInsideTheCardAndButtonsMatch(t *testing.T) {
	path := "/private/var/folders/h5/t85nd63d71x27_8gl5gw3l_w0000gn/T/TestUpdateCardInstallAndRollback3555449557/001/termocode.old"
	buttons := []Button{{ID: "cancel", Title: "Cancel"}, {ID: "rollback", Title: "Roll back", Style: StyleDestructive}}
	m := New("Roll back termocode?", "Puts back the termocode that the last update replaced ("+path+").", buttons)
	m.SetSize(100, 40)
	bx, _, bw, _ := m.Bounds()
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	for y, row := range rows {
		if w := ansi.StringWidth(strings.TrimRight(row, " ")); w > bx+bw+1 { // bw counts to the right border
			t.Errorf("row %d is %d cells wide, past the card's right edge %d: %q", y, w, bx+bw, row)
		}
	}
	if text := strings.Join(rows, ""); !strings.Contains(strings.ReplaceAll(strings.ReplaceAll(text, "│", ""), " ", ""), strings.ReplaceAll(path, " ", "")) {
		t.Errorf("the path is not all shown:\n%s", strings.Join(rows, "\n"))
	}
	for i, r := range m.ButtonRects() {
		if got := ansi.Cut(rows[r.Y], r.X, r.X+r.W); strings.TrimSpace(got) != buttons[i].Title {
			t.Errorf("button %s draws %q at its target, want %q", r.ID, got, buttons[i].Title)
		}
	}
}

// A long path is cut after one of its slashes, not inside a name.
func TestLongPathIsCutAfterASlash(t *testing.T) {
	got := cutLongWords([]string{"/private/var/folders/h5/t85nd63d71x27_8gl5gw3l_w0000gn/T/TestUpdate/001/termocode.old"}, 40)
	for _, piece := range got[:len(got)-1] {
		if !strings.HasSuffix(piece, "/") || ansi.StringWidth(piece) > 40 {
			t.Errorf("piece %q: want at most 40 cells, ending in /", piece)
		}
	}
	if strings.Join(got, "") != "/private/var/folders/h5/t85nd63d71x27_8gl5gw3l_w0000gn/T/TestUpdate/001/termocode.old" {
		t.Errorf("pieces %q lose text", got)
	}
}
