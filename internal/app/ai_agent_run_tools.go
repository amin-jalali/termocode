package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
)

// ── Agents that start agents (G-API) ──────────────────────────────────────
//
// agent_run_start asks on a card (Deny first, with the focus; it has no
// Always allow class, so every call asks, whatever the user allowed) and
// queues a run: a profile, or steps the AI writes (the profile_draft step
// schema), on a task. A chat runs one turn or run at a time, so the run
// starts in this chat when the current turn or run ends, exactly as if the
// user had sent the task with that profile: plan approval, agent worktrees,
// the budget card and the review cards all apply. The task becomes the
// run's user message and StoredRun.By says the AI started it.
//
// Limits: a run the AI started from a run the AI started (depth 2) cannot
// start another; at most aiRunMaxActive AI-started runs wait or run at
// once; a run started from a run with a budget gets what is left of it.
// The tools are mutating (start, stop) or read only (status, result);
// outside agents never get them (mcpServeAllowList).

const (
	aiRunMaxDepth  = 2       // chat → AI run → its child; the child's steps cannot start another
	aiRunMaxActive = 3       // AI-started runs that wait or run at once
	aiRunInline    = "steps" // the profile name of a run with the AI's own steps
	aiRunTaskMax   = 8000    // runes of a task
)

// aiAgentRunToolNames are the tools of this file, in list order.
var aiAgentRunToolNames = []string{"agent_run_start", "agent_run_status", "agent_run_result", "agent_run_stop"}

// aiAgentRunAllow is the Allow button's title on the cards.
var aiAgentRunAllow = map[string]string{"agent_run_start": "Start run", "agent_run_stop": "Stop run"}

// aiQueuedRun is a run the user allowed that waits for the chat.
type aiQueuedRun struct {
	id   string
	chat string
	prof ai.Profile
	task string
	by   ai.RunBy
}

// aiRunCaller is who gets the tools: a plain chat turn (the zero value)
// or the steps of run parentRun.
type aiRunCaller struct {
	depth     int     // the run's By.Depth (0: the chat, or a run the user started)
	parentRun string  // "" in a plain chat turn
	budget    float64 // the run's budget in dollars (0: none)
}

// aiRunSnap is what Validate and Describe check against (they run off
// the UI goroutine): a copy taken when the tools are built.
type aiRunSnap struct {
	caller    aiRunCaller
	profiles  map[string]ai.Profile
	untrusted map[string]bool
	slots     map[string]bool
	slotModel map[string]string
	parallel  int
	chatTitle string
	active    int
	aiCost    float64 // what the AI-started runs of this chat cost so far
	aiCostOK  bool
	worktrees bool
}

func (m *Model) aiRunSnapshot(c aiRunCaller) aiRunSnap {
	sn := aiRunSnap{caller: c, profiles: map[string]ai.Profile{}, untrusted: map[string]bool{}, slots: map[string]bool{},
		slotModel: map[string]string{}, parallel: ai.Settings{}.Parallel(), worktrees: ai.Settings{}.UseWorktrees(), aiCostOK: true}
	if m.ai == nil {
		for _, p := range ai.BuiltinProfiles() {
			sn.profiles[p.Name] = p
		}
		for _, n := range []string{ai.SlotMain, ai.SlotQuick, ai.SlotLocal} {
			sn.slots[n] = true
		}
		return sn
	}
	s := m.ai
	for _, n := range s.profiles.Names() {
		p, _ := s.profiles.Get(n)
		sn.profiles[n] = p
		if m.aiProfileTrust(p) != aiTrustUse {
			sn.untrusted[n] = true
		}
	}
	for n, ok := range s.slotKnown {
		sn.slots[n] = ok
	}
	if s.slotKnown == nil {
		for _, n := range []string{ai.SlotMain, ai.SlotQuick, ai.SlotLocal} {
			sn.slots[n] = true
		}
	}
	sn.slotModel[ai.SlotMain] = s.cfg.Model
	for n, sl := range s.settings.Models {
		if sl.Model != "" {
			sn.slotModel[n] = sl.Model
		} else {
			sn.slotModel[n] = ai.DefaultModel(sl.Provider)
		}
	}
	for n, sl := range s.slotFake { // tests and the demo shots
		sn.slotModel[n] = sl.cfg.Model
	}
	sn.parallel = s.settings.Parallel()
	sn.worktrees = s.settings.UseWorktrees()
	sn.chatTitle = s.conv.Title
	sn.active = s.aiRunsActive()
	sn.aiCost, sn.aiCostOK = s.aiStartedCost()
	return sn
}

