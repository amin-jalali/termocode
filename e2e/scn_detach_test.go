//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// editModel is a scripted OpenAI-compatible model: a request whose last
// message is the user's (no tool result yet) answers with one write_file
// call; a request that carries a tool result answers with done. It
// records every request body.
type editModel struct {
	srv  *httptest.Server
	path string // the file write_file writes
	text string // the text it writes
	done string // the answer after the tool result

	mu     sync.Mutex
	bodies []string
}

func newEditModel(t *testing.T, path, text, done string) *editModel {
	t.Helper()
	em := &editModel{path: path, text: text, done: done}
	em.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		body := string(b)
		em.mu.Lock()
		em.bodies = append(em.bodies, body)
		em.mu.Unlock()
		var chunk any
		if strings.Contains(body, `"role":"tool"`) {
			chunk = map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": done}}}}
		} else {
			a, _ := json.Marshal(map[string]any{"path": path, "content": text})
			chunk = map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
				map[string]any{"index": 0, "id": "call_w1", "function": map[string]any{"name": "write_file", "arguments": string(a)}}}}}}}
		}
		out, _ := json.Marshal(chunk)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", out)
	}))
	t.Cleanup(em.srv.Close)
	return em
}

// requests returns the bodies the model got so far.
func (em *editModel) requests() []string {
	em.mu.Lock()
	defer em.mu.Unlock()
	return append([]string(nil), em.bodies...)
}

// env points termocode's AI at the fake model.
func (em *editModel) env() []string {
	return []string{"TERMOCODE_AI_PROVIDER=ollama", "TERMOCODE_AI_MODEL=fake", "TERMOCODE_AI_BASE_URL=" + em.srv.URL + "/v1"}
}

