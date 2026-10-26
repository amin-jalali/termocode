package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// The Runs shots (dev action "runsdemo:<variant>"): sample runs in the
// structures Runs reads (the open chat with a live run, stored chats in
// memory, the session usage log), so the golden screens show the real
// view and page with no model and no file written.
//
//	list    the view and the overview: a live run, a run stopped when
//	        termocode closed (Resume), today's done and failed runs, older
//	detail  the live run's page
//	stall   the live run is stalled (no output for 1 m 32 s): Stop
//	empty   no run at all

func runsDemoCost(v float64) *float64 { return &v }

func runsDemoUse(model string, in, out int) *ai.Usage {
	return &ai.Usage{Model: model, Input: in, Output: out, Requests: 1, PeakContext: in}
}

// runsDemo sets the sample runs up and shows Runs.
func (m *Model) runsDemo(arg string) tea.Cmd {
	aiDemoMemoryOff()
	s := m.aiS()
	now := clock.Now()
	at := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }
	day := 2.0
	s.settings.DayBudgetUSD = &day
	st := m.runsS()
	st.fake = true
	st.chats = nil
	st.sel, st.detail, st.filter, st.profile = "", "", "", ""
	if arg == "empty" {
		s.conv = ai.NewConversation()
		s.profile = ai.ProfileReview
		cmd := m.revealRuns(true)
		return tea.Batch(cmd, m.runsShowPage("", false))
	}
	const sonnet, haiku = "claude-sonnet-4-5", "gpt-5-mini"
	// The open chat: a review run, its Fixer step working now.
	live := ai.Conversation{ID: "demo-live", Title: "fix the toast flicker", UpdatedAtMs: at(41 * time.Second)}
	live.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "fix the toast flicker"},
		{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "runlive", Profile: ai.ProfileReview, State: ai.RunStateRunning,
			StartedAtMs: at(41 * time.Second)},
			Steps: []ai.StoredStep{
				{ID: "reviewer", Title: "Reviewer", Slot: ai.SlotMain, Model: sonnet, Category: agentdag.CategoryDeep,
					State: string(agentdag.StateCompleted), StartedAtMs: at(41 * time.Second), ElapsedMs: 12000,
					Usage: runsDemoUse(sonnet, 3600, 500), Cost: runsDemoCost(0.009),
					Text:  "Found 2 problems: toast.go:41 re-renders on every tick; toast.go:88 ignores the width.",
					Tools: []string{"⚙ Read toast.go → 120 lines"}},
				{ID: "fixer", Title: "Fixer", Slot: ai.SlotQuick, Model: haiku, Category: agentdag.CategoryQuick,
					State: string(agentdag.StateRunning), StartedAtMs: at(29 * time.Second), Deps: []string{"reviewer"},
					Tools: []string{"⚙ Read toast.go → 120 lines", "⚙ Edit toast.go", "⚙ Edit toast_test.go"}},
			}},
	}
	s.conv = live
	s.runMsg = 1
	s.streaming = true
	s.runLaunched = now.Add(-41 * time.Second)
	s.agents.runOf = "runlive"
	s.agents.start = map[string]time.Time{"fixer": now.Add(-29 * time.Second)}
	s.agents.turns = map[string]int{"fixer": 2}
	act := &aiActivity{phase: aiPhaseTool, tool: "edit internal/toast/toast.go", since: now.Add(-2 * time.Second), lastAt: now.Add(-2 * time.Second)}
	if arg == "stall" { // the Fixer wrote a little, then nothing for 1 m 32 s
		act = &aiActivity{phase: aiPhaseWriting, since: now.Add(-100 * time.Second), lastAt: now.Add(-92 * time.Second)}
		s.runLaunched = now.Add(-150 * time.Second)
		s.agents.start["fixer"] = now.Add(-110 * time.Second)
		live.Messages[1].Run.StartedAtMs = at(150 * time.Second)
		live.Messages[1].Steps[0].StartedAtMs = at(150 * time.Second)
		live.Messages[1].Steps[1].StartedAtMs = at(110 * time.Second)
		live.Messages[1].Steps[0].ElapsedMs = 40000
	}
	s.agents.act = map[string]*aiActivity{"fixer": act}
	// Stored chats: a plan run stopped when termocode closed, a done
	// review, a work-until-done run whose verify still fails, older runs.
	notes := ai.Conversation{ID: "demo-notes", Title: "write release notes for 0.9", UpdatedAtMs: at(40 * time.Minute)}
	notes.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "write release notes for 0.9"},
		{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "runnotes", Profile: ai.ProfilePlan, State: ai.RunStateInterrupted,
			StartedAtMs: at(41 * time.Minute), ElapsedMs: 71000},
			Steps: []ai.StoredStep{
				{ID: "planner", Title: "Planner", Slot: ai.SlotMain, Model: sonnet, State: string(agentdag.StateCompleted),
					StartedAtMs: at(41 * time.Minute), ElapsedMs: 15000, Usage: runsDemoUse(sonnet, 4000, 400), Cost: runsDemoCost(0.010),
					Text: "Four steps: collect the changes, draft, polish, verify."},
				{ID: "collect", Title: "Collect changes", Slot: ai.SlotQuick, Model: haiku, State: string(agentdag.StateCompleted), Planned: true,
					Prompt: "List the changes since 0.8.", Deps: []string{"planner"}, StartedAtMs: at(41*time.Minute - 15*time.Second),
					ElapsedMs: 44000, Usage: runsDemoUse(haiku, 7500, 600), Cost: runsDemoCost(0.009), Text: "23 changes, 4 of them user facing."},
				{ID: "draft", Title: "Draft notes", Slot: ai.SlotQuick, Model: haiku, State: string(agentdag.StateCancelled), Planned: true,
					Prompt: "Draft the notes.", Deps: []string{"collect"}, StartedAtMs: at(40 * time.Minute)},
				{ID: "polish", Title: "Polish", Slot: ai.SlotQuick, Model: haiku, State: string(agentdag.StateCancelled), Planned: true,
					Prompt: "Polish the notes.", Deps: []string{"draft"}},
				{ID: "verify", Title: "Verify", Slot: ai.SlotMain, Model: sonnet, State: string(agentdag.StateCancelled), Planned: true,
					Prompt: "Check the notes.", Deps: []string{"polish"}},
			}},
	}
	why := ai.Conversation{ID: "demo-why", Title: "why does the toast flicker", UpdatedAtMs: at(68 * time.Minute)}
	why.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "why does the toast flicker"},
		{Role: ai.StoredAssistant, Text: "The toast re-renders on every tick.", Run: &ai.StoredRun{ID: "runwhy", Profile: ai.ProfileReview,
			State: ai.RunStateDone, StartedAtMs: at(68 * time.Minute), ElapsedMs: 58000},
			Steps: []ai.StoredStep{
				{ID: "reviewer", Title: "Reviewer", Slot: ai.SlotMain, Model: sonnet, State: string(agentdag.StateCompleted),
					StartedAtMs: at(68 * time.Minute), ElapsedMs: 20000, Usage: runsDemoUse(sonnet, 5400, 600), Cost: runsDemoCost(0.014)},
				{ID: "fixer", Title: "Fixer", Slot: ai.SlotQuick, Model: haiku, State: string(agentdag.StateCompleted),
					StartedAtMs: at(68*time.Minute - 20*time.Second), ElapsedMs: 38000, Usage: runsDemoUse(haiku, 8400, 700), Cost: runsDemoCost(0.017)},
			}},
	}
	quit := ai.Conversation{ID: "demo-quit", Title: "fix the double quit confirm", UpdatedAtMs: at(132 * time.Minute)}
	quit.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "/done fix the double quit confirm"},
		{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "runquit", Profile: ai.ProfileUntilDone, State: ai.RunStateDone,
			Verdict: "fail", Rounds: 3, MaxRounds: 3, StartedAtMs: at(132 * time.Minute), ElapsedMs: 130000},
			Steps: []ai.StoredStep{
				{ID: "planner", Title: "Planner", Slot: ai.SlotMain, Model: sonnet, State: string(agentdag.StateCompleted),
					StartedAtMs: at(132 * time.Minute), ElapsedMs: 14000, Usage: runsDemoUse(sonnet, 4000, 300), Cost: runsDemoCost(0.012)},
				{ID: "verify", Title: "Verify", Slot: ai.SlotMain, Model: sonnet, State: string(agentdag.StateCompleted), Round: 3,
					StartedAtMs: at(131 * time.Minute), ElapsedMs: 76000, Usage: runsDemoUse(sonnet, 21000, 900), Cost: runsDemoCost(0.038),
					Text: "VERDICT: FAIL\nquit_test.go:88 the card shows twice"},
				{ID: "fix", Title: "Fix", Slot: ai.SlotQuick, Model: haiku, State: string(agentdag.StateCompleted), Round: 3,
					StartedAtMs: at(130 * time.Minute), ElapsedMs: 40000, Usage: runsDemoUse(haiku, 13000, 1000), Cost: runsDemoCost(0.014)},
			}},
	}
	old := func(id, task string, ago time.Duration, cost float64) ai.Conversation {
		c := ai.Conversation{ID: id, Title: task, UpdatedAtMs: at(ago)}
		c.Messages = []ai.StoredMessage{
			{Role: ai.StoredUser, Text: task},
			{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "run" + id, Profile: ai.ProfileReview, State: ai.RunStateDone,
				StartedAtMs: at(ago), ElapsedMs: 50000},
				Steps: []ai.StoredStep{
					{ID: "reviewer", Title: "Reviewer", Model: sonnet, State: string(agentdag.StateCompleted), ElapsedMs: 20000,
						Usage: runsDemoUse(sonnet, 5000, 500), Cost: runsDemoCost(cost / 2)},
					{ID: "fixer", Title: "Fixer", Model: haiku, State: string(agentdag.StateCompleted), ElapsedMs: 30000,
						Usage: runsDemoUse(haiku, 7000, 600), Cost: runsDemoCost(cost / 2)},
				}},
		}
		return c
	}
	st.chats = []ai.Conversation{notes, why, quit,
		old("demo-o1", "tidy the status bar code", 26*time.Hour, 0.04),
		old("demo-o2", "add tests for the toast queue", 50*time.Hour, 0.06)}
	// This session: a plain chat turn and the done review's steps.
	s.usageLog = nil
	rec := func(run, agent, model string, cost float64) {
		c := cost
		s.recordUsage(aiUsageEntry{Run: run, Agent: agent, Model: model, U: *runsDemoUse(model, 1000, 100), Cost: &c})
	}
	rec("", "Assistant", sonnet, 0.012)
	rec("runwhy", "Reviewer", sonnet, 0.014)
	rec("runwhy", "Fixer", haiku, 0.017)
	s.runGen++
	cmd := m.revealRuns(true)
	switch arg {
	case "detail", "stall":
		return tea.Batch(cmd, m.runsShowPage("demo-live/runlive", true))
	}
	return tea.Batch(cmd, m.runsShowPage("", false))
}