// aiRunsActive counts the AI-started runs of the open chat that wait or
// run.
func (s *aiState) aiRunsActive() int {
	n := 0
	for _, q := range s.runQueue {
		if q.chat == s.conv.ID {
			n++
		}
	}
	if rm := s.aiRunMessage(); rm != nil && s.run != nil && rm.Run.By != nil {
		n++
	}
	return n
}

// aiStartedCost is what the open chat's AI-started runs cost so far.
func (s *aiState) aiStartedCost() (float64, bool) {
	sum, ok := 0.0, true
	for _, msg := range s.conv.Messages {
		if msg.Run != nil && msg.Run.By != nil {
			c, k := aiRunMsgCost(msg)
			sum += c
			ok = ok && k
		}
	}
	return sum, ok
}

// aiRunMsgCost sums a run message's step costs; ok is false when a step
// used tokens of a model without a known price (a CLI step on the
// subscription has none and counts as known).
func aiRunMsgCost(msg ai.StoredMessage) (float64, bool) {
	sum, ok := 0.0, true
	for _, st := range msg.Steps {
		if st.Cost != nil {
			sum += *st.Cost
		} else if st.Usage != nil && st.CLI == "" {
			ok = false
		}
	}
	return sum, ok
}

// ── agent_run_start ──────────────────────────────────────────────────────

// aiRunStartReq is a checked agent_run_start call.
type aiRunStartReq struct {
	prof     ai.Profile
	task     string
	worktree string // "auto" or "off"
}

func (sn aiRunSnap) checkStart(a map[string]any) (aiRunStartReq, error) {
	var r aiRunStartReq
	if err := aiArgsSecretError(a); err != nil {
		return r, err
	}
	if sn.caller.depth >= aiRunMaxDepth {
		return r, errors.New("this run was started by the AI from another AI-started run: it cannot start one more " +
			"(at most 2 levels). Do the work in this step, or say in your answer what the user should run")
	}
	r.task = strings.TrimSpace(ai.ArgString(a, "task"))
	if r.task == "" {
		return r, errors.New("task is required: what the run should do, as you would ask a colleague")
	}
	if n := len([]rune(r.task)); n > aiRunTaskMax {
		return r, fmt.Errorf("task is %d characters; keep it under %d (point to files instead of pasting them)", n, aiRunTaskMax)
	}
	switch r.worktree = strings.TrimSpace(ai.ArgString(a, "worktree")); r.worktree {
	case "":
		r.worktree = "auto"
	case "auto", "off":
	default:
		return r, errors.New(`worktree must be "auto" or "off"`)
	}
	name := strings.TrimSpace(ai.ArgString(a, "profile"))
	_, hasSteps := a["steps"]
	switch {
	case name != "" && hasSteps:
		return r, errors.New("give profile or steps, not both")
	case name != "":
		p, ok := sn.profiles[name]
		if !ok {
			return r, fmt.Errorf("there is no profile %q; profiles_list names them", aiCardText(name, 40))
		}
		if sn.untrusted[name] {
			return r, fmt.Errorf("%s is a project profile the user has not trusted; the user sends one message with it first", name)
		}
		r.prof = p
	case hasSteps:
		draft := map[string]any{"steps": a["steps"]}
		for _, k := range []string{"budget_usd", "categories"} {
			if v, ok := a[k]; ok {
				draft[k] = v
			}
		}
		p, err := aiDraftProfile(draft)
		if err != nil {
			return r, err
		}
		p.Name, p.Title = aiRunInline, "steps from the AI"
		if err := ai.ValidateProfile(p, sn.slots, sn.parallel); err != nil {
			return r, fmt.Errorf("%v (models_list names the slots)", err)
		}
		for i := range p.Steps { // as profiles.json fills them
			if p.Steps[i].ID == "" {
				p.Steps[i].ID = "step-" + strconv.Itoa(i+1)
			}
		}
		r.prof = p
	default:
		return r, errors.New("profile or steps is required (profiles_list names the profiles)")
	}
	if sn.active >= aiRunMaxActive {
		return r, fmt.Errorf("%d runs the AI started wait or run already (at most %d); wait for one to end "+
			"(agent_run_status) or stop one", sn.active, aiRunMaxActive)
	}
	return r, nil
}

