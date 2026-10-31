package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/crash"
)

// ── Background runs (OMX-6) ───────────────────────────────────────────
//
// A run the AI starts (agent_run_start) whose steps only read — every
// step has read-only tools, no planner, no loop, no coding CLI — starts
// at once and runs beside the chat: the turn that started it goes on,
// and so may other runs. Nothing it does asks the user (it cannot write
// files or run commands), so it needs none of the chat's cards. When it
// ends, its answer is handed to whoever started it (the chat turn, or
// the run step) in the same turn if that still works (Agent.Inbox),
// else the chat shows its card when idle. agent_run_wait waits for it;
// agent_run_send steers one of its steps. A run that may change files
// still waits for the chat (the queue in ai_agent_run_tools.go): it
// needs plan approval, worktrees and review cards.
//
// While the turn that started it goes, the run's messages are held here
// (AGENTS shows them, agentsHeld); when the chat is idle they join it.

// aiBgRun is a run the AI started that runs beside the chat.
type aiBgRun struct {
	id, chat string
	// user and msg are the run's task and run message. They are held
	// here while the chat turn that started the run goes, and copied
	// into the chat when it is idle (inConv); msg stays the truth.
	user, msg ai.StoredMessage
	inConv    bool
	h         *agentrun.Handle
	cancel    context.CancelFunc
	ch        chan tea.Msg
	slots     map[string]aiSlot
	parent    string        // the mailbox key of who started it
	fin       chan struct{} // closed when the run's end is recorded
	done      bool
	waiters   atomic.Int32 // agent_run_wait calls that wait for it now (their goroutines)
	convIdx   int          // where msg was last seen in the chat
}

// aiBgMail hands notes to the agents that work now (a chat turn, a run
// step), by key; each agent reads its notes after its tool calls.
type aiBgMail struct {
	mu   sync.Mutex
	open map[string][]string
}

// aiBgNotePrefix starts a note termocode hands to a working agent.
const aiBgNotePrefix = "[A note from termocode while you work]\n"

func (b *aiBgMail) openKey(k string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.open == nil {
		b.open = map[string][]string{}
	}
	if _, ok := b.open[k]; !ok {
		b.open[k] = nil
	}
}

func (b *aiBgMail) closeKey(k string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.open, k)
}

// push queues text for k; false when nobody with that key works now.
func (b *aiBgMail) push(k, text string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	q, ok := b.open[k]
	if !ok {
		return false
	}
	b.open[k] = append(q, text)
	return true
}

// inbox is the Agent.Inbox of the agent with key k.
func (b *aiBgMail) inbox(k string) func() []ai.Message {
	return func() []ai.Message {
		b.mu.Lock()
		q := b.open[k]
		if len(q) > 0 {
			b.open[k] = nil
		}
		b.mu.Unlock()
		var out []ai.Message
		for _, t := range q {
			out = append(out, ai.Message{Role: ai.RoleUser, Content: aiBgNotePrefix + t})
		}
		return out
	}
}

// aiBgTurnKey / aiBgStepKey are the mailbox keys of a chat turn and of
// a run's step.
func aiBgTurnKey(gen uint64) string       { return fmt.Sprintf("turn:%d", gen) }
func aiBgStepKey(run, node string) string { return "step:" + run + "\x00" + node }

// aiMailCtxKey carries a chat turn's mailbox key in its tools' ctx.
type aiMailCtxKey struct{}

// aiMailKeyOf is the mailbox key of the agent whose tool runs with ctx:
// its run step, else its chat turn ("": neither, e.g. an outside agent).
func aiMailKeyOf(ctx context.Context) string {
	if node, run := agentrun.NodeOf(ctx); node != "" {
		return aiBgStepKey(run, node)
	}
	k, _ := ctx.Value(aiMailCtxKey{}).(string)
	return k
}

// aiBgEligible reports whether run may run in the background: every
// step has the read-only or no tools (an allow or deny list only
// removes more), and none plans, runs a CLI or loops.
func aiBgEligible(run agentdag.Run) bool {
	if run.Loop != nil || len(run.Nodes) == 0 {
		return false
	}
	for _, n := range run.Nodes {
		if n.CLI != "" || n.Plan {
			return false
		}
		if p := n.Tools.Preset; p != agentdag.PresetReadOnly && p != agentdag.PresetNone {
			return false
		}
	}
	return true
}

// agentsHeld are the open chat's background runs that are not in the
// chat yet (AGENTS shows them after the chat's runs).
func (s *aiState) agentsHeld() []*ai.StoredMessage {
	var out []*ai.StoredMessage
	for _, b := range s.bgRuns {
		if !b.inConv && b.chat == s.conv.ID {
			out = append(out, &b.msg)
		}
	}
	return out
}

