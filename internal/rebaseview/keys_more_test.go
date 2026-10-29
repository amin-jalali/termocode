package rebaseview

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/git"
)

func TestHomeEndUpAndQuit(t *testing.T) {
	m := New("Rebase", todo3())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if m.Cursor() != 2 {
		t.Errorf("end: cursor %d", m.Cursor())
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.Cursor() != 1 {
		t.Errorf("up: cursor %d", m.Cursor())
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if m.Cursor() != 0 {
		t.Errorf("home: cursor %d", m.Cursor())
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.Cursor() != 0 {
		t.Errorf("up at the top: cursor %d", m.Cursor())
	}
	if _, msg := press(m, "q"); msg != (CloseMsg{}) {
		t.Errorf("q: %#v", msg)
	}
}

// Keys that mean nothing here change nothing (Alt+letter, pasted text,
// unknown letters, other messages).
func TestIgnoredInput(t *testing.T) {
	m := New("Rebase", todo3())
	before := order(m)
	for _, msg := range []tea.Msg{
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d"), Alt: true},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("dd")},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")},
		tea.WindowSizeMsg{Width: 10, Height: 10},
	} {
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		if cmd != nil || order(m) != before {
			t.Errorf("%#v changed the todo to %s", msg, order(m))
		}
	}
	m.SetMessage(9, "x")
	m.SetMessage(-1, "x")
	if order(m) != before {
		t.Errorf("SetMessage out of range changed the todo: %s", order(m))
	}
}

// A dropped row and a long subject still fit the card width.
func TestLongSubjectIsCut(t *testing.T) {
	td := todo3()
	td.Items[1].Subject = strings.Repeat("very long subject ", 20)
	m := New("Rebase", td)
	m.SetSize(60, 30)
	m, _ = press(m, "j", "d")
	box := strings.Split(ansi.Strip(m.Box()), "\n")
	w := ansi.StringWidth(box[0])
	found := false
	for _, l := range box {
		if ansi.StringWidth(l) != w {
			t.Errorf("row %q is %d wide, want %d", l, ansi.StringWidth(l), w)
		}
		if strings.Contains(l, "drop") && strings.Contains(l, "…") {
			found = true
		}
	}
	if !found {
		t.Errorf("no cut dropped row:\n%s", strings.Join(box, "\n"))
	}
	m.SetSize(0, 0)
	if m.Box() != "" {
		t.Error("a zero-size card draws something")
	}
}

// In a long todo the list scrolls with the cursor; a click maps to the
// row shown there, and only a left press moves the cursor.
func TestMouseOnScrolledList(t *testing.T) {
	var td git.RebaseTodo
	for i := 0; i < 40; i++ {
		td.Items = append(td.Items, git.RebaseItem{Action: git.RebasePick, Hash: "h", FullHash: string(rune('A' + i)), Subject: "row " + string(rune('A'+i))})
	}
	m := New("Rebase", td)
	m.SetSize(100, 20)
	for i := 0; i < 30; i++ {
		m = m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelDown)
	}
	if m.Cursor() != 30 {
		t.Fatalf("wheel down: cursor %d", m.Cursor())
	}
	ox, oy := m.Origin()
	box := strings.Split(ansi.Strip(m.Box()), "\n")
	row := -1
	for i, l := range box {
		if strings.Contains(l, "row Z") { // item 25
			row = i
		}
	}
	if row < 0 {
		t.Fatalf("row Z not shown:\n%s", strings.Join(box, "\n"))
	}
	if got := m.HandleMouse(ox+5, oy+row, tea.MouseActionPress, tea.MouseButtonRight); got.Cursor() != 30 {
		t.Errorf("right click moved the cursor to %d", got.Cursor())
	}
	m = m.HandleMouse(ox+5, oy+row, tea.MouseActionPress, tea.MouseButtonLeft)
	if m.Cursor() != 25 {
		t.Errorf("click on row Z: cursor %d, want 25", m.Cursor())
	}
}
