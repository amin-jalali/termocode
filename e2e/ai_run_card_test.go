//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// E2E-AI (part): the AI starts a run (tasks_run) and the owner decides on
// the card, on the real binary. The model is a fake server that asks for
// the default build task once.

const runCardTasks = `{"version":1,"tasks":[{"label":"build-it","command":"touch ran.marker && echo e2e-task-out","group":{"kind":"build","isDefault":true}}]}`

func startRunCard(t *testing.T) (*Term, string) {
	t.Helper()
	ws := NewRepo(t)
	if err := os.MkdirAll(filepath.Join(ws, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".termocode", "tasks.json"), []byte(runCardTasks), 0o644); err != nil {
		t.Fatal(err)
	}
	model := fakeChatModel(t, "tasks_run", map[string]any{"group": "build"})
	env := []string{"TERMOCODE_AI_PROVIDER=ollama", "TERMOCODE_AI_MODEL=fake", "TERMOCODE_AI_BASE_URL=" + model.URL + "/v1"}
	tm := startWindow(t, t.TempDir(), ws, env)
	tm.settle()
	tm.Send("\x1ba")
	tm.WaitText("AI CHAT", uiTimeout)
	tm.settle()
	tm.Type("build it")
	tm.Send(KeyEnter)
	tm.WaitText("AI wants to", 20*time.Second)
	if !tm.Has("Run the default build task") {
		t.Fatalf("the card does not say what runs:\n%s", tm.Screen())
	}
	if exists(filepath.Join(ws, "ran.marker")) {
		t.Fatal("the task ran before the owner answered")
	}
	return tm, ws
}

// Spec: Allow runs the task in a visible terminal tab.
func TestAIRunCardAllow(t *testing.T) {
	tm, ws := startRunCard(t)
	tm.settle()
	tm.ClickText("Allow   ")
	tm.WaitText("fake-done", 30*time.Second)
	waitUntil(t, "the task ran", 15*time.Second, func() bool { return exists(filepath.Join(ws, "ran.marker")) })
	tm.WaitText("e2e-task-out", uiTimeout)
}

// Spec: Deny stops the run: the task never starts.
func TestAIRunCardDeny(t *testing.T) {
	tm, ws := startRunCard(t)
	tm.settle()
	tm.ClickText("Deny   ")
	tm.WaitText("fake-done", 30*time.Second)
	time.Sleep(time.Second)
	if exists(filepath.Join(ws, "ran.marker")) || strings.Contains(tm.Screen(), "e2e-task-out") {
		t.Fatalf("the task ran after Deny:\n%s", tm.Screen())
	}
}
