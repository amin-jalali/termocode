package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// OMX-10: the chat's system prompt says today's date, the OS and shell.
func TestAIChatPromptHasEnvBlock(t *testing.T) {
	m, dir := aiReasoningModelDir(t)
	t.Cleanup(clock.Set(func() time.Time { return time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC) }))
	got := m.aiChatBasePrompt(dir, ai.Rules{}, "")
	for _, want := range []string{"Workspace root: " + dir, "Today is 2026-10-31 (Saturday). OS: ", "run_command has no terminal"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in the prompt:\n%s", want, got)
		}
	}
}

func aiReasoningModelDir(t *testing.T) (*Model, string) {
	t.Helper()
	return aiIDETestModel(t, &recordingAIProvider{})
}
