package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// OMX-4: the chat moves to the backup model and says so.

func TestAIChatFallsBackToTheBackupModel(t *testing.T) {
	m, _ := aiReasoningModel(t)
	main := &recordingAIProvider{replies: []ai.Chunk{{Err: ai.ErrNotConfigured}}}
	backup := &recordingAIProvider{replies: []ai.Chunk{{Delta: "answer from the backup"}, {Delta: "second answer"}}}
	m.ai.slotFake = map[string]aiSlot{
		ai.SlotMain: {p: main, cfg: ai.Config{Kind: ai.KindClaudeOAuth, Model: "claude-opus-5-5"}},
		"gpt":       {p: backup, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5"}},
	}
	m.ai.settings.Fallback = []string{"nope", "gpt"} // a slot that does not resolve is skipped
	driveAIChat(t, m, m.sendAIChat("q"), nil)
	var lines []string
	for _, sm := range m.ai.conv.Messages {
		lines = append(lines, string(sm.Role)+": "+sm.Text)
	}
	all := strings.Join(lines, "\n")
	if !strings.Contains(all, "main (claude-opus-5-5) failed: it is not set up. Now using gpt (gpt-5).") || !strings.Contains(all, "answer from the backup") {
		t.Fatalf("chat:\n%s", all)
	}
	if n := len(m.ai.conv.Messages); m.ai.conv.Messages[n-1].Model != "gpt-5" {
		t.Errorf("the answer is not labelled with the backup's model: %+v", m.ai.conv.Messages[n-1])
	}
	if strings.Contains(all, "error: ") {
		t.Errorf("an error was shown:\n%s", all)
	}
	driveAIChat(t, m, m.sendAIChat("again"), nil)
	backup.mu.Lock()
	defer backup.mu.Unlock()
	for _, msg := range backup.reqs[len(backup.reqs)-1].Messages {
		if strings.Contains(msg.Content, "Now using") {
			t.Errorf("the fallback line was sent to the model: %q", msg.Content)
		}
	}
}
