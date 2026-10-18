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

// pipelineModel is a scripted OpenAI-compatible model for a two-step
// agent pipeline. Step 1 ("You are step 1 of 2") waits until release is
// closed, then answers step1Out; step 2 answers step2Out. It records the
// bodies it got and closes served once step 2 got its answer.
type pipelineModel struct {
	srv     *httptest.Server
	release chan struct{}
	step1In chan struct{} // closed when step 1's request arrives
	served  chan struct{} // closed when step 2's answer was sent

	mu     sync.Mutex
	bodies []string
}

const (
	step1Out = "drafter-output-7f3a"
	step2Out = "fixer-final-answer-9c1e"
)

func newPipelineModel(t *testing.T) *pipelineModel {
	t.Helper()
	pm := &pipelineModel{release: make(chan struct{}), step1In: make(chan struct{}), served: make(chan struct{})}
	var once1, once2 sync.Once
	pm.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		pm.mu.Lock()
		pm.bodies = append(pm.bodies, body)
		pm.mu.Unlock()
		text := "unexpected request"
		switch {
		case strings.Contains(body, "You are step 1 of 2"):
			once1.Do(func() { close(pm.step1In) })
			select {
			case <-pm.release:
			case <-r.Context().Done():
				return
			}
			text = step1Out
		case strings.Contains(body, "You are step 2 of 2"):
			text = step2Out
		}
		w.Header().Set("Content-Type", "text/event-stream")
		content, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": text}}}})
		usage, _ := json.Marshal(map[string]any{"choices": []any{}, "model": "fake",
			"usage": map[string]any{"prompt_tokens": 1200, "completion_tokens": 300}})
		fmt.Fprintf(w, "data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", content, usage)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if text == step2Out {
			once2.Do(func() { close(pm.served) })
		}
	}))
	t.Cleanup(func() {
		select {
		case <-pm.release:
		default:
			close(pm.release)
		}
		pm.srv.Close()
	})
	return pm
}

// step2Body is the body of the request step 2 sent ("" when none).
func (pm *pipelineModel) step2Body() string {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, b := range pm.bodies {
		if strings.Contains(b, "You are step 2 of 2") {
			return b
		}
	}
	return ""
}

func waitChan(t *testing.T, ch <-chan struct{}, what string, d time.Duration) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(d):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// Spec (U20 AP-7): an agent pipeline run lives in the session process. A
// two-step profile on a scripted model starts a run; the client
// disconnects while step 1 is still going; the run finishes with no
// client attached; a new client attaches and sees both step cards, the
// final answer and the run row. Step 2's request holds step 1's answer.
func TestAIPipelineSurvivesDisconnect(t *testing.T) {
	pm := newPipelineModel(t)
	r := newSessionRig(t)
	r.env = append(r.env, "TERMOCODE_AI_PROVIDER=ollama", "TERMOCODE_AI_MODEL=fake",
		"TERMOCODE_AI_BASE_URL="+pm.srv.URL+"/v1")
	prof := filepath.Join(r.home, ".config", "termocode", "ai", "profiles.json")
	if err := os.MkdirAll(filepath.Dir(prof), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prof, []byte(`{"profiles": {"pipe": {"title": "Two steps", "steps": [
		{"id": "drafter", "title": "Drafter", "tools": {"preset": "none"}},
		{"id": "fixer", "title": "Fixer", "tools": {"preset": "none"}}
	]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tm := r.start()
	r.waitSocket(true)

	tm.Palette("AI: Switch Profile")
	tm.WaitText("AI Profile", uiTimeout)
	tm.Type("pipe")
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitGone("AI Profile", uiTimeout)

	tm.Send("\x1ba") // the AI chat
	tm.WaitText("AI CHAT", uiTimeout)
	tm.WaitText("pipe ▾", uiTimeout)
	tm.Type("run the pipeline")
	tm.Send(KeyEnter)
	waitChan(t, pm.step1In, "step 1's request", 20*time.Second)
	tm.WaitText("1 · Drafter", uiTimeout)
	tm.WaitText("2 · Fixer · fake · waiting", uiTimeout)

	// The SSH connection drops while step 1 runs.
	tm.Disconnect()
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(r.socket()); err != nil {
		t.Fatalf("session gone after the client died: %v", err)
	}

	// Step 1 answers with nobody attached; step 2 must still run.
	close(pm.release)
	waitChan(t, pm.served, "step 2's answer (the run went on with no client)", 20*time.Second)
	if b := pm.step2Body(); !strings.Contains(b, step1Out) || !strings.Contains(b, "### Output of step") {
		t.Fatalf("step 2's request does not hold step 1's answer:\n%.2000s", b)
	}

	// Reconnect: the run is done, with its cards and its answer.
	tm2 := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env, Cols: 200, Rows: 40})
	tm2.WaitText(step2Out, bootTimeout)
	tm2.WaitText("1 · Drafter", uiTimeout)
	tm2.WaitText("2 · Fixer", uiTimeout)
	tm2.WaitText("run · ", uiTimeout)
	tm2.WaitText("2 steps", uiTimeout)
	if tm2.Has("running") || tm2.Has("interrupted") {
		t.Errorf("run not finished after reattach:\n%s", tm2.Screen())
	}
	tm2.settle()
	tm2.Quit()
	r.waitSocket(false)
}
