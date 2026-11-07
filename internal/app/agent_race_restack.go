package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// ── Restack a lane (CMP-56) ─────────────────────────────────────────────
//
// b on the compare page puts a finished lane's work on top of your
// current commit as clean commits (git.RestackAgentWorktree), so the
// lane is ready to merge, push or turn into a pull request without the
// snapshot and "wip" commits. A card asks first (Cancel first): Squash
// into one commit asks for the message (the task, prefilled), Keep
// commits replays them as they are. Only the lane's branch changes; a
// clash with your commit changes nothing and names the files.

const (
	confirmKindRaceRestack   confirmKindEnum = 5656
	promptKindRaceRestackMsg promptKindEnum  = 5657
)

// raceRestackMsgMax is the longest prefilled commit subject.
const raceRestackMsgMax = 72

func (m *Model) askRaceRestack(r *agentrace.Race, l *agentrace.Lane) tea.Cmd {
	m.raceS().target = [2]string{r.ID, l.ID}
	work := l.Title + " worked on branch " + l.Branch + "."
	if files, _, _ := l.Totals(); l.Stats {
		work = fmt.Sprintf("%s changed %d file%s on branch %s.", l.Title, files, plural(files), l.Branch)
	}
	body := work + " Restack puts that work on top of your current commit (HEAD), so it is ready to merge, push or open a pull request." +
		"\n\nSquash into one commit: one clean commit; you type its message next." +
		"\nKeep commits: the lane's commits, one by one, as they are." +
		"\n\nYour files, index and branch do not change. If the lane clashes with your commit, nothing changes and the files are named." +
		" The old commits stay in the branch's reflog."
	m.confirm = confirm.Risky("Restack the lane of "+l.Title+"?", body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "squash", Title: "Squash into one commit", Style: confirm.StylePrimary},
		confirm.Button{ID: "keep", Title: "Keep commits"}).WithWideText().WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindRaceRestack
	return nil
}

// answerRaceRestack runs the restack card's answer (Cancel: nothing).
func (m *Model) answerRaceRestack(id string) tea.Cmd {
	s := m.raceS()
	t := s.target
	r := s.find(t[0])
	if r == nil {
		s.target = [2]string{}
		return nil
	}
	l := r.Lane(t[1])
	if l == nil || !l.Finished() {
		s.target = [2]string{}
		return nil
	}
	switch id {
	case "squash":
		// The target stays for the message prompt.
		m.prompt = prompt.New(" Restack · "+l.Title+" ", "Message of the clean commit:", raceRestackDefaultMsg(r.Prompt))
		m.prompt.SetNote("One commit on top of your current commit; Esc cancels and nothing changes.")
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen, m.promptKind = true, promptKindRaceRestackMsg
		return nil
	case "keep":
		s.target = [2]string{}
		m.raceMessage("Restacking " + l.Title + "…")
		return raceRestackCmd(aiWorkspaceRoot(), r.ID, l.ID, git.AgentRestackOptions{})
	}
	s.target = [2]string{}
	return nil
}

// onRaceRestackMessage restacks with squash once the message is typed
// (empty: the prefilled task).
func (m *Model) onRaceRestackMessage(value string) tea.Cmd {
	s := m.raceS()
	t := s.target
	s.target = [2]string{}
	r := s.find(t[0])
	if r == nil {
		return nil
	}
	l := r.Lane(t[1])
	if l == nil || !l.Finished() {
		return nil
	}
	msg := strings.TrimSpace(value)
	if msg == "" {
		msg = raceRestackDefaultMsg(r.Prompt)
	}
	m.raceMessage("Restacking " + l.Title + "…")
	return raceRestackCmd(aiWorkspaceRoot(), r.ID, l.ID, git.AgentRestackOptions{Squash: true, Message: msg})
}

// raceRestackDefaultMsg is the task as a commit subject (one line, cut).
func raceRestackDefaultMsg(task string) string {
	s := strings.Join(strings.Fields(task), " ")
	if rs := []rune(s); len(rs) > raceRestackMsgMax {
		s = strings.TrimSpace(string(rs[:raceRestackMsgMax-1])) + "…"
	}
	if s == "" {
		s = "agent work"
	}
	return s
}

// raceRestackedMsg is the end of a restack.
type raceRestackedMsg struct {
	race, lane string
	res        git.AgentRestack
	squash     bool
	err        error
}

// raceRestackCmd restacks lane off the UI goroutine.
func raceRestackCmd(root, race, lane string, opt git.AgentRestackOptions) tea.Cmd {
	return func() tea.Msg {
		res, err := git.RestackAgentWorktree(root, lane, opt)
		return raceRestackedMsg{race: race, lane: lane, res: res, squash: opt.Squash, err: err}
	}
}

func (m *Model) onRaceRestacked(msg raceRestackedMsg) tea.Cmd {
	r := m.raceS().find(msg.race)
	if r == nil {
		return nil
	}
	l := r.Lane(msg.lane)
	if l == nil {
		return nil
	}
	defer m.raceRefreshPage()
	if msg.err != nil {
		m.raceMessage("Restack failed; the lane's work is safe on " + l.Branch)
		return m.toastErr("Restack failed; the lane's work is safe on "+l.Branch, msg.err)
	}
	if len(msg.res.Conflicts) > 0 {
		l.Note = "restack clash: " + strings.Join(msg.res.Conflicts, ", ")
		text := "Not restacked: " + strings.Join(msg.res.Conflicts, ", ") + " clash with your commit; nothing changed (merge with m, or fix it by hand)"
		m.raceMessage(text)
		return tea.Batch(m.raceSave(), m.aiInfo(text))
	}
	short := msg.res.Onto
	if len(short) > 7 {
		short = short[:7]
	}
	l.Note = fmt.Sprintf("restacked on %s: %s → %s", short, raceCommitsWord(msg.res.Before), raceCommitsWord(msg.res.After))
	text := fmt.Sprintf("Restacked %s on %s: %s", l.Title, short, raceCommitsWord(msg.res.After))
	if msg.res.After == 0 {
		text = l.Title + "'s work is in your commit already: nothing left on the lane"
	}
	m.raceMessage(text)
	return tea.Batch(m.raceSave(), m.aiInfo(text), raceStatsCmd(aiWorkspaceRoot(), r.ID, []string{l.ID}))
}

func raceCommitsWord(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}
