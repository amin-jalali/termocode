package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/schedule"
	"github.com/amin-jalali/termocode/internal/workflow/source"
	"github.com/amin-jalali/termocode/internal/workflow/trigger"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// ── Event triggers (CMP-38) ──────────────────────────────────────────────
//
// A workflow's "on" list starts it on outside events. Like a schedule,
// a trigger works only while the workflow is trusted and on, workflows
// are enabled and this process owns the workspace's workflows. The
// github triggers are polled through gh every workflows_trigger_poll_min
// minutes; the webhooks are served on 127.0.0.1:workflows_webhook_port
// (0: off) and must be signed with the secret workflow-hook.<name>.
// Every event shows a card (Cancel first) unless the trigger says
// "ask": false; the run gets the event as trigger_* vars and, with
// "worktree": true, its own agent worktree. Writes still ask.

// Confirm kinds of the triggers (WFX range 3500–3539).
const (
	confirmKindWorkflowTrigger     confirmKindEnum = 3520
	confirmKindWorkflowTriggerList confirmKindEnum = 3521
)

// wfxTrigRecentMax is how many trigger events the list keeps.
const wfxTrigRecentMax = 20

// wfxTrigQueueMax caps the events that wait for a card.
const wfxTrigQueueMax = 20

// wfxTrigJob is one github trigger to poll.
type wfxTrigJob struct {
	workflow string
	index    int
	t        workflow.Trigger
}

// wfxTrigSeen is one handled event and what became of it.
type wfxTrigSeen struct {
	at      time.Time
	ev      trigger.Event
	outcome string // "started", "skipped", "waiting", "ignored: …", "failed: …"
}

// wfxTrigHost polls and serves the triggers of the workspace.
type wfxTrigHost struct {
	ch    chan tea.Msg
	gh    *trigger.GitHub
	every time.Duration

	mu    sync.Mutex
	jobs  []wfxTrigJob
	hooks map[string]trigger.Hook
	wake  chan struct{}

	ctx    context.Context
	cancel context.CancelFunc

	srv  *http.Server
	port int
}

// Messages of the triggers.
type (
	wfxTrigEventMsg struct{ ev trigger.Event }
	wfxTrigErrMsg   struct{ text string }
)

// wfxTrigPoll is workflows_trigger_poll_min as a duration.
func wfxTrigPoll() time.Duration {
	v, _ := workflowSettingValue("setting-workflows-trigger-poll")
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		n = workflow.DefaultTriggerPollMin
	}
	return time.Duration(n) * time.Minute
}

// wfxWebhookPort is workflows_webhook_port (0: no webhooks).
func wfxWebhookPort() int {
	v, _ := workflowSettingValue("setting-workflows-webhook-port")
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || n > 65535 {
		return 0
	}
	return n
}

// wfxTrigWanted are the github triggers to poll and the webhooks to
// serve now.
func (m *Model) wfxTrigWanted() ([]wfxTrigJob, map[string]trigger.Hook) {
	hooks := map[string]trigger.Hook{}
	var jobs []wfxTrigJob
	if !workflowsEnabled() {
		return nil, hooks
	}
	pol := wfxPolicy()
	for _, e := range m.wfxS().list {
		if e.Broken() || !e.Trusted || !e.Enabled || e.File == nil || len(e.File.Triggers) == 0 || pol.Blocked(e.File) != "" {
			continue
		}
		for i, t := range e.File.Triggers {
			switch {
			case t.GitHub != "":
				jobs = append(jobs, wfxTrigJob{workflow: e.Name, index: i, t: t})
			case t.Webhook != "":
				if _, dup := hooks[t.Webhook]; !dup { // a name used by two files: the first wins
					hooks[t.Webhook] = trigger.Hook{Workflow: e.Name, Trigger: i, Name: t.Webhook, From: t.From}
				}
			}
		}
	}
	return jobs, hooks
}

