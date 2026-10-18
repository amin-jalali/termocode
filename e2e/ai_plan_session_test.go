//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Task markers of the diamond plan (map → left, right → verify) and the
// answers of its steps. Each planned step's request holds its own task
// under "### Task", so the marker says which step is asking.
const (
	planMapTask    = "map-task-41d0"
	planLeftTask   = "left-task-8b2e"
	planRightTask  = "right-task-c5a7"
	planVerifyTask = "verify-task-03f9"

	planMapOut    = "map-answer-5e61"
	planLeftOut   = "left-answer-a9c4"
	planRightOut  = "right-answer-2d7b"
	planVerifyOut = "verify-final-answer-6f18"
)

// diamondPlan is the planner's answer: a fenced json block.
var diamondPlan = "Here is the plan.\n```json\n" + `{"nodes": [
  {"id": "map", "title": "Map", "prompt": "` + planMapTask + `", "category": "quick", "deps": []},
  {"id": "left", "title": "Left side", "prompt": "` + planLeftTask + `", "category": "quick", "deps": ["map"]},
  {"id": "right", "title": "Right side", "prompt": "` + planRightTask + `", "category": "quick", "deps": ["map"]},
  {"id": "verify", "title": "Verify", "prompt": "` + planVerifyTask + `", "category": "deep", "deps": ["left", "right"]}
]}` + "\n```\n"

// planModel is a scripted OpenAI-compatible model for a planned run. The
// planner gets diamondPlan; the two middle steps wait until release is
// closed (so both are in flight at once); verify answers planVerifyOut.
type planModel struct {
	srv     *httptest.Server
	release chan struct{}
	leftIn  chan struct{} // closed when the left step's request arrives
	rightIn chan struct{} // closed when the right step's request arrives
	served  chan struct{} // closed when verify's answer was sent

	mu     sync.Mutex
	bodies map[string]string // step → last request body
}

func newPlanModel(t *testing.T) *planModel {
	t.Helper()
	pm := &planModel{release: make(chan struct{}), leftIn: make(chan struct{}),
		rightIn: make(chan struct{}), served: make(chan struct{}), bodies: map[string]string{}}
	var onceL, onceR, onceV sync.Once
	pm.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		step, text := "other", "unexpected request"
		wait := func(in chan struct{}, once *sync.Once) bool {
			once.Do(func() { close(in) })
			select {
			case <-pm.release:
				return true
			case <-r.Context().Done():
				return false
			}
		}
		// Order matters: verify's request holds the left and right
		// answers, not their tasks; no planned step sees the plan.
		switch {
		case strings.Contains(body, "You are the planner"):
			step, text = "planner", diamondPlan
		case strings.Contains(body, planVerifyTask):
			step, text = "verify", planVerifyOut
		case strings.Contains(body, planLeftTask):
			step, text = "left", planLeftOut
		case strings.Contains(body, planRightTask):
			step, text = "right", planRightOut
		case strings.Contains(body, planMapTask):
			step, text = "map", planMapOut
		}
		pm.mu.Lock()
		pm.bodies[step] = body
		pm.mu.Unlock()
		switch step {
		case "left":
			if !wait(pm.leftIn, &onceL) {
				return
			}
		case "right":
			if !wait(pm.rightIn, &onceR) {
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		content, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": text}}}})
		usage, _ := json.Marshal(map[string]any{"choices": []any{}, "model": "fake",
			"usage": map[string]any{"prompt_tokens": 900, "completion_tokens": 200}})
		fmt.Fprintf(w, "data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", content, usage)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if step == "verify" {
			onceV.Do(func() { close(pm.served) })
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

func (pm *planModel) body(step string) string {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.bodies[step]
}

// Spec (U20 AP-10): a planned run lives in the session process. The
// built-in plan profile asks the planner; the fake planner answers a
// diamond (map → left, right → verify). Both middle steps are in flight
// at the same time; the AGENTS panel shows the steps and the wave
// header; the client disconnects while they run; the run finishes with
// no client; a new client sees the finished graph and the final answer.
// The final step's request holds both middle answers.
func TestAIPlanDiamondSurvivesDisconnect(t *testing.T) {
	pm := newPlanModel(t)
	r := newSessionRig(t)
	r.env = append(r.env, "TERMOCODE_AI_PROVIDER=ollama", "TERMOCODE_AI_MODEL=fake",
		"TERMOCODE_AI_BASE_URL="+pm.srv.URL+"/v1")

	tm := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env, Cols: 200, Rows: 48})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	r.waitSocket(true)

	tm.Palette("AI: Switch Profile")
	tm.WaitText("AI Profile", uiTimeout)
	tm.Type("plan")
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitGone("AI Profile", uiTimeout)

	tm.Send("\x1ba") // the AI chat
	tm.WaitText("AI CHAT", uiTimeout)
	tm.WaitText("plan ▾", uiTimeout)
	tm.Type("split the work")
	tm.Send(KeyEnter)

	// Both middle steps ask the model before either got an answer.
	waitChan(t, pm.leftIn, "the left step's request", 20*time.Second)
	waitChan(t, pm.rightIn, "the right step's request (it must run next to the left step)", 20*time.Second)
	if b := pm.body("left"); !strings.Contains(b, planMapOut) || strings.Contains(b, planRightTask) {
		t.Errorf("the left step's input is wrong (wants map's answer, not other tasks):\n%.2000s", b)
	}

	// The AGENTS panel: the wave header and every step.
	tm.Send("\x1bA") // Alt+Shift+A on a legacy terminal
	tm.WaitText("AGENTS", uiTimeout)
	tm.WaitText("wave 3/4", uiTimeout)
	for _, title := range []string{"Planner", "Map", "Left side", "Right side", "Verify"} {
		tm.WaitText(title, uiTimeout)
	}

	// The SSH connection drops while the middle steps run.
	tm.Disconnect()
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(r.socket()); err != nil {
		t.Fatalf("session gone after the client died: %v", err)
	}

	// The middle steps answer with nobody attached; verify must still run.
	close(pm.release)
	waitChan(t, pm.served, "verify's answer (the run went on with no client)", 20*time.Second)
	vb := pm.body("verify")
	for _, want := range []string{planLeftOut, planRightOut, "### Output of step"} {
		if !strings.Contains(vb, want) {
			t.Fatalf("the final step's request misses %q:\n%.3000s", want, vb)
		}
	}
	if strings.Contains(vb, planMapOut) {
		t.Errorf("the final step got map's answer, but it only depends on left and right:\n%.3000s", vb)
	}

	// Reconnect: the AGENTS panel shows the finished run.
	tm2 := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env, Cols: 200, Rows: 48})
	tm2.WaitText("AGENTS", bootTimeout)
	tm2.WaitText("wave 4/4 · done 5/5", uiTimeout)
	for _, title := range []string{"Planner", "Map", "Left side", "Right side", "Verify"} {
		tm2.WaitText(title, uiTimeout)
	}
	if tm2.Has("running") || tm2.Has("interrupted") {
		t.Errorf("run not finished after reattach:\n%s", tm2.Screen())
	}

	// Back to the chat: the final answer is the run's answer.
	tm2.Send("\x1ba")
	tm2.WaitText("AI CHAT", uiTimeout)
	tm2.WaitText(planVerifyOut, uiTimeout)
	tm2.WaitText("5 steps", uiTimeout)
	tm2.settle()
	tm2.Quit()
	r.waitSocket(false)
}
