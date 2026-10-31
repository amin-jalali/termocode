package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// ── Per-step control in AGENTS (OMX-22) ───────────────────────────────
//
// On the selected step of a run that goes: m sends it a message (its
// agent reads it after its current tool call, in the same turn), x stops
// only that step (a card asks first, Keep running first), o opens its
// whole transcript (what it wrote, its tool calls, the notes) in a
// read-only editor page.

const (
	promptKindAgentsSend  promptKindEnum  = 2700
	confirmKindAgentsStop confirmKindEnum = 2700
)

// agentsLogMax caps one step's transcript (bytes; the start is cut).
const agentsLogMax = 256 << 10

// agentsLogKeep caps how many step transcripts stay in memory.
const agentsLogKeep = 64

// agentsLogKey is a step's key in aiAgentsState.logs.
func agentsLogKey(runID, node string) string { return runID + "\x00" + node }

// agentsLogAdd appends text to a step's transcript.
func (s *aiState) agentsLogAdd(runID, node, text string) {
	if text == "" {
		return
	}
	if s.agents.logs == nil {
		s.agents.logs = map[string]*strings.Builder{}
	}
	k := agentsLogKey(runID, node)
	b := s.agents.logs[k]
	if b == nil {
		if len(s.agents.logs) >= agentsLogKeep {
			for old := range s.agents.logs { // any old one goes
				delete(s.agents.logs, old)
				break
			}
		}
		b = &strings.Builder{}
		s.agents.logs[k] = b
	}
	b.WriteString(text)
	if b.Len() > agentsLogMax {
		rest := b.String()[b.Len()-agentsLogMax/2:]
		b.Reset()
		b.WriteString("…(the start is cut)\n" + rest)
	}
}

// agentsLogEvent records one run event in the step's transcript.
func (s *aiState) agentsLogEvent(ev agentrun.Event) {
	switch ev.Kind {
	case agentrun.NodeStart:
		s.agentsLogAdd(ev.RunID, ev.NodeID, "\n── started ──\n")
	case agentrun.NodeText:
		s.agentsLogAdd(ev.RunID, ev.NodeID, ai.Redact(ev.AI.Text))
	case agentrun.NodeTool:
		if ev.AI.Kind == ai.EventToolStart {
			s.agentsLogAdd(ev.RunID, ev.NodeID, "\n⚙ "+ai.Redact(ev.AI.Detail)+"\n")
		} else {
			s.agentsLogAdd(ev.RunID, ev.NodeID, "  → "+aiShortResult(ev.AI.Detail)+"\n")
		}
	case agentrun.NodeMessage:
		if ev.Err == "" {
			s.agentsLogAdd(ev.RunID, ev.NodeID, "\n✉ you: "+ai.Redact(ev.Output)+"\n")
		}
	case agentrun.NodeStalled:
		s.agentsLogAdd(ev.RunID, ev.NodeID, "\n⏱ "+ev.Err+"\n")
	case agentrun.NodeFailed:
		s.agentsLogAdd(ev.RunID, ev.NodeID, "\n✘ "+ai.Redact(ev.Err)+"\n")
	case agentrun.NodeCancelled:
		if ev.Err != "" {
			s.agentsLogAdd(ev.RunID, ev.NodeID, "\n■ "+ev.Err+"\n")
		}
	}
}

// aiRunHandle is the handle of run id while it goes (nil: it does not).
func (s *aiState) aiRunHandle(id string) *agentrun.Handle {
	if rm := s.aiRunMessage(); rm != nil && s.run != nil && rm.Run.ID == id {
		return s.run
	}
	return nil
}

// agentsPick is the selected step of the shown run.
func (s *aiState) agentsPick() (agentsRun, agentsNode, bool) {
	d, ok := s.agentsData()
	if !ok {
		return d, agentsNode{}, false
	}
	i := s.agentsSelected(d)
	if i < 0 || i >= len(d.nodes) {
		return d, agentsNode{}, false
	}
	return d, d.nodes[i], true
}

// agentsLiveStep is the selected step when it runs now, with its run's
// handle; why says what is not so.
func (s *aiState) agentsLiveStep() (*agentrun.Handle, agentsRun, agentsNode, string) {
	d, n, ok := s.agentsPick()
	if !ok {
		return nil, d, n, "No step is selected"
	}
	h := s.aiRunHandle(d.msg.Run.ID)
	switch {
	case h == nil:
		return nil, d, n, "This run does not go now"
	case n.state != agentdag.StateRunning:
		return nil, d, n, "The step " + n.title + " does not run now"
	case n.st != nil && n.st.CLI != "":
		return nil, d, n, "A step that runs a coding CLI cannot get messages; stop it with x"
	}
	return h, d, n, ""
}