// aiRunStepLines are the steps of p for the card, with the model each
// step runs on when it is known.
func (sn aiRunSnap) aiRunStepLines(p ai.Profile) []string {
	var out []string
	for i, st := range p.Steps {
		title := st.Title
		if title == "" {
			title = st.ID
		}
		on := p.StepLabel(st)
		if st.CLI == "" && st.Provider == "" {
			slot := p.SlotFor(st)
			if mod := sn.slotModel[slot]; mod != "" && !strings.Contains(on, mod) {
				on += " → " + mod
			}
		}
		line := fmt.Sprintf("%d · %s · %s · tools %s", i+1, aiCardText(title, 40), aiCardText(on, 40), ai.StepTools(st))
		if st.Plan {
			line += " · planner"
		}
		out = append(out, line)
		if head := aiFirstLine(st.System); head != "" {
			out = append(out, "    "+aiCardText(head, 70))
		}
	}
	return out
}

func (sn aiRunSnap) describeStart(a map[string]any) string {
	if aiArgsSecretError(a) != nil {
		return aiSecretCardText
	}
	r, err := sn.checkStart(a)
	if err != nil {
		return "Start an agent run (refused: " + aiCardText(err.Error(), 100) + ")"
	}
	var b strings.Builder
	what := "profile " + r.prof.Name
	if r.prof.Name == aiRunInline && r.prof.Source == ai.ProfileSourceUser {
		what = "steps the AI wrote"
	}
	fmt.Fprintf(&b, "Start an agent run with %s (%d step%s):\n", what, len(r.prof.Steps), plural(len(r.prof.Steps)))
	for _, l := range sn.aiRunStepLines(r.prof) {
		b.WriteString("  " + l + "\n")
	}
	b.WriteString("\nTask:\n")
	lines := strings.Split(aiCleanText(r.task), "\n")
	if len(lines) > 12 {
		lines = append(lines[:12], "…")
	}
	for _, l := range lines {
		b.WriteString("  " + aiOneLineClip(l, 100) + "\n")
	}
	b.WriteString("\n")
	switch {
	case r.worktree == "off":
		b.WriteString("Worktrees: off (every step works in this folder)\n")
	case sn.worktrees:
		b.WriteString("Worktrees: auto (steps that run side by side work in their own git worktree)\n")
	default:
		b.WriteString("Worktrees: off in your settings\n")
	}
	switch {
	case sn.caller.budget > 0:
		fmt.Fprintf(&b, "Budget: what is left of this run's %s when it starts\n", aiDollars(sn.caller.budget))
	case r.prof.BudgetUSD > 0:
		fmt.Fprintf(&b, "Budget: %s\n", aiDollars(r.prof.BudgetUSD))
	}
	if sn.aiCost > 0 || !sn.aiCostOK {
		cost := aiDollars(sn.aiCost)
		if !sn.aiCostOK {
			cost += " (some models have no price)"
		}
		b.WriteString("AI-started runs in this chat cost " + cost + " so far\n")
	}
	title := aiCardText(sn.chatTitle, 40)
	if title == "" {
		title = "this chat"
	} else {
		title = "the chat \"" + title + "\""
	}
	by := "\nStarted by AI in " + title
	if sn.caller.parentRun != "" {
		by += ", from run " + sn.caller.parentRun
	}
	b.WriteString(by + ".\n")
	b.WriteString("It starts here when the current answer or run ends; plan approval still asks before steps that write.")
	return b.String()
}

