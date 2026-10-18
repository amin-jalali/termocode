package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

// tileUnicode draws the page with the unicode icon tier (rounded boxes).
func tileUnicode(t *testing.T) {
	t.Helper()
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	t.Cleanup(func() { theme.CurrentIconMode = old; invalidateWelcome() })
}

func tileTitles() []string {
	return []string{"Open file", "Command palette", "Find in files", "New terminal", "Open workspace", "Clone repository"}
}

// TestWelcomeTiles_SixTilesAt150x42: START is six rounded boxes, two
// columns by three rows, each with title, subtitle and keycap.
func TestWelcomeTiles_SixTilesAt150x42(t *testing.T) {
	welcomeModel(t)
	out := ansi.Strip(renderWelcome(150, 42, 0, true))
	if n := strings.Count(out, "╭"); n != 6 {
		t.Fatalf("want 6 tile tops, got %d:\n%s", n, out)
	}
	if n := strings.Count(out, "╰"); n != 6 {
		t.Fatalf("want 6 tile bottoms, got %d", n)
	}
	for _, s := range []string{"browse your filesystem", "run any action", "project-wide search",
		"integrated terminal", "recent folders", "git clone, then open it", "Ctrl+P", "F1", "F8", "Ctrl+T"} {
		mustContain(t, out, s)
	}
	for _, s := range tileTitles() {
		mustContain(t, out, s)
	}
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Open file") {
			line = l
		}
	}
	if !strings.Contains(line, "Command palette") {
		t.Errorf("Open file and Command palette not side by side: %q", line)
	}
	rows := welcomeRows(150, 42)
	if len(rows) < 6 {
		t.Fatalf("rows: %+v", rows)
	}
	for i := 0; i < 6; i++ {
		if rows[i].ID != "card:"+itoa(i) || rows[i].H < 3 {
			t.Errorf("row %d = %+v, want a tall card:%d", i, rows[i], i)
		}
	}
	if rows[0].Row != rows[1].Row || rows[2].Row != rows[0].Row+rows[0].H || rows[0].ColEnd > rows[1].ColStart {
		t.Errorf("tiles not in a 2-column grid: %+v %+v %+v", rows[0], rows[1], rows[2])
	}
}

// TestWelcomeTiles_ClickRunsTile: a click anywhere on a tile (also its
// second line) runs that tile's action.
func TestWelcomeTiles_ClickRunsTile(t *testing.T) {
	m := welcomeModel(t)
	m.View()
	pane := m.editorPaneLayout()
	h := welcomeRows(pane.W, pane.H)[1] // "Command palette"
	nm, _ := m.Update(tea.MouseMsg{X: pane.X + h.ColStart + 5, Y: pane.Top + h.Row + 2, Type: tea.MouseLeft, Action: tea.MouseActionPress})
	got := nm.(Model)
	if got.welcomeFocus != 1 {
		t.Errorf("click on tile 1 left the cursor on %d", got.welcomeFocus)
	}
	if !got.pickerOpen {
		t.Errorf("click on the Command palette tile did not open the palette")
	}
}

// TestWelcomeTiles_HoverMovesCursor: the pointer over a tile moves the
// cursor there.
func TestWelcomeTiles_HoverMovesCursor(t *testing.T) {
	m := welcomeModel(t)
	m.View()
	pane := m.editorPaneLayout()
	h := welcomeRows(pane.W, pane.H)[3]
	m = hoverAt(m, pane.X+h.ColStart+3, pane.Top+h.Row+1)
	if m.welcomeFocus != 3 {
		t.Fatalf("hover on tile 3: cursor %d", m.welcomeFocus)
	}
}

// TestWelcomeTiles_CursorMovesAcrossTiles: arrows move in the 2×3 grid,
// then down into the recent rows; Tab walks tile by tile.
func TestWelcomeTiles_CursorMovesAcrossTiles(t *testing.T) {
	m := welcomeModel(t)
	m.View()
	key := func(k tea.KeyType) { m = pressKey(m, tea.KeyMsg{Type: k}) }
	check := func(want int, what string) {
		t.Helper()
		if m.welcomeFocus != want {
			t.Fatalf("%s: cursor %d, want %d", what, m.welcomeFocus, want)
		}
	}
	check(0, "start")
	key(tea.KeyRight)
	check(1, "right")
	key(tea.KeyDown)
	check(3, "down")
	key(tea.KeyLeft)
	check(2, "left")
	key(tea.KeyDown)
	check(4, "down")
	key(tea.KeyDown)
	check(6, "down from the last tile row: first recent row")
	m.View()
	if got := cursorLine(t, m); !strings.Contains(got, "file-06.go") {
		t.Errorf("cursor on %q, want the newest recent file", got)
	}
	key(tea.KeyUp)
	check(4, "up from the first recent row: last tile row")
	m.welcomeFocus = 1
	key(tea.KeyUp)
	if want := len(welcomeRows(150, 42)) - 1; m.welcomeFocus != want {
		t.Errorf("up from the first tile row: cursor %d, want the last row %d", m.welcomeFocus, want)
	}
	m.welcomeFocus = 0
	key(tea.KeyTab)
	check(1, "tab")
	key(tea.KeyTab)
	check(2, "tab")
}

// TestWelcomeTiles_FocusedTileShowsCursor: the focused tile has the
// accent edge on its lines; the others have the plain one.
func TestWelcomeTiles_FocusedTileShowsCursor(t *testing.T) {
	tileUnicode(t)
	withWelcomeLists(t, 2, 1)
	for focus := 0; focus < 6; focus++ {
		out := ansi.Strip(renderWelcome(150, 42, focus, true))
		if n := strings.Count(out, "▌"); n < 2 {
			t.Errorf("focus %d: %d accent edge cells, want at least 2", focus, n)
		}
		if strings.Contains(out, "▎") {
			t.Errorf("focus %d: row cursor bar drawn next to a tile", focus)
		}
	}
}

// TestWelcomeTiles_EnterRunsTile: Enter on the focused tile runs it.
func TestWelcomeTiles_EnterRunsTile(t *testing.T) {
	m := welcomeModel(t)
	m.View()
	m.welcomeFocus = 1
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !nm.(Model).pickerOpen {
		t.Errorf("Enter on the Command palette tile did not open the palette")
	}
}

// TestWelcomeTiles_NarrowFallback: a narrow pane has one column of
// tiles; a pane too small even for that has the one-line rows.
func TestWelcomeTiles_NarrowFallback(t *testing.T) {
	tileUnicode(t)
	withWelcomeLists(t, 0, 0)
	out := ansi.Strip(renderWelcome(44, 40, 0, true))
	if n := strings.Count(out, "╭"); n != 6 {
		t.Errorf("44x40: want 6 stacked tiles, got %d tops:\n%s", n, out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.Count(l, "╭") > 1 {
			t.Errorf("44x40: two tiles on one line: %q", l)
		}
	}
	rows := welcomeRows(44, 40)
	if rows[1].Row <= rows[0].Row || rows[1].ColStart != rows[0].ColStart {
		t.Errorf("44x40: tiles not in one column: %+v %+v", rows[0], rows[1])
	}
	out = ansi.Strip(renderWelcome(100, 14, 0, true))
	if strings.Contains(out, "╭") {
		t.Errorf("100x14: tiles do not fit but are drawn:\n%s", out)
	}
	for _, s := range tileTitles() {
		mustContain(t, out, s)
	}
	if rows := welcomeRows(100, 14); len(rows) < 6 || rows[0].H > 1 {
		t.Errorf("100x14: want one-line rows, got %+v", rows)
	}
}
