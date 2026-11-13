//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// E2E-AI (part): Tab takes the next edit suggestion (G-NES) on the real
// binary. The model is a fake server that answers the next edit request
// (its user message has "Recent edits") with one JSON change.

// Spec: after an edit on line 1 the model proposes line 5. The first Tab
// jumps to it and shows the change, the second Tab applies it; the
// buffer then holds the new line and one request was made.
func TestAINextEditTabJumpsThenApplies(t *testing.T) {
	var reqs atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		answer := "ok"
		for _, m := range req.Messages {
			if m.Role == "user" && strings.Contains(fmt.Sprint(m.Content), "Recent edits") {
				reqs.Add(1)
				answer = `{"start":5,"end":5,"lines":["call nesmark add(1, 2)"]}`
			}
		}
		out, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": answer}}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", out)
	}))
	t.Cleanup(srv.Close)

	home, ws := t.TempDir(), NewRepo(t)
	body := "func add(a, b)\n\nreturn a + b\n\ncall add(1, 2)\n"
	if err := os.WriteFile(filepath.Join(ws, "calc.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	env := []string{"TERMOCODE_AI_PROVIDER=ollama", "TERMOCODE_AI_MODEL=fake", "TERMOCODE_AI_BASE_URL=" + srv.URL + "/v1"}
	tm := startWindow(t, home, ws, env)
	tm.settle()
	tm.OpenFile("calc.txt")
	tm.WaitText("func add(a, b)", uiTimeout)
	tm.Type("// ")
	tm.WaitText("// func add(a, b)", uiTimeout)
	tm.Send(KeyEsc)

	// A hint marks the target line; the first Tab jumps there and shows
	// the new line, the second Tab applies it.
	tm.WaitText("next edit", uiTimeout)
	tm.Send("\t")
	tm.WaitText("call nesmark add(1, 2)", uiTimeout)
	tm.settle()
	tm.Send("\t")
	tm.WaitGone("- call add(1, 2)", uiTimeout)
	tm.WaitText("call nesmark add(1, 2)", uiTimeout)
	if n := reqs.Load(); n != 1 {
		t.Errorf("%d next edit requests, want 1", n)
	}
	tm.quitDiscard()
}
