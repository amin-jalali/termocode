package app

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// ── Runs: the data ───────────────────────────────────────────────────────
//
// The Runs page (runs_*.go) is the monitoring home of AI work: which
// profile runs on which task, where it is now and what it costs. It owns
// no data of its own. Every run is an assistant message with a Run
// (ai.StoredRun and its ai.StoredStep cards) in a chat: the open chat
// (its run may be live) or a stored one (read from the chat store when
// the view opens). The live parts come from the same places the AGENTS
// view and the chat cards use: the step start times and each running
// step's activity with its stall warning (agents_panel.go, OM-5), the
// resume rule (aiRetryStepIn, OM-6) and the loop round (OM-3).

// runsKind is where a run is: its glyph, its colour and its verbs.
type runsKind int

const (
	runsRunning   runsKind = iota // live: a step works
	runsStalled                   // live: a running step sends nothing for too long (OM-5)
	runsPausing                   // live: Pause was asked; it pauses before the next step
	runsPaused                    // live: paused (Pause or the budget); Resume goes on
	runsResumable                 // stopped with steps left: Resume runs them (OM-6)
	runsDone
	runsFailed
	runsCancelled
)

// runsLive reports kinds whose run goes on (or waits) right now.
func (k runsKind) live() bool { return k <= runsPaused }

// runsNeedsYou reports kinds shown in amber.
func (k runsKind) needsYou() bool {
	return k == runsStalled || k == runsPausing || k == runsPaused || k == runsResumable
}

// runsGroup is a run's place in the list: by time.
type runsGroup int

const (
	runsGroupNow runsGroup = iota
	runsGroupToday
	runsGroupOlder
)

// runsKeep is how far back the list goes.
const runsKeep = 7 * 24 * time.Hour

// runsItem is one run as the Runs view shows it.
type runsItem struct {
	key       string // chat id + "/" + run id
	chat      string // the chat it is in
	chatTitle string
	idx       int  // its assistant message in that chat
	open      bool // in the open chat
	run       ai.StoredRun
	steps     []ai.StoredStep
	task      string // the user's message, one line
	started   time.Time
	elapsed   time.Duration
	tokens    int
	anyTok    bool
	cost      float64 // the known step costs
	costOK    bool    // every step that used tokens has a price (or is a subscription CLI)
	sub       bool    // a step ran on a subscription CLI (CS-3): its cost is the subscription
	kind      runsKind
	group     runsGroup
	resume    int // first step a resume runs (0: none)
	left      int // steps a resume runs
	stall     int // seconds without output (runsStalled)
	cur       int // the step where the run is now (-1: none)
	why       string
}

// runsStep is one step's live numbers.
type runsStep struct {
	elapsed time.Duration
	live    bool   // it runs now
	stalled int    // seconds without output (0: not stalled)
	now     string // what it does now: "read_file · 3 tool calls · last output 2 s ago"
}

// aiRunStarted is when run r started: its StartedAtMs, else the time in
// its id (aiNewRunID: base-36 Unix ms, 8 digits, then a counter), else
// fallback (the chat's last save).
func aiRunStarted(r ai.StoredRun, fallback time.Time) time.Time {
	if r.StartedAtMs > 0 {
		return time.UnixMilli(r.StartedAtMs)
	}
	if len(r.ID) > 8 {
		if ms, err := strconv.ParseInt(r.ID[:8], 36, 64); err == nil {
			t := time.UnixMilli(ms)
			if t.Year() >= 2020 && t.Year() < 2100 {
				return t
			}
		}
	}
	return fallback
}

// runsTask is the task of the run message i: the user message before it,
// cleaned to one line (secrets redacted).
func runsTask(msgs []ai.StoredMessage, i int) string {
	if i < 1 || i > len(msgs) || msgs[i-1].Role != ai.StoredUser {
		return "(no message)"
	}
	t := aiOneLine(aiCleanText(ai.Redact(msgs[i-1].Text)))
	t = strings.TrimSpace(strings.TrimPrefix(t, "/done"))
	if t == "" {
		return "(no message)"
	}
	return t
}

// runsCollect builds every run of the last runsKeep: the open chat's
// (live data included) and the stored chats', newest first.
func (m *Model) runsCollect(now time.Time) []runsItem {
	s := m.ai
	if s == nil {
		return nil
	}
	var out []runsItem
	add := func(c *ai.Conversation, open bool) {
		for i := range c.Messages {
			msg := &c.Messages[i]
			if msg.Run == nil || msg.Role != ai.StoredAssistant {
				continue
			}
			it := s.runsItemOf(c, i, open, now)
			if now.Sub(it.started) > runsKeep {
				continue
			}
			out = append(out, it)
		}
	}
	if len(s.conv.Messages) > 0 {
		add(&s.conv, true)
	}
	chats := m.runsChats()
	for k := range chats {
		c := &chats[k]
		if c.ID == s.conv.ID {
			continue
		}
		add(c, false)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.group != b.group {
			return a.group < b.group
		}
		return a.started.After(b.started)
	})
	return out
}

