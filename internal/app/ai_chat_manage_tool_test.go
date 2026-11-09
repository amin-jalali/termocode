package app

import (
	"strings"
	"testing"
)

// AI parity (chat history): chat_manage renames a saved chat at once
// (AI: Rename Chat… has no card) and deletes one after a card (Delete
// Chat… asks, Cancel first); the chat the AI answers in is never deleted.
func TestAIChatManageTool(t *testing.T) {
	r := webChatMoreRig(t)
	tl := aiToolOf(t, r.m, "chat_manage")
	if ask, class := tl.Gate(map[string]any{"op": "rename"}); ask || class != "" {
		t.Errorf("rename asks (%v %q)", ask, class)
	}
	if ask, class := tl.Gate(map[string]any{"op": "delete"}); !ask || class != "" {
		t.Errorf("delete: ask=%v class=%q, want a card with no Always allow", ask, class)
	}
	if out, err := aiToolDo(t, tl, map[string]any{"op": "rename", "id": "c-old", "title": "Better name"}); err != nil || !strings.Contains(out, "Better name") {
		t.Fatalf("rename: %q %v", out, err)
	}
	if c, err := r.m.ai.store.Load("c-old"); err != nil || c.Title != "Better name" {
		t.Fatalf("stored title %q %v", c.Title, err)
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "rename", "id": "c-1", "title": "Open one"}); err != nil || r.m.ai.conv.Title != "Open one" {
		t.Fatalf("rename of the open chat: %v %q", err, r.m.ai.conv.Title)
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "delete", "id": "c-1"}); err == nil {
		t.Fatal("the AI deleted the chat it answers in")
	}
	if card := tl.Describe(map[string]any{"op": "delete", "id": "c-old"}); !strings.Contains(card, "cannot be undone") {
		t.Errorf("delete card: %s", card)
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "delete", "id": "c-old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.ai.store.Load("c-old"); err == nil {
		t.Fatal("c-old is still stored")
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "rename", "id": "c-none", "title": "x"}); err == nil {
		t.Error("a missing chat was renamed")
	}
}
