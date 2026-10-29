package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/theme"
)

// G-PLAN: before an agent run starts a step that may write files or run
// commands, the chat shows its plan on a card: Deny (first and focused,
// so Enter alone never starts anything), Approve and Edit. Esc is Deny.
// ai_plan_approval (ask, auto, always) and the chat's own choice (AI:
// Toggle Plan Approval) decide when it asks. The approved step ids are
// saved with the run, so a resume does not ask again.

const confirmKindAIPlan confirmKindEnum = 2470

// aiPlanWait is the plan the running run waits on.
type aiPlanWait struct {
	nodes []agentdag.Node
	err   string // why the last edit did not fit
	round int
}

// aiPlanApproval is the chat's effective mode (ai.PlanApproval*).
func (s *aiState) aiPlanApproval() string {
	if v := s.conv.PlanApproval; v != "" {
		return ai.CheckPlanApproval(v)
	}
	return s.settings.PlanApprovalMode()
}

// aiRunApproval maps the chat's mode to the scheduler's.
func (s *aiState) aiRunApproval() string {
	switch s.aiPlanApproval() {
	case ai.PlanApprovalAuto:
		return agentrun.ApprovalOff
	case ai.PlanApprovalAlways:
		return agentrun.ApprovalAlways
	}
	return agentrun.ApprovalAsk
}

// toggleAIPlanApproval is AI: Toggle Plan Approval: this chat asks
// before writing steps, or runs plans without asking.
func (m *Model) toggleAIPlanApproval() tea.Cmd {
	s := m.aiS()
	if s.aiPlanApproval() == ai.PlanApprovalAuto {
		s.conv.PlanApproval = ai.PlanApprovalAsk
	} else {
		s.conv.PlanApproval = ai.PlanApprovalAuto
	}
	if len(s.conv.Messages) > 0 {
		if err := s.store.Save(&s.conv); err != nil {
			recordError("[ai] save chat: " + err.Error())
		}
	}
	if s.conv.PlanApproval == ai.PlanApprovalAuto {
		return m.aiInfo("Plan approval off in this chat: plans run without asking")
	}
	return m.aiInfo("Plan approval on in this chat: a plan that changes files or runs commands waits for you")
}

// aiToolsAct reports whether tools can change files (writes) or run
// commands (exec).
func aiToolsAct(tools []ai.Tool) (writes, exec bool) {
	for _, t := range tools {
		switch t.Class {
		case ai.ClassRun, ai.ClassContainers, ai.ClassServices:
			exec = true
		case ai.ClassFileWrite, ai.ClassReview, ai.ClassGit:
			writes = true
		default:
			if t.Mutating || t.Gate != nil {
				writes = true
			}
		}
	}
	return writes, exec
}

// aiNodeActs is what node n may do with tools (the chat's tools before
// the step's rule; a step that keeps every tool also gets the MCP tools,
// which may do anything).
func aiNodeActs(tools []ai.Tool, n agentdag.Node) (writes, exec bool) {
	if n.CLI != "" { // a subscription CLI: it writes and runs commands when it may write
		w := n.Tools.Preset == agentdag.PresetAll
		return w, w
	}
	r := n.Tools
	if (r.Preset == "" || r.Preset == agentdag.PresetAll) && len(r.Allow) == 0 {
		return true, true
	}
	return aiToolsAct(aiFilterTools(tools, r))
}

// onAIPlanEvent applies a G-PLAN run event (ok=false: not one).
func (m *Model) onAIPlanEvent(rm *ai.StoredMessage, ev agentrun.Event) (tea.Cmd, bool) {
	s := m.aiS()
	switch ev.Kind {
	case agentrun.RunAwaitApproval:
		rm.Run.PlanWait = true
		s.planWait = &aiPlanWait{nodes: ev.Nodes, err: ev.Err, round: ev.Round}
		m.aiSaveChat()
		if !m.confirmOpen || m.confirmKind == confirmKindAIPlan {
			m.openAIPlanCard()
		}
	case agentrun.RunApproved:
		rm.Run.PlanWait = false
		s.planWait = nil
		if s.run != nil {
			rm.Run.Approved = s.run.Snapshot().Approved
		}
		m.aiSaveChat()
		if s.run != nil {
			aiSaveRunSnapshot(s.conv.ID, ai.RunStateRunning, s.run.Snapshot())
		}
	case agentrun.RunPlanEdited:
		m.aiSyncPlanSteps(rm)
	case agentrun.RunPlanKept:
		for _, n := range ev.Nodes {
			if st := aiFindStep(rm, n.ID); st != nil && !contains(st.Notes, aiPlanKeptNote) {
				st.Notes = append(st.Notes, aiPlanKeptNote)
			}
		}
	default:
		return nil, false
	}
	return nil, true
}