// runsChats are the stored chats (read when the view opened; the demo
// may set them).
func (m *Model) runsChats() []ai.Conversation {
	if m.runs == nil {
		return nil
	}
	if !m.runs.fake && m.ai != nil && m.runs.loadedFor != m.ai.conv.ID {
		m.runsLoadChats() // another chat is open now
	}
	return m.runs.chats
}

// runsItemOf is run message i of chat c.
func (s *aiState) runsItemOf(c *ai.Conversation, i int, open bool, now time.Time) runsItem {
	msg := c.Messages[i]
	r := *msg.Run
	it := runsItem{key: c.ID + "/" + r.ID, chat: c.ID, chatTitle: c.Title, idx: i, open: open, run: r,
		steps: msg.Steps, task: runsTask(c.Messages, i), cur: -1, costOK: true}
	it.started = aiRunStarted(r, time.UnixMilli(c.UpdatedAtMs))
	live := open && s.streaming && s.runMsg == i
	for _, st := range msg.Steps {
		if st.CLI != "" {
			it.sub = true
		}
		if st.Usage != nil {
			it.anyTok = true
			it.tokens += aiUsageTokens(*st.Usage)
			if st.Cost == nil && st.CLI == "" {
				it.costOK = false
			}
		}
		if st.Cost != nil {
			it.cost += *st.Cost
		}
	}
	it.elapsed = time.Duration(r.ElapsedMs) * time.Millisecond
	if live && !s.runLaunched.IsZero() {
		it.elapsed += now.Sub(s.runLaunched)
	}
	left, _, _ := aiResumeLeft(msg)
	it.left = left
	switch {
	case live && r.State == ai.RunStatePaused:
		it.kind = runsPaused
		switch {
		case r.Limit != "" && r.ResumeAtMs > 0: // a CLI's usage limit (CS-4)
			it.why = "usage limit · resumes at " + time.UnixMilli(r.ResumeAtMs).In(now.Location()).Format("15:04")
		case r.Limit != "":
			it.why = "usage limit reached · Resume when it resets"
		case r.Budget > 0:
			it.why = "budget " + aiBudgetText(r.Budget) + " reached"
		default:
			it.why = "paused by you"
		}
	case live && s.runPausing:
		it.kind, it.why = runsPausing, "pauses after this step"
	case live:
		it.kind = runsRunning
		for k, st := range msg.Steps {
			if secs, ok := s.agentsStalled(r.ID, agentdag.State(st.State), st.ID, now); ok {
				it.kind, it.stall, it.cur = runsStalled, secs, k
				it.why = "no output for " + aiSecs(secs)
				break
			}
		}
	case r.State == ai.RunStateDone:
		it.kind = runsDone
		if r.Verdict == "fail" {
			it.kind, it.why = runsFailed, "verify still fails"
		}
	default:
		if open && s.streaming {
			it.resume = 0 // the open chat is busy with another turn
		} else {
			it.resume = aiRetryStepIn(c.Messages, i, s.profiles)
		}
		switch {
		case it.resume > 0 && r.State != ai.RunStateFailed:
			it.kind = runsResumable
			it.why = runsStopWhy(r.State) + " at " + runsClock(it.started.Add(it.elapsed)) + " · " +
				strconv.Itoa(left) + " of " + strconv.Itoa(len(msg.Steps)) + " steps left"
		case r.State == ai.RunStateFailed:
			it.kind, it.why = runsFailed, runsFailWhy(msg.Steps)
		default:
			it.kind, it.why = runsCancelled, runsStopWhy(r.State)
		}
	}
	if it.cur < 0 {
		it.cur = runsCurrentStep(msg.Steps)
	}
	day := func(t time.Time) time.Time {
		y, mo, d := t.Date()
		return time.Date(y, mo, d, 0, 0, 0, 0, t.Location())
	}
	switch {
	case it.kind.live():
		it.group = runsGroupNow
	case !day(it.started.In(now.Location())).Before(day(now)):
		it.group = runsGroupToday
		if it.kind == runsResumable {
			it.group = runsGroupNow // stopped today with steps left: it needs you
		}
	default:
		it.group = runsGroupOlder
	}
	return it
}

// runsCurrentStep is the step where the run is: the first running one,
// else the first that is not done, else the last.
func runsCurrentStep(steps []ai.StoredStep) int {
	for k, st := range steps {
		if agentdag.State(st.State) == agentdag.StateRunning {
			return k
		}
	}
	for k, st := range steps {
		if !agentdag.State(st.State).Done() {
			return k
		}
	}
	return len(steps) - 1
}

