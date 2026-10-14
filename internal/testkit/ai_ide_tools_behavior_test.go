package testkit

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Spec: Given the AI chat, when the user asks "open it" and the model
// answers with an open_file tool call, then the file opens in the editor
// as the active tab, with no confirm dialog. The agent used to say it
// could not open files because no tool switched the active buffer.
func TestAIOpenFileToolOpensTab(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		if strings.Contains(string(body), `"role":"tool"`) {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"fake-opened-it\"}}]}\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"open_file","arguments":"{\"path\":\"notes/todo.md\",\"line\":3}"}}]}}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	if testing.Short() {
		t.Skip("starts nvim")
	}
	d := New(t, Options{W: 160, H: 40, Nvim: true, Files: textFiles(),
		Setup: func(t testing.TB, _ string) {
			t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
			t.Setenv("TERMOCODE_AI_MODEL", "fake")
			t.Setenv("TERMOCODE_AI_BASE_URL", srv.URL+"/v1")
		}})
	open(d, "a.txt")
	d.Key("alt+a")
	if !d.WaitForText("AI CHAT", 2*time.Second) {
		t.Fatalf("AI panel did not open:\n%s", screen(d))
	}
	d.Type("open it")
	d.Key("enter")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "notes/todo.md" }, 5*time.Second) {
		got, a := tabs(d)
		t.Fatalf("todo.md not the active tab: active %q, tabs %v\n%s", a, got, screen(d))
	}
	if !d.WaitForText("fake-opened-it", 5*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "AI wants to") {
		t.Errorf("open_file asked for confirmation:\n%s", screen(d))
	}
}
