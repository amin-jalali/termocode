package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The chat panel (ai_panel.go): editing keys in the input, scroll keys,
// the wheel and the clickable buttons of the transcript.

func covPanelModel(t *testing.T) *Model {
	t.Helper()
	m := aiTestModel(t)
	m.actionsOpen, m.actionsPinned = true, true
	m.aiS().focused = true
	return &m
}

func covAIKey(m *Model, k tea.KeyMsg) bool {
	_, ok := m.routeAIPanelKey(k)
	return ok
}

// Typing and editing: words, a pasted tab / CR / control character,
// Ctrl+W, Delete, arrows, Home / End, Ctrl+U and a newline.
func TestCovAIPanelEditing(t *testing.T) {
	m := covPanelModel(t)
	s := m.aiS()
	covAIKey(m, covRunes("fix the"))
	covAIKey(m, tea.KeyMsg{Type: tea.KeySpace})
	covAIKey(m, covRunes("bug\tnow\r\x07"))
	if got := string(s.input); got != "fix the bug now\n" {
		t.Fatalf("input = %q", got)
	}
	covAIKey(m, tea.KeyMsg{Type: tea.KeyBackspace})
	covAIKey(m, tea.KeyMsg{Type: tea.KeyCtrlW})
	if got := string(s.input); got != "fix the bug " {
		t.Errorf("after Ctrl+W: %q", got)
	}
	covAIKey(m, tea.KeyMsg{Type: tea.KeyHome})
	covAIKey(m, tea.KeyMsg{Type: tea.KeyDelete})
	covAIKey(m, tea.KeyMsg{Type: tea.KeyRight})
	if got := string(s.input); got != "ix the bug " || s.caret != 1 {
		t.Errorf("after Home Delete Right: %q caret %d", got, s.caret)
	}
	covAIKey(m, tea.KeyMsg{Type: tea.KeyEnd})
	covAIKey(m, tea.KeyMsg{Type: tea.KeyRight})
	covAIKey(m, tea.KeyMsg{Type: tea.KeyDelete})
	if s.caret != len(s.input) {
		t.Errorf("End: caret %d of %d", s.caret, len(s.input))
	}
	covAIKey(m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if !strings.HasSuffix(string(s.input), "\n") {
		t.Errorf("Alt+Enter did not add a newline: %q", string(s.input))
	}
	covAIKey(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	if len(s.input) != 0 || s.caret != 0 {
		t.Errorf("Ctrl+U left %q", string(s.input))
	}
	if covAIKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true}) {
		t.Error("Alt+x was taken by the input")
	}
	if covAIKey(m, tea.KeyMsg{Type: tea.KeyF7}) {
		t.Error("F7 was taken by the input")
	}
}

// Scroll keys and the wheel move the transcript; Esc first drops the
// step-card selection, then leaves the panel.
func TestCovAIPanelScrollAndEsc(t *testing.T) {
	m := covPanelModel(t)
	s := m.aiS()
	for i := 0; i < 80; i++ {
		s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: "user", Text: "line " + itoa(i)})
	}
	covAIKey(m, tea.KeyMsg{Type: tea.KeyPgUp})
	if s.scroll == 0 {
		t.Fatal("pgup did not scroll")
	}
	up := s.scroll
	covAIKey(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if s.scroll >= up {
		t.Errorf("pgdown: %d → %d", up, s.scroll)
	}
	x := m.w - m.actionsColumnWidth() + 2
	m.handleAIPanelMouse(tea.MouseMsg{X: x, Y: 10, Type: tea.MouseWheelUp, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	after := s.scroll
	m.handleAIPanelMouse(tea.MouseMsg{X: x, Y: 10, Type: tea.MouseWheelDown, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if s.scroll >= after {
		t.Errorf("wheel: %d then %d", after, s.scroll)
	}
	s.agents.on = true
	m.handleAIPanelMouse(tea.MouseMsg{X: x, Y: 10, Type: tea.MouseWheelUp, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	m.handleAIPanelMouse(tea.MouseMsg{X: x, Y: 10, Type: tea.MouseWheelDown, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	s.agents.on = false
	s.cardSel = "step:1:1"
	covAIKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if s.cardSel != "" || !s.focused {
		t.Errorf("first Esc: sel %q focused %v", s.cardSel, s.focused)
	}
	covAIKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if s.focused || m.focus != FocusEditor {
		t.Error("second Esc did not leave the panel")
	}
	// A hidden panel gives up the focus.
	s.focused = true
	m.actionsOpen = false
	if covAIKey(m, covRunes("x")) || s.focused {
		t.Error("a hidden panel kept the keys")
	}
}

// The transcript's buttons: each runs its action; bad indexes do
// nothing; a code block is copied.
func TestCovAIPanelButtons(t *testing.T) {
	m := covPanelModel(t)
	s := m.aiS()
	var copied []string
	sink := func(x string) { copied = append(copied, x) }
	clipboardSink.Store(&sink)
	t.Cleanup(func() { clipboardSink.Store(nil) })
	s.conv.Messages = []ai.StoredMessage{{Role: "assistant", Text: "Try:\n```go\nfmt.Println(1)\n```\nor\n```\nls\n```"}}
	m.onAIButton("copy:0:1")
	if len(copied) != 1 || copied[0] != "ls" {
		t.Errorf("copied %q", copied)
	}
	for _, id := range []string{"copy:x", "copy:9", "copy:0:7", "copymsg:x", "run-retry:x", "undo-turn:x", "suggest:nope", "nothing"} {
		if m.onAIButton(id) != nil {
			t.Errorf("%s did something", id)
		}
	}
	if m.onAIButton("apply:0") == nil {
		t.Error("apply without an editor says nothing")
	}
	m.onAIButton("mcp-notnow:github")
	if !s.mcpNotNow["github"] {
		t.Error("Not now was not kept")
	}
	s.attachments = []aiAttachment{{Label: "a"}, {Label: "b"}}
	m.onAIButton("rm-attach:0")
	if len(s.attachments) != 1 || s.attachments[0].Label != "b" {
		t.Errorf("attachments = %+v", s.attachments)
	}
	m.onAIButton("rm-attach:7")
	m.onAIButton("configure")
	if !m.pickerOpen || m.pickerKind != pickerKindAIProvider {
		t.Error("configure did not open the providers")
	}
	m.pickerOpen = false
	m.onAIButton("attach-file")
	m.pickerOpen = false
	m.onAIButton("rules-open:" + t.TempDir() + "/AGENTS.md")
	m.onAIButton("plan-open:1")
	m.onAIButton("run-continue:1")
	if _, ok := aiCodeBlock("no code", 0); ok {
		t.Error("a code block in text without one")
	}
}