// aiPlanKeptNote is the one-line note of steps that joined an approved
// plan without a new question.
const aiPlanKeptNote = "✓ part of the plan you approved: no new question"

func (m *Model) aiSaveChat() {
	s := m.aiS()
	if err := s.store.Save(&s.conv); err != nil {
		recordError("[ai] save chat: " + err.Error())
	}
}

// aiSyncPlanSteps brings the step cards in line with an edited plan:
// titles, tasks, deps and skipped steps, and a card per new step (its
// model slot is resolved now; the run waits, no step runs).
func (m *Model) aiSyncPlanSteps(rm *ai.StoredMessage) {
	s := m.aiS()
	if s.run == nil {
		return
	}
	snap := s.run.Snapshot()
	for _, n := range snap.Nodes {
		if _, ok := s.runSlots[n.Slot]; !ok && n.CLI == "" {
			if sl, err := s.resolveSlot(n.Slot); err == nil {
				s.runSlots[n.Slot] = sl
			}
		}
		st := aiFindStep(rm, n.ID)
		if st == nil {
			rm.Steps = append(rm.Steps, ai.StoredStep{ID: n.ID, Title: n.Title, Slot: n.Slot, Model: s.runSlots[n.Slot].cfg.Model,
				Category: n.Category, State: string(agentdag.StatePending), Planned: true, Prompt: n.Prompt})
			st = &rm.Steps[len(rm.Steps)-1]
		}
		st.Deps = agentdag.Deps(snap, n.ID)
		if !n.State.Done() || n.State == agentdag.StateSkipped {
			st.Title, st.Prompt, st.State = n.Title, n.Prompt, string(n.State)
			if n.Slot != st.Slot && n.CLI == "" {
				st.Slot, st.Category, st.Model = n.Slot, n.Category, s.runSlots[n.Slot].cfg.Model
			}
		}
	}
	m.aiSaveChat()
	aiSaveRunSnapshot(s.conv.ID, ai.RunStateRunning, snap)
}

// aiPlanWaiting is the run message that waits for plan approval (nil:
// none).
func (s *aiState) aiPlanWaiting() *ai.StoredMessage {
	rm := s.aiRunMessage()
	if rm == nil || !rm.Run.PlanWait || s.run == nil || s.planWait == nil {
		return nil
	}
	return rm
}

// openAIPlanCard shows the plan the run waits on.
func (m *Model) openAIPlanCard() tea.Cmd {
	s := m.aiS()
	rm := s.aiPlanWaiting()
	if rm == nil {
		return m.aiInfo("No plan waits for you")
	}
	title := "✦ Approve this plan?"
	if rm.Run.Approved != nil {
		title = "✦ The plan grew: approve the new steps?"
	}
	m.confirm = confirm.Risky(title, m.aiPlanCardBody(rm),
		confirm.Button{ID: "deny", Title: "Deny"},
		confirm.Button{ID: "approve", Title: "Approve", Style: confirm.StylePrimary},
		confirm.Button{ID: "edit", Title: "Edit"}).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIPlan
	return nil
}

