package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// Spec (R4): every agent tool call is one line in the Output channel
// "AI": time, tool, short args, gate, who decided, result summary.
func TestAIAuditLog(t *testing.T) {
	t.Cleanup(clock.Set(func() time.Time { return time.Date(2026, 10, 15, 14, 3, 22, 0, time.Local) }))
	p := &scriptedAIProvider{replies: []ai.Chunk{
		toolCall("read_file", map[string]any{"path": "a.txt"}),
		toolCall("write_file", map[string]any{"path": "a.txt", "content": reviewNew}),
		toolCall("git_commit", map[string]any{"message": "change it"}),
		toolCall("lsp_rename", map[string]any{"path": "a.txt", "line": 1, "character": 1}),
	}}
	m, _, _ := reviewModel(t, p, map[string]string{"a.txt": reviewOld})
	m.output = newOutputStore()
	driveAIReview(t, m, m.sendAIChat("go"), func(m *Model) { reviewKey(t, m, "a"); reviewKey(t, m, "r") })
	m.aiUndoTurn(-1)
	lines := m.output.Lines(aiAuditChannel)
	want := []string{
		"14:03:22  read_file  path=a.txt  gate=auto  by=auto  → one (+9 lines)",
		"14:03:22  write_file  content=<10 lines, 49 B> path=a.txt  gate=review  by=user (review)  → The user accepted 1 of 2 changes to a.txt; only those were written. Rejected (NOT in the file): (+",
		"14:03:22  git_commit  message=\"change it\"  gate=confirm  by=user denied  → declined by user",
		"14:03:22  lsp_rename  character=1 line=1 path=a.txt  gate=invalid  by=refused  → error: new_name is required",
		"14:03:22  undo_turn  turn=0  gate=user  by=user  → Undo AI changes of a turn → restored a.txt",
	}
	if len(lines) != len(want) {
		t.Fatalf("%d lines:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	for i, w := range want {
		if !strings.HasPrefix(lines[i], w) {
			t.Errorf("line %d:\n got %s\nwant %s…", i, lines[i], w)
		}
	}
}

// Spec (MC-3): an MCP tool's audit line shows its display name
// (mcp.<server>.<tool>), not the wire name sent to the provider.
func TestAIAuditShowsToolLabel(t *testing.T) {
	t.Cleanup(clock.Set(func() time.Time { return time.Date(2026, 10, 15, 14, 3, 22, 0, time.Local) }))
	m := &Model{output: newOutputStore()}
	m.aiS().aiAuditStart(ai.Event{Kind: ai.EventToolStart, Tool: "mcp__fake__echo", Label: "mcp.fake.echo", Args: map[string]any{"text": "hi"}})
	m.aiAuditResult(ai.Event{Kind: ai.EventToolResult, Tool: "mcp__fake__echo", Label: "mcp.fake.echo", Detail: "echo: hi", Gate: ai.GateAuto, Decision: "auto"})
	lines := m.output.Lines(aiAuditChannel)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "14:03:22  mcp.fake.echo  text=hi  gate=auto") {
		t.Errorf("audit lines %q", lines)
	}
}

func TestAIAuditArgs(t *testing.T) {
	got := aiAuditArgs(map[string]any{"b": 2.0, "a": strings.Repeat("x", 60), "c": "two\nlines", "d": ""})
	want := `a="` + strings.Repeat("x", 39) + `…" b=2 c=<2 lines, 9 B> d=""`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
