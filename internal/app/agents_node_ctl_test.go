package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// nodeCtlRig is a review run on a step provider, with AGENTS shown.
type nodeCtlRig struct {
	t   *testing.T
	m   *Model
	p   stepProvider
	cmd tea.Cmd
}

func newNodeCtlRig(t *testing.T) *nodeCtlRig {
	t.Helper()
	p := stepProvider{steps: make(chan []ai.Chunk, 8)}
	m, _ := aiRunModel(t, p, p)
	m.actionsOpen, m.actionsPinned = true, true
	m.ai.focused = true
	r := &nodeCtlRig{t: t, m: m, p: p, cmd: m.sendAIChat("review the parser")}
	m.ai.agents.on = true
	r.until(func() bool { return r.step("reviewer").State == string(agentdag.StateRunning) })
	return r
}

func (r *nodeCtlRig) step(id string) *ai.StoredStep {
	r.t.Helper()
	st := aiFindStep(r.m.ai.aiRunMessage(), id)
	if st == nil {
		r.t.Fatalf("no step %s", id)
	}
	return st
}

// until routes chat messages until ok or the turn ends.
func (r *nodeCtlRig) until(ok func() bool) {
	r.t.Helper()
	for i := 0; i < 400 && !ok(); i++ {
		if r.cmd == nil {
			r.t.Fatal("the turn ended before the condition")
		}
		msg := nextAIMsg(r.t, r.cmd)
		if msg == nil {
			r.t.Fatal("no message")
		}
		r.m.handleAIMsg(msg)
		if _, done := msg.(aiChatDoneMsg); done {
			r.cmd = nil
			continue
		}
		r.cmd = waitAIChat(r.m.ai.chatCh)
	}
	if !ok() {
		r.t.Fatal("condition never true")
	}
}

func (r *nodeCtlRig) reply(text string) {
	r.p.steps <- []ai.Chunk{{Delta: text}, {Done: true}}
}

// OMX-22: m on the running step asks for a message; the step's agent
// reads it in the same turn, the card notes it and the transcript shows
// it with what the step wrote.
func TestAgentsSendMessageToStep(t *testing.T) {
	r := newNodeCtlRig(t)
	r.m.agentsKey(t, "m")
	if !r.m.promptOpen || r.m.promptKind != promptKindAgentsSend {
		t.Fatal("m did not open the message prompt")
	}
	if _, ok := r.m.handleAIPromptSubmit("check the tests too"); !ok {
		t.Fatal("prompt kind not routed")
	}
	r.reply("first look")
	r.until(func() bool {
		return strings.Contains(strings.Join(r.step("reviewer").Notes, "\n"), "✉ you: check the tests too")
	})
	r.reply("reviewed, tests included")
	r.until(func() bool { return r.step("reviewer").State == string(agentdag.StateCompleted) })
	if got := r.step("reviewer").Text; got != "reviewed, tests included" {
		t.Fatalf("answer %q", got)
	}
	d, n, ok := r.m.ai.agentsPick()
	for i := range d.nodes {
		if d.nodes[i].id == "reviewer" {
			n, ok = d.nodes[i], true
		}
	}
	if !ok {
		t.Fatal("no reviewer node")
	}
	tr := r.m.ai.agentsTranscript(d, n)
	wantAll(t, tr, "# Step Reviewer", "first look", "✉ you: check the tests too", "## Answer", "reviewed, tests included")
	r.reply("fixed")
	r.until(func() bool { return r.cmd == nil })
}

// OMX-22: x asks first (Keep running is the safe, focused answer); Stop
// step stops only that step: the step after it is blocked and the run
// ends as failed, saying why.
func TestAgentsStopOneStep(t *testing.T) {
	r := newNodeCtlRig(t)
	r.m.agentsKey(t, "x")
	if !r.m.confirmOpen || r.m.confirmKind != confirmKindAgentsStop || r.m.confirm.SafeID() != "keep" {
		t.Fatalf("stop card open=%v kind=%v", r.m.confirmOpen, r.m.confirmKind)
	}
	r.m.confirmOpen = false
	if cmd, ok := r.m.handleAIConfirm("keep"); !ok || cmd != nil {
		t.Fatal("keep must do nothing")
	}
	if r.m.ai.agents.target != "" {
		t.Fatal("target kept after keep")
	}
	r.m.agentsKey(t, "x")
	r.m.confirmOpen = false
	r.m.handleAIConfirm("stop")
	r.until(func() bool { return r.cmd == nil })
	rm := aiRunMsg(t, r.m)
	if rm.Run.State != ai.RunStateFailed {
		t.Fatalf("run state %s", rm.Run.State)
	}
	rev, fix := rm.Steps[0], rm.Steps[1]
	if rev.State != string(agentdag.StateCancelled) || !strings.Contains(strings.Join(rev.Notes, " "), "stopped by the user") {
		t.Fatalf("reviewer %+v", rev)
	}
	if fix.State != string(agentdag.StateBlocked) {
		t.Fatalf("fixer %s", fix.State)
	}
}

// OMX-22: m and x on a step that does not run say so.
func TestAgentsNodeKeysNeedARunningStep(t *testing.T) {
	m := agentsModel(t, agentsRunMsgs("r1", ai.RunStateDone, agentsDiamondSteps())...)
	m.agentsKey(t, "m")
	if m.promptOpen {
		t.Fatal("a prompt for a run that does not go")
	}
	m.agentsKey(t, "x")
	if m.confirmOpen {
		t.Fatal("a stop card for a run that does not go")
	}
}
