package app

import (
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentroll"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Worktree: Jump… (WTX-3) ──────────────────────────────────────────────
//
// One fuzzy list of the worktrees and the agents in them, from this
// window's terminal tabs and the other windows' roll-ups: the agents
// that wait for you first. A worktree opens the Open Recent Workspace
// way; an agent tab of this window is shown.

const pickerKindWorktreeJump pickerKindEnum = 3600

// wtAgentWords is an agent state for a hint.
func wtAgentWords(state string) string {
	switch state {
	case wtStateWaiting:
		return "waits for you"
	case wtStateDone:
		return "done"
	}
	return state
}

// wtJumpRows are the picker rows: agents (by state; this window's first),
// then every worktree that is not gone, in the view's order.
func wtJumpRows(items []wtItem, tabs []wtTabAgent, others []agentroll.Rollup) []picker.Item {
	entries := items
	type agentRow struct {
		item  picker.Item
		state string
	}
	var ags []agentRow
	owner := func(dir string) (wtItem, bool) {
		best, ok := wtItem{}, false
		for _, it := range entries {
			if it.e.Gone {
				continue
			}
			if dir == it.e.Path || strings.HasPrefix(dir, it.e.Path+"/") {
				if !ok || len(it.e.Path) > len(best.e.Path) {
					best, ok = it, true
				}
			}
		}
		return best, ok
	}
	dot := theme.ASCIIText(" · ")
	for i, t := range tabs {
		state := map[agents.State]string{agents.Working: wtStateWorking, agents.Waiting: wtStateWaiting, agents.Done: wtStateDone}[t.state]
		if t.name == "" || state == "" {
			continue
		}
		if it, ok := owner(t.cwd); ok {
			ags = append(ags, agentRow{picker.Item{ID: "tab:" + strconv.Itoa(i), Title: t.name + dot + wtTitle(it), Hint: wtAgentWords(state)}, state})
		}
	}
	n := 0
	for _, r := range others {
		for _, a := range r.Items {
			if it, ok := owner(a.Worktree); ok {
				ags = append(ags, agentRow{picker.Item{ID: "ag:" + strconv.Itoa(n) + ":" + it.e.Path, Title: a.Name + dot + wtTitle(it), Hint: wtAgentWords(a.State) + dot + "other window"}, a.State})
				n++
			}
		}
	}
	sort.SliceStable(ags, func(i, j int) bool { return wtStateRank(ags[i].state) < wtStateRank(ags[j].state) })
	out := make([]picker.Item, 0, len(ags)+len(items))
	for _, a := range ags {
		out = append(out, a.item)
	}
	for _, it := range items {
		if it.e.Gone || it.state == wtStateArchived {
			continue
		}
		hint := wtBranch(it.e)
		if it.state != wtStateIdle {
			hint = wtAgentWords(it.state) + dot + hint
		}
		if it.here {
			hint += dot + "you are here"
		}
		out = append(out, picker.Item{ID: "wt:" + it.e.Path, Title: wtTitle(it), Hint: hint})
	}
	return out
}

// worktreeJump is Worktree: Jump… / Ctrl+Shift+J: the list is read again
// (off the UI thread), then the picker opens.
func (m *Model) worktreeJump() tea.Cmd {
	if m.wt == nil {
		return nil
	}
	if m.wt.off {
		return m.openWorktreeJump()
	}
	return m.worktreesLoadFor(true)
}

// openWorktreeJump shows the picker from the list as it is now.
func (m *Model) openWorktreeJump() tea.Cmd {
	st := m.wt
	if st.err != "" {
		return m.pushToastDetail(toast.Info, "No worktrees here", "Worktree: Jump… works in a git repository.")
	}
	items := wtItems(st.entries, m.wtTabs(), st.others, st.root)
	m.picker = picker.NewItems(" Jump to a Worktree ", wtJumpRows(items, m.wtTabs(), st.others))
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindWorktreeJump
	return nil
}

// onWorktreeJumpPicked opens the picked worktree or shows the agent tab.
func (m *Model) onWorktreeJumpPicked(id string) tea.Cmd {
	if rest, ok := strings.CutPrefix(id, "tab:"); ok {
		i, err := strconv.Atoi(rest)
		if err != nil || i < 0 || i >= len(m.terminalTabs) {
			return nil
		}
		if !m.termOpen && len(m.hiddenTerminalTabs) > 0 {
			m.toggleTerminalPanel()
		}
		m.showPanelTab(panelKindTerminal)
		m.switchTerminalTab(i)
		return nil
	}
	path, ok := strings.CutPrefix(id, "wt:")
	if !ok {
		rest, ok2 := strings.CutPrefix(id, "ag:")
		if !ok2 {
			return nil
		}
		_, path, _ = strings.Cut(rest, ":")
	}
	m.wt.cursorPath = path
	m.wt.touch()
	return m.worktreeOpen()
}
