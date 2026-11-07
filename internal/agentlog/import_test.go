package agentlog

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (CMP-87): a past session becomes a termocode AI chat: prompts and
// answers in order (answers in a row joined), each tool call one display
// line with a ✘ when its result failed, tool results not copied, blank
// turns dropped, and the chat says where it came from.
func TestImportConversation(t *testing.T) {
	s := Session{CLI: Claude, ID: "0123456789abcdef", Start: time.Unix(1700000000, 0), Turns: []Turn{
		{Kind: User, Text: "fix the parser test"},
		{Kind: Assistant, Text: "Looking."},
		{Kind: ToolCall, Tool: "Read", Args: "parser.go"},
		{Kind: ToolResult, Tool: "Read", raw: "package parser // file contents"},
		{Kind: ToolCall, Tool: "Bash", Args: "go test ./..."},
		{Kind: ToolResult, Tool: "Bash", Failed: true, raw: "FAIL"},
		{Kind: Assistant, Text: "It fails on line 3."},
		{Kind: Assistant, Text: "Fixed it."},
		{Kind: User, Text: "   "},
		{Kind: User, Text: "thanks"},
	}}
	c := ImportConversation(s, "Claude Code")
	var got []string
	for _, m := range c.Messages {
		got = append(got, m.Role+": "+m.Text)
	}
	want := []string{
		"user: fix the parser test",
		"assistant: Looking.",
		"tool: ⚙ Read parser.go",
		"tool: ⚙ Bash go test ./... ✘",
		"assistant: It fails on line 3.\n\nFixed it.",
		"user: thanks",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("messages:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, m := range c.Messages {
		if strings.Contains(m.Text, "file contents") {
			t.Error("a tool result was copied")
		}
	}
	if c.ID == "" || c.Title != "fix the parser test" {
		t.Errorf("id %q title %q", c.ID, c.Title)
	}
	if c.ImportedFrom == nil || c.ImportedFrom.CLI != "claude" || c.ImportedFrom.Session != s.ID || c.ImportedFrom.Agent != "Claude Code" {
		t.Errorf("imported from %+v", c.ImportedFrom)
	}
}

// Spec (CMP-87): a long session keeps its newest messages and says how
// many earlier ones were left out; the text is redacted on the way in.
func TestImportConversationLongAndRedacted(t *testing.T) {
	var turns []Turn
	for i := 0; i < maxImportMessages+10; i++ {
		turns = append(turns, Turn{Kind: User, Text: "q"}, Turn{Kind: Assistant, Text: "a"})
	}
	turns = append(turns, Turn{Kind: User, Text: "key sk-ant-api03-ABCDEFGHIJKLMNOPQRSTUVWXYZ012345 here"})
	c := ImportConversation(Session{CLI: Codex, ID: "s1", Turns: turns}, "Codex")
	if len(c.Messages) != maxImportMessages+1 {
		t.Fatalf("%d messages, want %d", len(c.Messages), maxImportMessages+1)
	}
	if c.Messages[0].Role != ai.StoredNote || !strings.Contains(c.Messages[0].Text, "earlier messages were not imported") {
		t.Errorf("first message %+v", c.Messages[0])
	}
	if last := c.Messages[len(c.Messages)-1].Text; strings.Contains(last, "sk-ant-api03-ABCDEFGHIJKLMNOPQRSTUVWXYZ012345") {
		t.Errorf("not redacted: %q", last)
	}
	if c.ImportedFrom.Messages != len(c.Messages)-1 {
		t.Errorf("imported count %d", c.ImportedFrom.Messages)
	}
}

// Spec (CMP-87): a session with no prompt and no answer imports nothing.
func TestImportConversationEmpty(t *testing.T) {
	c := ImportConversation(Session{CLI: Claude, Turns: []Turn{{Kind: ToolResult, raw: "x"}, {Kind: User, Text: " "}}}, "Claude Code")
	if len(c.Messages) != 0 {
		t.Errorf("messages %+v", c.Messages)
	}
}
