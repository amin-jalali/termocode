package app

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// AI: Usage (AU-4): tokens and money by model and by agent, for the run
// you select (←/→, like AGENTS) and for this session. It is a second view
// of the AGENTS slot ("u" switches between the two), drawn from what the
// runs already record. The session numbers live in memory only (aiState
// usageLog) and are never written to disk.

// Which column the tables group by.
const (
	aiUsageByModel = iota
	aiUsageByAgent
)

// aiUsageLogMax caps the in-memory session log.
const aiUsageLogMax = 5000

// aiUsageEntry is one step (or one plain chat turn) that used a model.
type aiUsageEntry struct {
	Run   string // run id ("" for a plain chat turn)
	Agent string // the step's title
	Model string
	U     ai.Usage
	Cost  *float64 // dollars; nil: the price is not known
	Sub   bool     // a Claude sign-in: a subscription, not billed per token
}

// aiUsageRow is one table row: the sum of its entries.
type aiUsageRow struct {
	Name                  string
	Calls, In, Out, Total int
	Cost                  float64 // the known costs
	Priced, Unpriced      int     // entries with and without a known cost
}

// costText is the cost cell: "—" with no known price, "$0.020+" when
// only some prices are known (the sum is at least that much).
func (r aiUsageRow) costText() string {
	switch {
	case r.Priced == 0:
		return aiUsageDash()
	case r.Unpriced > 0:
		return aiDollars(r.Cost) + "+"
	}
	return aiDollars(r.Cost)
}

func aiUsageDash() string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return "-"
	}
	return "—"
}

func (r *aiUsageRow) add(e aiUsageEntry) {
	calls := max(e.U.Requests, 1)
	in := e.U.Input + e.U.CacheRead + e.U.CacheWrite
	r.Calls += calls
	r.In += in
	r.Out += e.U.Output
	r.Total += in + e.U.Output
	if e.Cost != nil {
		r.Cost += *e.Cost
		r.Priced++
	} else {
		r.Unpriced++
	}
}

// aiUsageSum is a table: rows (sorted by cost, then tokens), the total
// row and the models without a price.
type aiUsageSum struct {
	Rows  []aiUsageRow
	Total aiUsageRow
	// Unknown lists models with no known price (set ai_prices); Sub the
	// models used through a Claude sign-in.
	Unknown, Sub []string
}

// aiUsageSummarize groups entries by model or by agent.
func aiUsageSummarize(entries []aiUsageEntry, by int) aiUsageSum {
	var sum aiUsageSum
	at := map[string]int{}
	unknown, sub := map[string]bool{}, map[string]bool{}
	sum.Total.Name = "total"
	for _, e := range entries {
		name := e.Model
		if by == aiUsageByAgent {
			name = e.Agent
		}
		if name == "" {
			name = "?"
		}
		i, ok := at[name]
		if !ok {
			i = len(sum.Rows)
			at[name] = i
			sum.Rows = append(sum.Rows, aiUsageRow{Name: name})
		}
		sum.Rows[i].add(e)
		sum.Total.add(e)
		if e.Cost == nil {
			if e.Sub {
				sub[e.Model] = true
			} else {
				unknown[e.Model] = true
			}
		}
	}
	sort.SliceStable(sum.Rows, func(i, j int) bool {
		a, b := sum.Rows[i], sum.Rows[j]
		if a.Cost != b.Cost {
			return a.Cost > b.Cost
		}
		return a.Total > b.Total
	})
	for m := range unknown {
		sum.Unknown = append(sum.Unknown, m)
	}
	for m := range sub {
		sum.Sub = append(sum.Sub, m)
	}
	sort.Strings(sum.Unknown)
	sort.Strings(sum.Sub)
	return sum
}

// recordUsage adds an entry to the session log (memory only).
func (s *aiState) recordUsage(e aiUsageEntry) {
	if len(s.usageLog) >= aiUsageLogMax {
		s.usageLog = append(s.usageLog[:0], s.usageLog[len(s.usageLog)-aiUsageLogMax/2:]...)
	}
	s.usageLog = append(s.usageLog, e)
	s.usageGen++
}

