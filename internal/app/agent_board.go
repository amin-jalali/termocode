package app

import (
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentboard"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── AI: Agent Board… (CMP-2) ─────────────────────────────────────────────
//
// One list of every piece of agent work in the window, in four columns:
// NEEDS YOU, WORKING, REVIEW and DONE. The cards come from the places
// that already know them: AI runs (Runs), agent lanes in worktrees (the
// WORKTREES view: the agent in the lane, its checkpoint, its PR and CI)
// and the AI setup that waits for the user (Itermoc's NEEDS YOU). The
// picker's filter is the search. A card opens where its work lives; a
// run that can go on asks first (Cancel is the default) and resumes.

const (
	agentBoardPaletteID                         = "ai-board"
	pickerKindAgentBoard        pickerKindEnum  = 8231
	confirmKindAgentBoardResume confirmKindEnum = 8232
)

// boardRunState is a run's kind for the board.
func boardRunState(k runsKind) agentboard.RunState {
	switch k {
	case runsRunning, runsPausing:
		return agentboard.RunRunning
	case runsStalled:
		return agentboard.RunStalled
	case runsPaused:
		return agentboard.RunPaused
	case runsResumable:
		return agentboard.RunResumable
	case runsFailed:
		return agentboard.RunFailed
	case runsCancelled:
		return agentboard.RunCancelled
	}
	return agentboard.RunDone
}

// boardRunCard is run it as a card.
func boardRunCard(it runsItem, now time.Time) agentboard.Card {
	where := it.run.Profile
	if it.chatTitle != "" {
		where += theme.ASCIIText(" · ") + it.chatTitle
	}
	return agentboard.Card{
		ID:     "run:" + it.key,
		Source: agentboard.SourceRun,
		Col:    agentboard.RunColumn(boardRunState(it.kind), now.Sub(it.started)),
		Title:  it.task,
		Where:  strings.TrimSpace(where),
		Why:    "run " + runsKindWord(it),
		At:     it.started,
		Resume: it.kind == runsPaused || it.kind == runsPausing || (it.kind == runsResumable && it.resume > 0),
	}
}

// boardLaneCard is worktree it as a card; ok is false for a worktree with
// nothing to show (no agent, no checkpoint, no PR) and an archived one.
func boardLaneCard(it wtItem) (agentboard.Card, bool) {
	if it.state == wtStateArchived || it.e.Gone {
		return agentboard.Card{}, false
	}
	md := it.e.Meta
	pr, ci := "", ""
	if md.PR > 0 {
		pr, ci = md.Link.State, md.Link.CI
	}
	agent := ""
	if it.agents > 0 {
		agent = it.agent.state
	}
	col, why, on := agentboard.LaneColumn(agent, md.Checkpoint, pr, ci)
	if !on {
		return agentboard.Card{}, false
	}
	if agent != "" && it.agent.name != "" {
		why = it.agent.name + theme.ASCIIText(" · ") + why
		if it.agent.tab < 0 {
			why += theme.ASCIIText(" · ") + "other window"
		}
	}
	where := wtBranch(it.e)
	if md.PR > 0 {
		where += theme.ASCIIText(" · ") + "PR #" + strconv.Itoa(md.PR)
	}
	at := md.LastSeen
	if md.CheckpointAt.After(at) {
		at = md.CheckpointAt
	}
	id := "wt:" + it.e.Path
	if it.agents > 0 && it.agent.tab >= 0 {
		id = "tab:" + strconv.Itoa(it.agent.tab)
	}
	return agentboard.Card{ID: id, Source: agentboard.SourceLane, Col: col, Title: wtTitle(it),
		Where: where, Why: why, CI: ci, At: at}, true
}

// boardImID is the card id of an Itermoc row: "im:<kind>:<sec>:<name>".
func boardImID(r imRow) string {
	return "im:" + strconv.Itoa(int(r.kind)) + ":" + r.sec + ":" + r.name
}

// boardImRow reads an id boardImID made.
func boardImRow(id string) (imRow, bool) {
	rest, ok := strings.CutPrefix(id, "im:")
	if !ok {
		return imRow{}, false
	}
	parts := strings.SplitN(rest, ":", 3)
	if len(parts) != 3 || parts[1] == "" {
		return imRow{}, false
	}
	k, err := strconv.Atoi(parts[0])
	if err != nil || k < 0 || k > int(imRowProblem) {
		return imRow{}, false
	}
	return imRow{kind: imRowKind(k), sec: parts[1], name: parts[2]}, true
}

// agentBoardCards collects every card, sorted.
func (m *Model) agentBoardCards(now time.Time) []agentboard.Card {
	var out []agentboard.Card
	if m.ai != nil {
		for _, it := range m.runsCollect(now) {
			out = append(out, boardRunCard(it, now))
		}
	}
	if st := m.wt; st != nil && st.err == "" {
		for _, it := range wtItems(st.entries, m.wtTabs(), st.others, st.root) {
			if c, ok := boardLaneCard(it); ok {
				out = append(out, c)
			}
		}
	}
	if m.ai != nil {
		for _, n := range m.itermocSnapOf().needs() {
			out = append(out, agentboard.Card{ID: boardImID(n.row), Source: agentboard.SourceAI,
				Col: agentboard.NeedsYou, Title: n.name, Where: "AI setup", Why: n.what, At: now})
		}
	}
	agentboard.Sort(out)
	return out
}

// agentBoardRows are the picker rows: a header per column that has cards
// ("NEEDS YOU · 2"), then its cards with why · where · CI · age.
func agentBoardRows(cards []agentboard.Card, now time.Time) []picker.Item {
	cards = append([]agentboard.Card(nil), cards...)
	agentboard.Sort(cards)
	n := agentboard.Counts(cards)
	dot := theme.ASCIIText(" · ")
	var out []picker.Item
	last := agentboard.Column(-1)
	for _, c := range cards {
		group := c.Col.Title()
		if c.Col != last {
			out = append(out, picker.Item{ID: "col:" + group, Title: group + dot + strconv.Itoa(n[c.Col]), Header: true, Group: group})
			last = c.Col
		}
		hint := []string{}
		if c.Why != "" {
			hint = append(hint, c.Why)
		}
		if c.Where != "" {
			hint = append(hint, c.Where)
		}
		if w := agentboard.CIWord(c.CI); w != "" && !strings.Contains(c.Why, w) {
			hint = append(hint, w)
		}
		if !c.At.IsZero() {
			hint = append(hint, agentboard.Ago(now.Sub(c.At)))
		}
		out = append(out, picker.Item{ID: c.ID, Title: c.Title, Hint: strings.Join(hint, dot), Group: group})
	}
	return out
}

// openAgentBoardLoad is AI: Agent Board…: the worktrees are read again
// (off the UI thread) and then the board opens; without a git repository
// (or in the demo) it opens at once.
func (m *Model) openAgentBoardLoad() tea.Cmd {
	m.aiS()
	if m.wt == nil || m.wt.off {
		return m.openAgentBoard()
	}
	load := m.worktreesLoadFor(false)
	return func() tea.Msg {
		msg, ok := load().(wtLoadMsg)
		if !ok {
			return nil
		}
		msg.board = true
		return msg
	}
}

// openAgentBoard shows the board as it is now.
func (m *Model) openAgentBoard() tea.Cmd {
	m.aiS()
	m.runsLoadChats()
	now := runsNow()
	cards := m.agentBoardCards(now)
	if len(cards) == 0 {
		return m.pushToastDetail(toast.Info, "The Agent Board is empty",
			"Start an agent in a worktree (Worktree: New…) or ask the AI to run a profile; its work shows here.")
	}
	title := " Agent Board" + theme.ASCIIText(" — ") + agentboard.Summary(agentboard.Counts(cards)) + " "
	m.picker = picker.NewItems(title, agentBoardRows(cards, now))
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAgentBoard
	return nil
}

// onAgentBoardPicked opens the picked card where its work lives.
func (m *Model) onAgentBoardPicked(id string) tea.Cmd {
	switch {
	case strings.HasPrefix(id, "col:"):
		return nil
	case strings.HasPrefix(id, "run:"):
		key := strings.TrimPrefix(id, "run:")
		it, ok := m.runsFind(key)
		if !ok {
			return m.runsInfo("This run is not in the last 7 days any more")
		}
		if card := boardRunCard(it, runsNow()); card.Resume {
			return m.agentBoardResumeCard(it)
		}
		return m.agentBoardShowRun(key)
	case strings.HasPrefix(id, "im:"):
		row, ok := boardImRow(id)
		if !ok {
			return nil
		}
		return tea.Batch(m.revealItermocView(false, ""), m.itermocJumpTo(row))
	case strings.HasPrefix(id, "tab:"), strings.HasPrefix(id, "wt:"):
		if m.wt == nil {
			return nil
		}
		return m.onWorktreeJumpPicked(id)
	}
	return nil
}

// agentBoardShowRun opens run key's page in Runs.
func (m *Model) agentBoardShowRun(key string) tea.Cmd {
	cmd := m.revealRuns(false)
	return tea.Batch(cmd, m.runsShowPage(key, true))
}

// agentBoardResumeCard asks before a run goes on: it may spend tokens.
// Cancel is first and has the focus.
func (m *Model) agentBoardResumeCard(it runsItem) tea.Cmd {
	what := "The run goes on from where it paused."
	if it.kind == runsResumable {
		what = "The run starts again at step " + strconv.Itoa(it.resume) + " and runs the " + strconv.Itoa(max(it.left, 1)) + " steps that are left."
	}
	body := "\"" + rcCut(it.task, 60) + "\" (" + it.run.Profile + ")\n" + what + " It uses tokens like any run."
	m.confirm = confirm.Risky("Resume this run?", body,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "resume", Title: "Resume"},
		confirm.Button{ID: "open", Title: "Open its page"})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentBoardResume
	m.runsS().sel = it.key
	return nil
}

// answerAgentBoardResume is the Resume card's answer (Esc is Cancel).
func (m *Model) answerAgentBoardResume(id string) tea.Cmd {
	key := m.runsS().sel
	switch id {
	case "resume":
		it, ok := m.runsFind(key)
		if !ok {
			return m.runsInfo("This run is not in the last 7 days any more")
		}
		return m.runsResume(it)
	case "open":
		return m.agentBoardShowRun(key)
	}
	return nil
}
