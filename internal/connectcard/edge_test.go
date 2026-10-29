package connectcard

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

func waitCard() Card {
	return Card{Title: "Waiting", Rows: []Row{{Kind: Status, Text: "◷ Waiting for GitHub"}},
		Buttons: []Button{{ID: "cancel", Title: "Cancel"}, {ID: "copy", Title: "Copy code"}}}
}

// Spec (CX-5): a waiting step has no lit button: Enter does nothing
// until the user moves; ←/→ then light a button.
func TestCardWaitingStepEnterDoesNothing(t *testing.T) {
	m := New(waitCard())
	if _, c := m.Update(key(tea.KeyEnter)); c != nil {
		t.Fatalf("Enter with nothing lit = %v", run(c))
	}
	m, _ = m.Update(key(tea.KeyLeft))
	if _, c := m.Update(key(tea.KeyEnter)); run(c) != (ButtonMsg{ID: "cancel"}) {
		t.Fatal("← did not light the first button")
	}
	m, _ = m.Update(key(tea.KeyRight))
	if _, c := m.Update(key(tea.KeyEnter)); run(c) != (ButtonMsg{ID: "copy"}) {
		t.Fatal("→ did not move to the next button")
	}
	m, _ = m.Update(key(tea.KeyRight))
	if _, c := m.Update(key(tea.KeyEnter)); run(c) != (ButtonMsg{ID: "copy"}) {
		t.Fatal("→ past the last button did not stay on it")
	}
}

// Spec (CX-5): Tab onto the buttons of a waiting step lights the first.
func TestCardTabLightsFirstButton(t *testing.T) {
	c := waitCard()
	c.Rows = append(c.Rows, Row{Kind: Choice, ID: "a", Options: []string{"x", "y"}})
	m := New(c)
	m, _ = m.Update(key(tea.KeyUp)) // wraps from the choice to the buttons
	if m.FocusedID() != "" {
		t.Fatalf("focus %q, want the buttons", m.FocusedID())
	}
	if _, cmd := m.Update(key(tea.KeyEnter)); run(cmd) != (ButtonMsg{ID: "cancel"}) {
		t.Fatal("the first button is not lit")
	}
	m, _ = m.Update(key(tea.KeyShiftTab))
	if m.FocusedID() != "a" {
		t.Fatalf("shift+tab: focus %q", m.FocusedID())
	}
}

// Spec (CX-5): a card with nothing to focus ignores moves and keys.
func TestCardWithNothingToFocus(t *testing.T) {
	m := New(Card{Title: "Done", Rows: []Row{{Kind: OK, Text: "Connected"}}})
	for _, k := range []tea.KeyMsg{key(tea.KeyTab), key(tea.KeyEnter), key(tea.KeySpace), key(tea.KeyBackspace), runes("q"), key(tea.KeyF1)} {
		var c tea.Cmd
		m, c = m.Update(k)
		if c != nil {
			t.Fatalf("%v gave %v", k, run(c))
		}
	}
	if _, c := m.Update(tea.WindowSizeMsg{}); c != nil {
		t.Fatal("a non-key message gave a command")
	}
	if m.Card().Title != "Done" {
		t.Fatalf("Card() = %+v", m.Card())
	}
	m.SetValue("x")
	if m.Value() != "x" {
		t.Fatalf("Value = %q", m.Value())
	}
}

// Spec (CX-5): Space types a space in the input; Enter on an empty
// input sends nothing; SetValue fills the input.
func TestCardInputSpaceAndEmptyEnter(t *testing.T) {
	m := New(Card{Title: "Name", Rows: []Row{{Kind: Input, ID: "name", Text: "Name:"}},
		Buttons: []Button{{ID: "ok", Title: "OK", Primary: true}}})
	m, _ = m.Update(key(tea.KeySpace))
	if _, c := m.Update(key(tea.KeyEnter)); c != nil {
		t.Fatalf("Enter on a blank input = %v", run(c))
	}
	m.SetValue("my")
	m, _ = m.Update(key(tea.KeySpace))
	m, _ = m.Update(runes("server\n\tx"))
	if m.Value() != "my server  x" {
		t.Fatalf("value %q; newlines and tabs become spaces", m.Value())
	}
	m, _ = m.Update(key(tea.KeyTab)) // to the buttons
	m, _ = m.Update(key(tea.KeySpace))
	if m.Value() != "my server  x" {
		t.Fatalf("Space outside the input typed: %q", m.Value())
	}
}

