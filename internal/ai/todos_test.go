package ai

import (
	"strings"
	"testing"
)

func TestParseTodosChecksAndCleans(t *testing.T) {
	ts, err := ParseTodos(map[string]any{"todos": []any{
		map[string]any{"text": "  Read \x1b[31ma.go\n ", "status": "done"},
		map[string]any{"text": "Fix the bug", "status": "in_progress"},
		map[string]any{"text": "Run tests"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 3 || ts[0].Text != "Read a.go" || ts[0].Status != TodoDone || ts[2].Status != TodoPending {
		t.Fatalf("%+v", ts)
	}
	if TodosOpen(ts) != 2 {
		t.Fatalf("open %d", TodosOpen(ts))
	}
	for _, bad := range []map[string]any{
		{"todos": "x"},
		{"todos": []any{map[string]any{"text": ""}}},
		{"todos": []any{map[string]any{"text": "a", "status": "later"}}},
		{"todos": make([]any, MaxTodos+1)},
	} {
		if _, err := ParseTodos(bad); err == nil {
			t.Errorf("%v: no error", bad)
		}
	}
	if ts, err := ParseTodos(map[string]any{"todos": []any{}}); err != nil || len(ts) != 0 {
		t.Fatalf("an empty list clears: %v %v", ts, err)
	}
	long, _ := ParseTodos(map[string]any{"todos": []any{map[string]any{"text": strings.Repeat("é", 500)}}})
	if n := len([]rune(long[0].Text)); n > TodoTextMax {
		t.Fatalf("text not capped: %d", n)
	}
}

func TestTodosTextAndPrompt(t *testing.T) {
	ts := []Todo{{Text: "a", Status: TodoDone}, {Text: "b", Status: TodoActive}, {Text: "c", Status: TodoPending}}
	if got := TodosText(ts); got != "[x] a\n[~] b\n[ ] c" {
		t.Fatalf("%q", got)
	}
	if p := TodosPrompt(ts); !strings.Contains(p, "[~] b") || !strings.Contains(p, "2 open") {
		t.Fatalf("%q", p)
	}
	if TodosPrompt([]Todo{{Text: "a", Status: TodoDone}}) != "" || TodosPrompt(nil) != "" {
		t.Fatal("a list with nothing open needs no prompt")
	}
}
