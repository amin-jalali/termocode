package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Keys and overflow of the AI header and the AGENTS / USAGE views ─────
//
// `?` in AGENTS and USAGE opens a key card (the footer only says
// "? keys"). The header's "⋯" chip holds the chips that did not fit at a
// narrow width. The two global keys a terminal may swallow
// (Alt+Shift+A / Alt+Shift+P) have a second route without Shift
// (Alt+G / Alt+N); the card shows both.

// pickerKindAIMore is the menu behind the header's ⋯ chip.
const pickerKindAIMore pickerKindEnum = 1011

// agentsKeyRows are the rows of the AGENTS and USAGE key cards.
func agentsKeyRows(usage bool) []confirm.KeyRow {
	rows := [][]string{{"Move"}}
	if usage {
		rows = append(rows,
			[]string{"← / →", "the run above the tables: previous / next"},
			[]string{"PgUp / PgDn", "scroll"},
			[]string{"u", "back to AGENTS"},
		)
	} else {
		rows = append(rows,
			[]string{"Tab / Shift+Tab", "next / previous step"},
			[]string{"↓ ↑  j k", "the same"},
			[]string{"Enter / Space", "fold or open the step"},
			[]string{"← / →", "previous / next run"},
			[]string{"PgUp / PgDn", "scroll"},
			[]string{"d", "details on / off"},
			[]string{"t", "a dense task list instead of the cards (and back)"},
			[]string{"f", "follow the running step again"},
			[]string{"r", "resume a run that termocode stopped"},
			[]string{"a", "the plan card of a run that waits for you"},
			[]string{"u", "usage: tokens and cost by model and agent"},
			[]string{"w", "the step's own worktree: Review, Merge, Discard"},
			[]string{"m", "send the running step a message (it reads it after its current tool call)"},
			[]string{"x", "stop only the selected step (asks first)"},
			[]string{"o", "open the step's whole transcript in a read-only page"},
			[]string{"Worktrees"},
			[]string{"⑂ on a card", "the step works in its own git worktree (it ran at the same time as another step)"},
			[]string{"AI: Agent Worktrees…", "every agent worktree, also ones left over after a crash"},
		)
	}
	rows = append(rows,
		[]string{"Leave"},
		[]string{"Esc / q", "back to the chat"},
		[]string{"Ctrl+C", "stop the running agents"},
		[]string{"Two routes"},
		[]string{"Alt+Shift+A / Alt+G", "show or leave AGENTS"},
		[]string{"Alt+Shift+P / Alt+N", "next profile"},
		[]string{"", "(if your terminal swallows the first key, the second one works)"},
	)
	return keyRows(rows...)
}

// openAgentsKeyCard is `?` in AGENTS and USAGE.
func (m *Model) openAgentsKeyCard() tea.Cmd {
	usage := m.ai != nil && m.ai.agents.usage
	title := "Agents keys"
	if usage {
		title = "Usage keys"
	}
	return m.openKeyCard(title, agentsKeyRows(usage), help.Loc{Page: "guides/ai-profiles.md", Anchor: "the-agents-panel"}, nil)
}

// aiMoreLabel is the ⋯ chip.
func aiMoreLabel() string { return theme.GlyphMore.String() }

// openAIMoreMenu is a click on ⋯: what did not fit in the header.
func (m *Model) openAIMoreMenu() tea.Cmd {
	items := []picker.Item{
		{ID: "profile", Title: "Profile…", Hint: "pick the agent profile"},
		{ID: "services", Title: "Services", Hint: "what the chat can use"},
		{ID: "think", Title: "Next reasoning level", Hint: "how hard the model thinks"},
		{ID: "usage", Title: "Usage", Hint: "tokens and cost"},
		{ID: "agents", Title: "Agents", Hint: "the steps of the run"},
	}
	m.picker = picker.NewItems(" "+aiSpark()+" More ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIMore
	return nil
}

// onAIMorePicked runs a row of the ⋯ menu.
func (m *Model) onAIMorePicked(id string) tea.Cmd {
	switch id {
	case "profile":
		return m.openAIProfilePicker()
	case "services":
		return m.onMCPChip()
	case "think":
		return m.cycleAIReasoning()
	case "usage":
		return m.showAIUsage("")
	case "agents":
		return m.showAgents("")
	}
	return nil
}
