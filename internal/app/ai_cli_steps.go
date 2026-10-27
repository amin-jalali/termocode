package app

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/clock"
)

// Subscription CLI steps (Group CS). A profile step with "cli" runs
// Claude Code or Codex with the user's own login (internal/clistep)
// instead of a model slot: no API key, no per-token price. The step's
// prompt is the pipeline input (task, earlier answers, verify report)
// plus, for every step after the first, the work so far as a git diff
// (CS-2). A usage limit pauses the run at that step (CS-4): the run
// resumes by itself at the reset time when the CLI says one, or on
// Resume.

// aiCLIRun runs one CLI step (a var for tests: they never start a real
// CLI).
var aiCLIRun = clistep.Run

// aiCLIDiff is the hand-off's git diff (a var for tests).
var aiCLIDiff = clistep.GitDiff

// aiCLIPrompt is the stdin text of a CLI step: what the step is, its
// input and, for a hand-off, the git diff of the work so far.
func aiCLIPrompt(ctx context.Context, root, sys, input string, handoff bool) string {
	var b strings.Builder
	if s := strings.TrimSpace(sys); s != "" {
		b.WriteString(s)
		b.WriteString("\n\n")
	}
	b.WriteString(input)
	if handoff {
		if d := aiCLIDiff(ctx, root, clistep.DefaultDiffBytes); d != "" {
			b.WriteString("\n\n### Changes so far (git diff)\n")
			b.WriteString(d)
		}
	}
	return b.String()
}

// aiLimitResumeMargin is how long after the reset time a run that a
// usage limit paused goes on (the CLI's clock may be a little late).
const aiLimitResumeMargin = time.Minute

// aiLimitResumeMsg: the reset time of the paused run came.
type aiLimitResumeMsg struct {
	gen uint64
	run string
	at  int64 // the pause's ResumeAtMs: a tick of an earlier pause does nothing
}

// aiLimitResumeDelay is how long to wait from now for a limit that
// resets at reset.
func aiLimitResumeDelay(reset, now time.Time) time.Duration {
	return max(reset.Sub(now), 0) + aiLimitResumeMargin
}

// aiLimitPaused records a run that a CLI's usage limit paused and
// returns the tick that resumes it at the reset time (nil: no time).
func (m *Model) aiLimitPaused(rm *ai.StoredMessage, ev agentrun.Event) tea.Cmd {
	s := m.aiS()
	rm.Run.Limit = ai.Redact(agentdag.Clean(ev.Limit))
	rm.Run.ResumeAtMs = 0
	if ev.ResetAt.IsZero() {
		return nil
	}
	rm.Run.ResumeAtMs = ev.ResetAt.UnixMilli()
	gen, id, at := s.chatGen, rm.Run.ID, rm.Run.ResumeAtMs
	return tea.Tick(aiLimitResumeDelay(ev.ResetAt, clock.Now()), func(time.Time) tea.Msg {
		return aiLimitResumeMsg{gen: gen, run: id, at: at}
	})
}

// onAILimitResume goes on with the run a usage limit paused, when it is
// still that run and still paused.
func (m *Model) onAILimitResume(msg aiLimitResumeMsg) tea.Cmd {
	s := m.aiS()
	rm := s.aiRunMessage()
	if msg.gen != s.chatGen || s.run == nil || rm == nil || rm.Run.ID != msg.run ||
		rm.Run.State != ai.RunStatePaused || rm.Run.Limit == "" || rm.Run.ResumeAtMs != msg.at {
		return nil
	}
	rm.Run.State = ai.RunStateRunning
	s.runGen++
	s.run.Continue()
	return nil
}

// aiStepCost is a step card's cost: dollars, "subscription" for a CLI
// step (CS-3), "" when not known.
func aiStepCost(st ai.StoredStep) string {
	switch {
	case st.Cost != nil:
		return aiDollars(*st.Cost)
	case st.CLI != "":
		return "subscription"
	}
	return ""
}

// aiLimitNote is a step card's note for a usage limit.
func aiLimitNote(ev agentrun.Event) string {
	n := "⏸ usage limit"
	if !ev.ResetAt.IsZero() {
		n += " · resumes at " + ev.ResetAt.In(clock.Now().Location()).Format("15:04")
	}
	if msg := strings.TrimSpace(ai.Redact(agentdag.Clean(ev.Err))); msg != "" {
		n += " · " + msg
	}
	return n
}
