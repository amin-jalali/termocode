package connectcard

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func formCard(withURL bool) Card {
	rows := []Row{
		{Kind: Input, ID: "name", Text: "Name", Inline: true, Value: "gpt"},
		{Kind: Choice, ID: "kind", Text: "Provider", Options: []string{"openai", "compatible"}},
		{Kind: Input, ID: "model", Text: "Model", Inline: true, Placeholder: "empty = gpt-5-mini"},
	}
	if withURL {
		rows = append(rows, Row{Kind: Input, ID: "url", Text: "Base URL", Inline: true})
	}
	return Card{Title: "Add model slot", Rows: rows, Form: true,
		Buttons: []Button{{ID: "cancel", Title: "Cancel"}, {ID: "save", Title: "Save", Primary: true}}}
}

// Spec: a form card has one value per input; typing goes to the focused
// one; Value opens from Row.Value; Enter in any input (even empty) presses
// the primary button.
func TestFormInputs(t *testing.T) {
	m := New(formCard(false))
	m.SetSize(80, 24)
	if m.FocusedID() != "name" || m.InputValue("name") != "gpt" {
		t.Fatalf("focus %q name %q", m.FocusedID(), m.InputValue("name"))
	}
	m, _ = m.Update(runes("4"))
	m, _ = m.Update(key(tea.KeyTab)) // the choice
	m, _ = m.Update(key(tea.KeyTab)) // model
	if m.FocusedID() != "model" {
		t.Fatalf("focus %q", m.FocusedID())
	}
	_, cmd := m.Update(key(tea.KeyEnter))
	if b, ok := run(cmd).(ButtonMsg); !ok || b.ID != "save" {
		t.Errorf("Enter on an empty form input: %v", run(cmd))
	}
	m, _ = m.Update(runes("o3"))
	m, _ = m.Update(key(tea.KeyBackspace))
	if m.InputValue("name") != "gpt4" || m.InputValue("model") != "o" || m.Value() != "o" {
		t.Errorf("name %q model %q", m.InputValue("name"), m.InputValue("model"))
	}
	f := ansi.Strip(m.Box())
	if !strings.Contains(f, "Name") || !strings.Contains(f, "gpt4") {
		t.Errorf("frame:\n%s", f)
	}
}

// Spec: the placeholder shows while the field is empty.
func TestFormPlaceholder(t *testing.T) {
	m := New(formCard(false))
	m.SetSize(80, 24)
	if f := ansi.Strip(m.Box()); !strings.Contains(f, "empty = gpt-5-mini") {
		t.Errorf("no placeholder:\n%s", f)
	}
	m.SetInputValue("model", "x")
	if f := ansi.Strip(m.Box()); strings.Contains(f, "empty = gpt-5-mini") {
		t.Error("the placeholder stays over a value")
	}
}

// Spec: a form that grows a row (same title) keeps the values and the
// focused row; a row that goes away forgets its text.
func TestFormSetCardKeepsFocusAndValues(t *testing.T) {
	m := New(formCard(false))
	m.SetSize(80, 24)
	m, _ = m.Update(key(tea.KeyTab))
	m, _ = m.Update(key(tea.KeyRight)) // compatible
	m.SetCard(formCard(true))
	if m.FocusedID() != "kind" || m.Choice("kind") != 1 || m.InputValue("name") != "gpt" {
		t.Errorf("focus %q kind %d name %q", m.FocusedID(), m.Choice("kind"), m.InputValue("name"))
	}
	m.SetInputValue("url", "http://x")
	m.SetCard(formCard(false))
	m.SetCard(formCard(true))
	if m.InputValue("url") != "" {
		t.Error("a row that went away kept its text")
	}
}

// Spec: a click on an input focuses it.
func TestFormClickFocusesInput(t *testing.T) {
	m := New(formCard(false))
	m.SetSize(80, 24)
	ox, oy := m.Origin()
	_, tg := m.lines()
	for _, x := range tg {
		if x.option == -1 && m.card.Rows[x.row].ID == "model" {
			m, _ = m.HandleMouse(ox+1+x.x+2, oy+1+x.y, tea.MouseActionPress, tea.MouseButtonLeft)
		}
	}
	if m.FocusedID() != "model" {
		t.Errorf("focus %q", m.FocusedID())
	}
}

// Spec: a step that brings an input back (the error card's Try again to
// the paste screen, same title) puts the focus in the input, not on the
// buttons, so the user can paste at once.
func TestSetCardNewInputTakesFocus(t *testing.T) {
	paste := Card{Title: "Connect Linear", Rows: []Row{{Kind: Input, ID: "url", Text: "Paste it here"}},
		Buttons: []Button{{ID: "connect", Title: "Connect", Primary: true}, {ID: "cancel", Title: "Cancel"}}}
	errCard := Card{Title: "Connect Linear", Rows: []Row{{Kind: Text, Text: "That address belongs to another sign-in."}},
		Buttons: []Button{{ID: "retry", Title: "Try again", Primary: true}, {ID: "cancel", Title: "Cancel"}}}
	m := New(paste)
	m.SetCard(errCard)
	if m.FocusedID() != "" {
		t.Fatalf("error card: focus on %q, want the buttons", m.FocusedID())
	}
	m.SetCard(paste)
	if m.FocusedID() != "url" {
		t.Errorf("back on the paste screen: focus on %q, want the input", m.FocusedID())
	}
}
