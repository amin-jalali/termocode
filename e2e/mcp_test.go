//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeChatModel is an OpenAI-compatible streaming endpoint: the first
// request answers with one tool call, every later one with "fake-done".
func fakeChatModel(t *testing.T, tool string, args map[string]any) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		mu.Lock()
		i := n
		n++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		var chunk any
		if i == 0 {
			a, _ := json.Marshal(args)
			chunk = map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
				map[string]any{"index": 0, "id": "call_0", "function": map[string]any{"name": tool, "arguments": string(a)}}}}}}}
		} else {
			chunk = map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "fake-done"}}}}
		}
		b, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Spec (MC-4): the real binary with a shell MCP server (newline JSON over
// stdio). Nothing starts with termocode; the servers list shows it
// stopped; the first tool call of the chat starts it and its answer
// reaches the model; after mcp_idle_timeout it is gone again and the
// list says stopped, with its tool remembered.
func TestMCPClientLazyStart(t *testing.T) {
	script, err := filepath.Abs("testdata/fake-mcp.sh")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	marker := filepath.Join(home, "fake-mcp.running")
	log := filepath.Join(home, "fake-mcp.log")
	cfg := filepath.Join(home, ".config", "termocode")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpJSON, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"fake": map[string]any{
		"command": "sh", "args": []string{script}, "env": map[string]string{"FAKE_MARKER": marker, "FAKE_LOG": log}}}})
	if err := os.WriteFile(filepath.Join(cfg, "mcp.json"), mcpJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"mcp_idle_timeout":"2s"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	model := fakeChatModel(t, "mcp__fake__echo", map[string]any{"text": "hi"})

	tm := Start(t, StartOpts{Cols: 140, Rows: 40, Home: home, Env: []string{
		"TERMOCODE_AI_PROVIDER=ollama", "TERMOCODE_AI_MODEL=fake", "TERMOCODE_AI_BASE_URL=" + model.URL + "/v1"}})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	if exists(marker) || exists(log) {
		t.Fatal("the MCP server started with termocode")
	}

	tm.Palette("AI: MCP Servers")
	tm.WaitText("fake", uiTimeout)
	tm.WaitText("local · stopped", uiTimeout)
	if exists(marker) {
		t.Error("listing the servers started one")
	}
	tm.Send(KeyEsc)
	tm.settle()

	tm.Send("\x1ba") // the AI chat
	tm.WaitText("AI CHAT", uiTimeout)
	tm.Type("echo hi")
	tm.Send(KeyEnter)
	tm.WaitText("fake-done", 20*time.Second)
	tm.WaitText("mcp.fake.echo", uiTimeout)
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "tools/call") {
		t.Fatalf("the server got no call: %q", b)
	}

	deadline := time.Now().Add(15 * time.Second)
	for exists(marker) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if exists(marker) {
		t.Fatal("the MCP server still runs after the idle timeout")
	}
	tm.Palette("AI: MCP Servers")
	tm.WaitText("local · stopped · 1 tools", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Quit()
}
