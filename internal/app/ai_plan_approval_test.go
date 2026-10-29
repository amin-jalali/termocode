package app

import (
	"os"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
)

// G-PLAN: the plan card in the chat.

// driveUntilPlanCard routes chat messages until the plan card is open.
func driveUntilPlanCard(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for i := 0; i < 200; i++ {
		msg := nextAIMsg(t, cmd)
		if _, ok := m.handleAIMsg(msg); !ok {
			t.Fatalf("unrouted %T", msg)
		}
		if _, done := msg.(aiChatDoneMsg); done {
			t.Fatal("the run ended without a plan card")
		}
		if m.confirmOpen && m.confirmKind == confirmKindAIPlan {
			return
		}
		cmd = waitAIChat(m.ai.chatCh)
	}
	t.Fatal("no plan card")
}

// aiAskModel is the review profile (a read-only reviewer, then a fixer
// that writes) with plan approval on.
func aiAskModel(t *testing.T) (*Model, *recordingAIProvider, *recordingAIProvider) {
	t.Helper()
	rev := &recordingAIProvider{replies: []ai.Chunk{{Delta: "quit.go:42 shows the card twice"}}}
	fix := &recordingAIProvider{replies: []ai.Chunk{{Delta: "fixed it"}}}
	m, _ := aiRunModel(t, rev, fix)
	m.ai.settings.PlanApproval = ai.PlanApprovalAsk
	return m, rev, fix
}

// Spec: a plan with a writing step shows its card before the first step:
// numbered steps with their models and what they do; Deny comes first
// and has the focus (Enter alone never starts anything); the chat says
// it waits. Approve runs the plan and saves the approval with the chat.
func TestAIPlanCardBeforeWritingSteps(t *testing.T) {
	m, rev, fix := aiAskModel(t)
	driveUntilPlanCard(t, m, m.sendAIChat("review and fix"))
	if len(rev.reqs)+len(fix.reqs) != 0 {
		t.Fatal("a step ran before the plan was approved")
	}
	box := ansi.Strip(m.confirm.Box())
	for _, w := range []string{"Approve this plan?", "1. Reviewer · claude-sonnet-4-5", "only reads", "2. Fixer · gpt-5-mini", "changes files", "runs commands"} {
		if !strings.Contains(box, w) {
			t.Errorf("card lacks %q:\n%s", w, box)
		}
	}
	if b := m.confirm.Buttons(); len(b) != 3 || b[0].ID != "deny" || b[1].ID != "approve" || b[2].ID != "edit" || m.confirm.SafeID() != "deny" {
		t.Errorf("buttons %+v safe %q", b, m.confirm.SafeID())
	}
	_, cmd := m.confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if sel, ok := cmd().(confirm.SelectMsg); !ok || sel.ID != "deny" {
		t.Errorf("Enter on a fresh card picks %+v", sel)
	}
	rm := aiRunMsg(t, m)
	if !rm.Run.PlanWait {
		t.Error("the run message does not say it waits")
	}
	got := aiTranscriptText(m, 100)
	if !strings.Contains(got, "Waiting for you: approve the plan") || !strings.Contains(got, "Review plan") {
		t.Errorf("transcript:\n%s", got)
	}
	m.confirmOpen = false
	m.handleAIConfirm("approve")
	driveAIChat(t, m, waitAIChat(m.ai.chatCh), nil)
	rm = aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || rm.Text != "fixed it" || rm.Run.PlanWait {
		t.Fatalf("run %+v text %q", rm.Run, rm.Text)
	}
	if got := strings.Join(rm.Run.Approved, ","); got != "reviewer,fixer" {
		t.Errorf("approved %q", got)
	}
	saved, err := m.ai.store.Load(m.ai.conv.ID)
	if err != nil || strings.Join(saved.Messages[1].Run.Approved, ",") != "reviewer,fixer" {
		t.Errorf("chat file %+v %v", saved.Messages, err)
	}
}

