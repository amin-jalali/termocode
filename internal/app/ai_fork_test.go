package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// forkModel is a model with one saved chat of two turns open.
func forkModel(t *testing.T) (*Model, ai.ChatStore) {
	t.Helper()
	m := &Model{w: 120, h: 40}
	s := m.aiS()
	s.store = ai.ChatStore{Dir: t.TempDir()}
	s.conv = ai.Conversation{ID: "orig", Title: "Parser tests", Messages: []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "first question"},
		{Role: ai.StoredAssistant, Text: "first answer"},
		{Role: ai.StoredUser, Text: "second question"},
		{Role: ai.StoredAssistant, Text: "second answer"},
	}}
	if err := s.store.Save(&s.conv); err != nil {
		t.Fatal(err)
	}
	return m, s.store
}

// G-FORK: forking from an AI message opens a new chat with the history
// up to it, an empty input and a note; the original stays saved as it was.
func TestAIForkFromAnswer(t *testing.T) {
	m, store := forkModel(t)
	m.aiS().input = []rune("typed before")
	m.aiForkChat(1)
	s := m.aiS()
	if s.conv.ID == "orig" || len(s.conv.Messages) != 2 || s.conv.Messages[1].Text != "first answer" {
		t.Fatalf("open chat %q %+v", s.conv.ID, s.conv.Messages)
	}
	if string(s.input) != "" {
		t.Errorf("input %q, want empty", string(s.input))
	}
	if s.conv.Title != "Parser tests (fork)" || s.conv.ForkNote() != "Forked from Parser tests at message 2" {
		t.Errorf("title %q note %q", s.conv.Title, s.conv.ForkNote())
	}
	orig, err := store.Load("orig")
	if err != nil || len(orig.Messages) != 4 || orig.Title != "Parser tests" {
		t.Fatalf("original changed: %+v %v", orig, err)
	}
	saved, err := store.Load(s.conv.ID)
	if err != nil || saved.ForkedFrom == nil || len(saved.Messages) != 2 {
		t.Fatalf("fork not saved: %+v %v", saved, err)
	}
	if sess := loadSession(); sess.AILastChat != s.conv.ID {
		t.Errorf("session last chat %q, want the fork (survives a restart)", sess.AILastChat)
	}
	if !!m.toast.Empty() {
		t.Error("no toast after the fork")
	}
}

// Forking from a user message puts its text in the input box (to edit),
// it is not resent.
func TestAIForkFromQuestion(t *testing.T) {
	m, _ := forkModel(t)
	m.aiForkChat(2)
	s := m.aiS()
	if len(s.conv.Messages) != 2 || string(s.input) != "second question" || s.caret != len(s.input) {
		t.Fatalf("messages %d input %q caret %d", len(s.conv.Messages), string(s.input), s.caret)
	}
	if s.streaming {
		t.Error("the question was resent")
	}
}

// While a reply streams, the fork starts at the last finished message:
// the running turn's question goes to the input; its partial answer is
// not copied. The original keeps its turn so far.
func TestAIForkWhileStreaming(t *testing.T) {
	m, store := forkModel(t)
	s := m.aiS()
	s.conv.Messages = append(s.conv.Messages,
		ai.StoredMessage{Role: ai.StoredUser, Text: "third question"},
		ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ Read a.go"},
		ai.StoredMessage{Role: ai.StoredAssistant, Text: "half an ans"})
	s.streaming, s.turnMsg = true, 4
	cancelled := false
	s.chatCancel = func() { cancelled = true }
	m.aiForkChat(6)
	if !cancelled || s.streaming {
		t.Error("the stream still runs in the background")
	}
	if len(s.conv.Messages) != 4 || string(s.input) != "third question" {
		t.Fatalf("fork %d messages, input %q", len(s.conv.Messages), string(s.input))
	}
	orig, _ := store.Load("orig")
	if len(orig.Messages) != 7 {
		t.Fatalf("original lost its running turn: %d messages", len(orig.Messages))
	}
}

// A chat with nothing in it cannot be forked: a toast says so.
func TestAIForkEmpty(t *testing.T) {
	m := &Model{w: 120, h: 40}
	s := m.aiS()
	s.store = ai.ChatStore{Dir: t.TempDir()}
	s.conv = ai.NewConversation()
	id := s.conv.ID
	m.dispatchAIPalette("ai-fork-chat")
	if m.pickerOpen || s.conv.ID != id || !!m.toast.Empty() {
		t.Fatalf("picker %v id changed %v toast %v", m.pickerOpen, s.conv.ID != id, !m.toast.Empty())
	}
}

// The palette entry lists the questions and answers newest first; the
// pick forks there. Works without an AI set up (no provider needed).
func TestAIForkPalettePicker(t *testing.T) {
	m, _ := forkModel(t)
	m.dispatchAIPalette("ai-fork-chat")
	if !m.pickerOpen || m.pickerKind != pickerKindAIFork {
		t.Fatal("no message picker")
	}
	items := aiForkItems(m.aiS())
	if len(items) != 4 || items[0].ID != "3" || items[0].Title != "AI: second answer" || items[3].Title != "You: first question" {
		t.Fatalf("items %+v", items)
	}
	m.handleAIPickerSelect("2")
	if string(m.aiS().input) != "second question" {
		t.Fatalf("pick did not fork: input %q", string(m.aiS().input))
	}
}

// The fork's note is the first transcript line.
func TestAIForkNoteShown(t *testing.T) {
	m, _ := forkModel(t)
	m.aiForkChat(3)
	lines := m.buildAITranscript(60)
	text := ""
	for _, l := range lines[:min(4, len(lines))] {
		text += stripANSI(l.styled) + l.text + "\n"
	}
	if !strings.Contains(text, "Forked from Parser tests at message 4") {
		t.Fatalf("no note at the top:\n%s", text)
	}

}
