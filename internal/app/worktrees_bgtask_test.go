package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/containers"
)

// bgFake swaps the engine of background tasks for a recorder.
type bgFake struct {
	mu    sync.Mutex
	calls [][]string
	out   string
}

func bgFakeEngine(t *testing.T) *bgFake {
	t.Helper()
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_HOST", "")
	f := &bgFake{}
	oldL, oldR := wtBgLocate, wtBgRun
	wtBgLocate = func(string) containers.Engine { return containers.Engine{Binary: "docker", Kind: "docker"} }
	wtBgRun = func(_ context.Context, _ containers.Engine, argv []string) (string, string, int, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, argv)
		return f.out, "", 0, nil
	}
	t.Cleanup(func() { wtBgLocate, wtBgRun = oldL, oldR })
	return f
}

func (f *bgFake) n() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// bgApply runs cmd and applies its applyMsg.
func bgApply(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	am, ok := cmd().(applyMsg)
	if !ok {
		t.Fatal("not an applyMsg")
	}
	am(m)
}

// CMP-1: b asks for the task, the card shows the sandbox (Cancel first,
// only the worktree mounted, keys by name) and Cancel / Esc start
// nothing; Start runs one detached `run -d` with the task.
func TestWorktreeBgTaskCardThenStart(t *testing.T) {
	f := bgFakeEngine(t)
	m, _, x, _ := wtRealModel(t)
	m.wt.cursorPath = x
	if c := wtKey(t, m, "b"); c != nil || !m.promptOpen || m.promptKind != promptKindWTBgTask {
		t.Fatalf("b did not ask for the task: open %v kind %d", m.promptOpen, m.promptKind)
	}
	m.promptOpen = false
	if _, ok := m.handleAIPromptSubmit("fix the flaky toast test"); !ok {
		t.Fatal("the prompt is not routed")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindWTBgTask || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no Cancel-first card: open %v kind %d safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	box := wtCardAll(m)
	for _, want := range []string{"Start in background", "sandbox", "ANTHROPIC_API_KEY", "laptop can be off", "run -d"} {
		if !strings.Contains(box, want) {
			t.Errorf("card lacks %q:\n%s", want, box)
		}
	}
	m.confirmOpen = false
	if c := m.onAIConfirmClosed(); c != nil {
		t.Fatal("Esc started the task")
	}
	if f.n() != 0 {
		t.Fatal("the engine was called before Start")
	}

	wtKey(t, m, "b")
	m.promptOpen = false
	m.handleAIPromptSubmit("fix the flaky toast test")
	m.confirmOpen = false
	cmd, ok := m.handleAIConfirm("start")
	if !ok {
		t.Fatal("the card is not routed")
	}
	bgApply(t, m, cmd)
	if f.n() != 1 {
		t.Fatalf("engine calls = %d, want 1", f.n())
	}
	line := strings.Join(f.calls[0], " ")
	for _, want := range []string{"run -d --name termocode-task-", "-v " + x + ":" + x, "-w " + x, "--cap-drop ALL"} {
		if !strings.Contains(line, want) {
			t.Errorf("run lacks %q:\n%s", want, line)
		}
	}
	if !strings.Contains(line, "fix the flaky toast test") {
		t.Errorf("the task is not passed:\n%s", line)
	}
}

// CMP-1: a remote engine context is refused without a prompt.
func TestWorktreeBgTaskRefusesRemoteEngine(t *testing.T) {
	bgFakeEngine(t)
	m, _, x, _ := wtRealModel(t)
	t.Setenv("DOCKER_HOST", "ssh://me@build-box")
	m.wt.cursorPath = x
	wtKey(t, m, "b")
	if m.promptOpen {
		t.Fatal("a remote engine was offered")
	}
}

// CMP-1: B lists the task containers; Enter on a running one offers
// Follow output and Stop (Close first); Stop calls `stop`; an ended one
// offers Remove container instead.
func TestWorktreeBgTasksListAndActions(t *testing.T) {
	f := bgFakeEngine(t)
	f.out = "termocode-task-a1\trunning\tUp 2 minutes\ta1\t/r/w\tclaude\tfix it\n" +
		"termocode-task-b2\texited\tExited (3) 1 hour ago\tb2\t/r/w\tcodex\told one\n"
	m := wtTestModel(t, t.TempDir())
	cmd, ok := m.dispatchWorktreePalette("worktree-bg-tasks")
	if !ok || !m.pickerOpen || m.pickerKind != pickerKindWTBgTasks {
		t.Fatal("worktree-bg-tasks did not open the list")
	}
	bgApply(t, m, cmd)
	if len(m.wt.bgTasks) != 2 {
		t.Fatalf("tasks = %d", len(m.wt.bgTasks))
	}
	m.pickerOpen = false
	if _, ok := m.handleAIPickerSelect(wtBgTaskPfx + "a1"); !ok || !m.confirmOpen || m.confirmKind != confirmKindWTBgTaskAct {
		t.Fatal("Enter on a task did not open its card")
	}
	if m.confirm.SafeID() != "cancel" || !strings.Contains(wtBox(m), "Stop the task") {
		t.Fatalf("running card:\n%s", wtBox(m))
	}
	m.confirmOpen = false
	stop, _ := m.handleAIConfirm("stop")
	bgApply(t, m, stop)
	if got := strings.Join(f.calls[len(f.calls)-1], " "); got != "stop termocode-task-a1" {
		t.Fatalf("stop call %q", got)
	}
	m.handleAIPickerSelect(wtBgTaskPfx + "b2")
	if !strings.Contains(wtBox(m), "Remove container") || strings.Contains(wtBox(m), "Stop the task") {
		t.Fatalf("ended card:\n%s", wtBox(m))
	}
	m.confirmOpen = false
	if c := m.onAIConfirmClosed(); c != nil {
		t.Fatal("Esc did something")
	}
}

// CMP-1: the start palette command opens the prompt for the cursor's
// worktree; the AI may never run either command.
func TestWorktreeBgTaskPaletteAndPolicy(t *testing.T) {
	bgFakeEngine(t)
	m, _, x, _ := wtRealModel(t)
	m.wt.cursorPath = x
	if _, ok := m.dispatchWorktreePalette("worktree-bg-task"); !ok || !m.promptOpen {
		t.Fatal("worktree-bg-task did not ask for the task")
	}
	for _, id := range []string{"worktree-bg-task", "worktree-bg-tasks"} {
		if p := aiActionPolicies[id]; p.Gate != aiGateNever {
			t.Errorf("%s: gate %q", id, p.Gate)
		}
	}
}

// wtCardAll is the whole text of a scrolling card: every scroll step's
// box, borders and line breaks gone (the card itself is not changed).
func wtCardAll(m *Model) string {
	c := m.confirm
	var parts []string
	for i := 0; i < 60; i++ {
		parts = append(parts, strings.Join(strings.Fields(strings.ReplaceAll(ansi.Strip(c.Box()), "│", " ")), " "))
		c, _ = c.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	return strings.Join(parts, " ")
}