func (s *aiState) aiBgFind(id string) *aiBgRun {
	for _, b := range s.bgRuns {
		if b.id == id {
			return b
		}
	}
	return nil
}

// aiBgActive counts chat's background runs that go.
func (s *aiState) aiBgActive(chat string) int {
	n := 0
	for _, b := range s.bgRuns {
		if !b.done && b.chat == chat {
			n++
		}
	}
	return n
}

// Messages of a background run (its own channel, never the chat's).
type (
	aiBgEventMsg struct {
		id string
		ev agentrun.Event
		ch chan tea.Msg
	}
	aiBgDoneMsg struct {
		id    string
		final agentrun.Event
	}
	aiBgCallMsg struct {
		id    string
		ctx   context.Context
		fn    aiIDEFunc
		reply chan aiIDEReply
		ch    chan tea.Msg
	}
)

// newAIBgCaller is the IDE caller of background run id.
func newAIBgCaller(id string, ch chan tea.Msg) aiIDECaller {
	return func(ctx context.Context, fn aiIDEFunc) (string, error) {
		reply := make(chan aiIDEReply, 1)
		select {
		case ch <- aiBgCallMsg{id: id, ctx: ctx, fn: fn, reply: reply, ch: ch}:
		case <-ctx.Done():
			return "", errAIChatStopped
		}
		select {
		case r := <-reply:
			return r.out, r.err
		case <-ctx.Done():
			return "", errAIChatStopped
		}
	}
}

// handleAIBgMsg routes the background runs' messages.
func (m *Model) handleAIBgMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case aiBgEventMsg:
		m.onAIBgEvent(msg)
		return waitAIChat(msg.ch), true
	case aiBgCallMsg:
		s := m.aiS()
		if b := s.aiBgFind(msg.id); b == nil || b.done || msg.ctx.Err() != nil {
			msg.reply <- aiIDEReply{err: errAIChatStopped}
			return waitAIChat(msg.ch), true
		}
		out, cmd, err := msg.fn(m)
		msg.reply <- aiIDEReply{out: out, err: err}
		return tea.Batch(cmd, waitAIChat(msg.ch)), true
	case aiBgDoneMsg:
		return m.onAIBgDone(msg), true
	}
	return nil, false
}

