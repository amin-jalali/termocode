package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// The chat (ai_chat.go): stale messages of an old chat, a chat file
// that can not be saved, history, the panel toggle, and the attach and
// apply actions.

// Messages of a chat the user left (New Chat) are dropped; a stale
// confirm is denied, never shown.
func TestCovAIChatStaleMessages(t *testing.T) {
	m := aiTestModel(t)
	s := m.aiS()
	old := s.chatGen
	m.newAIChat()
	if s.chatGen == old {
		t.Fatal("New Chat kept the generation")
	}
	ch := make(chan tea.Msg, 1)
	if m.onAIChatEvent(aiChatEventMsg{gen: old, ev: ai.Event{Kind: ai.EventText, Text: "late"}, ch: ch}) == nil {
		t.Error("a stale event did not keep draining")
	}
	if len(s.conv.Messages) != 0 {
		t.Errorf("a stale event changed the new chat: %+v", s.conv.Messages)
	}
	reply := make(chan bool, 1)
	m.onAIChatConfirm(aiChatConfirmMsg{gen: old, tool: ai.Tool{Name: "write_file"}, reply: reply, ch: ch})
	if <-reply || m.confirmOpen {
		t.Error("a stale confirm was allowed or shown")
	}
	if m.onAIChatDone(aiChatDoneMsg{gen: old}) != nil {
		t.Error("a stale done did something")
	}
	if m.answerAIToolConfirm("allow") != nil {
		t.Error("an answer with nothing pending did something")
	}
}

// New Chat while a turn runs stops it; a turn that ends with an empty
// answer drops it; a chat file that can not be written is logged.
func TestCovAIChatDoneAndCancel(t *testing.T) {
	m := aiTestModel(t)
	s := m.aiS()
	cancelled := false
	s.chatCancel = func() { cancelled = true }
	m.newAIChat()
	if !cancelled {
		t.Error("New Chat did not stop the running turn")
	}
	s.conv.Messages = []ai.StoredMessage{{Role: ai.StoredUser, Text: "hi"}, {Role: ai.StoredAssistant, Text: "  "}}
	s.streaming = true
	if err := os.MkdirAll(s.store.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(s.store.Dir, 0o500)
	t.Cleanup(func() { _ = os.Chmod(s.store.Dir, 0o755) })
	m.onAIChatDone(aiChatDoneMsg{gen: s.chatGen})
	if s.streaming || len(s.conv.Messages) != 1 {
		t.Errorf("after done: streaming %v messages %+v", s.streaming, s.conv.Messages)
	}
	if os.Getuid() != 0 {
		cfg, _ := configDir()
		if b, _ := os.ReadFile(filepath.Join(cfg, "errors.log")); !strings.Contains(string(b), "[ai] save chat") {
			t.Errorf("errors.log has no failed save:\n%s", b)
		}
	}
}

// History: empty says so; a chat that can not be read is a toast;
// picking one while a turn runs stops the turn; ages read well.
func TestCovAIChatHistory(t *testing.T) {
	m := aiTestModel(t)
	if m.openAIHistory() == nil || m.pickerOpen {
		t.Error("an empty history opened a picker")
	}
	if m.onAIHistoryPicked("no-such-chat") == nil || !strings.Contains(covToastText(&m), "Could not open chat") {
		t.Errorf("missing chat: %q", covToastText(&m))
	}
	s := m.aiS()
	s.conv.Messages = []ai.StoredMessage{{Role: ai.StoredUser, Text: "saved one"}}
	if err := s.store.Save(&s.conv); err != nil {
		t.Fatal(err)
	}
	id := s.conv.ID
	m.newAIChat()
	cancelled := false
	s.chatCancel = func() { cancelled = true }
	m.onAIHistoryPicked(id)
	if !cancelled || s.conv.ID != id {
		t.Errorf("history pick: cancelled %v id %q", cancelled, s.conv.ID)
	}
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	t.Cleanup(clock.Set(func() time.Time { return now }))
	if got := aiAge(now.Add(-3 * time.Hour).UnixMilli()); got != "3h ago" {
		t.Errorf("hours = %q", got)
	}
	if got := aiAge(now.Add(-50 * time.Hour).UnixMilli()); got != "2d ago" {
		t.Errorf("days = %q", got)
	}
}

// The status badge toggles the panel; Apply last code block needs a code
// block and an open file.
func TestCovAIChatToggleAndApply(t *testing.T) {
	m := aiTestModel(t)
	m.toggleAIPanel()
	if !m.actionsPanelVisible() || !m.aiS().focused {
		t.Fatal("the badge did not open the panel")
	}
	m.toggleAIPanel()
	if m.actionsPanelVisible() || m.aiS().focused {
		t.Error("the badge did not close the panel")
	}
	if m.aiApplyLastCodeBlock() == nil || !strings.Contains(covToastText(&m), "No code block in this chat") {
		t.Errorf("no code: %q", covToastText(&m))
	}
	m.aiS().conv.Messages = []ai.StoredMessage{{Role: ai.StoredAssistant, Text: "```go\nx := 1\n```"}, {Role: ai.StoredUser, Text: "thanks"}}
	if m.aiApplyLastCodeBlock() == nil || !strings.Contains(covToastText(&m), "Open the target file") {
		t.Errorf("no editor: %q", covToastText(&m))
	}
	if m.aiAttachCurrent() == nil {
		t.Error("attach without a provider says nothing")
	}
	m.aiS().provider = covScriptedProvider{}
	if m.aiAttachCurrent() == nil || !strings.Contains(covToastText(&m), "Open a file first") {
		t.Errorf("attach without a file: %q", covToastText(&m))
	}
}

// Attach with the editor: a selection attaches its lines; a big file is
// cut.
func TestCovAIChatAttachNvim(t *testing.T) {
	m, c, path := covActionsModel(t)
	m.aiS().provider = covScriptedProvider{}
	_ = c.Command("call cursor(3, 1)")
	_ = c.Input("Vj")
	m.aiAttachCurrent()
	if a := m.aiS().attachments; len(a) != 1 || a[0].Label != "main.go:3-4" || !strings.Contains(a[0].Text, "func main() {") {
		t.Errorf("selection attach: %+v", a)
	}
	_ = c.Input("\x1b")
	big := strings.Repeat("// filler line for the size cap\n", aiMaxAttach/20)
	if err := os.WriteFile(path, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = c.Command("edit!")
	m.aiAttachCurrent()
	if a := m.aiS().attachments; len(a) != 2 || !strings.HasSuffix(a[1].Text, "… [truncated]") {
		t.Errorf("big file attach: %d attachments", len(a))
	}
}
