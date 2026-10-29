package diffview

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// longFiles: a 300-line file with changes near the top, the middle and the
// end, plus a second file. Lines are long so the view can scroll sideways.
func longFiles() []File {
	var o, n strings.Builder
	for i := 1; i <= 300; i++ {
		line := fmt.Sprintf("line %03d %s end%03d", i, strings.Repeat("·", 120), i)
		fmt.Fprintln(&o, line)
		switch i {
		case 2, 150, 299:
			line = "CHANGED " + line
		}
		fmt.Fprintln(&n, line)
	}
	return []File{{Name: "long.txt", Old: o.String(), New: n.String()}, {Name: "two.txt", Old: "a\n", New: "b\n"}}
}

func longCard(review bool) Model {
	m := New("Review", longFiles(), review)
	m.SetSize(100, 20)
	m.Action("expand") // show every line, no fold bands
	return m
}

func keyT(m Model, t tea.KeyType) (Model, tea.Msg) {
	m, cmd := m.Update(tea.KeyMsg{Type: t})
	if cmd == nil {
		return m, nil
	}
	return m, cmd()
}

func TestScrollKeys(t *testing.T) {
	m := longCard(false)
	top := plain(m)
	if !strings.Contains(top, "line 001") {
		t.Fatalf("top view has no first line:\n%s", top)
	}
	if m2, _ := keyT(m, tea.KeyUp); plain(m2) != top {
		t.Error("up at the top moved the view")
	}
	m, _ = key(m, "j")
	if s := plain(m); s == top || strings.Contains(s, "line 001") {
		t.Errorf("j did not scroll one line:\n%s", s)
	}
	m, _ = key(m, "k")
	if plain(m) != top {
		t.Error("k did not scroll back")
	}
	m, _ = keyT(m, tea.KeyPgDown)
	if s := plain(m); strings.Contains(s, "line 005 ") {
		t.Errorf("page down still shows line 5:\n%s", s)
	}
	m, _ = keyT(m, tea.KeyPgUp)
	if plain(m) != top {
		t.Error("page up did not come back to the top")
	}
	m, _ = key(m, "G")
	end := plain(m)
	if !strings.Contains(end, "line 300") || strings.Contains(end, "line 001") {
		t.Errorf("G did not go to the end:\n%s", end)
	}
	if m2, _ := keyT(m, tea.KeyDown); plain(m2) != end {
		t.Error("down at the end moved past the last line")
	}
	m, _ = keyT(m, tea.KeyHome)
	if plain(m) != top {
		t.Error("home did not go to the top")
	}
	m, _ = keyT(m, tea.KeyEnd)
	if plain(m) != end {
		t.Error("end did not go to the end")
	}
}

func TestSideScrollKeysAndBounds(t *testing.T) {
	m := longCard(false)
	left := plain(m)
	if m2, _ := key(m, "h"); plain(m2) != left {
		t.Error("h at the left edge moved the view")
	}
	m, _ = key(m, "l")
	if s := plain(m); s == left || strings.Contains(s, "line 001") {
		t.Errorf("l did not scroll right:\n%s", s)
	}
	m, _ = keyT(m, tea.KeyLeft)
	if plain(m) != left {
		t.Error("left did not scroll back")
	}
	for i := 0; i < 50; i++ {
		m, _ = keyT(m, tea.KeyRight)
	}
	far := plain(m)
	m, _ = key(m, "l")
	if plain(m) != far {
		t.Error("side scroll has no right bound")
	}
}

func TestMouseWheel(t *testing.T) {
	m := longCard(false)
	top := plain(m)
	x, y := m.Origin()
	x, y = x+5, y+5
	m, _, in := m.HandleMouse(x, y, tea.MouseActionPress, tea.MouseButtonWheelDown)
	if !in || plain(m) == top {
		t.Fatal("wheel down did not scroll")
	}
	m, _, _ = m.HandleMouse(x, y, tea.MouseActionPress, tea.MouseButtonWheelUp)
	if plain(m) != top {
		t.Error("wheel up did not scroll back")
	}
	m, _, _ = m.HandleMouse(x, y, tea.MouseActionPress, tea.MouseButtonWheelRight)
	if plain(m) == top {
		t.Error("wheel right did not scroll sideways")
	}
	m, _, _ = m.HandleMouse(x, y, tea.MouseActionPress, tea.MouseButtonWheelLeft)
	if plain(m) != top {
		t.Error("wheel left did not scroll back")
	}
	if _, cmd, in := m.HandleMouse(x, y, tea.MouseActionPress, tea.MouseButtonRight); !in || cmd != nil {
		t.Errorf("right click: inside %v cmd %v, want inside and nothing done", in, cmd)
	}
	if _, _, in := m.HandleMouse(-50, -50, tea.MouseActionPress, tea.MouseButtonLeft); in {
		t.Error("a click far outside the card counted as inside")
	}
}

// z folds and unfolds every unchanged run.
func TestExpandAllToggle(t *testing.T) {
	m := New("Review", longFiles(), false)
	m.SetSize(100, 40)
	if !strings.Contains(plain(m), "unchanged lines") {
		t.Fatal("no fold band at start")
	}
	m, _ = key(m, "z")
	if strings.Contains(plain(m), "unchanged lines") {
		t.Error("z did not unfold")
	}
	m, _ = key(m, "z")
	if !strings.Contains(plain(m), "unchanged lines") {
		t.Error("z again did not fold")
	}
}

