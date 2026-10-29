package rebaseview

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/git"
)

func todo3() git.RebaseTodo {
	var t git.RebaseTodo
	for _, n := range []string{"a", "b", "c"} {
		t.Items = append(t.Items, git.RebaseItem{Action: git.RebasePick, Hash: n + "111111", FullHash: n, Subject: "subject " + n})
	}
	return t
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "alt+down":
		return tea.KeyMsg{Type: tea.KeyDown, Alt: true}
	case "alt+up":
		return tea.KeyMsg{Type: tea.KeyUp, Alt: true}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// press sends keys and returns the model and the last message emitted.
func press(m Model, keys ...string) (Model, tea.Msg) {
	var out tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(key(k))
		if cmd != nil {
			out = cmd()
		}
	}
	return m, out
}

func order(m Model) string {
	var s []string
	for _, it := range m.Todo().Items {
		s = append(s, it.FullHash+":"+string(it.Action))
	}
	return strings.Join(s, " ")
}

// G-RBS: p r e s f d set the row's action; j/k move the cursor; J/K and
// Alt+Up/Down move the row (the cursor follows it).
func TestKeysSetActionsAndReorder(t *testing.T) {
	m := New("Rebase", todo3())
	m.SetSize(100, 30)
	m, _ = press(m, "j", "s", "j", "f")
	if got := order(m); got != "a:pick b:squash c:fixup" {
		t.Fatalf("actions %q", got)
	}
	m, _ = press(m, "e", "d", "p")
	if got := order(m); got != "a:pick b:squash c:pick" {
		t.Fatalf("last key wins %q", got)
	}
	m, _ = press(m, "K")
	if got := order(m); got != "a:pick c:pick b:squash" || m.Cursor() != 1 {
		t.Fatalf("K: %q cursor %d", order(m), m.Cursor())
	}
	m, _ = press(m, "alt+up")
	if got := order(m); got != "c:pick a:pick b:squash" || m.Cursor() != 0 {
		t.Fatalf("alt+up: %q cursor %d", order(m), m.Cursor())
	}
	m, _ = press(m, "J", "alt+down")
	if got := order(m); got != "a:pick b:squash c:pick" || m.Cursor() != 2 {
		t.Fatalf("J, alt+down: %q cursor %d", order(m), m.Cursor())
	}
	m, _ = press(m, "k", "k", "k", "down")
	if m.Cursor() != 1 {
		t.Fatalf("cursor %d", m.Cursor())
	}
}

// G-RBS: the card shows each row and the preview line ("3 commits → 2
// commits"); an invalid todo shows why and Enter does not start it.
func TestPreviewAndStart(t *testing.T) {
	m := New("Rebase main", todo3())
	m.SetSize(100, 30)
	m, _ = press(m, "j", "f")
	box := ansi.Strip(m.Box())
	for _, want := range []string{"Rebase main", "pick", "fixup", "subject b", "3 commits → 2 commits"} {
		if !strings.Contains(box, want) {
			t.Errorf("box lacks %q:\n%s", want, box)
		}
	}
	_, msg := press(m, "enter")
	start, ok := msg.(StartMsg)
	if !ok || start.Todo.Items[1].Action != git.RebaseFixup {
		t.Fatalf("enter gives %#v", msg)
	}
	m, _ = press(m, "k", "f")
	m, msg = press(m, "enter")
	if msg != nil {
		t.Fatalf("invalid todo started: %#v", msg)
	}
	if box := ansi.Strip(m.Box()); !strings.Contains(box, "nothing above") {
		t.Errorf("no reason shown:\n%s", box)
	}
	if _, msg := press(m, "esc"); msg != (CloseMsg{}) {
		t.Errorf("esc gives %#v", msg)
	}
}

// G-RBS: r marks the row reword and asks the app for its message; the new
// message shows on the row.
func TestReword(t *testing.T) {
	m := New("Rebase", todo3())
	m.SetSize(100, 30)
	m, msg := press(m, "j", "r")
	rw, ok := msg.(RewordMsg)
	if !ok || rw.Index != 1 || rw.Item.FullHash != "b" {
		t.Fatalf("r gives %#v", msg)
	}
	m.SetMessage(1, "better subject\n\nbody")
	if it := m.Todo().Items[1]; it.Action != git.RebaseReword || it.Message != "better subject\n\nbody" {
		t.Fatalf("row %+v", it)
	}
	if box := ansi.Strip(m.Box()); !strings.Contains(box, "better subject") {
		t.Errorf("new subject not shown:\n%s", box)
	}
}

// G-RBS: a click on a row puts the cursor on it; the wheel moves it.
func TestMouse(t *testing.T) {
	m := New("Rebase", todo3())
	m.SetSize(100, 30)
	ox, oy := m.Origin()
	box := strings.Split(ansi.Strip(m.Box()), "\n")
	row := -1
	for i, l := range box {
		if strings.Contains(l, "subject c") {
			row = i
		}
	}
	m = m.HandleMouse(ox+5, oy+row, tea.MouseActionPress, tea.MouseButtonLeft)
	if m.Cursor() != 2 {
		t.Fatalf("click: cursor %d", m.Cursor())
	}
	m = m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelUp)
	if m.Cursor() != 1 {
		t.Fatalf("wheel: cursor %d", m.Cursor())
	}
	m = m.HandleMouse(ox+5, oy, tea.MouseActionPress, tea.MouseButtonLeft) // the title
	if m.Cursor() != 1 {
		t.Fatalf("title click moved the cursor to %d", m.Cursor())
	}
}
