package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// OMX-25: the chat's agent knows the model's context window
// (ai_context_windows over the built-in table) and cuts big tool results
// when the window is nearly full.
func TestAIChatFitsToolResultsToTheWindow(t *testing.T) {
	p := &recordingAIProvider{replies: []ai.Chunk{
		{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "read_file", Args: map[string]any{"path": "big.txt"}}},
			Usage: &ai.Usage{Model: "fake-1", Input: 9000, Requests: 1, PeakContext: 9000}},
		{Delta: "read it"},
	}}
	m, dir := aiIDETestModel(t, p)
	m.ai.settings.ContextWindows = map[string]int{"fake-1": 10000}
	big := strings.Repeat("line of text\n", 3000)
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	driveAIChat(t, m, m.sendAIChat("read big.txt"), nil)
	p.mu.Lock()
	defer p.mu.Unlock()
	msgs := p.reqs[len(p.reqs)-1].Messages
	res := msgs[len(msgs)-1]
	if res.Role != ai.RoleTool || !strings.Contains(res.Content, "cut to fit the context window") {
		t.Fatalf("tool result not cut (%d chars): %.200q", len(res.Content), res.Content)
	}
}