// Spec: Esc on the card is Deny: the run stops, no step ran and the chat
// says why.
func TestAIPlanEscDenies(t *testing.T) {
	m, rev, fix := aiAskModel(t)
	driveUntilPlanCard(t, m, m.sendAIChat("review and fix"))
	m.confirmOpen = false
	m.onAIConfirmClosed()
	driveAIChat(t, m, waitAIChat(m.ai.chatCh), nil)
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateCancelled || !strings.Contains(rm.Text, "You denied the plan") || rm.Run.PlanWait {
		t.Fatalf("run %+v text %q", rm.Run, rm.Text)
	}
	if len(rev.reqs)+len(fix.reqs) != 0 {
		t.Error("a step ran after Deny")
	}
}

// Spec: the Deny chip in the chat stops the run too.
func TestAIPlanDenyChip(t *testing.T) {
	m, _, fix := aiAskModel(t)
	driveUntilPlanCard(t, m, m.sendAIChat("review and fix"))
	m.confirmOpen = false
	_ = m.onAIButton("plan-deny:" + strconv.Itoa(len(m.ai.conv.Messages)-1))
	driveAIChat(t, m, waitAIChat(m.ai.chatCh), nil)
	if rm := aiRunMsg(t, m); rm.Run.State != ai.RunStateCancelled || len(fix.reqs) != 0 {
		t.Fatalf("run %+v", rm.Run)
	}
}

// Spec: Edit opens the plan as JSON in a file; saving it re-reads the
// plan and shows the card again with the new steps (a dropped step is
// skipped, a new one gets a card); Approve runs the edited plan.
func TestAIPlanEditRoundTrip(t *testing.T) {
	p := &funcAIProvider{reply: planReply(appDiamondPlan)}
	m := aiPlanModel(t, p)
	m.ai.settings.PlanApproval = ai.PlanApprovalAsk
	driveUntilPlanCard(t, m, m.sendAIChat("make the parser faster"))
	box := ansi.Strip(m.confirm.Box())
	if !strings.Contains(box, "1. Map · gpt-5-mini") || !strings.Contains(box, "TASK-left") {
		t.Errorf("card:\n%s", box)
	}
	m.confirmOpen = false
	m.handleAIConfirm("edit")
	rm := aiRunMsg(t, m)
	path, err := aiPlanEditPath(rm.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), `"prompt": "TASK-right"`) {
		t.Fatalf("plan file %q %v", b, err)
	}
	edited := `{"nodes": [
  {"id": "map", "title": "Map", "prompt": "TASK-map", "deps": ["planner"]},
  {"id": "left", "title": "Left", "prompt": "TASK-left", "deps": ["map"]},
  {"id": "verify", "title": "Verify all", "prompt": "TASK-verify", "category": "deep", "deps": ["left"]}
]}`
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	m.onAIPlanSaved(path)
	driveUntilPlanCard(t, m, waitAIChat(m.ai.chatCh))
	box = ansi.Strip(m.confirm.Box())
	if strings.Contains(box, "Right") || !strings.Contains(box, "3. Verify all") {
		t.Errorf("card after the edit:\n%s", box)
	}
	rm = aiRunMsg(t, m)
	if st := aiFindStep(rm, "right"); st == nil || st.State != string(agentdag.StateSkipped) {
		t.Errorf("right card %+v", st)
	}
	if st := aiFindStep(rm, "verify"); st == nil || st.Title != "Verify all" || strings.Join(st.Deps, ",") != "left" {
		t.Errorf("verify card %+v", st)
	}
	m.confirmOpen = false
	m.handleAIConfirm("approve")
	driveAIChat(t, m, waitAIChat(m.ai.chatCh), nil)
	rm = aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || rm.Text != "answer of verify" {
		t.Fatalf("run %+v text %q", rm.Run, rm.Text)
	}
	if r, ok := p.find(func(r ai.Request) bool { return strings.Contains(lastUserContent(r), "TASK-right") }); ok {
		t.Errorf("the dropped step ran:\n%s", lastUserContent(r))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the plan file stays after the run: %v", err)
	}
}

