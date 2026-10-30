package app

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
)

// chatPerfModel is a 200×50 true-colour window with the AI chat panel
// open on a long conversation: n messages (half questions, half
// Markdown answers with lists and a code block).
func chatPerfModel(tb testing.TB, n int) Model {
	tb.Helper()
	tb.Setenv("XDG_CONFIG_HOME", tb.TempDir())
	perfColour(tb)
	m0, _ := newTestModel(tb).Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m := m0.(Model)
	m.actionsOpen, m.actionsPinned = true, true
	m.actionsWidth, m.actionsWidthSet = 70, true
	s := m.aiS()
	s.provider = fakeAIProvider{}
	for i := 0; i < n/2; i++ {
		s.conv.Messages = append(s.conv.Messages,
			ai.StoredMessage{Role: ai.StoredUser, Text: fmt.Sprintf("Question %d: why does the list jump when I scroll it fast?", i)},
			ai.StoredMessage{Role: ai.StoredAssistant, Text: fmt.Sprintf("## Answer %d\n\nThe **list** redraws on every `MouseWheel` event, so *each* row is styled again.\n\n", i) +
				"1. first point with `code`\n2. second point that is long enough to wrap twice in a narrow panel like this one\n\n" +
				"```go\nfunc (m *Model) scroll(d int) {\n\tm.top = max(0, m.top+d)\n\tm.dirty = true\n}\n```\n\n> tip: cache the rows"})
	}
	m.applyLayout()
	if v := ansi.Strip(m.View()); !strings.Contains(v, "cache the rows") {
		tb.Fatalf("chat panel does not show the conversation:\n%s", v)
	}
	return m
}

// BenchmarkAIChatFrame500 is one frame with a 500-message chat open.
func BenchmarkAIChatFrame500(b *testing.B) {
	m := chatPerfModel(b, 500)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

// BenchmarkAIChatStreamToken is one streamed token at the end of a
// 500-message chat: the event (Update) and the frame after it. Answers
// of 70 tokens stream one after another.
func BenchmarkAIChatStreamToken(b *testing.B) {
	m := chatPerfModel(b, 500)
	s := m.aiS()
	s.streaming = true
	ch := make(chan tea.Msg)
	words := []string{" the", " list", " **redraws**", " `rows`", "\n\n", " and", " then"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%70 == 0 { // a new turn
			s.conv.Messages = append(s.conv.Messages[:500], ai.StoredMessage{Role: ai.StoredUser, Text: "and then?"})
		}
		nm, _ := m.Update(aiChatEventMsg{gen: s.chatGen, ev: ai.Event{Kind: ai.EventText, Text: words[i%len(words)]}, ch: ch})
		m = nm.(Model)
		_ = m.View()
	}
}
