package ai

import (
	"context"
	"strings"
	"testing"
)

// OMX-6: messages that arrive while a turn works (a user's note to a
// step, a child run's finish) join the turn after the tool results, and
// a reply that would end the turn goes on once more when one waits.
func TestAgentInboxJoinsTheTurn(t *testing.T) {
	p := &scripted{tools: true, replies: []Chunk{
		{ToolCalls: []ToolCall{{ID: "1", Name: "noop"}}},
		{Delta: "first answer"},
		{Delta: "final answer"},
	}}
	inbox := [][]Message{
		{{Role: RoleUser, Content: "note one"}}, // after the tool result
		{{Role: RoleUser, Content: "note two"}}, // when the turn would end
	}
	a := &Agent{Provider: p, Native: true,
		Tools: []Tool{{Name: "noop", Run: func(context.Context, map[string]any) (string, error) { return "ok", nil }}},
		Inbox: func() []Message {
			if len(inbox) == 0 {
				return nil
			}
			m := inbox[0]
			inbox = inbox[1:]
			return m
		}}
	_, added := collectEvents(a, []Message{{Role: RoleSystem, Content: "s"}, {Role: RoleUser, Content: "go"}})
	if p.calls != 3 {
		t.Fatalf("requests = %d, want 3", p.calls)
	}
	if last := p.seen[1][len(p.seen[1])-1]; last.Content != "note one" {
		t.Fatalf("second request ends with %+v", last)
	}
	if last := p.seen[2][len(p.seen[2])-1]; last.Content != "note two" {
		t.Fatalf("third request ends with %+v", last)
	}
	if got := added[len(added)-1].Content; !strings.Contains(got, "final answer") {
		t.Fatalf("last added %q", got)
	}
}