// Spec (CX-5): a form with no primary button: Enter in an input does
// nothing.
func TestCardFormWithoutPrimary(t *testing.T) {
	m := New(Card{Title: "Form", Form: true, Rows: []Row{{Kind: Input, ID: "a"}}, Buttons: []Button{{ID: "cancel", Title: "Cancel"}}})
	if _, c := m.Update(key(tea.KeyEnter)); c != nil {
		t.Fatalf("Enter = %v", run(c))
	}
}

// Spec (CX-5): success and failure lines show their mark once and wrap
// under it; a long value in the input shows its end after a "…".
func TestCardOKFailAndLongInput(t *testing.T) {
	long := strings.Repeat("word ", 30)
	m := New(Card{Title: "Result", Rows: []Row{
		{Kind: OK, Text: "Connected " + long},
		{Kind: Blank},
		{Kind: Fail, Text: "Failed " + long},
		{Kind: Input, ID: "v"},
	}, Buttons: []Button{{ID: "ok", Title: "OK", Primary: true}}})
	m.SetSize(80, 40)
	m.SetValue(strings.Repeat("a", 100) + "END")
	s := ansi.Strip(m.Box())
	if strings.Count(s, theme.GlyphOK.String()+" Connected") != 1 || strings.Count(s, theme.GlyphFail.String()+" Failed") != 1 {
		t.Fatalf("marks:\n%s", s)
	}
	if strings.Count(s, "word") != 60 {
		t.Fatalf("wrapped text lost words: %d\n%s", strings.Count(s, "word"), s)
	}
	if !strings.Contains(s, "…a") || !strings.Contains(s, "END") {
		t.Fatalf("long input not cut at the front:\n%s", s)
	}
}

// Spec (CX-5): a choice without a label starts at the left; the card is
// never narrower than 36 cells; with no size it draws nothing.
func TestCardChoiceNoLabelAndMinWidth(t *testing.T) {
	m := New(Card{Title: "Pick", Rows: []Row{{Kind: Choice, ID: "c", Options: []string{"one", "two"}}}})
	if m.Box() != "" {
		t.Fatal("a card with no size drew something")
	}
	m.SetSize(20, 24)
	lines := strings.Split(ansi.Strip(m.Box()), "\n")
	if w := ansi.StringWidth(lines[0]); w != 36 {
		t.Fatalf("width %d, want 36", w)
	}
	on := theme.GlyphRadioOn.String() + " one"
	found := false
	for _, l := range lines {
		if strings.Contains(l, "│  "+on) {
			found = true
		}
	}
	if !found {
		t.Fatalf("unlabeled choice:\n%s", strings.Join(lines, "\n"))
	}
}

// Spec (CX-5): only a left press acts; a release or right click does
// nothing; a click on empty space does nothing.
func TestCardMouseIgnoresOtherClicks(t *testing.T) {
	m := New(ghCard())
	m.SetSize(120, 40)
	rs := m.ButtonRects()
	if _, c := m.HandleMouse(rs[0].X+1, rs[0].Y, tea.MouseActionRelease, tea.MouseButtonLeft); c != nil {
		t.Fatal("a release pressed a button")
	}
	if _, c := m.HandleMouse(rs[0].X+1, rs[0].Y, tea.MouseActionPress, tea.MouseButtonRight); c != nil {
		t.Fatal("a right click pressed a button")
	}
	if _, c := m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonLeft); c != nil {
		t.Fatal("a click outside pressed something")
	}
}

// Spec (CX-5): a word longer than the line is cut; a link narrower than
// five cells is just cut at the end.
func TestWrapAndCutMiddle(t *testing.T) {
	got := wrap("ab "+strings.Repeat("x", 12)+" cd", 5)
	want := []string{"ab", "xxxxx", "xxxxx", "xx cd"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("wrap = %q, want %q", got, want)
	}
	if got := wrap("abc", 0); strings.Join(got, "|") != "a|b|c" {
		t.Fatalf("wrap w=0 = %q", got)
	}
	if got := cutMiddle("abcdefgh", 4); got != "abc…" {
		t.Fatalf("cutMiddle = %q", got)
	}
	if got := clamp(5, 0); got != 0 {
		t.Fatalf("clamp on empty = %d", got)
	}
}
