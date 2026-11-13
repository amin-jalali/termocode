package confirm

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

func twoButtons() []Button {
	return []Button{{ID: "yes", Title: "Yes", Style: StylePrimary}, {ID: "no", Title: "No", Style: StyleDestructive}}
}

func sized(m Model) Model {
	m.SetSize(100, 40)
	return m
}

func TestAccessors(t *testing.T) {
	m := New("T", "M", twoButtons())
	if m.Title() != "T" || m.Message() != "M" || m.Init() != nil {
		t.Fatal("accessors")
	}
	if got := m.Buttons(); len(got) != 2 || got[0].ID != "yes" {
		t.Fatalf("buttons %v", got)
	}
}

// WithFocus moves the focus to a valid button and ignores a bad index.
func TestWithFocus(t *testing.T) {
	m := New("T", "M", twoButtons())
	if m.WithFocus(1).cursor != 1 {
		t.Fatal("focus not moved")
	}
	if m.WithFocus(5).cursor != 0 || m.WithFocus(-1).cursor != 0 {
		t.Fatal("bad index moved the focus")
	}
}

// A caption shows centred under the picture, in the body colour.
func TestCaptionUnderPicture(t *testing.T) {
	m := sized(New("Scan", "Open on your phone", twoButtons()).WithPicture([]string{"▀▀▀▀", "▄▄▄▄"}).WithCaption("http://x.test/a"))
	out := ansi.Strip(m.Box())
	pic := strings.Index(out, "▀▀▀▀")
	cap := strings.Index(out, "http://x.test/a")
	if pic < 0 || cap < pic {
		t.Fatalf("caption not under the picture:\n%s", out)
	}
}

// Up, PgDn and PgUp scroll a long text, never past its ends.
func TestScrollKeys(t *testing.T) {
	msg := strings.Repeat("line of text\n", 60)
	m := sized(New("T", msg, twoButtons()).WithScrollText())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.scroll != 8 {
		t.Fatalf("PgDn: %d", m.scroll)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.scroll != 7 {
		t.Fatalf("Up: %d", m.scroll)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.scroll != 0 {
		t.Fatalf("PgUp past the top: %d", m.scroll)
	}
	for i := 0; i < 20; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if m.scroll <= 0 || m.scroll > len(m.fullTextLines()) {
		t.Fatalf("PgDn past the end: %d", m.scroll)
	}
}

// Words wrap at spaces; a space at the edge ends the line; a word longer
// than the line is cut.
func TestFullTextLinesWrapping(t *testing.T) {
	m := sized(New("T", strings.Repeat("x", 120)+"\n"+strings.Repeat("ab ", 40), twoButtons()).WithScrollText())
	lines := m.fullTextLines()
	if len(lines) < 4 {
		t.Fatalf("only %d lines", len(lines))
	}
	for _, l := range lines {
		if strings.HasPrefix(l, " ") || strings.HasSuffix(l, " ") && strings.TrimSpace(l) == "" {
			t.Fatalf("line starts with a space or is blank: %q", l)
		}
	}
	if strings.Join(lines[:2], "")[:20] != strings.Repeat("x", 20) {
		t.Fatal("long word lost")
	}
}

func TestTruncMiddleTiny(t *testing.T) {
	if truncMiddle("abcdef", 1) != "…" || truncMiddle("abcdef", 0) != "…" {
		t.Fatal("tiny width")
	}
	if got := truncMiddle("abcdefghij", 5); got != "ab…ij" {
		t.Fatalf("got %q", got)
	}
}

// A very long message ends in an "…and N more lines" row on a plain card.
func TestLongMessageEndsInMoreLines(t *testing.T) {
	m := sized(New("T", strings.Repeat("word\n", 40), twoButtons()))
	if !strings.Contains(ansi.Strip(m.Box()), "more lines") {
		t.Fatal("no 'more lines' row")
	}
}

// Without a window size nothing is drawn and nothing is clickable.
func TestNoSizeNoDrawing(t *testing.T) {
	m := New("T", "M", twoButtons())
	if m.View() != "" || m.Box() != "" || m.ButtonRects() != nil {
		t.Fatal("drew without a size")
	}
	if x, y, w, h := m.Bounds(); x+y+w+h != 0 {
		t.Fatal("bounds without a size")
	}
	if _, cmd, inside := m.HandleMouse(1, 1, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil || inside {
		t.Fatal("click without a size")
	}
	if m.ButtonAt(0, 0) != "" {
		t.Fatal("button without a size")
	}
}

// A narrow window still gets the minimum card width and a card that is
// pushed to the corner never has a negative origin.
func TestTinyWindow(t *testing.T) {
	m := New("T", "M", twoButtons())
	m.SetSize(20, 6)
	x, y, w, _ := m.Bounds()
	if x < 0 || y < 0 || w < 20 {
		t.Fatalf("bounds %d,%d w=%d", x, y, w)
	}
	if m.Box() == "" {
		t.Fatal("no box")
	}
}

// Clicks that are not a left press do nothing; ButtonAt finds the id.
func TestHandleMouseOtherButtonsAndButtonAt(t *testing.T) {
	m := sized(New("T", "M", twoButtons()))
	rects := m.ButtonRects()
	r := rects[1]
	if _, cmd, inside := m.HandleMouse(r.X, r.Y, tea.MouseActionPress, tea.MouseButtonRight); cmd != nil || !inside {
		t.Fatal("right click picked a button")
	}
	if _, cmd, _ := m.HandleMouse(r.X, r.Y, tea.MouseActionRelease, tea.MouseButtonLeft); cmd != nil {
		t.Fatal("release picked a button")
	}
	if m.ButtonAt(r.X, r.Y) != "no" || m.ButtonAt(0, 0) != "" {
		t.Fatal("ButtonAt")
	}
}

// Header icon and button looks per icon mode and style.
func TestBoxIconModesAndButtonStyles(t *testing.T) {
	old := theme.CurrentIconMode
	t.Cleanup(func() { theme.CurrentIconMode = old })
	theme.CurrentIconMode = theme.IconModeNerdFont
	m := sized(New("T", "M", []Button{{ID: "a", Title: "A", Style: StyleDestructive}, {ID: "b", Title: "B", Style: StylePrimary}, {ID: "c", Title: "C"}}))
	if !strings.Contains(m.Box(), "") {
		t.Fatal("no nerd warning icon")
	}
	theme.CurrentIconMode = theme.IconModeASCII
	if !strings.Contains(ansi.Strip(m.Box()), "!") {
		t.Fatal("no ascii warning icon")
	}
	for _, active := range []bool{true, false} {
		for _, b := range m.buttons {
			if m.renderButton(b, active) == "" {
				t.Fatal("empty button")
			}
		}
	}
}

// The exact command at the end of a message is drawn after the plain words.
func TestCommandLineDrawnLast(t *testing.T) {
	m := sized(New("T", "Plain words here\nCommand: rm -rf x", twoButtons()))
	out := ansi.Strip(m.Box())
	if strings.Index(out, "Plain words") > strings.Index(out, "Command: rm") {
		t.Fatal("command before the words")
	}
}

// A key table row with an empty What still lays out one line.
func TestKeyTableEmptyDescription(t *testing.T) {
	m := sized(New("Keys", "", twoButtons()).WithKeys([]KeyRow{{Key: "Ctrl+X"}, {Key: "", What: "Group"}, {Key: "F1", What: "help"}}))
	lines := m.keyTableLines(60)
	if len(lines) < 3 {
		t.Fatalf("lines %v", lines)
	}
}