func TestFileStepping(t *testing.T) {
	m := longCard(false)
	m, _ = keyT(m, tea.KeyShiftTab)
	if m.Current() != 1 {
		t.Errorf("shift+tab from the first file: current %d, want the last", m.Current())
	}
	m.Action("prev-file")
	if m.Current() != 0 {
		t.Errorf("prev-file: current %d", m.Current())
	}
	m.Action("next-file")
	if m.Current() != 1 {
		t.Errorf("next-file: current %d", m.Current())
	}
	one := New("t", []File{{Name: "x", Old: "a\n", New: "b\n"}}, false)
	if cmd := one.Action("next-file"); cmd != nil || one.Current() != 0 {
		t.Error("stepping with one file did something")
	}
}

func TestJumpBetweenChanges(t *testing.T) {
	m := longCard(true)
	m.Action("next") // change 2, in the middle
	if s := plain(m); !strings.Contains(s, "CHANGED line 150") {
		t.Errorf("next did not scroll to change 2:\n%s", s)
	}
	m.Action("prev")
	m.Action("prev") // wraps to the last change
	if s := plain(m); !strings.Contains(s, "CHANGED line 299") {
		t.Errorf("prev from the first change did not wrap to the last:\n%s", s)
	}
}

func TestModeActions(t *testing.T) {
	m := longCard(false)
	m.SetSize(200, 30)
	m.Action("mode-unified")
	if m.splitNow() {
		t.Error("mode-unified left the split view")
	}
	m.Action("mode")
	if !m.splitNow() {
		t.Error("mode from unified did not split")
	}
	m.Action("mode-split")
	if !m.splitNow() {
		t.Error("mode-split not split")
	}
}

// A later proposal joins the open review as a new file.
func TestAddFileAndDecisions(t *testing.T) {
	m := demo(true)
	if !m.Review() || m.Files() != 1 {
		t.Fatalf("review %v files %d", m.Review(), m.Files())
	}
	i := m.AddFile(File{Name: "y.txt", Old: "1\n", New: "2\n"})
	if i != 1 || m.Files() != 2 || m.Pending() != 2 {
		t.Fatalf("AddFile = %d, files %d pending %d", i, m.Files(), m.Pending())
	}
	if m.AllDecided() {
		t.Error("nothing decided yet")
	}
	m.SetDecision(0, Accepted)
	m.SetDecision(1, Rejected)
	m.SetDecision(5, Accepted)  // no such change: ignored
	m.SetDecision(-1, Rejected) // ignored
	if d := m.Decisions(0); d[0] != Accepted || d[1] != Rejected {
		t.Errorf("decisions %v", d)
	}
	if !m.AllDecided() {
		t.Error("both changes decided, AllDecided false")
	}
	m.MarkDone(9, "x") // out of range: ignored
	m.MarkDone(-1, "x")
	if m.Pending() != 2 {
		t.Errorf("MarkDone out of range changed pending to %d", m.Pending())
	}
	m.MarkDone(0, "done")
	m.SetDecision(0, Pending) // a done file is locked
	if d := m.Decisions(0); d[0] != Accepted {
		t.Errorf("decision changed on a done file: %v", d)
	}
	if m.Current() != 1 {
		t.Fatalf("MarkDone did not move to the next file: %d", m.Current())
	}
	m.Select(0)
	if cmd := m.Action("accept"); cmd != nil {
		t.Error("accept on a finished review file did something")
	}
	empty := New("t", nil, true)
	empty.SetSize(80, 20)
	if empty.AllDecided() {
		t.Error("a card with no file is not all decided")
	}
	empty.Scroll(3)
	empty.Action("next")
	empty.Action("expand")
	if s := plain(empty); s != "" {
		t.Errorf("a card with no file draws %q, want nothing", s)
	}
}

func TestUpdateIgnoresOtherMessages(t *testing.T) {
	m := demo(true)
	before := plain(m)
	m, cmd := m.Update(tea.WindowSizeMsg{Width: 10, Height: 10})
	if cmd != nil || plain(m) != before {
		t.Error("a non-key message changed the card")
	}
	if m.Init() != nil {
		t.Error("Init returned a command")
	}
}

// Review keys do nothing in a commit card.
func TestCommitCardIgnoresReviewKeys(t *testing.T) {
	for _, k := range []string{"x", "a", "A", "R", "e"} {
		if _, msg := key(commitCard(150, 42), k); msg != nil {
			t.Errorf("%s in a commit card: %#v", k, msg)
		}
	}
}

func TestClickFileAndHunkIDs(t *testing.T) {
	m := longCard(true)
	m.click("hunk:2")
	if m.files[0].focus != 2 {
		t.Errorf("hunk:2 focus %d", m.files[0].focus)
	}
	m.click("hunk:99") // no such change: focus stays
	if m.files[0].focus != 2 {
		t.Errorf("hunk:99 moved focus to %d", m.files[0].focus)
	}
	m.click("file:1")
	if m.Current() != 1 {
		t.Errorf("file:1 current %d", m.Current())
	}
	if m.click("file:x1") != nil || m.Current() != 1 {
		t.Error("a bad id did something")
	}
}