// goTestEnv lets `go test` inside termocode reuse the host's build cache
// (a fresh one compiles the standard library: slow) and never download a
// toolchain.
func goTestEnv(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOCACHE").Output()
	if err != nil {
		t.Skipf("go env: %v", err)
	}
	return []string{"GOCACHE=" + strings.TrimSpace(string(out)), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod"}
}

// waitFileHas waits until the file at p holds want.
func waitFileHas(t *testing.T, p, want string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !strings.Contains(readFileOrEmpty(p), want) {
		if time.Now().After(deadline) {
			t.Fatalf("%s: no %q after %v (has %q)", filepath.Base(p), want, d, readFileOrEmpty(p))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Gated scripts: each start and end appends a line to a log file, and
// each waits for its gate file, so the test controls when they finish
// and can count how often they ran.
const (
	scnTaskJSON = `{"version": "2.0.0", "tasks": [{"label": "slow-task", "command": "sh",
  "args": ["-c", "echo start >> task-runs.log; echo task-started-7; while [ ! -f task.gate ]; do sleep 0.1; done; echo end >> task-runs.log; echo task-done-7"]}]}`
	scnGateTest = `package main

import (
	"os"
	"testing"
	"time"
)

func TestGate(t *testing.T) {
	f, err := os.OpenFile("test-runs.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("start\n")
	for i := 0; i < 1200; i++ {
		if _, err := os.Stat("test.gate"); err == nil {
			f.WriteString("end\n")
			f.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("no gate")
}
`
)

// countLines counts the lines of p equal to s.
func countLines(p, s string) int {
	n := 0
	for _, l := range strings.Split(readFileOrEmpty(p), "\n") {
		if l == s {
			n++
		}
	}
	return n
}

// Spec (SCN-3): the user detaches while an AI edit waits in the review
// card, a task runs in a terminal and a test run is going. The task and
// the tests finish while no client is attached. A new client attaches,
// sees the waiting review, accepts it; the file on disk has the AI's
// text, the chat goes on, the task and the test results are on screen,
// and nothing ran twice or got lost.
func TestSCN3DetachWithReviewTaskAndTests(t *testing.T) {
	const aiText = "first note\nai-line-3c\n"
	model := newEditModel(t, "notes.txt", aiText, "ai-finished-3c")
	r := newSessionRig(t)
	r.env = append(append(r.env, model.env()...), goTestEnv(t)...)
	writeFile(t, filepath.Join(r.ws, "go.mod"), "module scn\n\ngo 1.21\n")
	writeFile(t, filepath.Join(r.ws, "gate_test.go"), scnGateTest)
	writeFile(t, filepath.Join(r.ws, ".termocode", "tasks.json"), scnTaskJSON)
	notes := filepath.Join(r.ws, "notes.txt")
	taskLog := filepath.Join(r.ws, "task-runs.log")
	testLog := filepath.Join(r.ws, "test-runs.log")

	tm := r.start()
	r.waitSocket(true)

	// (b) a task in a terminal.
	tm.Palette("Tasks: Run Task...")
	tm.Type("slow-task")
	tm.settle()
	tm.Send(KeyEnter)
	waitFileHas(t, taskLog, "start", 20*time.Second)
	tm.WaitText("task-started-7", uiTimeout)

	// (c) a test run.
	tm.Palette("Testing: Run All Tests")
	waitFileHas(t, testLog, "start", 90*time.Second)

	// (a) an AI edit waiting in the review card.
	tm.Send("\x1ba")
	tm.WaitText("AI CHAT", uiTimeout)
	tm.Type("add a line to notes")
	tm.Send(KeyEnter)
	tm.WaitText("Accept file", 20*time.Second)
	tm.settle()

	// Hide the card (Esc) and detach: the review keeps waiting.
	tm.Send(KeyEsc)
	tm.WaitGone("Accept file", uiTimeout)
	tm.WaitText("1 file to review", uiTimeout)
	tm.Palette("Session: Detach")
	select {
	case <-tm.exited:
	case <-time.After(10 * time.Second):
		t.Fatalf("client still running after Session: Detach\n%s", tm.Screen())
	}
	tm.WaitText("the session keeps running", uiTimeout)
	if _, err := os.Stat(r.socket()); err != nil {
		t.Fatalf("session gone after detach: %v", err)
	}

	// While no client is attached: the task and the tests finish; the
	// review still waits, so the file is unchanged.
	writeFile(t, filepath.Join(r.ws, "task.gate"), "")
	writeFile(t, filepath.Join(r.ws, "test.gate"), "")
	waitFileHas(t, taskLog, "end", 20*time.Second)
	waitFileHas(t, testLog, "end", 20*time.Second)
	if got := readFileOrEmpty(notes); got != "first note\n" {
		t.Fatalf("notes.txt changed before the review was accepted: %q", got)
	}

	tm2 := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env, Cols: 140, Rows: 40})
	tm2.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm2.WaitText("1 file to review", uiTimeout)
	tm2.settle()
	tm2.ClickText("Review")
	tm2.WaitText("Accept file", uiTimeout)
	tm2.WaitText("ai-line-3c", uiTimeout)
	tm2.settle()
	tm2.ClickText("Accept file")
	tm2.WaitText("ai-finished-3c", 20*time.Second)
	if got := readFileOrEmpty(notes); got != aiText {
		t.Errorf("notes.txt after accept: %q, want %q", got, aiText)
	}
	tm2.settle()
	tm2.CheckScreen("after accept", mainFrame)

	// The task's terminal shows its end; the test run its result.
	tm2.Palette("View: Test Results")
	tm2.WaitText("TestGate", uiTimeout)
	tm2.Send(KeyCtrlT)
	tm2.WaitText("task-done-7", uiTimeout)

	if n := countLines(taskLog, "start"); n != 1 {
		t.Errorf("the task started %d times, want 1", n)
	}
	if n := countLines(testLog, "start"); n != 1 {
		t.Errorf("the tests ran %d times, want 1", n)
	}
	calls := 0
	for _, b := range model.requests() {
		if !strings.Contains(b, `"role":"tool"`) {
			calls++
		}
	}
	if calls != 1 {
		t.Errorf("the model was asked %d times for the edit, want 1", calls)
	}
	tm2.Quit()
	r.waitSocket(false)
}
