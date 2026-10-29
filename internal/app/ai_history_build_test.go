package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The provider history: one user message per turn (context first), and
// one assistant message with the turn's tool lines (run steps' too) and
// its text parts joined; a turn with nothing to say adds no message.
func TestAIChatHistoryShape(t *testing.T) {
	m := aiTestModel(t)
	m.ai.conv.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "q1", Context: "ctx"},
		{Role: ai.StoredTool, Text: "⚙ Read a → 1 line"},
		{Role: ai.StoredAssistant, Text: " part one "},
		{Role: ai.StoredTool, Text: "⚙ Read b → 2 lines"},
		{Role: ai.StoredAssistant, Text: "part two\n"},
		{Role: ai.StoredUser, Text: "q2"},
		{Role: ai.StoredAssistant, Text: "  "},
		{Role: ai.StoredUser, Text: "q3"},
		{Role: ai.StoredAssistant, Text: "ran", Steps: []ai.StoredStep{{Tools: []string{"⚙ s1"}}, {Tools: []string{"⚙ s2"}}}},
		{Role: ai.StoredUser, Text: "q4"},
		{Role: ai.StoredTool, Text: "⚙ only a tool"},
	}
	got := m.aiChatHistory("SYS")
	want := []ai.Message{
		{Role: ai.RoleSystem, Content: "SYS"},
		{Role: ai.RoleUser, Content: "ctx\n\nq1"},
		{Role: ai.RoleAssistant, Content: "[tools I ran]\n⚙ Read a → 1 line\n⚙ Read b → 2 lines\n\npart one part two"},
		{Role: ai.RoleUser, Content: "q2"},
		{Role: ai.RoleUser, Content: "q3"},
		{Role: ai.RoleAssistant, Content: "[tools I ran]\n⚙ s1\n⚙ s2\n\nran"},
		{Role: ai.RoleUser, Content: "q4"},
		{Role: ai.RoleAssistant, Content: "[tools I ran]\n⚙ only a tool"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d messages: %+v", len(got), got)
	}
	for i := range want {
		if got[i].Role != want[i].Role || got[i].Content != want[i].Content {
			t.Errorf("message %d = %s %q, want %s %q", i, got[i].Role, got[i].Content, want[i].Role, want[i].Content)
		}
	}
	if n := len(m.aiChatHistoryN("SYS", 3)); n != 3 {
		t.Errorf("first 3 stored messages gave %d", n)
	}
}

func BenchmarkAIChatHistory2k(b *testing.B) {
	m := perfAIChat(b, 2000, perfAIProvider{})
	b.ReportAllocs()
	for b.Loop() {
		m.aiChatHistory("SYS")
	}
}
