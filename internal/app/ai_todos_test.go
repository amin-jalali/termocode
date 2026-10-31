package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

func todoCall(id string, todos ...[2]string) ai.Chunk {
	list := make([]any, 0, len(todos))
	for _, t := range todos {
		list = append(list, map[string]any{"text": t[0], "status": t[1]})
	}
	return ai.Chunk{ToolCalls: []ai.ToolCall{{ID: id, Name: "todo_write", Args: map[string]any{"todos": list}}}}
}

func TestAITodoWriteKeepsTheListAndShowsACard(t *testing.T) {
	m := aiTestModel(t)
	s := m.ai
	p := &compactFake{replies: []ai.Chunk{
		todoCall("c1", [2]string{"Read a.go", "done"}, [2]string{"Fix the bug", "in_progress"}, [2]string{"Run tests", "pending"}),
		{Delta: "working"},
		{Delta: "next"},
	}}
	s.provider = p
	s.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	runAITurn(t, &m, "fix it")
	if len(s.conv.Todos) != 3 || s.conv.Todos[1].Status != ai.TodoActive {
		t.Fatalf("todos %+v", s.conv.Todos)
	}
	got := aiTranscriptText(&m, 80)
	for _, want := range []string{"Todos · 1 of 3 done", "☑ Read a.go", "◐ Fix the bug", "☐ Run tests"} {
		if !strings.Contains(got, want) {
			t.Errorf("card misses %q:\n%s", want, got)
		}
	}
	saved, _ := s.store.Load(s.conv.ID)
	if len(saved.Todos) != 3 {
		t.Fatal("todos not saved with the chat")
	}
	runAITurn(t, &m, "go on")
	r := p.requests()
	if sys := r[len(r)-1].Messages[0].Content; !strings.Contains(sys, "[ ] Run tests") || !strings.Contains(sys, "2 open") {
		t.Fatalf("system prompt has no todos:\n%s", sys)
	}
}

func TestAITodoToolsAreReadOnlyForTheWorkspace(t *testing.T) {
	m := aiTestModel(t)
	m.ai.conv.Todos = []ai.Todo{{Text: "a", Status: ai.TodoPending}}
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(&m)
		return out, err
	}
	tools := aiTodoTools(call)
	for _, name := range []string{"todo_write", "todo_read"} {
		tl, ok := ai.FindTool(tools, name)
		if !ok || tl.Mutating || tl.Class != "" {
			t.Fatalf("%s: %+v", name, ok)
		}
	}
	read, _ := ai.FindTool(tools, "todo_read")
	if out, err := read.Run(context.Background(), nil); err != nil || out != "[ ] a" {
		t.Fatalf("read %q %v", out, err)
	}
	write, _ := ai.FindTool(tools, "todo_write")
	if _, err := write.Run(context.Background(), map[string]any{"todos": "bad"}); err == nil {
		t.Fatal("bad list accepted")
	}
	if len(m.ai.conv.Todos) != 1 {
		t.Fatal("a refused write changed the list")
	}
}
