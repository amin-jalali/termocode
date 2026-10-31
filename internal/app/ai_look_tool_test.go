package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// Spec (OMX-32): the chat offers look_at; with a main model that has no
// image input it says so instead of sending the picture.
func TestAILookAtToolInChat(t *testing.T) {
	aiTestModel(t)
	m := &Model{nvim: &nvim.Client{}}
	dir := t.TempDir()
	writeAIRules(t, dir, "shot.png", "\x89PNG fake")
	tool, ok := ai.FindTool(m.aiChatTools(dir, mcpServeNoCall), "look_at")
	if !ok {
		t.Fatal("no look_at tool in the chat")
	}
	if tool.Mutating {
		t.Error("look_at asks before it runs")
	}
	if _, err := tool.Run(context.Background(), map[string]any{"path": "shot.png"}); err == nil || !strings.Contains(err.Error(), "can not see images") {
		t.Errorf("no model: err = %v", err)
	}
}
