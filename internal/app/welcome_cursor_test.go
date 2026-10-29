package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

// welcomeModel is a 150×42 app on the start page with 7 recent files and
// 2 workspaces, keys in the editor.
func welcomeModel(t *testing.T) Model {
	t.Helper()
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	t.Cleanup(func() { theme.CurrentIconMode = old })
	withWelcomeLists(t, 7, 2)
	nm, _ := newTestModel(t).Update(tea.WindowSizeMsg{Width: 150, Height: 42})
	m := nm.(Model)
	m.focus = FocusEditor
	return m
}

func pressKey(m Model, k tea.KeyMsg) Model {
	nm, _ := m.Update(k)
	return nm.(Model)
}

// cursorLine is the plain text of the editor-pane row that carries the
// cursor bar (▎), or of the focused tile's title line (▌ edge, text up
// to the tile's right border).
func cursorLine(t *testing.T, m Model) string {
	t.Helper()
	pane := m.editorPaneLayout()
	var found []string
	for y, line := range strings.Split(m.View(), "\n") {
		if y < pane.Top || y >= pane.Top+pane.H {
			continue
		}
		cell := strings.TrimLeft(ansi.Strip(ansi.Cut(line, pane.X, pane.X+pane.W)), " ")
		if strings.HasPrefix(cell, "▎") {
			found = append(found, strings.TrimSpace(strings.TrimPrefix(cell, "▎")))
		}
	}
	if len(found) == 0 {
		for y, line := range strings.Split(m.View(), "\n") {
			if y < pane.Top || y >= pane.Top+pane.H {
				continue
			}
			cell := ansi.Strip(ansi.Cut(line, pane.X, pane.X+pane.W))
			if _, after, ok := strings.Cut(cell, "▌"); ok {
				after, _, _ = strings.Cut(after, "│")
				return strings.TrimSpace(after) // the first ▌ line is the title line
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("want one cursor row, got %d: %q", len(found), found)
	}
	return found[0]
}

// TestWelcome_CursorCyclesAllRows: ↓ walks START, then the recent files,
// the All… row and the workspaces; it wraps at both ends; Home / End jump.
func TestWelcome_CursorCyclesAllRows(t *testing.T) {
	m := welcomeModel(t)
	down := tea.KeyMsg{Type: tea.KeyDown}
	up := tea.KeyMsg{Type: tea.KeyUp}
	if got := cursorLine(t, m); !strings.Contains(got, "Open file") {
		t.Fatalf("start: cursor on %q", got)
	}
	// Tab walks every row in reading order (the arrows move through the
	// tile grid, see welcome_tiles_test.go).
	down = tea.KeyMsg{Type: tea.KeyTab}
	want := []string{"Command palette", "Find in files", "New terminal", "Open workspace", "Clone repository",
		"file-06.go", "file-05.go", "file-04.go", "file-03.go", "file-02.go", "All 7 recent files",
		"welcome-ws-1", "welcome-ws-0", "All workspaces", "Open file"}
	for i, w := range want {
		m = pressKey(m, down)
		if got := cursorLine(t, m); !strings.Contains(got, w) {
			t.Fatalf("down #%d: cursor on %q, want %q", i+1, got, w)
		}
	}
	m = pressKey(m, up)
	if got := cursorLine(t, m); !strings.Contains(got, "All workspaces") {
		t.Fatalf("up from the top: cursor on %q, want the last row", got)
	}
	m = pressKey(m, tea.KeyMsg{Type: tea.KeyHome})
	if got := cursorLine(t, m); !strings.Contains(got, "Open file") {
		t.Fatalf("Home: cursor on %q", got)
	}
	m = pressKey(m, tea.KeyMsg{Type: tea.KeyEnd})
	if got := cursorLine(t, m); !strings.Contains(got, "All workspaces") {
		t.Fatalf("End: cursor on %q", got)
	}
}

// TestWelcome_HoverMovesCursorOverRecents: the pointer moves the one
// cursor onto a recent file row too.
func TestWelcome_HoverMovesCursorOverRecents(t *testing.T) {
	m := welcomeModel(t)
	m.View()
	pane := m.editorPaneLayout()
	hits := welcomeRecentHits(pane.W, pane.H)
	if len(hits) < 2 {
		t.Fatalf("recent hits: %+v", hits)
	}
	m = hoverAt(m, pane.X+hits[1].ColStart+4, pane.Top+hits[1].Row)
	if got := cursorLine(t, m); !strings.Contains(got, "file-05.go") {
		t.Fatalf("hover on the 2nd recent row: cursor on %q", got)
	}
}

// TestWelcome_BlurredCursorUsesDimBar: while another pane has the keys
// the cursor row keeps a quieter wash and a TextDimmer bar.
func TestWelcome_BlurredCursorUsesDimBar(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	m := welcomeModel(t)
	invalidateWelcome()
	focused := m.View()
	if !strings.Contains(focused, lgSGR("48", theme.Wash(theme.BgEditor, theme.WashSelectedFocused))) {
		t.Fatal("focused page: no focused cursor wash")
	}
	m.focus = FocusExplorer
	blurred := m.View()
	if strings.Contains(blurred, lgSGR("48", theme.Wash(theme.BgEditor, theme.WashSelectedFocused))) {
		t.Error("explorer has the keys, but the start page cursor still has the focused wash")
	}
	if !strings.Contains(blurred, lgSGR("48", theme.Wash(theme.BgEditor, theme.WashSelected))) {
		t.Error("blurred cursor lost its wash")
	}
}