// wfxTrigSync starts, changes or stops the poller and the webhook
// server so they match the list. It returns the listen cmd the first
// time.
func (m *Model) wfxTrigSync() tea.Cmd {
	st := m.wfxS()
	jobs, hooks := m.wfxTrigWanted()
	if st.notOwner || (len(jobs) == 0 && len(hooks) == 0) {
		if st.trig != nil {
			st.trig.stop()
			st.trig = nil
		}
		return nil
	}
	var cmd tea.Cmd
	if st.trig == nil {
		st.trig = m.newWfxTrigHost()
		cmd = wfxListen(st.trig.ch)
	}
	h := st.trig
	h.mu.Lock()
	h.jobs, h.hooks = jobs, hooks
	h.mu.Unlock()
	if h.gh.State != nil {
		ids := map[string]bool{}
		for _, j := range jobs {
			ids[trigger.ID(j.workflow, j.index, j.t)] = true
		}
		h.gh.State.Keep(ids)
	}
	h.syncServer(len(hooks) > 0, wfxWebhookPort(), m.wfxHookSecret)
	h.kick()
	return cmd
}

func (m *Model) newWfxTrigHost() *wfxTrigHost {
	st := m.wfxS()
	state := trigger.MemState()
	if st.root != "" {
		if s, err := trigger.OpenState(st.root); err == nil {
			state = s
		} else {
			recordError("workflow triggers: state: " + err.Error())
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &wfxTrigHost{
		ch:    make(chan tea.Msg, 64),
		gh:    &trigger.GitHub{Run: ghci.DefaultRunner(source.DefaultGitHubTimeout), Dir: st.ws, State: state},
		every: wfxTrigPoll(),
		wake:  make(chan struct{}, 1),
		ctx:   ctx, cancel: cancel,
	}
	crash.Go("workflow/trigger.poll", h.loop)
	return h
}

// wfxHookSecret reads a hook's signing secret from the secret store.
// The value never leaves the webhook check.
func (m *Model) wfxHookSecret(name string) (string, bool) {
	s, err := m.secretStore()
	if err != nil || s == nil {
		return "", false
	}
	v, ok, err := s.Get(workflow.WebhookSecretName(name))
	if err != nil || !ok || v == "" {
		return "", false
	}
	return v, true
}

func (h *wfxTrigHost) kick() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// loop polls every github trigger, then waits h.every (or a kick).
func (h *wfxTrigHost) loop() {
	for {
		h.pollOnce(h.ctx)
		select {
		case <-h.ctx.Done():
			return
		case <-h.wake:
		case <-clock.After(h.every):
		}
	}
}

// pollOnce polls each github trigger once and sends its new events.
func (h *wfxTrigHost) pollOnce(ctx context.Context) {
	h.mu.Lock()
	jobs := append([]wfxTrigJob(nil), h.jobs...)
	h.mu.Unlock()
	for _, j := range jobs {
		if ctx.Err() != nil {
			return
		}
		evs, err := h.gh.Poll(ctx, j.workflow, j.index, j.t)
		if err != nil {
			h.send(wfxTrigErrMsg{text: "workflow " + j.workflow + ": " + err.Error()})
		}
		for _, e := range evs {
			h.send(wfxTrigEventMsg{ev: e})
		}
	}
}

// send gives a message to the UI goroutine (dropped when the app is
// far behind: the poll state keeps the event as seen, so this only
// loses a run the user would see in the list as missing).
func (h *wfxTrigHost) send(msg tea.Msg) {
	select {
	case h.ch <- msg:
	case <-h.ctx.Done():
	default:
		recordError("workflow triggers: too many events at once; one was dropped")
	}
}

// lookup is the webhook server's hook table.
func (h *wfxTrigHost) lookup(name string) (trigger.Hook, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	k, ok := h.hooks[name]
	return k, ok
}

// syncServer starts the webhook server on 127.0.0.1:port when want, and
// stops it when not (or when the port changed).
func (h *wfxTrigHost) syncServer(want bool, port int, secret func(string) (string, bool)) {
	if h.srv != nil && (!want || port != h.port) {
		_ = h.srv.Close()
		h.srv, h.port = nil, 0
	}
	if !want || port == 0 || h.srv != nil {
		return
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		h.send(wfxTrigErrMsg{text: "workflow webhooks: " + err.Error()})
		return
	}
	var smu sync.Mutex
	hs := &trigger.Server{
		Lookup: h.lookup,
		Secret: func(name string) (string, bool) { // the store is not made for many goroutines
			smu.Lock()
			defer smu.Unlock()
			return secret(name)
		},
		Deliver: func(e trigger.Event) { h.send(wfxTrigEventMsg{ev: e}) },
	}
	mux := http.NewServeMux()
	mux.Handle(trigger.HookPrefix, hs)
	h.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	h.port = port
	srv := h.srv
	crash.Go("workflow/trigger.webhooks", func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.send(wfxTrigErrMsg{text: "workflow webhooks: " + err.Error()})
		}
	})
}

