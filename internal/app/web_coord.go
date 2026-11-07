package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Coordinator runs from the phone (MOB-64, mobile plan §3.9). The phone
// asks for a planned run on a task (ai.run_start); that is a laptop card
// (Cancel first) naming the profile, its steps and the task, and only the
// laptop's yes sends the task with that profile in the open chat, as if
// the user had typed it there with the profile's chip: trust checks, plan
// approval (the phone's "plan" card, Reject first), agent worktrees, the
// budget card and the review cards all apply. runs.send hands a running
// step a message, as m on the laptop's Agents view; the audit line keeps
// the run and the step, never the text.

func init() {
	webAskOps[webproto.OpAIRunStart] = (*Model).webAskRunStart
	webOps[webproto.OpRunsSend] = (*Model).webRunsSend
}

// webCoordDefault is the profile of a coordinator run that names none.
const webCoordDefault = ai.ProfilePlan

// webCoordTaskLines caps the task lines the laptop card shows.
const webCoordTaskLines = 12

// webAskRunStart checks a coordinator run request and builds its laptop
// card; nil: it answered the call (a refusal).
func (m *Model) webAskRunStart(call *webCall) *webAsk {
	a := call.args.(*webproto.AIRunStartArgs)
	name := a.Profile
	if name == "" {
		name = webCoordDefault
	}
	task := strings.TrimSpace(a.Task)
	s := m.aiS()
	p, ok := s.profiles.Get(name)
	switch {
	case !m.aiConfigured():
		call.fail(webproto.CodeConflict, "AI is not set up on the laptop.")
		return nil
	case !ok:
		call.fail(webproto.CodeNotFound, "There is no such profile.")
		return nil
	case !p.Planned():
		call.fail(webproto.CodeBadRequest, "That profile does not plan. Pick a profile with a planner step.")
		return nil
	case m.aiProfileTrust(p) != aiTrustUse:
		call.fail(webproto.CodeForbidden, "Trust this project profile on the laptop first.")
		return nil
	case s.streaming:
		call.fail(webproto.CodeBusy, "The AI is still answering. Stop it or wait.")
		return nil
	}
	summary := name
	if a.Ref != "" {
		summary += " · " + a.Ref
	}
	return &webAsk{
		summary: summary,
		title:   "Start a coordinator run?",
		body:    webCoordCardBody(p, task, a.Ref),
		action:  "Start run",
		run:     func(m *Model) tea.Cmd { return m.webStartCoordRun(name, task) },
	}
}

// webCoordCardBody is the laptop card's text: the profile, its steps,
// the task and what comes next.
func webCoordCardBody(p ai.Profile, task, ref string) string {
	var b strings.Builder
	title := p.Title
	if title == "" {
		title = p.Name
	}
	fmt.Fprintf(&b, "Profile %s (%s), %d step%s:\n", aiCardText(p.Name, 40), aiCardText(title, 60), len(p.Steps), plural(len(p.Steps)))
	for i, st := range p.Steps {
		t := st.Title
		if t == "" {
			t = st.ID
		}
		line := fmt.Sprintf("  %d · %s", i+1, aiCardText(t, 40))
		if st.Plan {
			line += " · planner"
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\nTask")
	if ref != "" {
		b.WriteString(" (" + aiCardText(ref, 60) + ")")
	}
	b.WriteString(":\n")
	lines := strings.Split(aiCleanText(task), "\n")
	if len(lines) > webCoordTaskLines {
		lines = append(lines[:webCoordTaskLines], "…")
	}
	for _, l := range lines {
		b.WriteString("  " + aiOneLineClip(l, 100) + "\n")
	}
	b.WriteString("\nThe run starts in the open chat. The planner's plan asks you first (Reject first); every write still asks.")
	return b.String()
}

// webStartCoordRun runs after the laptop's yes: the task goes to the open
// chat with profile name, once (the user's own chip stays for their next
// message). Things may have changed while the card waited, so it checks
// again.
func (m *Model) webStartCoordRun(name, task string) tea.Cmd {
	s := m.aiS()
	if s.streaming {
		return m.aiInfo("Coordinator run not started: the AI is answering. Ask again when it is free")
	}
	p, ok := s.profiles.Get(name)
	if !ok || !p.Planned() || m.aiProfileTrust(p) != aiTrustUse {
		return m.aiInfo("Coordinator run not started: profile " + aiCardText(name, 40) + " is gone or not trusted")
	}
	old := s.kwProfile
	s.kwProfile = name
	cmd := m.sendAIChat(task)
	s.kwProfile = old
	return cmd
}

// webRunsLive are the steps of run it that run now and can get a message:
// a live run of this window with a handle, steps not on a coding CLI.
func (m *Model) webRunsLive(it runsItem) []webproto.RunStepRef {
	if it.kind != runsRunning && it.kind != runsStalled {
		return nil
	}
	if m.aiS().aiRunHandle(it.run.ID) == nil {
		return nil
	}
	var out []webproto.RunStepRef
	for _, st := range it.steps {
		if st.State != string(agentdag.StateRunning) || st.CLI != "" || !webCleanOK(st.ID, true) {
			continue
		}
		if len(out) == webproto.MaxRunLive {
			break
		}
		out = append(out, webproto.RunStepRef{ID: st.ID, Title: webName(st.Title)})
	}
	return out
}

// webRunsSend hands a running step of a run the phone's message.
func (m *Model) webRunsSend(call *webCall) tea.Cmd {
	a := call.args.(*webproto.RunsSendArgs)
	var it runsItem
	found := false
	for _, x := range m.runsCollect(clock.Now()) {
		if x.key == a.Key {
			it, found = x, true
			break
		}
	}
	if !found {
		call.fail(webproto.CodeNotFound, "That run is gone.")
		return nil
	}
	live := m.webRunsLive(it)
	step := a.Step
	switch {
	case len(live) == 0:
		call.fail(webproto.CodeConflict, "No step of this run is working now.")
		return nil
	case step == "" && len(live) > 1:
		call.fail(webproto.CodeConflict, "Several steps work now. Pick one.")
		return nil
	case step == "":
		step = live[0].ID
	case !webRunsHasStep(live, step):
		call.fail(webproto.CodeConflict, "That step is not working now.")
		return nil
	}
	h := m.aiS().aiRunHandle(it.run.ID)
	if h == nil {
		call.fail(webproto.CodeConflict, "This run is not going now.")
		return nil
	}
	if err := h.Send(step, a.Text); err != nil {
		m.webAuditOp(call, it.key+" · "+step, "refused")
		call.fail(webproto.CodeConflict, "Not sent: "+err.Error()+".")
		return nil
	}
	m.webAuditOp(call, it.key+" · "+step, "ok")
	call.reply(nil)
	return nil
}

func webRunsHasStep(live []webproto.RunStepRef, id string) bool {
	for _, r := range live {
		if r.ID == id {
			return true
		}
	}
	return false
}