// aiToolRunStart queues the run the user allowed (on the UI goroutine).
func (m *Model) aiToolRunStart(ctx context.Context, c aiRunCaller, r aiRunStartReq) (string, tea.Cmd, error) {
	s := m.aiS()
	if c.depth >= aiRunMaxDepth {
		return "", nil, errors.New("this run cannot start another run (at most 2 levels)")
	}
	if n := s.aiRunsActive(); n >= aiRunMaxActive {
		return "", nil, fmt.Errorf("%d runs the AI started wait or run already (at most %d)", n, aiRunMaxActive)
	}
	by := ai.RunBy{Chat: s.conv.ID, ChatTitle: s.conv.Title, ParentRun: c.parentRun, Depth: c.depth + 1, ParentBudget: c.budget}
	if r.worktree == "off" {
		by.Worktree = "off"
	}
	if node, run := agentrun.NodeOf(ctx); node != "" && run == c.parentRun {
		if st := aiFindStep(s.aiRunMessage(), node); st != nil {
			by.Step = aiCleanText(st.Title)
			if by.Step == "" {
				by.Step = node
			}
		}
	}
	id := aiNewRunID()
	s.runQueue = append(s.runQueue, aiQueuedRun{id: id, chat: s.conv.ID, prof: r.prof, task: r.task, by: by})
	s.runGen++
	out, err := aiItermocJSON(struct {
		RunID string `json:"run_id"`
		State string `json:"state"`
		Note  string `json:"note"`
	}{id, "queued", "The user allowed it. The run starts in this chat when the current answer or run ends; " +
		"you cannot wait for it in this turn. Finish your answer now; agent_run_status and agent_run_result read it later."})
	return out, nil, err
}

// aiStartQueuedRun starts the open chat's next queued run (nil when
// none can start). Queued runs of another chat are dropped: that chat
// is not open any more.
func (m *Model) aiStartQueuedRun() tea.Cmd {
	s := m.aiS()
	for len(s.runQueue) > 0 && !s.streaming {
		q := s.runQueue[0]
		s.runQueue = s.runQueue[1:]
		if q.chat != s.conv.ID {
			s.aiRunNotStarted(q.id, "its chat was closed before it could start")
			continue
		}
		if q.by.ParentBudget > 0 {
			spent := 0.0
			for _, msg := range s.conv.Messages {
				if msg.Run != nil && (msg.Run.ID == q.by.ParentRun || (msg.Run.By != nil && msg.Run.By.ParentRun == q.by.ParentRun)) {
					c, _ := aiRunMsgCost(msg)
					spent += c
				}
			}
			left := q.by.ParentBudget - spent
			if left < 0.005 {
				why := "the budget " + aiDollars(q.by.ParentBudget) + " of run " + q.by.ParentRun + " is used up"
				s.aiRunNotStarted(q.id, why)
				s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredError,
					Text: "The run " + q.id + " the AI asked for did not start: " + why + "."})
				continue
			}
			q.by.Budget = left
		}
		if q.prof.Name == aiRunInline && q.prof.Source == ai.ProfileSourceUser {
			if s.runInline == nil {
				s.runInline = map[string]ai.Profile{}
			}
			s.runInline[q.id] = q.prof
		}
		n := len(s.conv.Messages)
		by := q.by
		cmd := m.startAIRunAs(q.task, q.prof, ai.LoadProjectRules(aiWorkspaceRoot()), q.id, &by)
		if len(s.conv.Messages) == n { // the profile cannot run (a slot is not set up): its toast says why
			s.aiRunNotStarted(q.id, "its profile cannot run now (a model slot is not set up)")
			delete(s.runInline, q.id)
			if cmd != nil {
				return cmd
			}
			continue
		}
		return cmd
	}
	return nil
}

// aiRunNotStarted remembers why a queued run never started (the status
// tool says it).
func (s *aiState) aiRunNotStarted(id, why string) {
	if s.runNotStarted == nil {
		s.runNotStarted = map[string]string{}
	}
	s.runNotStarted[id] = why
	s.runGen++
}