func (h *wfxTrigHost) stop() {
	h.cancel()
	if h.srv != nil {
		_ = h.srv.Close()
		h.srv = nil
	}
}

// wfxTrigStop ends the triggers (tests; the process end does it too).
func (m *Model) wfxTrigStop() {
	if m.wfx != nil && m.wfx.trig != nil {
		m.wfx.trig.stop()
		m.wfx.trig = nil
	}
}

// handleWfxTrigMsg handles the triggers' messages. ok=false: not ours.
func (m *Model) handleWfxTrigMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case wfxTrigEventMsg:
		return tea.Batch(m.wfxTrigOnEvent(msg.ev), m.wfxTrigListen()), true
	case wfxTrigErrMsg:
		recordError(msg.text)
		return m.wfxTrigListen(), true
	}
	return nil, false
}

func (m *Model) wfxTrigListen() tea.Cmd {
	if st := m.wfxS(); st.trig != nil {
		return wfxListen(st.trig.ch)
	}
	return nil
}

// wfxTrigRecord keeps ev in the recent list (newest last).
func (m *Model) wfxTrigRecord(ev trigger.Event, outcome string) {
	st := m.wfxS()
	st.trigRecent = append(st.trigRecent, wfxTrigSeen{at: clock.Now(), ev: ev, outcome: outcome})
	if n := len(st.trigRecent); n > wfxTrigRecentMax {
		st.trigRecent = st.trigRecent[n-wfxTrigRecentMax:]
	}
}

// wfxTrigCheck finds the workflow and trigger of ev and says why it may
// not run ("" = it may).
func (m *Model) wfxTrigCheck(ev trigger.Event) (wire.Entry, workflow.Trigger, string) {
	e, ok := wire.Find(m.wfxS().list, ev.Workflow)
	switch {
	case !ok:
		return e, workflow.Trigger{}, "the workflow file is gone"
	case e.Broken():
		return e, workflow.Trigger{}, "the workflow file has problems"
	case !e.Trusted || !e.Enabled:
		return e, workflow.Trigger{}, "the workflow is not trusted and on"
	case !workflowsEnabled():
		return e, workflow.Trigger{}, "workflows are turned off"
	case ev.Trigger < 0 || ev.Trigger >= len(e.File.Triggers):
		return e, workflow.Trigger{}, "the trigger is no longer in the file"
	}
	if why := wfxPolicy().Blocked(e.File); why != "" {
		return e, workflow.Trigger{}, "blocked: " + why
	}
	return e, e.File.Triggers[ev.Trigger], ""
}