// Spec: an edit that does not fit keeps the plan; the card says why.
func TestAIPlanBadEditShowsTheError(t *testing.T) {
	m, _, _ := aiAskModel(t)
	driveUntilPlanCard(t, m, m.sendAIChat("review and fix"))
	m.confirmOpen = false
	m.handleAIConfirm("edit")
	path, _ := aiPlanEditPath(aiRunMsg(t, m).Run.ID)
	if err := os.WriteFile(path, []byte(`{"nodes": [{"id": "fixer", "deps": ["ghost"]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m.onAIPlanSaved(path)
	driveUntilPlanCard(t, m, waitAIChat(m.ai.chatCh))
	if box := ansi.Strip(m.confirm.Box()); !strings.Contains(box, "Your edit did not fit") || !strings.Contains(box, `unknown dep`) ||
		!strings.Contains(box, "1. Reviewer") {
		t.Errorf("card:\n%s", box)
	}
	m.handleAIConfirm("deny")
	driveAIChat(t, m, waitAIChat(m.ai.chatCh), nil)
}

// Spec: AI: Toggle Plan Approval turns the question off for this chat
// (and on again); the setting stays as it is.
func TestAIPlanToggle(t *testing.T) {
	m, _, _ := aiAskModel(t)
	m.toggleAIPlanApproval()
	if m.ai.conv.PlanApproval != ai.PlanApprovalAuto || m.ai.aiPlanApproval() != ai.PlanApprovalAuto {
		t.Fatalf("after toggle %q", m.ai.conv.PlanApproval)
	}
	driveAIChat(t, m, m.sendAIChat("review and fix"), nil)
	if rm := aiRunMsg(t, m); rm.Run.State != ai.RunStateDone || m.confirmOpen {
		t.Fatalf("run %+v", rm.Run)
	}
	m.toggleAIPlanApproval()
	if m.ai.aiPlanApproval() != ai.PlanApprovalAsk {
		t.Errorf("toggled back %q", m.ai.aiPlanApproval())
	}
	if m.ai.settings.PlanApproval != ai.PlanApprovalAsk {
		t.Errorf("the setting changed: %q", m.ai.settings.PlanApproval)
	}
}

// Spec: a read-only plan runs without the card under ask; always shows
// it.
func TestAIPlanReadOnlyPlan(t *testing.T) {
	const ro = `{"profiles": {"lookers": {"steps": [{"id": "a", "title": "A", "tools": {"preset": "read-only"}}, {"id": "b", "title": "B", "tools": {"preset": "none"}}]}}}`
	for _, mode := range []string{ai.PlanApprovalAsk, ai.PlanApprovalAlways} {
		p := &funcAIProvider{reply: func(ai.Request) ai.Chunk { return ai.Chunk{Delta: "looked"} }}
		m, _ := aiRunModel(t, p, p)
		writeProfiles(t, ro)
		m.ai.loadProfiles(ai.Credentials{})
		m.ai.profile = "lookers"
		m.ai.settings.PlanApproval = mode
		if mode == ai.PlanApprovalAlways {
			driveUntilPlanCard(t, m, m.sendAIChat("look"))
			if box := ansi.Strip(m.confirm.Box()); !strings.Contains(box, "only reads") {
				t.Errorf("card:\n%s", box)
			}
			m.confirmOpen = false
			m.handleAIConfirm("approve")
			driveAIChat(t, m, waitAIChat(m.ai.chatCh), nil)
		} else {
			driveAIChat(t, m, m.sendAIChat("look"), nil)
			if m.confirmOpen {
				t.Error("a read-only plan asked under ask")
			}
		}
		if rm := aiRunMsg(t, m); rm.Run.State != ai.RunStateDone {
			t.Errorf("%s: run %+v", mode, rm.Run)
		}
	}
}

// Spec (OM-6): a resumed run whose plan was approved does not ask again;
// a run that stopped before its approval asks.
func TestAIPlanResume(t *testing.T) {
	for _, approved := range []bool{true, false} {
		block := &blockingAIProvider{started: make(chan struct{}, 1), stopped: make(chan struct{})}
		fix := &recordingAIProvider{replies: []ai.Chunk{{Delta: "fixed it"}}}
		m, _ := aiRunModel(t, block, fix)
		m.ai.settings.PlanApproval = ai.PlanApprovalAsk
		driveUntilPlanCard(t, m, m.sendAIChat("review and fix"))
		m.confirmOpen = false
		cmd := waitAIChat(m.ai.chatCh)
		if approved {
			m.handleAIConfirm("approve")
			for aiRunMsg(t, m).Run.Approved == nil { // RunApproved reaches the chat
				m.handleAIMsg(nextAIMsg(t, cmd))
				cmd = waitAIChat(m.ai.chatCh)
			}
			<-block.started // the reviewer runs
		}
		// termocode stops while the run waits or runs.
		m.ai.chatCancel()
		for {
			msg := nextAIMsg(t, cmd)
			m.handleAIMsg(msg)
			if _, done := msg.(aiChatDoneMsg); done {
				break
			}
			cmd = waitAIChat(m.ai.chatCh)
		}
		rm := aiRunMsg(t, m)
		rm.Run.State, rm.Run.PlanWait = ai.RunStateRunning, true // as saved while it ran
		aiMarkInterrupted(&m.ai.conv)
		if rm.Run.PlanWait || rm.Run.State != ai.RunStateInterrupted {
			t.Fatalf("interrupted run %+v", rm.Run)
		}
		rev := &recordingAIProvider{replies: []ai.Chunk{{Delta: "found it"}}}
		m.ai.slotFake[ai.SlotMain] = aiSlot{p: rev, cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5"}}
		cmd = m.aiRetryRun(len(m.ai.conv.Messages) - 1)
		if cmd == nil {
			t.Fatal("no resume")
		}
		if approved {
			driveAIChat(t, m, cmd, nil)
			if rm := aiRunMsg(t, m); rm.Run.State != ai.RunStateDone || len(fix.reqs) != 1 || m.confirmOpen {
				t.Errorf("approved resume: run %+v, fixer %d, card open %v", rm.Run, len(fix.reqs), m.confirmOpen)
			}
			continue
		}
		driveUntilPlanCard(t, m, cmd)
		if len(rev.reqs) != 0 {
			t.Error("a step ran before the approval")
		}
		m.handleAIConfirm("deny")
		driveAIChat(t, m, waitAIChat(m.ai.chatCh), nil)
	}
}

// Spec: the AI can never approve its own plan: the palette entries and
// the setting are never for the AI.
func TestAIPlanPolicyNever(t *testing.T) {
	for _, id := range []string{"ai-plan-approval", "ai-plan-review"} {
		p, ok := aiActionPolicyFor(id)
		if !ok || p.Gate != aiGateNever {
			t.Errorf("%s: %+v", id, p)
		}
	}
	if p, ok := aiSettingPolicies["ai_plan_approval"]; !ok || p.Gate != aiGateNever {
		t.Errorf("ai_plan_approval: %+v", p)
	}
}

// Spec: an unknown ai_plan_approval value is ask.
func TestAIPlanApprovalSettingDefault(t *testing.T) {
	for v, want := range map[string]string{"": "ask", "auto": "auto", "always": "always", "sometimes": "ask"} {
		if got := (ai.Settings{PlanApproval: v}).PlanApprovalMode(); got != want {
			t.Errorf("%q: %q", v, got)
		}
	}
}

// Spec: the AGENTS view says a run waits for the user; a opens the plan
// card there.
func TestAIPlanAgentsWaitingForYou(t *testing.T) {
	agentsUnicode(t)
	m, _, _ := aiAskModel(t)
	driveUntilPlanCard(t, m, m.sendAIChat("review and fix"))
	m.confirmOpen = false
	m.ai.agents.on = true
	m.focusAIForTest()
	wantAll(t, agentsText(m, 90, 30), "waiting for you · approve the plan · a opens it")
	m.agentsKey(t, "a")
	if !m.confirmOpen || m.confirmKind != confirmKindAIPlan {
		t.Fatal("a did not open the plan card")
	}
	m.confirmOpen = false
	m.handleAIConfirm("approve")
	driveAIChat(t, m, waitAIChat(m.ai.chatCh), nil)
	wantAll(t, agentsText(m, 90, 30), "!waiting for you")
	m.agentsKey(t, "a")
	if m.confirmOpen {
		t.Error("a opened a card for a run that does not wait")
	}
}