// aiPlanCardBody lists the steps of the plan: what each does, its model,
// whether it changes files or runs commands, and the run's limits.
func (m *Model) aiPlanCardBody(rm *ai.StoredMessage) string {
	s := m.aiS()
	w := s.planWait
	var b strings.Builder
	if w.err != "" {
		b.WriteString("✘ Your edit did not fit: " + w.err + "\nThe plan below did not change.\n\n")
	}
	b.WriteString("Nothing that changes files or runs commands starts before you answer.\n")
	fix := ""
	var loop *agentdag.Loop
	if s.run != nil {
		loop = s.run.Snapshot().Loop
		if loop != nil {
			fix = loop.Fix.ID
		}
	}
	for i, n := range w.nodes {
		b.WriteString("\n")
		head := strconv.Itoa(i+1) + ". "
		if n.ID == fix {
			head = "If verify fails: "
		}
		model := s.runSlots[n.Slot].cfg.Model
		if n.CLI != "" {
			model = ai.CLITitle(n.CLI)
		}
		if model == "" {
			model = n.Slot
		}
		line := head + n.Title
		if model != "" {
			line += " · " + model
		}
		b.WriteString(line + "\n")
		var does []string
		writes, exec := agentdag.Writes(n), false
		if s.runActs != nil {
			writes, exec = s.runActs(n)
		}
		ascii := theme.CurrentIconMode == theme.IconModeASCII
		if writes {
			does = append(does, aiPlanIcon("✎ ", ascii)+"changes files")
		}
		if exec {
			does = append(does, aiPlanIcon("▶ ", ascii)+"runs commands")
		}
		if len(does) == 0 {
			does = append(does, "only reads")
		}
		b.WriteString("   " + strings.Join(does, " · ") + "\n")
		if task := aiOneLine(n.Prompt); task != "" {
			b.WriteString("   " + aiCutRunes(task, 160) + "\n")
		}
	}
	var limits []string
	if loop != nil && loop.MaxRounds > 0 {
		limits = append(limits, fmt.Sprintf("up to %d rounds of verify and fix", loop.MaxRounds))
	}
	if prof, ok := s.profiles.Get(rm.Run.Profile); ok && prof.BudgetUSD > 0 {
		limits = append(limits, "budget "+aiBudgetText(prof.BudgetUSD)+" (it pauses there)")
	}
	if len(limits) > 0 {
		b.WriteString("\nLimits: " + strings.Join(limits, " · ") + "\n")
	}
	b.WriteString("\nEdit opens the plan as JSON; save it to see the new plan here.")
	return b.String()
}

func aiPlanIcon(glyph string, ascii bool) string {
	if ascii {
		return ""
	}
	return glyph
}

func aiCutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// answerAIPlan answers the plan card: "approve" runs the plan, "edit"
// opens it as a file, anything else ("deny", Esc) stops the run.
func (m *Model) answerAIPlan(id string) tea.Cmd {
	s := m.aiS()
	rm := s.aiPlanWaiting()
	if rm == nil {
		return nil
	}
	switch id {
	case "approve":
		s.run.Approve()
		return nil
	case "edit":
		return m.editAIPlan(rm)
	}
	s.run.Deny()
	return m.aiInfo("Plan denied: the run stops")
}

// aiPlanEditPath is the file the plan of run id is edited in (in the
// state folder, never in the project).
func aiPlanEditPath(id string) (string, error) {
	dir, err := aiRunsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "plans", aiSafeFileName(id)+".json"), nil
}

func aiSafeFileName(id string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return r
		}
		return '_'
	}, id)
}

// editAIPlan writes the plan the run waits on to its file and opens it.
func (m *Model) editAIPlan(rm *ai.StoredMessage) tea.Cmd {
	s := m.aiS()
	path, err := aiPlanEditPath(rm.Run.ID)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0o700)
	}
	if err == nil {
		err = os.WriteFile(path, []byte(agentrun.PlanText(s.run.Snapshot())), 0o600)
	}
	if err != nil {
		return m.aiToastErr("Could not open the plan", err)
	}
	m.openFileAtLine(path, 1)
	return m.aiInfo("Edit the plan and save it: the plan card comes back. Leave a step out to skip it.")
}

// onAIPlanSaved sends a saved plan file to the run that waits on it.
func (m *Model) onAIPlanSaved(path string) tea.Cmd {
	if m.ai == nil {
		return nil
	}
	s := m.aiS()
	rm := s.aiPlanWaiting()
	if rm == nil {
		return nil
	}
	want, err := aiPlanEditPath(rm.Run.ID)
	if err != nil || filepath.Clean(path) != filepath.Clean(want) {
		return nil
	}
	b, err := os.ReadFile(want)
	if err != nil {
		return m.aiToastErr("Could not read the plan", err)
	}
	s.run.EditPlan(string(b))
	return nil
}

// aiRemovePlanFile removes the edit file of run id (the run ended).
func aiRemovePlanFile(id string) {
	if path, err := aiPlanEditPath(id); err == nil {
		_ = os.Remove(path)
	}
}

// aiPlanWaitLines are the chat lines of a run that waits for the user's
// answer on its plan.
func aiPlanWaitLines(i int, width, inner int) []aiLine {
	ind := strings.Repeat(" ", aiIndent)
	var out []aiLine
	for _, l := range wrapPlain("Waiting for you: approve the plan before steps that change files or run commands start.", max(inner, 8)) {
		out = append(out, aiLine{text: ind + l, style: aiStyle(theme.Fg(theme.DiagWarning))})
	}
	return append(out, aiChips(width, []aiChipSpec{{"Review plan", "plan-open:" + strconv.Itoa(i)}, {"Deny", "plan-deny:" + strconv.Itoa(i)}}))
}