// wfxTrigOnEvent handles one event: a card, or a run at once.
func (m *Model) wfxTrigOnEvent(ev trigger.Event) tea.Cmd {
	_, t, why := m.wfxTrigCheck(ev)
	if why != "" {
		m.wfxTrigRecord(ev, "ignored: "+why)
		return nil
	}
	if !t.Ask && !wfxPolicy().DeniesUnattended() {
		return m.wfxTrigStart(ev, false)
	}
	st := m.wfxS()
	if len(st.trigQueue) >= wfxTrigQueueMax {
		m.wfxTrigRecord(ev, "ignored: too many events wait for an answer")
		return nil
	}
	st.trigQueue = append(st.trigQueue, ev)
	m.wfxTrigRecord(ev, "waiting")
	if !m.confirmOpen {
		m.wfxTrigShowCard()
		return nil
	}
	return m.aiInfo(ev.Workflow + ": a trigger event waits for you (Workflows: Trigger Events…)")
}

// wfxTrigShowCard shows the card of the first waiting event.
func (m *Model) wfxTrigShowCard() {
	st := m.wfxS()
	if len(st.trigQueue) == 0 {
		return
	}
	ev := st.trigQueue[0]
	_, t, _ := m.wfxTrigCheck(ev)
	where := "It runs in the workspace."
	if t.Worktree {
		where = "It runs in a new agent worktree"
		if t.Base != "" {
			where += " from " + trigger.Clean(t.Base)
		}
		where += "."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s\n", ev.Workflow, trigger.Clean(ev.Summary()))
	if ev.Author != "" {
		fmt.Fprintf(&b, "By %s. ", trigger.Clean(ev.Author))
	}
	if ev.URL != "" {
		fmt.Fprintf(&b, "%s\n", trigger.Clean(ev.URL))
	} else {
		b.WriteString("\n")
	}
	b.WriteString("The text of this event comes from outside. The run gets it as trigger_* vars, never as a command.\n")
	b.WriteString(where + " Every write still asks you on a card.")
	if n := len(st.trigQueue) - 1; n > 0 {
		fmt.Fprintf(&b, "\n%d more event(s) wait after this one.", n)
	}
	m.wfxCard(confirmKindWorkflowTrigger, "Start "+ev.Workflow+" for this event?", b.String(),
		confirm.Button{ID: "cancel", Title: "Cancel"}, confirm.Button{ID: "start", Title: "Start run"})
}

// answerWorkflowTrigger answers the trigger card (and the list card).
func (m *Model) answerWorkflowTrigger(id string) tea.Cmd {
	st := m.wfxS()
	if m.confirmKind == confirmKindWorkflowTriggerList {
		if id == "next" {
			m.wfxTrigShowCard()
		}
		return nil
	}
	if len(st.trigQueue) == 0 {
		return nil
	}
	ev := st.trigQueue[0]
	st.trigQueue = st.trigQueue[1:]
	var cmd tea.Cmd
	if id == "start" {
		cmd = m.wfxTrigStart(ev, true)
	} else {
		m.wfxTrigSetOutcome(ev.Key, "skipped")
	}
	if !m.confirmOpen {
		m.wfxTrigShowCard()
	}
	return cmd
}

// wfxTrigSetOutcome changes the outcome of the newest recent row of key.
func (m *Model) wfxTrigSetOutcome(key, outcome string) {
	r := m.wfxS().trigRecent
	for i := len(r) - 1; i >= 0; i-- {
		if r[i].ev.Key == key {
			r[i].outcome = outcome
			return
		}
	}
	m.wfxTrigRecord(trigger.Event{Key: key}, outcome)
}

// wfxTrigStart starts the run of ev; byHand: the user said Start on the
// card (the run page opens).
func (m *Model) wfxTrigStart(ev trigger.Event, byHand bool) tea.Cmd {
	set := func(outcome string) {
		if byHand {
			m.wfxTrigSetOutcome(ev.Key, outcome)
		} else {
			m.wfxTrigRecord(ev, outcome)
		}
	}
	e, t, why := m.wfxTrigCheck(ev)
	if why == "" && m.wfxRunning(e.Name) != nil {
		why = string(schedule.SkippedRunning)
	}
	if why != "" {
		set("ignored: " + why)
		if byHand {
			return m.aiInfo(ev.Workflow + ": " + why)
		}
		return nil
	}
	opts := wfxRunOpts{vars: ev.Vars()}
	if t.Worktree {
		wt, err := git.CreateAgentWorktreeFrom(m.wfxS().ws, git.AgentWorktreeID("wf-trigger-"+e.Name, ev.Key+"-"+wire.NewRunID()), t.Base)
		if err != nil {
			set("failed: " + agentCtlShort(err.Error()))
			if byHand {
				return m.aiInfo(e.Name + ": the worktree: " + agentCtlShort(err.Error()))
			}
			return nil
		}
		opts.dir = wt.Dir()
		opts.vars["trigger_worktree"] = wt.Dir()
		opts.vars["trigger_worktree_branch"] = wt.Branch
	}
	_, h, cmd, err := m.wfxStartRunWith(e, false, byHand, nil, opts)
	if err != nil {
		set("failed: " + agentCtlShort(err.Error()))
		if byHand {
			return m.aiInfo(e.Name + ": " + agentCtlShort(err.Error()))
		}
		return nil
	}
	if l := m.wfxS().live[h.Snapshot().ID]; l != nil && !byHand {
		l.scheduled = true // unattended: it notifies like a scheduled run
	}
	set("started")
	return cmd
}

// wfxTrigList is the palette's list of recent trigger events: a card
// with the waiting ones first.
func (m *Model) wfxTrigList() tea.Cmd {
	buttons := []confirm.Button{{ID: "close", Title: "Close"}}
	if n := len(m.wfxS().trigQueue); n > 0 {
		buttons = append(buttons, confirm.Button{ID: "next", Title: fmt.Sprintf("Answer %d waiting", n)})
	}
	m.confirm = confirm.New("Workflow triggers", m.wfxTrigText(), buttons)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindWorkflowTriggerList
	return nil
}

// wfxTrigText is the text of the trigger list (its card's and the AI's
// workflow_control op triggers).
func (m *Model) wfxTrigText() string {
	st := m.wfxS()
	var b strings.Builder
	jobs, hooks := m.wfxTrigWanted()
	fmt.Fprintf(&b, "%d github trigger(s) polled every %s · %d webhook(s)", len(jobs), wfxTrigPoll(), len(hooks))
	if len(hooks) > 0 {
		if p := wfxWebhookPort(); p > 0 {
			fmt.Fprintf(&b, " on 127.0.0.1:%d/hooks/<name>", p)
		} else {
			b.WriteString(" (off: set workflows_webhook_port)")
		}
	}
	if st.notOwner {
		b.WriteString("\nAnother termocode owns this workspace's workflows; its triggers run there.")
	}
	b.WriteString("\n")
	if len(st.trigRecent) == 0 {
		b.WriteString("\nNo trigger events yet.")
	}
	for i := len(st.trigRecent) - 1; i >= 0 && i >= len(st.trigRecent)-10; i-- {
		r := st.trigRecent[i]
		fmt.Fprintf(&b, "\n%s  %s · %s · %s", r.at.Format("15:04"), r.ev.Workflow, trigger.Clean(r.ev.Summary()), r.outcome)
	}
	return b.String()
}

// wfxTrigWords is the TRIGGERS rows of a workflow page.
func wfxTrigWords(t workflow.Trigger) string {
	if t.Webhook != "" {
		s := "webhook " + t.Webhook + " (" + t.From + ")"
		if p := wfxWebhookPort(); p > 0 {
			s += " · POST 127.0.0.1:" + strconv.Itoa(p) + trigger.HookPrefix + t.Webhook
		}
		return s
	}
	s := "github " + t.GitHub
	switch {
	case len(t.Labels) > 0:
		s += " [" + strings.Join(t.Labels, ", ") + "]"
	case t.GitHub == "mention":
		s += " " + t.Text
	}
	if t.Branch != "" {
		s += " on " + t.Branch
	}
	if t.Repo != "" {
		s += " · " + t.Repo
	}
	if !t.Ask {
		s += " · starts without asking"
	}
	if t.Worktree {
		s += " · new worktree"
	}
	return s
}
