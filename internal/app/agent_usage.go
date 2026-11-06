package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/agentusage"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/statusbar"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Plan limits of the outside agents (AGX-12, owner decision D4) ───────
//
// internal/agentusage keeps one window per agent account: the share of
// the plan used, the reset, the last limit and today's tokens. It is fed
// only by a limit message on an agent tab's screen (agent_limit.go) and,
// with agent_transcripts = read, by the session file the chat view reads.
// No vendor endpoint is called. The status bar shows the pill
// "✦ Claude 62% ↻14:30" (hidden when nothing is known); a click opens
// AI: Usage, whose PLANS block lists every account.

const (
	agentUsagePlansPaletteID = "agent-usage-plans"
	agentPlanStatusID        = "status:plan"
)

// agentUsageBook is the model's book (made on first use).
func (m *Model) agentUsageBook() *agentusage.Book {
	if m.agentUsage == nil {
		m.agentUsage = agentusage.New()
	}
	return m.agentUsage
}

// agentUsageKey is the account of an agent tab.
func agentUsageKey(t *terminalTab) agentusage.Key {
	return agentusage.Key{CLI: t.Agent, Account: t.Account}
}

// agentUsageLimit records a limit seen on tab t (source 1).
func (m *Model) agentUsageLimit(t *terminalTab, msg string, reset, now time.Time) {
	m.agentUsageBook().Limit(agentUsageKey(t), msg, reset, now)
	m.aiS().usageGen++ // the usage view draws again
}

// agentUsageSession records the numbers of a session file read with
// agent_transcripts on (source 2).
func (m *Model) agentUsageSession(agentID, account string, s agentlog.Session) {
	if agentID == "" || s.ID == "" {
		return
	}
	at := s.End
	if at.IsZero() {
		at = clock.Now()
	}
	m.agentUsageBook().Transcript(agentusage.Key{CLI: agentID, Account: account}, s.ID,
		s.Usage.LimitPct, s.Usage.LimitReset, s.Usage.In+s.Usage.Out, at)
	m.aiS().usageGen++
}

// agentUsageWindows is every known window now.
func (m Model) agentUsageWindows() []agentusage.Window {
	if m.agentUsage == nil {
		return nil
	}
	return m.agentUsage.Windows(clock.Now())
}

// fillPlanStatus sets the plan pill of the status bar.
func (m Model) fillPlanStatus(st *statusbar.State) {
	ws := m.agentUsageWindows()
	if len(ws) == 0 {
		return
	}
	now := clock.Now()
	st.Plan, st.PlanShort = agentusage.Pill(ws, now)
	for _, w := range ws {
		if w.Limited(now) {
			st.PlanLimited = true
		}
	}
}

// onStatusPlanClick is a click on the plan pill: AI: Usage.
func (m *Model) onStatusPlanClick() tea.Cmd { return m.showAIUsage("") }

// agentAccountAutoContinue is "on" / "off" for the Plans row of an
// account: the setting, or a tab's own switch when one of its tabs has
// one.
func (m Model) agentAccountAutoContinue(k agentusage.Key) string {
	v := loadSettings().AgentAutoContinue
	on := v != nil && *v
	for i := range m.terminalTabs {
		t := &m.terminalTabs[i]
		if agentUsageKey(t) == k && t.AutoContinue != autoContinueDefault {
			on = t.AutoContinue == autoContinueOn
		}
	}
	if on {
		return "on"
	}
	return "off"
}

// usagePlansBlock draws the PLANS rows of AI: Usage.
func (m Model) usagePlansBlock(width int) []string {
	out := []string{agText(theme.TextSecondary, "PLANS · outside agents", width)}
	ws := m.agentUsageWindows()
	if len(ws) == 0 {
		for _, l := range wrapPlain("No plan numbers yet. They come from a limit message on an agent tab, and from the agents' session files when agent_transcripts is read; termocode never asks the vendor.", max(width-2, 8)) {
			out = append(out, agText(theme.TextDim, l, width))
		}
		return out
	}
	now := clock.Now()
	loc := now.Location()
	for _, w := range ws {
		used := "—"
		switch {
		case w.Limited(now):
			used = "limit"
		case w.PctKnown():
			used = agentusage.Pct(w.UsedPct)
		}
		reset := "—"
		if !w.ResetAt.IsZero() && now.Before(w.ResetAt) {
			reset = "↻" + w.ResetAt.In(loc).Format("15:04")
		}
		last := "no limit"
		if !w.LastLimit.IsZero() {
			last = "limit " + w.LastLimit.In(loc).Format("15:04")
		}
		auto := "auto-continue " + m.agentAccountAutoContinue(w.Key)
		fg := theme.TextPrimary
		if w.Limited(now) {
			fg = theme.AccentAmber
		}
		tok := aiTokShort(w.TokensToday) + " tok"
		if width >= 80 {
			out = append(out, agText(fg, padCells(w.Label(), 16)+padCells(used, 7)+padCells(reset, 8)+
				padCells(last, 13)+padCells(tok, 11)+auto, width))
			continue
		}
		// Narrow (the usual side slot): the numbers, then a dim line.
		out = append(out, agText(fg, padCells(w.Label(), 16)+padCells(used, 7)+reset, width),
			agText(theme.TextDim, "  "+last+" · "+tok+" · "+auto, width))
	}
	return out
}

// padCells pads s with spaces to n cells (one space at least).
func padCells(s string, n int) string {
	w := cellWidth(s)
	if w >= n {
		return s + " "
	}
	b := []byte(s)
	for ; w < n; w++ {
		b = append(b, ' ')
	}
	return string(b)
}

// aiPlanLine is one account in usage_summary: numbers, times and names
// only (never the limit message).
type aiPlanLine struct {
	CLI         string     `json:"cli"`
	Account     string     `json:"account,omitempty"`
	UsedPct     *float64   `json:"used_pct,omitempty"`
	Limited     bool       `json:"limited"`
	Reset       *time.Time `json:"reset,omitempty"`
	LastLimit   *time.Time `json:"last_limit,omitempty"`
	TokensToday int        `json:"tokens_today"`
}

// aiPlanLines is the plans section of usage_summary.
func (m *Model) aiPlanLines() []aiPlanLine {
	now := clock.Now()
	out := []aiPlanLine{}
	for _, w := range m.agentUsageWindows() {
		l := aiPlanLine{CLI: w.CLI, Account: w.Account, Limited: w.Limited(now), TokensToday: w.TokensToday}
		if w.PctKnown() {
			v := w.UsedPct
			l.UsedPct = &v
		}
		if !w.ResetAt.IsZero() {
			r := w.ResetAt
			l.Reset = &r
		}
		if !w.LastLimit.IsZero() {
			r := w.LastLimit
			l.LastLimit = &r
		}
		out = append(out, l)
	}
	return out
}

func init() {
	aiActionPolicies[agentUsagePlansPaletteID] = aiNever("a read-only screen for the user; the AI reads the numbers through usage_summary")
}

// agentChatAccount is the account of the chat view's tab ("" for none).
func (m *Model) agentChatAccount(c *agentChatState) string {
	return c.account
}