// aiSlotIsSub reports whether slot runs through a Claude sign-in.
func (s *aiState) aiSlotIsSub(slot string) bool {
	cfg, ok := s.aiSlotConfig(slot)
	return ok && cfg.Kind == ai.KindClaudeOAuth
}

// runUsage is the entries of the run of message idx, from its step
// cards (what the chat file keeps already).
func (s *aiState) runUsage(idx int) []aiUsageEntry {
	msg := s.conv.Messages[idx]
	var out []aiUsageEntry
	for _, st := range msg.Steps {
		if st.Usage == nil {
			continue
		}
		agent := st.Title
		if agent == "" {
			agent = st.ID
		}
		model := st.Usage.Model
		if model == "" {
			model = st.Model
		}
		out = append(out, aiUsageEntry{Run: msg.Run.ID, Agent: aiCleanText(agent), Model: model, U: *st.Usage,
			Cost: st.Cost, Sub: s.aiSlotIsSub(st.Slot)})
	}
	return out
}

// showAIUsage is AI: Usage and a click on a run row's tokens: the usage
// view of the AGENTS slot on run id ("" = the running or the newest).
func (m *Model) showAIUsage(id string) tea.Cmd {
	cmd := m.showAgents(id)
	m.aiS().agents.usage = true
	return cmd
}

// ── Drawing ────────────────────────────────────────────────────────────

