package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/aimemory"
)

func aiRecallLessons() *[]aimemory.Lesson {
	ls := []aimemory.Lesson{
		{ID: "mig", Date: "2026-10-01", Title: "Migrations", Text: "Run make migrate after you change the schema", Paths: []string{"db/schema.sql"}},
		{ID: "n1", Date: "2026-10-02", Title: "Alpha", Text: "one"},
		{ID: "n2", Date: "2026-10-03", Title: "Beta", Text: "two"},
		{ID: "n3", Date: "2026-10-04", Title: "Gamma", Text: "three"},
	}
	return &ls
}

// Spec (OMX-18): while memory is on, a tool call about a lesson's file
// recalls that lesson: the model gets it with the tool result and the
// chat shows "recalled: <title>". The same lesson is not recalled twice
// in one chat; lessons already in the turn's prompt are not recalled.
func TestAIRecallAfterToolCall(t *testing.T) {
	main := &recordingAIProvider{replies: []ai.Chunk{
		{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "read_file", Args: map[string]any{"path": "db/schema.sql"}}}},
		{ToolCalls: []ai.ToolCall{{ID: "c2", Name: "read_file", Args: map[string]any{"path": "db/schema.sql"}}}},
		{Delta: "done"},
	}}
	m, root := aiMemoryModel(t, main, nil, aiMemoryOn)
	m.ai.memFake = aiRecallLessons()
	if err := os.MkdirAll(filepath.Join(root, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "db", "schema.sql"), []byte("create table t();\n"), 0o644)
	driveAIChatMemory(t, m, m.sendAIChat("hello"))
	if len(main.reqs) != 3 {
		t.Fatalf("requests %d", len(main.reqs))
	}
	if sys := main.reqs[0].Messages[0].Content; strings.Contains(sys, "make migrate") {
		t.Fatalf("the old lesson is in the prompt already; the test needs it out:\n%s", sys)
	}
	if got := aiReqText(main.reqs[1]); !strings.Contains(got, "[Recalled lesson") || !strings.Contains(got, "Run make migrate") {
		t.Errorf("first result lacks the lesson:\n%s", got)
	}
	if got := aiReqText(main.reqs[2]); strings.Count(got, "[Recalled lesson") != 1 {
		t.Errorf("lesson recalled twice:\n%s", got)
	}
	tr := aiTranscriptText(m, 100)
	if strings.Count(tr, "recalled: Migrations") != 1 {
		t.Errorf("transcript chip:\n%s", tr)
	}
	for _, n := range []string{"Alpha", "Beta", "Gamma"} {
		if strings.Contains(tr, "recalled: "+n) {
			t.Errorf("prompt lesson %s recalled", n)
		}
	}
}

// Spec (OMX-18): in "check" mode a new match is recalled only when the
// quick model says yes; "off" and memory off recall nothing.
func TestAIRecallCheckAndOff(t *testing.T) {
	m, root := aiMemoryModel(t, &recordingAIProvider{}, &recordingAIProvider{replies: []ai.Chunk{{Delta: "No."}}}, aiMemoryOn)
	m.ai.memFake = aiRecallLessons()
	cfg, _ := ai.ConfigPath()
	os.MkdirAll(filepath.Dir(cfg), 0o755)
	os.WriteFile(cfg, []byte(`{"ai_memory_recall":"check"}`), 0o644)
	ok := func(context.Context, map[string]any) (string, error) { return "text", nil }
	tool := m.aiRecallTurn(root, "hello").wrap([]ai.Tool{{Name: "read_file", Run: ok}})[0]
	if res, _ := tool.Run(context.Background(), map[string]any{"path": "db/schema.sql"}); res != "text" {
		t.Errorf("a lesson the quick model refused was recalled: %q", res)
	}
	os.WriteFile(cfg, []byte(`{"ai_memory_recall":"off"}`), 0o644)
	if r := m.aiRecallTurn(root, "hello"); r != nil {
		t.Error("recall on with ai_memory_recall off")
	}
	os.WriteFile(cfg, []byte(`{}`), 0o644)
	setAIMemory(aiMemoryOff)
	if r := m.aiRecallTurn(root, "hello"); r != nil {
		t.Error("recall on with memory off")
	}
}
