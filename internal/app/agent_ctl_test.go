package app

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentctl"
	"github.com/amin-jalali/termocode/internal/agents"
)

func agentCtlAsk(m *Model, req agentctl.Request) (agentCtlCall, func() (agentctl.Reply, bool)) {
	call := agentCtlCall{req: req, reply: make(chan agentctl.Reply, 1)}
	m.onAgentCtl(call)
	return call, func() (agentctl.Reply, bool) {
		select {
		case r := <-call.reply:
			return r, true
		default:
			return agentctl.Reply{}, false
		}
	}
}

func agentCtlModel(t *testing.T) *Model {
	t.Helper()
	m, _, _, _ := aiSkillsModel(t)
	m.terminalTabs = []terminalTab{{BufID: 5, Name: "zsh"}, {BufID: 7, Name: "claude", Agent: "claude", AgentState: agents.Working}}
	m.termOpen, m.terminalActiveTab = true, 1
	old := agentCtlPoll
	agentCtlPoll = time.Millisecond
	t.Cleanup(func() { agentCtlPoll = old })
	return m
}

// Spec (OMX-33): list gives each tab its id, agent and state; wait
// answers when the agent reaches the state (here by its own report,
// which marks the tab as hook-driven); bad tabs and states are errors.
func TestAgentCtlListWaitReport(t *testing.T) {
	m := agentCtlModel(t)
	_, got := agentCtlAsk(m, agentctl.Request{Op: agentctl.OpList})
	rep, ok := got()
	if !ok || !rep.OK || len(rep.Tabs) != 2 || rep.Tabs[1].ID != "b7" || rep.Tabs[1].State != "working" || !rep.Tabs[1].Active || rep.Tabs[0].Agent != "" {
		t.Fatalf("list %+v", rep)
	}
	_, got = agentCtlAsk(m, agentctl.Request{Op: agentctl.OpWait, Tab: "b7", Until: "done"})
	if _, ok := got(); ok {
		t.Fatal("wait answered before the agent was done")
	}
	_, rgot := agentCtlAsk(m, agentctl.Request{Op: agentctl.OpReport, Tab: "b7", State: "done"})
	if r, _ := rgot(); !r.OK {
		t.Fatalf("report %+v", r)
	}
	if !m.terminalTabs[1].AgentHook || m.terminalTabs[1].AgentState != agents.Done {
		t.Fatal("report did not set the state")
	}
	m.agentCtlCheckWaits()
	if r, ok := got(); !ok || r.State != "done" {
		t.Fatalf("wait %+v %v", r, ok)
	}
	for _, bad := range []agentctl.Request{
		{Op: agentctl.OpWait, Tab: "nope"},
		{Op: agentctl.OpWait, Tab: "b7", Until: "idle"},
		{Op: agentctl.OpReport, Tab: "b5", State: "done"}, // no agent, no --agent
		{Op: agentctl.OpReport, Tab: "b7", State: "sleepy"},
		{Op: "dance"},
	} {
		_, got := agentCtlAsk(m, bad)
		if r, ok := got(); !ok || r.OK || r.Error == "" {
			t.Errorf("%+v: %+v", bad, r)
		}
	}
	_, got = agentCtlAsk(m, agentctl.Request{Op: agentctl.OpReport, Tab: "b5", Agent: "codex", State: "waiting"})
	if r, _ := got(); !r.OK || m.terminalTabs[0].Agent != "codex" {
		t.Errorf("report with --agent: %+v", r)
	}
	// A wait times out.
	_, got = agentCtlAsk(m, agentctl.Request{Op: agentctl.OpWait, Tab: "b5", Until: "done", TimeoutS: 1})
	m.ctlS().waits[0].deadline = time.Now().Add(-time.Second)
	m.agentCtlCheckWaits()
	if r, ok := got(); !ok || r.OK {
		t.Errorf("timeout %+v", r)
	}
}

// Spec (OMX-33): prompt asks first with a Deny-first card; Deny (and
// Esc) refuse; "Allow for this session" lets the next prompt in at once.
func TestAgentCtlPromptCard(t *testing.T) {
	m := agentCtlModel(t)
	_, got := agentCtlAsk(m, agentctl.Request{Op: agentctl.OpPrompt, Tab: "b7", Text: "fix the tests"})
	if !m.confirmOpen || m.confirmKind != confirmKindAgentCtlPrompt || m.confirm.SafeID() != "deny" {
		t.Fatal("no Deny-first card")
	}
	if _, ok := got(); ok {
		t.Fatal("answered before the card")
	}
	m.confirmOpen = false
	m.onAIConfirmClosed() // Esc
	if r, ok := got(); !ok || r.OK {
		t.Fatalf("Esc: %+v", r)
	}
	_, got = agentCtlAsk(m, agentctl.Request{Op: agentctl.OpPrompt, Tab: "b7", Text: "again"})
	m.answerAIRulesForTest("session")
	if r, ok := got(); !ok || r.Error != "the editor is not running" { // no nvim here: the send itself fails
		t.Fatalf("allowed: %+v", r)
	}
	m.confirmOpen = false
	_, got = agentCtlAsk(m, agentctl.Request{Op: agentctl.OpPrompt, Tab: "b7", Text: "third"})
	if m.confirmOpen {
		t.Fatal("asked again after Allow for this session")
	}
	if _, ok := got(); !ok {
		t.Fatal("no answer")
	}
	_, got = agentCtlAsk(m, agentctl.Request{Op: agentctl.OpPrompt, Tab: "b7", Text: "  "})
	if r, _ := got(); r.OK {
		t.Error("empty prompt sent")
	}
}