// aiAgentRunTools are the tools of this file for caller c.
func (m *Model) aiAgentRunTools(call aiIDECaller, c aiRunCaller) []ai.Tool {
	sn := m.aiRunSnapshot(c)
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	return append([]ai.Tool{
		{
			Name:    "agent_run_start",
			Display: "start an agent run",
			Description: "Start another agent run on a task: a profile (profiles_list), or steps you write (each with a title " +
				"and a prompt, as in profile_draft). The user confirms on a card. The run starts in this chat when your current " +
				"answer (or run) ends, like a message the user sends with that profile, so plan approval and agent worktrees " +
				"apply; you cannot wait for it in this turn. Returns the run id for agent_run_status and agent_run_result. " +
				"At most 3 such runs at once; a run started this way may start one more level, not two.",
			Params: map[string]any{
				"task":    str("what the run should do (its first message)"),
				"profile": str("profile name from profiles_list (or give steps)"),
				"steps": map[string]any{"type": "array", "description": "the run's own steps in order, instead of a profile",
					"items": map[string]any{"type": "object", "properties": aiProfileStepProps(), "required": []string{"title"}}},
				"budget_usd": map[string]any{"type": "number", "description": "with steps: pause the run once it cost this much (optional)"},
				"worktree": map[string]any{"type": "string", "enum": []string{"auto", "off"},
					"description": "auto (default): steps that run side by side get their own git worktree, as the user's settings say; off: every step works in the workspace"},
			},
			Required: []string{"task"},
			// No Always allow class: every call asks (G-API).
			Mutating: true,
			Validate: func(a map[string]any) error { _, err := sn.checkStart(a); return err },
			Describe: sn.describeStart,
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				r, err := sn.checkStart(a)
				if err != nil {
					return "", err
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiToolRunStart(ctx, c, r) })
			},
		},
	}, aiRunReadTools(call)...)
}

// aiAgentRunTool reports a tool of this file.
func aiAgentRunTool(name string) bool {
	for _, n := range aiAgentRunToolNames {
		if n == name {
			return true
		}
	}
	return false
}

// aiDropQueuedRuns drops the queued runs (why: the status tool says it)
// and returns how many there were.
func (m *Model) aiDropQueuedRuns(why string) int {
	s := m.aiS()
	n := len(s.runQueue)
	for _, q := range s.runQueue {
		s.aiRunNotStarted(q.id, why)
	}
	s.runQueue = nil
	return n
}

// ── agent_run_status / agent_run_result ──────────────────────────────────

// Text caps of the read tools (runes).
const (
	aiRunAnswerMax = 4000
	aiRunStepMax   = 1500
)

// aiRunClip is s redacted and cleaned, cut at n runes.
func aiRunClip(s string, n int) string {
	s = strings.TrimSpace(aiCleanText(ai.Redact(s)))
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "\n…(cut: " + strconv.Itoa(len(r)-n) + " more characters)"
	}
	return s
}

// aiRunFound is a run of the open chat by id: its message (nil while it
// is queued or when it never started), its queue entry, why it never
// started, and whether it is the live one.
type aiRunFound struct {
	idx    int
	msg    *ai.StoredMessage
	queued *aiQueuedRun
	why    string
	live   bool
}

func (m *Model) aiRunFind(id string) (aiRunFound, error) {
	s := m.aiS()
	id = strings.TrimSpace(id)
	if id == "" {
		return aiRunFound{}, errors.New("run_id is required (agent_run_start returns it)")
	}
	for i := range s.runQueue {
		if q := &s.runQueue[i]; q.id == id && q.chat == s.conv.ID {
			return aiRunFound{queued: q}, nil
		}
	}
	for i := range s.conv.Messages {
		if msg := &s.conv.Messages[i]; msg.Run != nil && msg.Run.ID == id {
			return aiRunFound{idx: i, msg: msg, live: s.run != nil && s.runMsg == i}, nil
		}
	}
	if why, ok := s.runNotStarted[id]; ok {
		return aiRunFound{why: why}, nil
	}
	return aiRunFound{}, fmt.Errorf("there is no run %q in this chat", aiCardText(id, 30))
}

type aiRunStepStatus struct {
	ID       string   `json:"id"`
	Title    string   `json:"title,omitempty"`
	State    string   `json:"state"`
	Model    string   `json:"model,omitempty"`
	CostUSD  *float64 `json:"cost_usd,omitempty"`
	Error    string   `json:"error,omitempty"`
	Worktree string   `json:"worktree_branch,omitempty"`
}

