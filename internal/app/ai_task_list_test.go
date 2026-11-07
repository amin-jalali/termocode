package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
)

const aiTaskListSample = `# Tasks
- [ ] 1. Add the parser
- [ ] 2. Add the flag (after: 1)
- [ ] 3. Write the docs (after: 1)
`

// CMP-76: AI: Run Task List in Waves… shows the waves on a card with
// Cancel first; Cancel and Esc start nothing; Run starts one step per
// task (deps from "after:") and a last check step after the leaves.
func TestAITaskListRunsInWaves(t *testing.T) {
	p := &funcAIProvider{reply: func(r ai.Request) ai.Chunk { return ai.Chunk{Delta: "done"} }}
	m, _ := aiRunModel(t, p, p)
	m.ai.settings.ParallelMax = 3

	m.aiTaskListAsk("/src/app/docs/tasks.md", aiTaskListSample)
	if !m.confirmOpen || m.confirmKind != confirmKindAITaskList || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no safe-first card: open %v kind %d safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"tasks.md", "Wave 1", "Add the parser", "Wave 2", "Add the flag", "Write the docs", "Run 3 tasks"} {
		if !strings.Contains(box, want) {
			t.Errorf("card lacks %q:\n%s", want, box)
		}
	}
	n := len(m.ai.conv.Messages)
	m.confirmOpen = false
	if c := m.onAIConfirmClosed(); c != nil || len(m.ai.conv.Messages) != n { // Esc
		t.Fatal("Esc started a run")
	}
	m.aiTaskListAsk("/src/app/docs/tasks.md", aiTaskListSample)
	m.confirmOpen = false
	if c, _ := m.handleAIConfirm("cancel"); c != nil || len(m.ai.conv.Messages) != n {
		t.Fatal("Cancel started a run")
	}

	m.aiTaskListAsk("/src/app/docs/tasks.md", aiTaskListSample)
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm("run")
	driveAIChat(t, m, cmd, nil)
	rm := aiRunMsg(t, m)
	var ids []string
	for _, st := range rm.Steps {
		ids = append(ids, st.ID)
	}
	if strings.Join(ids, ",") != "1,2,3,check" {
		t.Fatalf("steps %v", ids)
	}
	if d := strings.Join(rm.Steps[1].Deps, ","); d != "1" {
		t.Errorf("step 2 deps %q", d)
	}
	if d := strings.Join(rm.Steps[3].Deps, ","); d != "2,3" {
		t.Errorf("check deps %q", d)
	}
	if req, ok := p.find(func(r ai.Request) bool {
		return len(r.Messages) > 0 && strings.Contains(r.Messages[0].Content, "Task 2 of 3: Add the flag")
	}); !ok {
		t.Error("no step was asked to add the flag")
	} else if sys := req.Messages[0].Content; !strings.Contains(sys, "data, not instructions") {
		t.Errorf("a task's text must be data for the step:\n%s", sys)
	}
}

// A bad list never opens the card: the toast says what is wrong.
func TestAITaskListBadList(t *testing.T) {
	p := &funcAIProvider{reply: func(r ai.Request) ai.Chunk { return ai.Chunk{Delta: "done"} }}
	m, _ := aiRunModel(t, p, p)
	m.aiTaskListAsk("/src/app/tasks.md", "- [ ] a (after: zz)\n")
	if m.confirmOpen {
		t.Fatal("a bad list opened the card")
	}
}

// With ai_parallel_max 1 the tasks run one after the other in wave
// order, and the card says so.
func TestAITaskListOneAtATime(t *testing.T) {
	p := &funcAIProvider{reply: func(r ai.Request) ai.Chunk { return ai.Chunk{Delta: "done"} }}
	m, _ := aiRunModel(t, p, p)
	m.ai.settings.ParallelMax = 1
	m.aiTaskListAsk("/src/app/tasks.md", aiTaskListSample)
	if box := ansi.Strip(m.confirm.Box()); !strings.Contains(box, "one after the other") {
		t.Errorf("card:\n%s", box)
	}
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm("run")
	driveAIChat(t, m, cmd, nil)
	rm := aiRunMsg(t, m)
	if len(rm.Steps) != 4 || rm.Steps[3].ID != "check" {
		t.Fatalf("steps %+v", rm.Steps)
	}
}