func runsStopWhy(state string) string {
	switch state {
	case ai.RunStateInterrupted:
		return "termocode closed"
	case ai.RunStateCancelled:
		return "stopped"
	case ai.RunStateFailed:
		return "failed"
	}
	return state
}

// runsFailWhy is the first failed step's error, short.
func runsFailWhy(steps []ai.StoredStep) string {
	for _, st := range steps {
		if st.Error != "" {
			return aiOneLine(aiCleanText(st.Error))
		}
	}
	return "failed"
}

// runsStepLive is step k's live numbers at now.
func (s *aiState) runsStepLive(it runsItem, k int, now time.Time) runsStep {
	st := it.steps[k]
	out := runsStep{elapsed: time.Duration(st.ElapsedMs) * time.Millisecond}
	if !it.kind.live() || agentdag.State(st.State) != agentdag.StateRunning || s.agents.runOf != it.run.ID {
		return out
	}
	out.live = true
	if t, ok := s.agents.start[st.ID]; ok {
		out.elapsed += now.Sub(t)
	}
	v, ok := s.agentsNodeView(it.run.ID, st.ID, now)
	if !ok {
		return out
	}
	if v.stalled {
		out.stalled = max(v.secs, 1)
	}
	var parts []string
	switch v.phase {
	case aiPhaseTool:
		parts = append(parts, aiCleanText(v.tool))
	case aiPhaseWriting:
		parts = append(parts, "writing")
	case aiPhaseRetry:
		parts = append(parts, "retry "+strconv.Itoa(v.retry)+" of "+strconv.Itoa(v.retries))
	default:
		parts = append(parts, "waiting for the model")
	}
	if n := len(st.Tools); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" tool call"+plural(n))
	}
	if a := s.agents.act[st.ID]; a != nil && !a.lastAt.IsZero() {
		parts = append(parts, "last output "+aiSecs(int(now.Sub(a.lastAt)/time.Second))+" ago")
	}
	out.now = strings.Join(parts, " · ")
	return out
}

// runsSpend is what the AI cost: today (the runs that started today and
// this session's plain chat turns) by model, and this session.
type runsSpend struct {
	today       float64
	byModel     []runsModelSpend // most first
	session     float64
	sessionRuns int
	runsToday   int
}

type runsModelSpend struct {
	model string
	usd   float64
}

// runsSpendOf adds up items (the runs) and the session usage log.
func (s *aiState) runsSpendOf(items []runsItem) runsSpend {
	var sp runsSpend
	by := map[string]float64{}
	for _, it := range items {
		if it.group == runsGroupOlder {
			continue
		}
		sp.runsToday++
		for _, st := range it.steps {
			if st.Cost != nil {
				by[st.Model] += *st.Cost
				sp.today += *st.Cost
			}
		}
	}
	runs := map[string]bool{}
	for _, e := range s.usageLog {
		if e.Cost != nil {
			sp.session += *e.Cost
			if e.Run == "" { // a plain chat turn: no run card holds it
				by[e.Model] += *e.Cost
				sp.today += *e.Cost
			}
		}
		if e.Run != "" {
			runs[e.Run] = true
		}
	}
	sp.sessionRuns = len(runs)
	for k, v := range by {
		if v > 0 {
			sp.byModel = append(sp.byModel, runsModelSpend{k, v})
		}
	}
	sort.Slice(sp.byModel, func(i, j int) bool {
		if sp.byModel[i].usd != sp.byModel[j].usd {
			return sp.byModel[i].usd > sp.byModel[j].usd
		}
		return sp.byModel[i].model < sp.byModel[j].model
	})
	return sp
}

// runsDayBudget is the daily spend line (ai_day_budget_usd; 0: none).
func (s *aiState) runsDayBudget() float64 {
	if s == nil || s.settings.DayBudgetUSD == nil || *s.settings.DayBudgetUSD < 0 {
		return 0
	}
	return *s.settings.DayBudgetUSD
}

// runsRunBudget is run it's budget: the paused run's, else its profile's.
func (s *aiState) runsRunBudget(it runsItem) float64 {
	if it.run.Budget > 0 {
		return it.run.Budget
	}
	if p, ok := s.profiles.Get(it.run.Profile); ok {
		return p.BudgetUSD
	}
	return 0
}

// runsProfileTitle is the profile's title ("" when it is gone).
func (s *aiState) runsProfileTitle(name string) string {
	if p, ok := s.profiles.Get(name); ok {
		return aiCleanText(p.Title)
	}
	return ""
}

// runsNow is the time the view is drawn at.
func runsNow() time.Time { return clock.Now() }
