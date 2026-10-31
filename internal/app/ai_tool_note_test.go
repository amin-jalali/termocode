package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (MCPX-38): a tool's note for the user shows as its own line under
// the tool line (after the result arrow), and never goes to the model.
func TestAIChatToolNoteForUserOnly(t *testing.T) {
	m := aiTestModel(t)
	s := m.aiS()
	ch := make(chan tea.Msg, 1)
	ev := func(e ai.Event) { m.onAIChatEvent(aiChatEventMsg{gen: s.chatGen, ev: e, ch: ch}) }
	s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredUser, Text: "draw it"})
	ev(ai.Event{Kind: ai.EventToolStart, Tool: "mcp__fake__chart", Detail: "mcp.fake.chart"})
	ev(ai.Event{Kind: ai.EventToolNote, Tool: "mcp__fake__chart", Detail: "open the chart at https://example.com/c"})
	ev(ai.Event{Kind: ai.EventToolResult, Tool: "mcp__fake__chart", Detail: "done"})
	n := len(s.conv.Messages)
	if n < 2 || s.conv.Messages[n-2].Text != "⚙ mcp.fake.chart → done" || s.conv.Messages[n-1].Text != aiToolNoteMark+"open the chart at https://example.com/c" {
		t.Fatalf("messages %+v", s.conv.Messages)
	}
	for _, msg := range m.aiChatHistory("sys") {
		if strings.Contains(msg.Content, "example.com/c") {
			t.Fatalf("the note went to the model: %q", msg.Content)
		}
	}
}