type aiRunStatusInfo struct {
	RunID        string            `json:"run_id"`
	State        string            `json:"state"`
	Profile      string            `json:"profile,omitempty"`
	Task         string            `json:"task,omitempty"`
	StartedByAI  bool              `json:"started_by_ai"`
	Depth        int               `json:"depth,omitempty"`
	ParentRun    string            `json:"parent_run,omitempty"`
	Steps        []aiRunStepStatus `json:"steps,omitempty"`
	CostUSD      float64           `json:"cost_usd"`
	CostComplete bool              `json:"cost_complete"`
	BudgetUSD    float64           `json:"budget_usd,omitempty"`
	WaitingFor   []string          `json:"waiting_for_user,omitempty"`
	Note         string            `json:"note,omitempty"`
}

// aiRunStatusOf is the status of run id (on the UI goroutine).
func (m *Model) aiRunStatusOf(id string) (aiRunStatusInfo, error) {
	f, err := m.aiRunFind(id)
	if err != nil {
		return aiRunStatusInfo{}, err
	}
	out := aiRunStatusInfo{RunID: strings.TrimSpace(id), CostComplete: true}
	switch {
	case f.queued != nil:
		q := f.queued
		out.State, out.Profile, out.Task = "queued", q.prof.Name, aiRunClip(q.task, 200)
		out.StartedByAI, out.Depth, out.ParentRun = true, q.by.Depth, q.by.ParentRun
		for _, st := range q.prof.Steps {
			out.Steps = append(out.Steps, aiRunStepStatus{ID: st.ID, Title: aiCleanText(st.Title), State: string(agentdag.StatePending)})
		}
		out.Note = "It starts in this chat when the current answer or run ends."
		return out, nil
	case f.msg == nil:
		out.State, out.Note = "not_started", "It never started: "+f.why+"."
		return out, nil
	}
	s := m.aiS()
	r := f.msg.Run
	out.State, out.Profile = r.State, r.Profile
	out.Task = aiRunClip(runsTask(s.conv.Messages, f.idx), 200)
	if r.By != nil {
		out.StartedByAI, out.Depth, out.ParentRun = true, r.By.Depth, r.By.ParentRun
	}
	for _, st := range f.msg.Steps {
		ss := aiRunStepStatus{ID: st.ID, Title: aiCleanText(st.Title), State: st.State, Model: st.Model, CostUSD: st.Cost,
			Error: aiRunClip(st.Error, 300)}
		if st.Worktree != nil {
			ss.Worktree = st.Worktree.Branch
		}
		out.Steps = append(out.Steps, ss)
	}
	out.CostUSD, out.CostComplete = aiRunMsgCost(*f.msg)
	if prof, ok := s.runInline[r.ID]; ok {
		out.BudgetUSD = aiRunBudget(prof, r.By)
	} else if prof, ok := s.profiles.Get(r.Profile); ok {
		out.BudgetUSD = aiRunBudget(prof, r.By)
	}
	if f.live {
		switch {
		case r.PlanWait:
			out.WaitingFor = append(out.WaitingFor, "plan approval: the user approves, edits or denies the plan")
		case r.State == ai.RunStatePaused && r.Budget > 0:
			out.WaitingFor = append(out.WaitingFor, "budget reached: the user continues or stops the run")
		case r.State == ai.RunStatePaused:
			out.WaitingFor = append(out.WaitingFor, "paused: the user resumes the run")
		}
		if m.confirmOpen && m.confirmKind == confirmKindAITool && s.confirm != nil {
			out.WaitingFor = append(out.WaitingFor, "a card asks the user to allow "+s.confirm.tool.Name)
		}
	} else if r.State == ai.RunStateRunning || r.State == ai.RunStatePaused {
		out.State = ai.RunStateInterrupted // a stored run that is not live does not go on
	}
	return out, nil
}

type aiRunStepResult struct {
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	State  string `json:"state"`
	Answer string `json:"answer,omitempty"`
	Error  string `json:"error,omitempty"`
}

type aiRunChangedFile struct {
	Path   string `json:"path"`
	Status string `json:"status"` // A, M or D
	Step   string `json:"step"`
	Branch string `json:"worktree_branch"`
}

type aiRunResultInfo struct {
	RunID        string             `json:"run_id"`
	State        string             `json:"state"`
	Answer       string             `json:"answer,omitempty"`
	Steps        []aiRunStepResult  `json:"steps,omitempty"`
	ChangedFiles []aiRunChangedFile `json:"changed_files,omitempty"`
	FilesInTree  int                `json:"files_changed_in_workspace,omitempty"`
	Note         string             `json:"note,omitempty"`
}

