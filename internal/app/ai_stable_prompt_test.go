package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// aiUserContents are the contents of r's user messages, in order.
func aiUserContents(r ai.Request) []string {
	var out []string
	for _, msg := range r.Messages {
		if msg.Role == ai.RoleUser {
			out = append(out, msg.Content)
		}
	}
	return out
}

// Spec (lean request): the system prompt and the tool list are byte for
// byte the same on every turn of a chat, so the provider's prompt cache
// keeps them. What changes between turns (the todo list, the memory
// lessons) goes with the turn's user message, and an earlier turn's
// message is sent again exactly as it was sent then.
func TestAIChatStableSystemPrompt(t *testing.T) {
	r1, r2 := aiHeadTwoTurns(t)
	if s1, s2 := r1.Messages[0].Content, r2.Messages[0].Content; s1 != s2 {
		t.Errorf("the system prompt changed between turns:\nturn 1 only: %q\nturn 2 only: %q", aiLinesOnlyIn(s1, s2), aiLinesOnlyIn(s2, s1))
	}
	if a, b := aiHeadToolNames(r1), aiHeadToolNames(r2); a != b {
		t.Errorf("the tool list changed between turns:\n%s\n%s", a, b)
	}
	for i := range r1.Tools {
		if r1.Tools[i].Description != r2.Tools[i].Description {
			t.Errorf("tool %s's description changed between turns", r1.Tools[i].Name)
		}
	}
	sys := r1.Messages[0].Content
	if strings.Contains(sys, "read the code") || strings.Contains(sys, "Run make build") {
		t.Errorf("todos or lessons in the system prompt:\n%s", sys)
	}
	u1, u2 := aiUserContents(r1), aiUserContents(r2)
	if len(u1) != 1 || len(u2) != 2 {
		t.Fatalf("user messages %d and %d, want 1 and 2", len(u1), len(u2))
	}
	if !strings.Contains(u1[0], "hello") || !strings.Contains(u1[0], "[~] read the code") || !strings.Contains(u1[0], "Run make build") {
		t.Errorf("turn 1's message lacks its todos or lessons:\n%s", u1[0])
	}
	if u2[0] != u1[0] {
		t.Errorf("turn 1's message was sent differently in turn 2:\n%q\n%q", u1[0], u2[0])
	}
	if !strings.Contains(u2[1], "go on") || !strings.Contains(u2[1], "[~] fix the bug") || !strings.Contains(u2[1], "Run make test") {
		t.Errorf("turn 2's message lacks its todos or lessons:\n%s", u2[1])
	}
}
