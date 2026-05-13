package replacebar

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewStartsOnFindField(t *testing.T) {
	m := New()
	if m.focus != fieldFind {
		t.Fatalf("default focus must be fieldFind")
	}
	if m.Find() != "" || m.Replace() != "" {
		t.Fatalf("new model must have empty fields")
	}
}

func TestUpdateTypingWritesToFocusedField(t *testing.T) {
	m := New()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("foo")})
	if m.Find() != "foo" {
		t.Fatalf("find want 'foo', got %q", m.Find())
	}
	if m.Replace() != "" {
		t.Fatalf("replace must remain empty: got %q", m.Replace())
	}

	// Tab → focus replace.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != fieldReplace {
		t.Fatalf("Tab should focus replace")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bar")})
	if m.Replace() != "bar" {
		t.Fatalf("replace want 'bar', got %q", m.Replace())
	}
	if m.Find() != "foo" {
		t.Fatalf("find should be unchanged: got %q", m.Find())
	}
}

func TestUpdateShiftTabCycles(t *testing.T) {
	m := New()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focus != fieldReplace {
		t.Fatalf("ShiftTab from find should land on replace")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focus != fieldFind {
		t.Fatalf("ShiftTab from replace should land on find")
	}
}

func TestUpdateBackspaceRemovesFromFocused(t *testing.T) {
	m := New()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ab")})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.Find() != "a" {
		t.Fatalf("after backspace find want 'a', got %q", m.Find())
	}
}

func TestUpdateEnterEmitsReplaceNext(t *testing.T) {
	m := New()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter expected to emit ReplaceNextMsg")
	}
	msg, ok := cmd().(ReplaceNextMsg)
	if !ok {
		t.Fatalf("want ReplaceNextMsg, got %T", cmd())
	}
	if msg.Find != "a" || msg.Replace != "b" {
		t.Fatalf("payload want a/b, got %+v", msg)
	}
}

func TestUpdateAltEnterEmitsReplaceAll(t *testing.T) {
	m := New()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if cmd == nil {
		t.Fatal("Alt+Enter expected to emit ReplaceAllMsg")
	}
	if _, ok := cmd().(ReplaceAllMsg); !ok {
		t.Fatalf("want ReplaceAllMsg, got %T", cmd())
	}
}

func TestUpdateEscEmitsClose(t *testing.T) {
	m := New()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc must emit CloseMsg")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatalf("want CloseMsg, got %T", cmd())
	}
}

func TestUpdateSpaceWritesToFocused(t *testing.T) {
	m := New()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if m.Find() != " " {
		t.Fatalf("space want ' ', got %q", m.Find())
	}
}

func TestViewEmptyWithoutWidth(t *testing.T) {
	m := New()
	if v := m.View(); v != "" {
		t.Fatalf("zero width view should be empty, got %q", v)
	}
}

func TestViewRendersBothRows(t *testing.T) {
	m := New()
	m.SetWidth(80)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("F")})
	out := m.View()
	if !strings.Contains(out, "Find") || !strings.Contains(out, "Replace") {
		t.Fatalf("both labels expected: %s", out)
	}
	// Two-line layout.
	if !strings.Contains(out, "\n") {
		t.Fatalf("expected newline-separated rows")
	}
}