// agentsSendKey is m: ask for the message to the selected step.
func (m *Model) agentsSendKey() tea.Cmd {
	s := m.aiS()
	_, d, n, why := s.agentsLiveStep()
	if why != "" {
		return m.aiInfo(why)
	}
	s.agents.target = agentsLogKey(d.msg.Run.ID, n.id)
	m.prompt = prompt.New("Message to "+aiCardText(n.title, 30), "It reads it after its current tool call:", "")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindAgentsSend
	return nil
}

// onAgentsSend sends the typed message to the step it was typed for.
func (m *Model) onAgentsSend(text string) tea.Cmd {
	s := m.aiS()
	runID, node, _ := strings.Cut(s.agents.target, "\x00")
	s.agents.target = ""
	if strings.TrimSpace(text) == "" {
		return nil
	}
	h := s.aiRunHandle(runID)
	if h == nil {
		return m.aiInfo("The run ended before the message was sent")
	}
	if err := h.Send(node, text); err != nil {
		return m.aiInfo("Not sent: " + err.Error())
	}
	return m.aiInfo("Sent: the step reads it after its current tool call")
}

// agentsStopKey is x: ask before stopping the selected step.
func (m *Model) agentsStopKey() tea.Cmd {
	s := m.aiS()
	_, d, n, why := s.agentsLiveStep()
	if why != "" && !strings.HasPrefix(why, "A step that runs a coding CLI") {
		return m.aiInfo(why)
	}
	s.agents.target = agentsLogKey(d.msg.Run.ID, n.id)
	body := "Step \"" + aiCardText(n.title, 40) + "\" ends now without its answer.\n" +
		"Steps that need its answer cannot run; the other steps go on. Retry from the step runs it again later."
	m.confirm = confirm.Risky("Stop this step?", body,
		confirm.Button{ID: "keep", Title: "Keep running"},
		confirm.Button{ID: "stop", Title: "Stop step", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentsStop
	return nil
}

// answerAgentsStop is the stop card's answer.
func (m *Model) answerAgentsStop(id string) tea.Cmd {
	s := m.aiS()
	runID, node, _ := strings.Cut(s.agents.target, "\x00")
	s.agents.target = ""
	if id != "stop" {
		return nil
	}
	h := s.aiRunHandle(runID)
	if h == nil {
		return m.aiInfo("The run ended already")
	}
	if err := h.StopNode(node); err != nil {
		return m.aiInfo("Not stopped: " + err.Error())
	}
	return nil
}

// agentsTranscript is the selected step's whole transcript as text.
func (s *aiState) agentsTranscript(d agentsRun, n agentsNode) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Step %s\n\nRun %s · %s", n.title, d.msg.Run.ID, n.state)
	if st := n.st; st != nil && st.Model != "" {
		b.WriteString(" · " + st.Model)
	}
	b.WriteString("\n")
	if log := s.agents.logs[agentsLogKey(d.msg.Run.ID, n.id)]; log != nil {
		b.WriteString("\n## What it did\n" + log.String() + "\n")
	} else if st := n.st; st != nil && len(st.Tools) > 0 {
		b.WriteString("\n## Tool calls\n")
		for _, t := range st.Tools {
			b.WriteString(t + "\n")
		}
	}
	if st := n.st; st != nil {
		if len(st.Notes) > 0 {
			b.WriteString("\n## Notes\n")
			for _, t := range st.Notes {
				b.WriteString(t + "\n")
			}
		}
		if st.Error != "" {
			b.WriteString("\n## Error\n" + ai.Redact(st.Error) + "\n")
		}
		if st.Text != "" && n.state.Done() {
			b.WriteString("\n## Answer\n" + ai.Redact(st.Text) + "\n")
		}
	}
	return b.String()
}

// agentsOpenKey is o: the selected step's transcript in a read-only page.
func (m *Model) agentsOpenKey() tea.Cmd {
	s := m.aiS()
	d, n, ok := s.agentsPick()
	if !ok {
		return m.aiInfo("No step is selected")
	}
	text := s.agentsTranscript(d, n)
	if m.nvim == nil {
		return m.aiInfo("The editor is not ready")
	}
	m.focus = FocusEditor
	m.ensureEditorWindowCurrent()
	if err := m.openReadOnlyPage("step "+n.id+" · "+d.msg.Run.ID, text, "", "markdown"); err != nil {
		return m.aiToastErr("Could not open the transcript", err)
	}
	return nil
}
