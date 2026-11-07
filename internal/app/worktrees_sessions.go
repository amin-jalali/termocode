package app

import (
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── WORKTREES view: several agent sessions on one worktree (CMP-28) ─────
//
// A worktree can have more than one agent at work: separate sessions on
// the same files, each in its own terminal tab. T lists the sessions of
// the selected worktree (Enter shows the tab); S starts one more, even
// of an agent that already runs there. A second session of the same
// agent is numbered in the tab bar ("Claude Code 2").

const (
	wtSessionNewPaletteID                   = "worktree-new-session"
	wtSessionsPaletteID                     = "worktree-sessions"
	pickerKindWTSessions     pickerKindEnum = 3628
	pickerKindWTSessionAgent pickerKindEnum = 3629
	wtSessionNewRowID                       = "new"
)

func init() {
	aiActionPolicies[wtSessionNewPaletteID] = aiNever(aiNoPicker + "; the user picks the agent")
	aiActionPolicies[wtSessionsPaletteID] = aiNever(aiNoPicker)
}

// wtSessionStateWords is a tab's agent state in words ("idle" when its
// screen says nothing yet).
func wtSessionStateWords(s agents.State) (words, rank string) {
	switch s {
	case agents.Working:
		return wtAgentWords(wtStateWorking), wtStateWorking
	case agents.Waiting:
		return wtAgentWords(wtStateWaiting), wtStateWaiting
	case agents.Done:
		return wtAgentWords(wtStateDone), wtStateDone
	}
	return "idle", wtStateIdle
}

// wtSessionRows are the sessions list rows of the worktree at path: the
// agent tabs in it (waiting first, then working, then the rest, else in
// tab order), then a row that starts one more.
func wtSessionRows(entries []worktrees.Entry, tabs []wtTabAgent, path string) []picker.Item {
	type row struct {
		item picker.Item
		rank int
	}
	var rows []row
	for i, t := range tabs {
		if t.name == "" || wtOwner(entries, t.cwd) != path {
			continue
		}
		words, st := wtSessionStateWords(t.state)
		rows = append(rows, row{picker.Item{ID: "tab:" + strconv.Itoa(i), Title: t.name, Hint: words}, wtStateRank(st)})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].rank < rows[j].rank })
	out := make([]picker.Item, 0, len(rows)+1)
	for _, r := range rows {
		out = append(out, r.item)
	}
	return append(out, picker.Item{ID: wtSessionNewRowID, Title: "+ New agent session…", Hint: "starts one more agent in this worktree"})
}

// wtSessionAgentRows are the installed agents a new session in dir can
// run; the hint says which session number it becomes.
func wtSessionAgentRows(tabs []terminalTab, dir string) []picker.Item {
	var out []picker.Item
	for _, a := range agents.Registry {
		if !agentInstalled(a) {
			continue
		}
		hint := a.Bin
		if n := agentSessionNo(tabs, a.ID, dir, ""); n > 1 {
			hint += " · session " + strconv.Itoa(n)
		}
		out = append(out, picker.Item{ID: agentPickAgentPfx + a.ID, Title: a.Title, Hint: hint})
	}
	return out
}

// wtSessionTarget is the cursor's worktree for a sessions command; why
// is set when it cannot have one.
func (m *Model) wtSessionTarget() (it wtItem, ok bool, cmd tea.Cmd) {
	it, ok = m.wtSelected()
	if !ok {
		return it, false, m.revealWorktrees()
	}
	if why := wtUsable(it); why != "" {
		return it, false, m.pushToastDetail(toast.Info, "This worktree cannot have agent sessions", why)
	}
	return it, true, nil
}

// worktreeSessions lists the cursor's agent sessions (T,
// worktree-sessions).
func (m *Model) worktreeSessions() tea.Cmd {
	it, ok, cmd := m.wtSessionTarget()
	if !ok {
		return cmd
	}
	st := m.wt
	st.sessionsPath = it.e.Path
	rows := wtSessionRows(st.entries, m.wtTabs(), it.e.Path)
	m.picker = picker.NewItems(" Agent sessions in "+wtTitle(it)+" ", rows)
	m.picker.SetNote([]string{"Enter shows the session's tab; the last row starts one more."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindWTSessions
	return nil
}

// onWTSessionPicked shows the picked session's tab, or starts one more.
func (m *Model) onWTSessionPicked(id string) tea.Cmd {
	if id == wtSessionNewRowID {
		it, ok := m.wtItemByPath(m.wt.sessionsPath)
		if !ok {
			return nil
		}
		return m.openWTSessionAgent(it)
	}
	if strings.HasPrefix(id, "tab:") {
		return m.onWorktreeJumpPicked(id) // shows that tab
	}
	return nil
}

// worktreeNewSession opens the agents for one more session in the
// cursor's worktree (S, worktree-new-session).
func (m *Model) worktreeNewSession() tea.Cmd {
	it, ok, cmd := m.wtSessionTarget()
	if !ok {
		return cmd
	}
	return m.openWTSessionAgent(it)
}

// openWTSessionAgent opens the list of installed agents; the one picked
// starts in it in a new tab. The worktree you are in starts in the
// workspace.
func (m *Model) openWTSessionAgent(it wtItem) tea.Cmd {
	st := m.wt
	st.sessionDir = it.e.Path
	if it.here {
		st.sessionDir = ""
	}
	rows := wtSessionAgentRows(m.terminalTabs, st.sessionDir)
	if len(rows) == 0 {
		return m.pushToastDetail(toast.Info, "No agent is installed", "F1 → Agent: Start… lists the agents and installs them. Guide: guides/agents.md")
	}
	m.picker = picker.NewItems(" New agent session in "+wtTitle(it)+" ", rows)
	m.picker.SetNote([]string{"Enter starts it in a new terminal tab, next to the sessions already there."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindWTSessionAgent
	return nil
}

// onWTSessionAgentPicked starts the picked agent as a new session.
func (m *Model) onWTSessionAgentPicked(id string) tea.Cmd {
	aid, ok := strings.CutPrefix(id, agentPickAgentPfx)
	if !ok {
		return nil
	}
	a, ok := agents.ByID(aid)
	if !ok {
		return nil
	}
	return m.startAgentSession(a, m.wt.sessionDir)
}
