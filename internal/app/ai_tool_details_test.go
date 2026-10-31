package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

func transcriptText(m Model) (string, []aiLineButton) {
	var rows []string
	var buttons []aiLineButton
	for _, l := range m.buildAITranscript(80) {
		rows = append(rows, l.text+l.styled)
		buttons = append(buttons, l.buttons...)
	}
	return strings.Join(rows, "\n"), buttons
}

// Spec (MCPX-33, UX-11): an MCP call's chat line has a "details" button;
// a click opens its full arguments (secrets hidden) and its result under
// the line, a second click closes them. Other tools have no button.
func TestAIChatMCPCallDetails(t *testing.T) {
	m := aiTestModel(t)
	s := m.aiS()
	ch := make(chan tea.Msg, 1)
	ev := func(e ai.Event) { m.onAIChatEvent(aiChatEventMsg{gen: s.chatGen, ev: e, ch: ch}) }
	s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredUser, Text: "go"})
	ev(ai.Event{Kind: ai.EventToolStart, Tool: "read_file", Label: "read_file", Detail: "Read a.go"})
	ev(ai.Event{Kind: ai.EventToolResult, Tool: "read_file", Label: "read_file", Detail: "package a"})
	key := "sk-" + "test0123456789abcdefghijklmnop"
	ev(ai.Event{Kind: ai.EventToolStart, Tool: "mcp__fake__echo", Label: "mcp.fake.echo", Detail: "mcp.fake.echo text=hi",
		Args: map[string]any{"text": "hello there", "key": key}})
	ev(ai.Event{Kind: ai.EventToolResult, Tool: "mcp__fake__echo", Label: "mcp.fake.echo", Detail: "echo: hello there\nsecond line"})
	text, buttons := transcriptText(m)
	var ids []string
	for _, b := range buttons {
		if strings.HasPrefix(b.id, "tooldetail:") {
			ids = append(ids, b.id)
		}
	}
	if len(ids) != 1 || !strings.Contains(text, "▸ details") || strings.Contains(text, "second line") {
		t.Fatalf("closed: buttons %v\n%s", ids, text)
	}
	m.onAIButton(ids[0])
	text, _ = transcriptText(m)
	for _, want := range []string{"▾ details", "args", `"text": "hello there"`, "result", "second line"} {
		if !strings.Contains(text, want) {
			t.Fatalf("open: missing %q in\n%s", want, text)
		}
	}
	if strings.Contains(text, key) {
		t.Fatalf("a secret in the details:\n%s", text)
	}
	m.onAIButton(ids[0])
	if text, _ = transcriptText(m); strings.Contains(text, "second line") {
		t.Fatalf("still open:\n%s", text)
	}
}