// aiBgStart starts run (checked by aiBgEligible) in the background for
// task. key is the mailbox key of who started it.
func (m *Model) aiBgStart(prof ai.Profile, run agentdag.Run, task string, by ai.RunBy, key string) (tea.Cmd, error) {
	s := m.aiS()
	slots, err := s.aiRunPreflight(run, prof.CategorySlots())
	if err != nil {
		return nil, err
	}
	for _, n := range run.Nodes {
		if _, ok := slots[n.Slot]; !ok {
			sl, err := s.resolveSlot(n.Slot)
			if err != nil {
				return nil, fmt.Errorf("step %q: %w", n.Title, err)
			}
			slots[n.Slot] = sl
		}
	}
	budget := aiRunBudget(prof, &by)
	if by.ParentBudget > 0 {
		left := by.ParentBudget - s.aiParentSpent(by.ParentRun)
		if left < 0.005 {
			return nil, fmt.Errorf("the budget %s of run %s is used up", aiDollars(by.ParentBudget), by.ParentRun)
		}
		by.Budget, budget = left, left
	}
	run.CreatedAt = clock.Now()
	b := &aiBgRun{id: run.ID, chat: s.conv.ID, parent: key, slots: slots, fin: make(chan struct{}), ch: make(chan tea.Msg, 64)}
	b.user = ai.StoredMessage{Role: ai.StoredUser, Text: task}
	steps := make([]ai.StoredStep, len(run.Nodes))
	chain := aiIsChain(run)
	for i, n := range run.Nodes {
		steps[i] = ai.StoredStep{ID: n.ID, Title: n.Title, Slot: n.Slot, Model: slots[n.Slot].cfg.Model,
			Category: n.Category, State: string(agentdag.StatePending)}
		if !chain {
			steps[i].Deps = agentdag.Deps(run, n.ID)
		}
	}
	b.msg = ai.StoredMessage{Role: ai.StoredAssistant, Steps: steps,
		Run: &ai.StoredRun{ID: run.ID, Profile: prof.Name, State: ai.RunStateRunning, StartedAtMs: run.CreatedAt.UnixMilli(), By: &by}}

	root := aiWorkspaceRoot()
	call := newAIBgCaller(b.id, b.ch)
	caller := aiRunCaller{depth: by.Depth, parentRun: run.ID, budget: budget}
	tools := m.aiChatToolsBy(root, call, caller)
	base := m.aiChatBasePrompt(root, ai.LoadProjectRules(root), "")
	pricing := ai.NewPricing(s.settings.Prices)
	maxSteps := s.settings.MaxSteps
	mail := &s.bgMail
	total := len(run.Nodes)
	index := map[string]int{}
	for i, n := range run.Nodes {
		index[n.ID] = i + 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	send := func(msg tea.Msg) {
		select {
		case b.ch <- msg:
		case <-ctx.Done():
		}
	}
	opts := agentrun.Options{
		Parallel:   s.settings.Parallel(),
		User:       task,
		Categories: prof.CategorySlots(),
		Budget:     budget,
		StallAfter: s.settings.StepStall(),
		Cost: func(n agentdag.Node, u ai.Usage) (float64, bool) {
			return pricing.Cost(slots[n.Slot].cfg.Kind, u)
		},
		Emit: func(e agentrun.Event) { send(aiBgEventMsg{id: b.id, ev: e, ch: b.ch}) },
		Build: func(ctx context.Context, n agentdag.Node, input string) (*ai.Agent, []ai.Message, error) {
			sl := slots[n.Slot]
			if sl.p == nil {
				return nil, nil, fmt.Errorf("model slot %q is not set up", n.Slot)
			}
			ts := aiFilterTools(tools, n.Tools)
			native := sl.p.SupportsTools()
			sys := ai.SystemPrompt(base+aiStepPrompt(n, index[n.ID], total)+aiBgStepNote, ts, native)
			k := aiBgStepKey(run.ID, n.ID)
			mail.openKey(k)
			// No Confirm: a background step never asks; a tool that would
			// ask is declined (its tools are read-only anyway).
			return &ai.Agent{Provider: sl.p, Tools: ts, Native: native, MaxSteps: maxSteps, Inbox: mail.inbox(k)},
				[]ai.Message{{Role: ai.RoleSystem, Content: sys}, {Role: ai.RoleUser, Content: input}}, nil
		},
		Finished: func(_ context.Context, n agentdag.Node) { mail.closeKey(aiBgStepKey(run.ID, n.ID)) },
	}
	h, err := agentrun.Start(ctx, run, opts)
	if err != nil {
		cancel()
		return nil, err
	}
	b.h, b.cancel = h, cancel
	s.bgRuns = append(s.bgRuns, b)
	s.runGen++
	ch := b.ch
	crash.Go("a background agent run", func() {
		final := h.Wait()
		cancel()
		ch <- aiBgDoneMsg{id: b.id, final: final}
		close(ch)
	})
	return waitAIChat(ch), nil
}

// aiBgStepNote tells a background step what it may do.
const aiBgStepNote = "\n\nYou run in the background, beside the user's chat: you can read and search, not change files or run " +
	"commands, and nobody answers questions. Finish with a clear, complete answer: it goes to the agent that started you."

// aiParentSpent is what run parent and the runs it started cost so far
// in the open chat (held background runs too).
func (s *aiState) aiParentSpent(parent string) float64 {
	spent := 0.0
	add := func(msg ai.StoredMessage) {
		if msg.Run != nil && (msg.Run.ID == parent || (msg.Run.By != nil && msg.Run.By.ParentRun == parent)) {
			c, _ := aiRunMsgCost(msg)
			spent += c
		}
	}
	for _, msg := range s.conv.Messages {
		add(msg)
	}
	for _, msg := range s.agentsHeld() {
		add(*msg)
	}
	return spent
}

// onAIBgEvent applies one event of a background run to its cards.
func (m *Model) onAIBgEvent(msg aiBgEventMsg) {
	s := m.aiS()
	b := s.aiBgFind(msg.id)
	if b == nil {
		return
	}
	ev := msg.ev
	s.runGen++
	s.agentsLogEvent(ev)
	rm := &b.msg
	if ev.Kind == agentrun.RunPaused { // the budget: nobody can answer a card here
		b.cancel()
		rm.Run.Spent, rm.Run.Budget = ev.Cost, ev.Budget
		return
	}
	st := aiFindStep(rm, ev.NodeID)
	if st == nil {
		return
	}
	now := clock.Now()
	switch ev.Kind {
	case agentrun.NodeStart:
		*st = ai.StoredStep{ID: st.ID, Title: st.Title, Slot: st.Slot, Model: st.Model, Category: st.Category, Deps: st.Deps,
			State: string(agentdag.StateRunning), StartedAtMs: now.UnixMilli(), Notes: st.Notes, ElapsedMs: st.ElapsedMs}
	case agentrun.NodeText:
		st.Text = ev.Tail
	case agentrun.NodeTool:
		if ev.AI.Kind == ai.EventToolStart {
			st.Tools = append(st.Tools, "⚙ "+ev.AI.Detail)
		} else if n := len(st.Tools); n > 0 {
			st.Tools[n-1] += " → " + aiShortResult(ev.AI.Detail)
		}
	case agentrun.NodeDone:
		st.State, st.Text = string(agentdag.StateCompleted), ev.Output
		m.aiBgStepUsage(b, st, ev)
	case agentrun.NodeFailed:
		st.State, st.Error = string(agentdag.StateFailed), ai.Redact(ev.Err)
		recordError("[ai] background step " + st.ID + ": " + st.Error)
		m.aiBgStepUsage(b, st, ev)
	case agentrun.NodeCancelled:
		st.State = string(agentdag.StateCancelled)
		if ev.Err != "" {
			st.Error = ev.Err
			st.Notes = append(st.Notes, "■ "+ev.Err)
		}
		st.ElapsedMs += ev.Elapsed.Milliseconds()
	case agentrun.NodeBlocked:
		st.State = string(agentdag.StateBlocked)
	case agentrun.NodeMessage:
		if ev.Err == "" {
			st.Notes = append(st.Notes, "✉ "+aiCardText(ai.Redact(ev.Output), 60))
		}
	case agentrun.NodeStalled:
		st.State = string(agentdag.StatePending)
		st.Notes = append(st.Notes, "⏱ "+ev.Err)
		st.ElapsedMs += ev.Elapsed.Milliseconds()
	case agentrun.NodeRepair:
		st.Notes = append(st.Notes, "↻ "+ai.Redact(ev.Err)+", asked again")
	}
	m.aiBgSync(b)
}

// aiBgStepUsage records a finished background step's time, tokens and
// cost.
func (m *Model) aiBgStepUsage(b *aiBgRun, st *ai.StoredStep, ev agentrun.Event) {
	s := m.aiS()
	st.ElapsedMs += ev.Elapsed.Milliseconds()
	u := ev.Usage
	if u == nil {
		return
	}
	c := *u
	st.Usage = st.Usage.Add(&c)
	kind := b.slots[st.Slot].cfg.Kind
	if usd, ok := ai.NewPricing(s.settings.Prices).Cost(kind, c); ok {
		st.Cost = &usd
	}
	model := c.Model
	if model == "" {
		model = st.Model
	}
	s.recordUsage(aiUsageEntry{Run: b.id, Agent: aiCleanText(st.Title), Model: model, U: c, Cost: st.Cost, Sub: kind == ai.KindClaudeOAuth})
}

// onAIBgDone records the end of a background run and hands its answer
// to who started it.
func (m *Model) onAIBgDone(msg aiBgDoneMsg) tea.Cmd {
	s := m.aiS()
	b := s.aiBgFind(msg.id)
	if b == nil || b.done {
		return nil
	}
	final := msg.final
	rm := &b.msg
	for _, n := range b.h.Snapshot().Nodes {
		if st := aiFindStep(rm, n.ID); st != nil {
			st.State = string(n.State)
			switch n.State {
			case agentdag.StateRunning, agentdag.StatePending, agentdag.StateScheduled, agentdag.StatePaused:
				st.State = string(agentdag.StateCancelled)
			}
			if n.Error != "" && st.Error == "" {
				st.Error = n.Error
			}
		}
	}
	rm.Run.ElapsedMs += final.Elapsed.Milliseconds()
	switch final.Kind {
	case agentrun.RunDone:
		rm.Run.State, rm.Text = ai.RunStateDone, final.Output
	case agentrun.RunFailed:
		rm.Run.State = ai.RunStateFailed
		recordError("[ai] background run: " + ai.Redact(final.Err))
	default:
		rm.Run.State = ai.RunStateCancelled
		if rm.Run.Budget > 0 {
			rm.Text = "The run stopped: it reached its budget " + aiDollars(rm.Run.Budget) + "."
		}
	}
	b.done = true
	close(b.fin)
	s.runGen++
	m.aiBgSync(b)
	var cmd tea.Cmd
	if b.waiters.Load() == 0 && !s.bgMail.push(b.parent, aiBgEndNote(b, final)) {
		cmd = m.aiInfo("Background run " + b.id + " " + aiBgEndWord(rm.Run.State))
	}
	if b.chat == s.conv.ID && !s.streaming {
		m.aiBgFlush()
	} else if b.chat != s.conv.ID {
		m.aiBgSaveElsewhere(b)
	}
	return cmd
}

func aiBgEndWord(state string) string {
	switch state {
	case ai.RunStateDone:
		return "is done"
	case ai.RunStateFailed:
		return "failed"
	}
	return "stopped"
}

// aiBgEndNote is the note an ended run hands to who started it.
func aiBgEndNote(b *aiBgRun, final agentrun.Event) string {
	rm := b.msg
	var w strings.Builder
	fmt.Fprintf(&w, "The background run %s you started ended: %s.\nTask: %s\n", b.id, rm.Run.State, aiRunClip(b.user.Text, 160))
	switch {
	case rm.Run.State == ai.RunStateDone:
		w.WriteString("Its answer:\n" + aiRunClip(rm.Text, aiRunAnswerMax/2) + "\n")
	case final.Err != "":
		w.WriteString("Why: " + aiRunClip(final.Err, 300) + "\n")
	}
	w.WriteString("agent_run_result " + b.id + " has each step's answer.")
	return w.String()
}

// aiBgSync copies a background run's message into the open chat when it
// is there already.
func (m *Model) aiBgSync(b *aiBgRun) {
	s := m.aiS()
	if !b.inConv || b.chat != s.conv.ID {
		return
	}
	if i := b.convIdx; i < len(s.conv.Messages) && s.conv.Messages[i].Run != nil && s.conv.Messages[i].Run.ID == b.id {
		s.conv.Messages[i] = aiBgCopy(b.msg)
		return
	}
	for i := range s.conv.Messages {
		if r := s.conv.Messages[i].Run; r != nil && r.ID == b.id {
			s.conv.Messages[i] = aiBgCopy(b.msg)
			b.convIdx = i
			return
		}
	}
	b.inConv = false // the chat was loaded again without it: it joins again
}

func aiBgCopy(msg ai.StoredMessage) ai.StoredMessage {
	cp := msg
	cp.Steps = append([]ai.StoredStep(nil), msg.Steps...)
	if msg.Run != nil {
		r := *msg.Run
		cp.Run = &r
	}
	return cp
}

// aiBgFlush adds the open chat's held background runs to it (the chat
// is idle) and saves it.
func (m *Model) aiBgFlush() {
	s := m.aiS()
	if s.streaming {
		return
	}
	added := false
	for _, b := range s.bgRuns {
		if b.inConv || b.chat != s.conv.ID {
			continue
		}
		s.conv.Messages = append(s.conv.Messages, b.user, aiBgCopy(b.msg))
		b.inConv, added, b.convIdx = true, true, len(s.conv.Messages)-1
	}
	keep := s.bgRuns[:0]
	for _, b := range s.bgRuns {
		if !(b.done && b.inConv) {
			keep = append(keep, b)
		}
	}
	s.bgRuns = keep
	if added {
		s.runGen++
		if err := s.store.Save(&s.conv); err != nil {
			recordError("[ai] save chat: " + err.Error())
		}
	}
}

// aiBgSaveElsewhere writes an ended background run into its own chat
// when another chat is open.
func (m *Model) aiBgSaveElsewhere(b *aiBgRun) {
	s := m.aiS()
	c, err := s.store.Load(b.chat)
	if err != nil {
		recordError("[ai] background run " + b.id + ": its chat: " + err.Error())
		return
	}
	found := false
	for i := range c.Messages {
		if r := c.Messages[i].Run; r != nil && r.ID == b.id {
			c.Messages[i], found = aiBgCopy(b.msg), true
		}
	}
	if !found {
		c.Messages = append(c.Messages, b.user, aiBgCopy(b.msg))
	}
	if err := s.store.Save(&c); err != nil {
		recordError("[ai] save chat: " + err.Error())
	}
	b.inConv = true
	m.aiBgFlush() // drops it from the list
}

// aiBgStopAll stops every background run (Ctrl+C); it returns how many.
func (m *Model) aiBgStopAll() int {
	n := 0
	for _, b := range m.aiS().bgRuns {
		if !b.done {
			b.cancel()
			n++
		}
	}
	return n
}

// aiBgTurnInbox opens the mailbox of chat turn gen and is its agent's
// Inbox (closed in onAIChatDone).
func (m *Model) aiBgTurnInbox(gen uint64) func() []ai.Message {
	s := m.aiS()
	k := aiBgTurnKey(gen)
	s.bgMail.openKey(k)
	return s.bgMail.inbox(k)
}
