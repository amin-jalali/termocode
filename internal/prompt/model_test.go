package prompt

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewStoresFields(t *testing.T) {
	m := New("Title", "Label", "init")
	if m.title != "Title" || m.label != "Label" || m.value != "init" {
		t.Fatalf("New did not store fields: %+v", m)
	}
}

func TestUpdateTypingAppendsAndBackspaceRemoves(t *testing.T) {
	m := New("", "", "")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ab")})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if m.value != "ab c" {
		t.Fatalf("value want 'ab c', got %q", m.value)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.value != "ab " {
		t.Fatalf("backspace value want 'ab ', got %q", m.value)
	}
}

func TestUpdateEnterEmitsSubmitTrimmed(t *testing.T) {
	m := New("", "", "  hello  ")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected submit cmd")
	}
	msg, ok := cmd().(SubmitMsg)
	if !ok {
		t.Fatalf("want SubmitMsg, got %T", cmd())
	}
	if msg.Value != "hello" {
		t.Fatalf("submit value want 'hello', got %q", msg.Value)
	}
}

func TestUpdateEnterEmptyValueIgnored(t *testing.T) {
	m := New("", "", "   ")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		// SubmitMsg must not fire when trimmed value is empty.
		if _, ok := cmd().(SubmitMsg); ok {
			t.Fatal("Enter on empty value must not submit")
		}
	}
}

func TestUpdateEscAndCtrlCEmitClose(t *testing.T) {
	for _, k := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyCtrlC}} {
		m := New("", "", "x")
		_, cmd := m.Update(k)
		if cmd == nil {
			t.Fatalf("%v: expected close cmd", k.Type)
		}
		if _, ok := cmd().(CloseMsg); !ok {
			t.Fatalf("%v: want CloseMsg, got %T", k.Type, cmd())
		}
	}
}

// Mouse-tracking SGR codes can arrive as KeyRunes when mouse mode is
// mid-toggle, sometimes split across multiple events. The filter must
// drop them so they never appear in m.value (otherwise partial chunks
// flicker as the mouse moves). Real typing must still work.
func runesEvent(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestUpdateDropsMouseSGRSingleEvent(t *testing.T) {
	m := New("", "", "")
	m, _ = m.Update(runesEvent("[<35;70;19M"))
	if m.value != "" {
		t.Fatalf("full mouse SGR fragment must be dropped, got %q", m.value)
	}
}

func TestUpdateDropsMouseSGRSplitAcrossEvents(t *testing.T) {
	m := New("", "", "")
	for _, chunk := range []string{"[<35", ";70", ";19M"} {
		m, _ = m.Update(runesEvent(chunk))
	}
	if m.value != "" {
		t.Fatalf("split mouse SGR fragments must not accumulate, got %q", m.value)
	}
}

func TestUpdateAllowsBracketAlone(t *testing.T) {
	m := New("", "", "")
	m, _ = m.Update(runesEvent("["))
	if m.value != "[" {
		t.Fatalf("single `[` should type as-is, got %q", m.value)
	}
}

func TestUpdateAllowsAbcTyping(t *testing.T) {
	m := New("", "", "")
	for _, c := range []string{"a", "b", "c"} {
		m, _ = m.Update(runesEvent(c))
	}
	if m.value != "abc" {
		t.Fatalf("typing 'abc' want 'abc', got %q", m.value)
	}
}

func TestUpdateAllowsBracketedText(t *testing.T) {
	m := New("", "", "")
	m, _ = m.Update(runesEvent("[abc]"))
	if m.value != "[abc]" {
		t.Fatalf("bracketed text want '[abc]', got %q", m.value)
	}
}

func TestUpdateAllowsPersianText(t *testing.T) {
	m := New("", "", "")
	m, _ = m.Update(runesEvent("سلام"))
	if m.value != "سلام" {
		t.Fatalf("persian text want 'سلام', got %q", m.value)
	}
}

func TestIsMouseFragmentEventHeuristic(t *testing.T) {
	cases := []struct {
		name string
		in   string
		drop bool
	}{
		{"empty", "", false},
		{"single bracket", "[", false},
		{"single M", "M", false},
		{"sgr head", "[<35", true},
		{"sgr middle", ";70", true},
		{"sgr tail", ";19M", true},
		{"full sgr", "[<35;70;19M", true},
		{"bracketed letters", "[abc]", false},
		{"persian", "سلام", false},
		{"two letters", "ab", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isMouseFragmentEvent([]rune(tc.in))
			if got != tc.drop {
				t.Fatalf("isMouseFragmentEvent(%q) = %v, want %v", tc.in, got, tc.drop)
			}
		})
	}
}

func TestUpdateNonKeyMsgIsNoOp(t *testing.T) {
	m := New("t", "l", "v")
	out, cmd := m.Update(struct{}{})
	if cmd != nil {
		t.Fatal("non-key msg must not produce cmd")
	}
	if out.value != "v" {
		t.Fatal("non-key msg must not mutate value")
	}
}

func TestViewEmptyAtZeroSize(t *testing.T) {
	m := New("t", "l", "v")
	if v := m.View(); v != "" {
		t.Fatalf("zero size: view want empty, got %q", v)
	}
}

func TestViewIncludesTitleLabelValue(t *testing.T) {
	m := New("MyTitle", "MyLabel", "MyValue")
	m.SetSize(80, 20)
	out := m.View()
	for _, want := range []string{"MyTitle", "MyLabel", "MyValue"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q: %s", want, out)
		}
	}
}
