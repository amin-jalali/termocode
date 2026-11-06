package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/raceview"
)

// raceDemo is the race card or the compare page with a made-up race
// (DevActionMsg "racedemo:<card|running|done|merged>", for the goldens):
// no git, no tab, no CLI runs and nothing is saved.
func (m *Model) raceDemo(variant string) {
	s := m.raceS()
	if variant == "card" {
		s.opts = []raceview.Option{
			{ID: "claude", Title: "Claude Code", Hint: "claude · signed in"},
			{ID: "codex", Title: "Codex", Hint: "codex"},
			{ID: "gemini", Title: "Gemini CLI", Hint: "gemini"},
			{ID: "opencode", Title: "OpenCode", Hint: "opencode"},
			{ID: "amp", Title: "Amp", Disabled: true, Hint: "its screen cannot be read, so the task cannot be typed safely"},
		}
		s.form = raceview.NewForm(s.opts, "make the toast queue thread-safe", "go test ./internal/toast/...")
		for _, i := range []int{0, 1} {
			s.form.SetFocus(raceview.FocusLane(i))
			s.form, _ = s.form.Update(raceDemoSpace)
		}
		s.form.SetFocus(raceview.FocusStart)
		s.form.SetSize(m.w, m.h)
		s.formOn = true
		return
	}
	now := clock.Now().UnixMilli()
	cost := 0.0
	r := agentrace.Race{ID: "race-398", Number: 398, Name: "race/make-the-toast-queue-thread-safe", Prompt: "make the toast queue thread-safe",
		Verify: "go test ./internal/toast/...", StartedMs: now - 4*60*1000, Lanes: []agentrace.Lane{
			{ID: "race-398-1-claude", Agent: "claude", Title: "Claude Code", State: agentrace.Done, Stats: true, Branch: "termocode/agent-race-398-1-claude",
				Files: []agentrace.FileStat{{Status: "M", Path: "internal/toast/queue.go", Add: 31, Del: 7}, {Status: "M", Path: "internal/toast/queue_test.go", Add: 10, Del: 2}},
				Note:  "used a mutex"},
			{ID: "race-398-2-codex", Agent: "codex", Title: "Codex", State: agentrace.Working},
			{ID: "race-398-3-gemini", Agent: "gemini", Title: "Gemini CLI", State: agentrace.Waiting, Note: "asks to trust the folder"},
		}}
	if variant != "running" {
		r.Lanes[0].Verify = agentrace.VerifyResult(0)
		r.Lanes[1] = agentrace.Lane{ID: "race-398-2-codex", Agent: "codex", Title: "Codex", State: agentrace.Done, Stats: true, Branch: "termocode/agent-race-398-2-codex",
			Files: []agentrace.FileStat{{Status: "M", Path: "internal/toast/queue.go", Add: 61, Del: 28}, {Status: "A", Path: "internal/toast/chan.go", Add: 27},
				{Status: "M", Path: "internal/toast/queue_test.go", Add: 0, Del: 2}},
			Verify: agentrace.VerifyResult(1), Note: "channel rewrite"}
		r.Lanes[2] = agentrace.Lane{ID: "race-398-3-gemini", Agent: "gemini", Title: "Gemini CLI", State: agentrace.Stopped, Stats: true,
			Files: []agentrace.FileStat{{Status: "M", Path: "internal/toast/queue.go", Add: 12}}, Verify: agentrace.VerifyResult(0), Cost: &cost}
	}
	if variant == "merged" {
		r.Lanes[0].State, r.Winner = agentrace.Merged, r.Lanes[0].ID
		r.Lanes[1].State, r.Lanes[1].Note = agentrace.Discarded, "kept on termocode/agent-race-398-2-codex"
		r.Summary = []string{"Claude Code guards the queue with a mutex and adds two race tests; its tests pass.",
			"Codex rewrites the queue around a channel (+88 -30) and one queue test fails.",
			"Gemini CLI only locks Push; Claude Code's lane looks the most complete."}
		r.SummaryCost = "gpt-5-mini · $0.003"
	}
	s.races = []agentrace.Race{r}
	s.dir = ""
	m.openRacePage(r.ID)
	if variant == "merged" {
		s.page.SetMessage("Merged 2 files from Claude Code")
	}
}

var raceDemoSpace = tea.KeyMsg{Type: tea.KeySpace}
