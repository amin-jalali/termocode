package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// aiGuardCall validates and runs one tool of tools as a turn does.
func aiGuardCall(t *testing.T, tools []ai.Tool, name string, args map[string]any) error {
	t.Helper()
	tl, ok := ai.FindTool(tools, name)
	if !ok {
		t.Fatalf("no tool %q", name)
	}
	if tl.Validate != nil {
		if err := tl.Validate(args); err != nil {
			return err
		}
	}
	_, err := tl.Run(context.Background(), args)
	return err
}

// A chat turn's write_file refuses a file this chat never read; the
// read counts for the next turns of the same chat, not for a new chat;
// ai_write_needs_read false turns the guard off.
func TestAIChatTurnGuardsUnreadWrites(t *testing.T) {
	m := aiTestModel(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := m.aiS()
	s.settings.WriteNeedsRead = nil // the default: on
	s.conv = ai.NewConversation()
	turn := func() []ai.Tool { return m.aiGuardWrites(ai.WorkspaceTools(ai.Workspace{Root: dir}), dir) }
	write := map[string]any{"path": "a.txt", "content": "two\n"}

	if err := aiGuardCall(t, turn(), "write_file", write); err == nil || !strings.Contains(err.Error(), "read it first") {
		t.Fatalf("unread write: %v", err)
	}
	if err := aiGuardCall(t, turn(), "read_file", map[string]any{"path": "a.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := aiGuardCall(t, turn(), "write_file", write); err != nil {
		t.Fatalf("next turn after a read: %v", err)
	}

	s.conv = ai.NewConversation()
	if err := aiGuardCall(t, turn(), "write_file", write); err == nil {
		t.Fatal("a new chat wrote a file it never read")
	}

	off := false
	s.settings.WriteNeedsRead = &off
	if err := aiGuardCall(t, turn(), "write_file", write); err != nil {
		t.Fatalf("guard off: %v", err)
	}
}

// A real chat turn: the model's write_file on a file it never read gets
// the refusal as its tool result (no card) and the file stays; after a
// read_file the same write goes through.
func TestAIChatTurnRefusesUnreadWrite(t *testing.T) {
	write := map[string]any{"path": "a.txt", "content": "two\n"}
	p := &scriptedAIProvider{replies: []ai.Chunk{
		toolCall("write_file", write),
		toolCall("read_file", map[string]any{"path": "a.txt"}),
		toolCall("write_file", write),
	}}
	m, dir := aiIDETestModel(t, p)
	m.ai.settings.WriteNeedsRead = nil
	m.ai.settings.AutoApprove = ai.ToolClasses
	m.ai.settings.EditReview = ai.EditReviewAuto
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	driveAIChat(t, m, m.sendAIChat("change a.txt"), nil)
	res := p.toolResults()
	if len(res) != 3 || !strings.Contains(res[0], "read it first") || !strings.Contains(res[0], "edit_file") {
		t.Fatalf("tool results = %q", res)
	}
	if got := readFile(path); got != "two\n" {
		t.Fatalf("a.txt = %q (results %q)", got, res)
	}
}
