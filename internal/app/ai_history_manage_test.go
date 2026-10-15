package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

func historyModel(t *testing.T) (*Model, ai.ChatStore) {
	t.Helper()
	m := &Model{w: 120, h: 40}
	s := m.aiS()
	s.store = ai.ChatStore{Dir: t.TempDir()}
	for _, c := range []ai.Conversation{
		{ID: "c1", Title: "First chat", Messages: []ai.StoredMessage{{Role: ai.StoredUser, Text: "hi"}}},
		{ID: "c2", Title: "Second chat", Messages: []ai.StoredMessage{{Role: ai.StoredUser, Text: "yo"}}},
	} {
		c := c
		if err := s.store.Save(&c); err != nil {
			t.Fatal(err)
		}
	}
	return m, s.store
}

func ids(store ai.ChatStore) map[string]string {
	out := map[string]string{}
	for _, c := range store.List() {
		out[c.ID] = c.Title
	}
	return out
}

// Spec (2026-10-15, "how do I manage my history?"): AI: Delete Chat…
// picks a chat, asks, and deletes only that one; the open chat is
// replaced by a new one when it was the deleted one.
func TestAIHistoryDelete(t *testing.T) {
	m, store := historyModel(t)
	m.aiS().conv = ai.Conversation{ID: "c1", Title: "First chat"}
	m.dispatchAIPalette("ai-history-delete")
	if !m.pickerOpen {
		t.Fatal("no chat picker")
	}
	m.handleAIPickerSelect("c1")
	if !m.confirmOpen {
		t.Fatal("no confirm before deleting")
	}
	m.handleAIConfirm("delete")
	got := ids(store)
	if _, ok := got["c1"]; ok || got["c2"] != "Second chat" {
		t.Fatalf("chats after delete: %v", got)
	}
	if m.aiS().conv.ID == "c1" {
		t.Error("the deleted chat is still the open chat")
	}
}

// Cancel keeps the chat.
func TestAIHistoryDeleteCancel(t *testing.T) {
	m, store := historyModel(t)
	m.dispatchAIPalette("ai-history-delete")
	m.handleAIPickerSelect("c2")
	m.handleAIConfirm("cancel")
	if len(ids(store)) != 2 {
		t.Fatalf("cancel deleted: %v", ids(store))
	}
}

// AI: Rename Chat… asks for a new title and saves it.
func TestAIHistoryRename(t *testing.T) {
	m, store := historyModel(t)
	m.dispatchAIPalette("ai-history-rename")
	m.handleAIPickerSelect("c2")
	if !m.promptOpen {
		t.Fatal("no prompt for the new title")
	}
	m.handleAIPromptSubmit("Release notes")
	if got := ids(store)["c2"]; got != "Release notes" {
		t.Fatalf("title %q", got)
	}
}

// AI: Clear Chat History asks once, then removes every chat.
func TestAIHistoryClear(t *testing.T) {
	m, store := historyModel(t)
	m.dispatchAIPalette("ai-history-clear")
	if !m.confirmOpen {
		t.Fatal("no confirm before clearing")
	}
	m.handleAIConfirm("clear")
	if n := len(ids(store)); n != 0 {
		t.Fatalf("%d chats left", n)
	}
}
