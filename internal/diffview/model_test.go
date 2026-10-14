package diffview

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func key(m Model, k string) (Model, tea.Msg) {
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	m, cmd := m.Update(msg)
	if cmd == nil {
		return m, nil
	}
	return m, cmd()
}

func plain(m Model) string { return ansi.Strip(m.Box()) }

const oldText = "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\n"
const newText = "a\nB\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nM\n"

func demo(review bool) Model {
	m := New("Review AI changes", []File{{Name: "x.txt", Old: oldText, New: newText}}, review)
	m.SetSize(120, 30)
	return m
}

// Spec: the card shows the file, the stats, both changes with Accept /
// Reject, and folds the unchanged middle into a band.
func TestRenderShowsChangesAndFold(t *testing.T) {
	s := plain(demo(true))
	for _, want := range []string{"Review AI changes", "x.txt", "+2", "−2", "Change 1 of 2", "Change 2 of 2",
		"Accept", "Reject", "unchanged lines", "Accept file", "Reject file", "Apply selected"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	for i, l := range strings.Split(s, "\n") {
		if w := ansi.StringWidth(l); w != 116 {
			t.Errorf("row %d is %d cells wide, want 116: %q", i, w, l)
		}
	}
	if n := len(strings.Split(s, "\n")); n != 28 {
		t.Errorf("%d rows, want 28", n)
	}
}

// Spec: a / r decide the focused change; deciding the last one finishes
// the file with exactly the accepted changes.
func TestDecideEachChangeFinishes(t *testing.T) {
	m := demo(true)
	m, msg := key(m, "a") // change 1 accepted, focus moves to change 2
	if msg != nil {
		t.Fatalf("finished early: %#v", msg)
	}
	if d := m.Decisions(0); d[0] != Accepted || d[1] != Pending {
		t.Fatalf("decisions %v", d)
	}
	m, msg = key(m, "r")
	fin, ok := msg.(FinishMsg)
	if !ok {
		t.Fatalf("no FinishMsg: %#v", msg)
	}
	if fin.File != 0 || !fin.Accepted[0] || fin.Accepted[1] || fin.Edit {
		t.Errorf("finish %+v", fin)
	}
	if got := m.Diff(0).Apply(fin.Accepted); got != "a\nB\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\n" {
		t.Errorf("applied %q", got)
	}
}

// Spec: pressing a twice undoes the decision.
func TestToggleBackToPending(t *testing.T) {
	m := demo(true)
	m, _ = key(m, "a")
	m, _ = key(m, "p") // back to change 1
	m, _ = key(m, "a")
	if d := m.Decisions(0); d[0] != Pending {
		t.Errorf("decisions %v", d)
	}
}

func TestWholeFileKeys(t *testing.T) {
	for k, want := range map[string]int{"A": 2, "R": 0} {
		_, msg := key(demo(true), k)
		fin, ok := msg.(FinishMsg)
		if !ok || len(fin.Accepted) != want {
			t.Errorf("%s: %#v", k, msg)
		}
	}
	// enter: pending changes count as not accepted.
	m, _ := key(demo(true), "a")
	_, msg := key(m, "enter")
	if fin, ok := msg.(FinishMsg); !ok || len(fin.Accepted) != 1 || !fin.Accepted[0] {
		t.Errorf("enter: %#v", msg)
	}
	// e with nothing marked takes everything and asks for the editor.
	_, msg = key(demo(true), "e")
	if fin, ok := msg.(FinishMsg); !ok || len(fin.Accepted) != 2 || !fin.Edit {
		t.Errorf("e: %#v", msg)
	}
	if _, msg := key(demo(true), "esc"); msg != (HideMsg{}) {
		t.Errorf("esc: %#v", msg)
	}
}

// Spec: a read-only card has no decisions and Enter closes it.
func TestReadOnly(t *testing.T) {
	m := demo(false)
	if s := plain(m); strings.Contains(s, "Accept") || !strings.Contains(s, "Close") {
		t.Errorf("read-only card:\n%s", s)
	}
	if _, msg := key(m, "a"); msg != nil {
		t.Errorf("a: %#v", msg)
	}
	if _, msg := key(m, "enter"); msg != (HideMsg{}) {
		t.Errorf("enter: %#v", msg)
	}
}

// findText returns the screen cell of the first occurrence of s.
func findText(t *testing.T, m Model, s string) (int, int) {
	t.Helper()
	x0, y0 := m.Origin()
	for y, l := range strings.Split(plain(m), "\n") {
		if i := strings.Index(l, s); i >= 0 {
			return x0 + ansi.StringWidth(l[:i]) + 1, y0 + y
		}
	}
	t.Fatalf("%q not on the card:\n%s", s, plain(m))
	return 0, 0
}

func click(m Model, x, y int) (Model, tea.Msg) {
	m, cmd, _ := m.HandleMouse(x, y, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd == nil {
		return m, nil
	}
	return m, cmd()
}

// Spec: the hunk buttons, the footer buttons, the fold band and the view
// switch work with the mouse.
func TestMouse(t *testing.T) {
	m := demo(true)
	x, y := findText(t, m, icons().check+" Accept ")
	if id := m.ButtonAt(x, y); id != "hacc:0" {
		t.Fatalf("button at Accept: %q", id)
	}
	m, _ = click(m, x, y)
	if d := m.Decisions(0); d[0] != Accepted {
		t.Fatalf("decisions %v", d)
	}
	x, y = findText(t, m, "unchanged lines")
	m, _ = click(m, x, y)
	if strings.Contains(plain(m), "unchanged lines") {
		t.Error("band did not unfold")
	}
	x, y = findText(t, m, " Split ")
	m, _ = click(m, x, y)
	if !m.splitNow() {
		t.Error("Split did not switch")
	}
	x, y = findText(t, m, "Reject file")
	if _, msg := click(m, x, y); msg == nil || len(msg.(FinishMsg).Accepted) != 0 {
		t.Errorf("Reject file: %#v", msg)
	}
}

// Spec: with several files a list shows them; MarkDone moves to the next
// file and locks the done one.
func TestFilesList(t *testing.T) {
	m := New("Review AI changes", DemoFiles(), true)
	m.SetSize(150, 40)
	s := plain(m)
	if !strings.Contains(s, "FILES  2") || !strings.Contains(s, "model_tes") {
		t.Fatalf("no file list:\n%s", s)
	}
	m.MarkDone(0, "Applied 4 of 4 changes")
	if m.Current() != 1 || m.Pending() != 1 || !m.Done(0) {
		t.Errorf("current %d pending %d", m.Current(), m.Pending())
	}
	m, _ = key(m, "tab")
	if m.Current() != 0 || !strings.Contains(plain(m), "Applied 4 of 4 changes") {
		t.Errorf("done file:\n%s", plain(m))
	}
	if _, msg := key(m, "A"); msg != nil {
		t.Errorf("a done file finished again: %#v", msg)
	}
}

// Spec: a change of only the final newline can still be accepted.
func TestEOLOnly(t *testing.T) {
	m := New("t", []File{{Name: "x", Old: "a\n", New: "a"}}, true)
	m.SetSize(100, 20)
	if !strings.Contains(plain(m), "Only the final newline differs") {
		t.Errorf("%s", plain(m))
	}
	_, msg := key(m, "a")
	if fin, ok := msg.(FinishMsg); !ok || !fin.Accepted[0] {
		t.Errorf("%#v", msg)
	}
}

// Spec: tiny windows never panic.
func TestTinySizes(t *testing.T) {
	for _, sz := range [][2]int{{0, 0}, {10, 5}, {30, 8}, {41, 11}} {
		m := New("t", DemoFiles(), true)
		m.SetSize(sz[0], sz[1])
		_ = m.Box()
		m, _ = key(m, "n")
		m.HandleMouse(1, 1, tea.MouseActionPress, tea.MouseButtonLeft)
	}
}
