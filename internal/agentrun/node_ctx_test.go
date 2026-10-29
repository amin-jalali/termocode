package agentrun

import (
	"context"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// G-API: a node's tools know which node and run call them (a tool that
// starts another run names the step that asked); outside a run there is
// none.
func TestNodeOfContext(t *testing.T) {
	if id, run := NodeOf(context.Background()); id != "" || run != "" {
		t.Fatalf("outside a run: %q %q", id, run)
	}
	var mu sync.Mutex
	seen := map[string]string{}
	h := newHarness(map[string]*fakeProv{
		"a": {replies: []ai.Chunk{{ToolCalls: []ai.ToolCall{{ID: "1", Name: "who"}}}, {Delta: "done"}}},
		"b": {replies: []ai.Chunk{{ToolCalls: []ai.ToolCall{{ID: "1", Name: "who"}}}, {Delta: "done"}}},
	})
	h.tools = []ai.Tool{{Name: "who", Run: func(ctx context.Context, _ map[string]any) (string, error) {
		id, run := NodeOf(ctx)
		mu.Lock()
		seen[id] = run
		mu.Unlock()
		return "ok", nil
	}}}
	r, err := Start(context.Background(), chain("a", "b"), h.options(1))
	if err != nil {
		t.Fatal(err)
	}
	if end := wait(t, r); end.Kind != RunDone {
		t.Fatalf("end %+v", end)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen["a"] != "run1" || seen["b"] != "run1" || len(seen) != 2 {
		t.Fatalf("seen %v", seen)
	}
}
