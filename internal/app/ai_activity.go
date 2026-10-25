package app

import (
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// The activity line (B1): one quiet row under the last chat message says
// what the model is doing — waiting for the first token, writing, running
// a tool, retrying — and warns when nothing arrives for too long. A 1 s
// tick runs only while a turn runs, so an idle chat costs no CPU.

// aiPhase is what the running turn does now.
type aiPhase uint8

const (
	aiPhaseNone    aiPhase = iota
	aiPhaseWaiting         // request sent, no token yet
	aiPhaseWriting         // text is streaming
	aiPhaseTool            // a tool runs (or waits for the user)
	aiPhaseRetry           // the server was busy; trying again
)

// Stall thresholds: no token for aiStallToken while writing, or no first
// token for aiStallFirst. A tool may run for minutes and never warns.
const (
	aiStallToken = 30 * time.Second
	aiStallFirst = 90 * time.Second
)

// aiActivity is the running turn's phase and its times.
type aiActivity struct {
	phase          aiPhase
	tool           string    // display name of the running tool
	since          time.Time // the phase began
	lastAt         time.Time // last token (or request sent)
	retry, retries int
	ticking        bool // a 1 s tick is in flight
}

// aiActivityTickMsg redraws the activity line once a second.
type aiActivityTickMsg struct{}

// begin starts a turn: waiting for the model.
func (a *aiActivity) begin(now time.Time) {
	*a = aiActivity{phase: aiPhaseWaiting, since: now, lastAt: now, ticking: a.ticking}
}

// onEvent moves the phase with an agent event.
func (a *aiActivity) onEvent(ev ai.Event, now time.Time) {
	switch ev.Kind {
	case ai.EventTurnStart:
		a.lastAt = now
		if a.phase != aiPhaseRetry { // "retry n of m" stays until an answer
			a.phase, a.since = aiPhaseWaiting, now
		}
	case ai.EventText:
		if a.phase != aiPhaseWriting {
			a.phase, a.since = aiPhaseWriting, now
		}
		a.lastAt = now
	case ai.EventToolStart:
		a.phase, a.since, a.tool = aiPhaseTool, now, ev.Label
		if a.tool == "" {
			a.tool = ev.Tool
		}
	case ai.EventToolResult:
		a.phase, a.since, a.lastAt = aiPhaseWaiting, now, now
	case ai.EventRetry:
		a.phase, a.since, a.lastAt = aiPhaseRetry, now, now
		a.retry, a.retries = ev.Attempt, ev.Attempts
	case ai.EventDone, ai.EventError:
		a.phase = aiPhaseNone
	}
}

// aiActivityView is what the line shows. It is comparable and has no
// pointers, so the transcript cache keys on it without allocating.
type aiActivityView struct {
	phase          aiPhase
	tool           string
	secs           int // phase seconds (waiting / tool) or silent seconds (stalled)
	retry, retries int
	stalled        bool
}

// aiActivityView is the line's state at now (zero without a running turn).
func (m *Model) aiActivityView(now time.Time) aiActivityView {
	if m.ai == nil {
		return aiActivityView{}
	}
	return m.ai.activityView(now)
}

func (s *aiState) activityView(now time.Time) aiActivityView {
	if !s.streaming {
		return aiActivityView{}
	}
	return s.act.view(now)
}

// view is the activity's state at now. The AGENTS cards keep one
// aiActivity per node and use the same thresholds through it.
func (a *aiActivity) view(now time.Time) aiActivityView {
	if a.phase == aiPhaseNone {
		return aiActivityView{}
	}
	v := aiActivityView{phase: a.phase}
	silent := now.Sub(a.lastAt)
	switch a.phase {
	case aiPhaseWaiting, aiPhaseRetry:
		v.stalled = silent >= aiStallFirst
	case aiPhaseWriting:
		v.stalled = silent >= aiStallToken
	}
	switch {
	case v.stalled:
		v.secs = int(silent / time.Second)
	case a.phase == aiPhaseWaiting || a.phase == aiPhaseTool:
		v.secs = int(now.Sub(a.since) / time.Second)
	}
	if a.phase == aiPhaseTool {
		v.tool = a.tool
	}
	if a.phase == aiPhaseRetry {
		v.retry, v.retries = a.retry, a.retries
	}
	return v
}

// aiSecs is "4 s" or "1 m 5 s".
func aiSecs(n int) string {
	if n < 0 {
		n = 0
	}
	if n < 60 {
		return strconv.Itoa(n) + " s"
	}
	return strconv.Itoa(n/60) + " m " + strconv.Itoa(n%60) + " s"
}

// lines are the activity rows, wrapped to inner columns (none without
// a running turn).
func (v aiActivityView) lines(ind string, inner int) []aiLine {
	var text string
	style := theme.Fg(theme.TextDim).Italic(true)
	switch {
	case v.phase == aiPhaseNone:
		return nil
	case v.stalled:
		text = theme.GlyphAIStall.String() + " no answer for " + aiSecs(v.secs) + " — Esc to stop"
		style = theme.Fg(theme.DiagWarning)
	case v.phase == aiPhaseWaiting:
		text = theme.GlyphAIWaiting.String() + " waiting for the model · " + aiSecs(v.secs)
	case v.phase == aiPhaseWriting:
		text = theme.GlyphAIWriting.String() + " writing"
	case v.phase == aiPhaseTool:
		text = theme.GlyphAITool.String() + " " + aiCleanText(v.tool) + " · " + aiSecs(v.secs)
	case v.phase == aiPhaseRetry:
		text = theme.GlyphAIRetry.String() + " retry " + strconv.Itoa(v.retry) + " of " + strconv.Itoa(v.retries)
	}
	var out []aiLine
	for _, l := range wrapPlain(text, inner) {
		out = append(out, aiLine{text: ind + l, style: style})
	}
	return out
}

// aiActivityTick arms the 1 s redraw tick while a turn runs and the
// chat slot (the chat or the AGENTS view) is shown, at most one at a
// time. Update calls it after every message (so showing the slot arms
// it again); the tick's own message re-arms it, so the chain ends by
// itself one tick after the turn or after the slot hides.
func (m *Model) aiActivityTick() tea.Cmd {
	s := m.ai
	if s == nil || !s.streaming || s.act.ticking || !m.actionsPanelVisible() {
		return nil
	}
	s.act.ticking = true
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return aiActivityTickMsg{} })
}

// onAIActivityTick: the frame redraws; re-arm while the turn runs.
func (m *Model) onAIActivityTick() tea.Cmd {
	if m.ai != nil {
		m.ai.act.ticking = false
	}
	return m.aiActivityTick()
}