// aiTokShort is a token count for a table cell: 812, 4.1k, 1.2M.
func aiTokShort(n int) string {
	switch {
	case n < 1000:
		return strconv.Itoa(n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}

// The column sets of a usage table: total and cost; plus the calls;
// plus the tokens in and out.
const (
	usageColsMin = iota
	usageColsSlim
	usageColsWide
)

// usageCols decides the columns for a text width: the most columns that
// leave the name 14 cells (or what is left in the smallest set).
func usageCols(width int) (cols, nameW int) {
	const minimal = 6 + 1 + 8
	const slim = 5 + 1 + minimal
	const full = slim + 1 + 6 + 1 + 6
	switch {
	case width-full-1 >= 14:
		return usageColsWide, width - full - 1
	case width-slim-1 >= 14:
		return usageColsSlim, width - slim - 1
	}
	return usageColsMin, max(width-minimal-1, 6)
}

func usageCell(s string, w int) string {
	return strings.Repeat(" ", max(w-runewidth.StringWidth(s), 0)) + s
}

func usageRowText(name string, r aiUsageRow, cols, nameW int) string {
	name = runewidth.Truncate(name, nameW, "…")
	line := name + strings.Repeat(" ", nameW-runewidth.StringWidth(name))
	if cols >= usageColsSlim {
		line += " " + usageCell(strconv.Itoa(r.Calls), 5)
	}
	if cols == usageColsWide {
		line += " " + usageCell(aiTokShort(r.In), 6) + " " + usageCell(aiTokShort(r.Out), 6)
	}
	return line + " " + usageCell(aiTokShort(r.Total), 6) + " " + usageCell(r.costText(), 8)
}

// usageTable draws one table into out.
func usageTable(out *[]string, width int, title, nameHead string, sum aiUsageSum) {
	cols, nameW := usageCols(width - 2)
	hdr := nameHead + strings.Repeat(" ", max(nameW-runewidth.StringWidth(nameHead), 0))
	if cols >= usageColsSlim {
		hdr += " " + usageCell("calls", 5)
	}
	if cols == usageColsWide {
		hdr += " " + usageCell("in", 6) + " " + usageCell("out", 6)
	}
	hdr += " " + usageCell("total", 6) + " " + usageCell("cost", 8)
	*out = append(*out, agText(theme.TextSecondary, title, width), agText(theme.TextDim, hdr, width))
	for _, r := range sum.Rows {
		*out = append(*out, agText(theme.TextPrimary, usageRowText(r.Name, r, cols, nameW), width))
	}
	*out = append(*out, agText(theme.TextSecondary, usageRowText("total", sum.Total, cols, nameW), width))
}

// usageNotes are the lines under the tables for prices that are not known.
func usageNotes(sums ...aiUsageSum) []string {
	seenU, seenS := map[string]bool{}, map[string]bool{}
	var unknown, sub []string
	for _, s := range sums {
		for _, m := range s.Unknown {
			if !seenU[m] {
				seenU[m] = true
				unknown = append(unknown, m)
			}
		}
		for _, m := range s.Sub {
			if !seenS[m] {
				seenS[m] = true
				sub = append(sub, m)
			}
		}
	}
	var out []string
	if len(unknown) > 0 {
		out = append(out, "price unknown for "+strings.Join(unknown, ", ")+" (set ai_prices)")
	}
	if len(sub) > 0 {
		out = append(out, strings.Join(sub, ", ")+": Claude sign-in, not billed per token")
	}
	return out
}

// usageBuild draws the usage view: the same head as AGENTS (the run you
// look at), then the run's tables, then the session's.
func (m Model) usageBuild(width int) aiAgentsCache {
	s := m.ai
	out := aiAgentsCache{ok: true, selTop: -1, selBot: -1}
	out.foot = agText(theme.TextDim, "? keys · ←→ runs · u agents · Esc chat", width)
	if theme.CurrentIconMode == theme.IconModeASCII {
		out.foot = agText(theme.TextDim, "? keys · <- -> runs · u agents · Esc chat", width)
	}
	divider := agSeg(theme.BorderSubtle, strings.Repeat(agentsGlyphs().h, width))
	idx, nth, of, haveRun := s.agentsShown()
	left := "usage"
	status := "tokens and cost by model and by agent"
	if haveRun {
		run := s.conv.Messages[idx].Run
		left = "run " + run.ID
		if run.Profile != "" {
			left += " · " + run.Profile
		}
		if of > 1 {
			left += " (" + strconv.Itoa(nth) + "/" + strconv.Itoa(of) + ")"
		}
		status = aiRunSummaryText(run, s.conv.Messages[idx].Steps)
		if aiRunLive(run) {
			status = "running · " + status
		}
	}
	out.head = []string{agText(theme.TextPrimary, left, width), agText(theme.TextDim, status, width), divider}

	var sums []aiUsageSum
	blank := agRow("", width)
	if haveRun {
		entries := s.runUsage(idx)
		if len(entries) == 0 {
			out.body = append(out.body, agText(theme.TextSecondary, "THIS RUN", width),
				agText(theme.TextDim, "No tokens counted for this run yet.", width))
		} else {
			byModel, byAgent := aiUsageSummarize(entries, aiUsageByModel), aiUsageSummarize(entries, aiUsageByAgent)
			usageTable(&out.body, width, "THIS RUN · by model", "model", byModel)
			out.body = append(out.body, blank)
			usageTable(&out.body, width, "THIS RUN · by agent", "agent", byAgent)
			sums = append(sums, byModel)
		}
	} else {
		out.body = append(out.body, agText(theme.TextSecondary, "THIS RUN", width),
			agText(theme.TextDim, "No pipeline run in this chat yet.", width))
	}
	out.body = append(out.body, blank)
	if len(s.usageLog) == 0 {
		out.body = append(out.body, agText(theme.TextSecondary, "THIS SESSION", width),
			agText(theme.TextDim, "No model calls in this session yet.", width))
	} else {
		byModel, byAgent := aiUsageSummarize(s.usageLog, aiUsageByModel), aiUsageSummarize(s.usageLog, aiUsageByAgent)
		usageTable(&out.body, width, "THIS SESSION · by model", "model", byModel)
		out.body = append(out.body, blank)
		usageTable(&out.body, width, "THIS SESSION · by agent", "agent", byAgent)
		sums = append(sums, byModel)
	}
	if notes := usageNotes(sums...); len(notes) > 0 {
		out.body = append(out.body, blank)
		for _, n := range notes {
			for _, l := range wrapPlain(n, max(width-2, 8)) {
				out.body = append(out.body, agText(theme.DiagWarning, l, width))
			}
		}
	}
	out.body = append(out.body, blank)
	for _, l := range wrapPlain("Session numbers are kept in memory only; they are gone when termocode closes.", max(width-2, 8)) {
		out.body = append(out.body, agText(theme.TextDim, l, width))
	}
	return out
}