// aiRunWT is a step's open worktree the result lists the files of.
type aiRunWT struct{ id, step, branch string }

// aiRunResultOf is the result of run id (on the UI goroutine) and the
// open worktrees whose files the caller lists (off it: git runs).
func (m *Model) aiRunResultOf(id string) (aiRunResultInfo, []aiRunWT, error) {
	st, err := m.aiRunStatusOf(id)
	if err != nil {
		return aiRunResultInfo{}, nil, err
	}
	out := aiRunResultInfo{RunID: st.RunID, State: st.State}
	f, _ := m.aiRunFind(id)
	if f.msg == nil {
		out.Note = st.Note
		return out, nil, nil
	}
	var wts []aiRunWT
	for _, s := range f.msg.Steps {
		sr := aiRunStepResult{ID: s.ID, Title: aiCleanText(s.Title), State: s.State, Error: aiRunClip(s.Error, 300)}
		if agentdag.State(s.State) == agentdag.StateCompleted {
			sr.Answer = aiRunClip(s.Text, aiRunStepMax)
		}
		out.Steps = append(out.Steps, sr)
		switch {
		case s.Worktree.Open():
			wts = append(wts, aiRunWT{id: s.Worktree.ID, step: s.ID, branch: s.Worktree.Branch})
		case s.Worktree == nil:
			out.FilesInTree += s.Files
		}
	}
	switch st.State {
	case ai.RunStateDone:
		out.Answer = aiRunClip(f.msg.Text, aiRunAnswerMax)
	case ai.RunStateRunning, ai.RunStatePaused:
		out.Note = "The run is not finished; the answers of finished steps are below. Ask again later."
	default:
		out.Note = "The run ended without a final answer (" + st.State + ")."
	}
	if len(wts) > 0 {
		out.Note = strings.TrimSpace(out.Note + " Changes in worktrees wait for the user to merge or discard them.")
	}
	return out, wts, nil
}

// aiRunReadTools are agent_run_status and agent_run_result.
func aiRunReadTools(call aiIDECaller) []ai.Tool {
	idParam := map[string]any{"run_id": map[string]any{"type": "string", "description": "the id agent_run_start returned (or a run id of this chat)"}}
	return []ai.Tool{
		{
			Name: "agent_run_status",
			Description: "The state of an agent run of this chat: queued, running, paused, done, failed, cancelled, " +
				"interrupted or not_started; each step's state and model, the cost so far, and what waits for the user " +
				"(plan approval, budget, a card). Read only.",
			Params:   idParam,
			Required: []string{"run_id"},
			Describe: func(a map[string]any) string { return "Agent run status " + aiCardText(ai.ArgString(a, "run_id"), 20) },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				id := ai.ArgString(a, "run_id")
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					st, err := m.aiRunStatusOf(id)
					if err != nil {
						return "", nil, err
					}
					out, err := aiItermocJSON(st)
					return out, nil, err
				})
			},
		},
		{
			Name: "agent_run_result",
			Description: "The result of an agent run of this chat: the final answer, each finished step's answer (long " +
				"text is cut) and the files it changed (in agent worktrees: path, status and branch; they wait for the " +
				"user's merge). Read only.",
			Params:   idParam,
			Required: []string{"run_id"},
			Describe: func(a map[string]any) string { return "Agent run result " + aiCardText(ai.ArgString(a, "run_id"), 20) },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				id := ai.ArgString(a, "run_id")
				var res aiRunResultInfo
				var wts []aiRunWT
				root := ""
				if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
					var err error
					res, wts, err = m.aiRunResultOf(id)
					root = aiWorkspaceRoot()
					return "", nil, err
				}); err != nil {
					return "", err
				}
				for _, w := range wts {
					ch, err := git.AgentChanges(root, w.id)
					if err != nil {
						res.Note = strings.TrimSpace(res.Note + " The worktree of step " + w.step + " could not be read.")
						continue
					}
					for _, c := range ch {
						res.ChangedFiles = append(res.ChangedFiles, aiRunChangedFile{Path: c.Path, Status: c.Status, Step: w.step, Branch: w.branch})
					}
				}
				return aiItermocJSON(res)
			},
		},
	}
}
