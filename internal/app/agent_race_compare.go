package app

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/raceview"
)

// ── The race compare page (AGX-5, plan 2.5) ─────────────────────────────
//
// One row per lane: state, files, +/- (git.AgentNumstat, read when the
// lane ends), the verify result (the command's exit code in a task tab
// per lane) and the cost when known. Enter opens the read-only diff card
// (G-WT review), m merges the lane with the G-WT merge (a conflict stops
// before any file is written and asks, Cancel first), then a card asks
// about the other lanes with Keep first; d discards one lane (Cancel
// first); b restacks it onto your current commit (agent_race_restack.go).
// A lane whose agent still works is never merged or discarded.

const (
	confirmKindRaceConflict confirmKindEnum = 3721
	confirmKindRaceOthers   confirmKindEnum = 3722
	confirmKindRaceDiscard  confirmKindEnum = 3723
)

// raceSpawnVerify runs the verify command in lane l's worktree in a task
// tab and returns its buffer (a var for tests).
var raceSpawnVerify = func(m *Model, l agentrace.Lane, line string) (int, error) {
	return m.spawnTerminalJob(terminalJob{Label: "verify · " + l.Title, Cmdline: line, Dir: l.Worktree, Task: "race-verify:" + l.ID})
}

// raceAct runs a key of the compare page.
func (m *Model) raceAct(a raceview.ActionMsg) tea.Cmd {
	s := m.raceS()
	r := s.current()
	if a.Action == raceview.ActClose {
		s.pageOn = false
		return nil
	}
	if r == nil {
		return nil
	}
	m.raceSyncTabs()
	l := r.Lane(a.Lane)
	root := aiWorkspaceRoot()
	switch a.Action {
	case raceview.ActStop:
		return m.raceStop(r)
	case raceview.ActRefresh:
		var ids []string
		for _, l := range r.Finished() {
			ids = append(ids, l.ID)
		}
		if len(ids) == 0 {
			m.raceMessage("No lane is done yet: the files show when a lane is done")
			return nil
		}
		return raceStatsCmd(root, r.ID, ids)
	case raceview.ActReview:
		if l == nil || l.Closed() || l.Worktree == "" {
			return nil
		}
		return aiWorktreeReviewCmd(root, l.Title, ai.StoredWorktree{ID: l.ID, Branch: l.Branch})
	case raceview.ActMerge:
		if l == nil || !l.Finished() {
			return nil
		}
		return raceMergeCmd(root, r.ID, l.ID, false)
	case raceview.ActDiscard:
		if l == nil || !l.Finished() {
			return nil
		}
		return m.askRaceDiscard(r, l)
	case raceview.ActRestack:
		if l == nil || !l.Finished() {
			return nil
		}
		return m.askRaceRestack(r, l) // agent_race_restack.go
	case raceview.ActVerify:
		return m.raceVerify(r)
	case raceview.ActSummary:
		return m.raceSummary(r)
	case raceview.ActJudge:
		return m.raceJudge(r)
	}
	return nil
}

// raceMessage shows text on the page's status line.
func (m *Model) raceMessage(text string) {
	if s := m.raceS(); s.pageOn {
		s.page.SetMessage(text)
	}
}

// ── Files ────────────────────────────────────────────────────────────────

// raceStatsMsg carries lanes' changed files read off the UI goroutine.
type raceStatsMsg struct {
	race  string
	files map[string][]agentrace.FileStat
	errs  map[string]string
}

// raceStatsCmd reads the changed files of lanes of race.
func raceStatsCmd(root, race string, lanes []string) tea.Cmd {
	return func() tea.Msg {
		msg := raceStatsMsg{race: race, files: map[string][]agentrace.FileStat{}, errs: map[string]string{}}
		for _, id := range lanes {
			st, err := git.AgentNumstat(root, id)
			if err != nil {
				msg.errs[id] = aiOneLine(err.Error())
				continue
			}
			fs := make([]agentrace.FileStat, len(st))
			for i, f := range st {
				fs[i] = agentrace.FileStat{Status: f.Status, Path: f.Path, Add: f.Add, Del: f.Del, Binary: f.Binary}
			}
			msg.files[id] = fs
		}
		return msg
	}
}

// ── Merge ────────────────────────────────────────────────────────────────

// raceMergedMsg is the end of a merge (or of Keep on branch).
type raceMergedMsg struct {
	race, lane string
	res        git.AgentMerge
	err        error
	force      bool
	kept       bool
}

// raceMergeCmd merges lane into the user's tree (G-WT merge). Without
// force a conflict writes nothing. After a merge the worktree goes; the
// branch too unless the merge had conflicts.
func raceMergeCmd(root, race, lane string, force bool) tea.Cmd {
	return func() tea.Msg {
		res, err := git.MergeAgentWorktree(root, lane, force)
		if err != nil || (len(res.Conflicts) > 0 && !force) {
			return raceMergedMsg{race: race, lane: lane, res: res, err: err, force: force}
		}
		err = git.RemoveAgentWorktree(root, lane, len(res.Conflicts) == 0)
		return raceMergedMsg{race: race, lane: lane, res: res, err: err, force: force}
	}
}

func raceKeepCmd(root, race, lane string) tea.Cmd {
	return func() tea.Msg {
		if _, err := git.AgentChanges(root, lane); err != nil { // the work is committed on the branch first
			return raceMergedMsg{race: race, lane: lane, err: err, kept: true}
		}
		return raceMergedMsg{race: race, lane: lane, err: git.RemoveAgentWorktree(root, lane, false), kept: true}
	}
}

func (m *Model) onRaceMerged(msg raceMergedMsg) tea.Cmd {
	s := m.raceS()
	r := s.find(msg.race)
	if r == nil {
		return nil
	}
	l := r.Lane(msg.lane)
	if l == nil {
		return nil
	}
	defer m.raceRefreshPage()
	if msg.err != nil {
		return m.toastErr("Merge failed; the lane's work is safe on "+l.Branch, msg.err)
	}
	if msg.kept {
		l.State, l.Note = agentrace.Discarded, "kept on "+l.Branch
		return tea.Batch(m.raceSave(), m.aiInfo("Worktree removed; the work stays on branch "+l.Branch))
	}
	if len(msg.res.Conflicts) > 0 && !msg.force {
		return m.askRaceConflict(r, l, msg.res.Conflicts)
	}
	l.State = agentrace.Merged
	if !r.Split() { // a split has no winner: every part may be merged (CMP-4)
		r.Winner = l.ID
	}
	_, sync := m.aiSyncIDE("", aiSyncDisk)
	n := len(msg.res.Applied)
	text := fmt.Sprintf("Merged %d file%s from %s", n, plural(n), l.Title)
	if len(msg.res.Conflicts) > 0 {
		l.Note = "conflict markers; " + l.Branch + " kept"
		text += "; fix the conflict markers in " + strings.Join(msg.res.Conflicts, ", ")
	}
	if r.Split() {
		if left := splitPartsLeft(r); left > 0 {
			text += fmt.Sprintf("; %d more part%s to merge (m)", left, plural(left))
		}
		return tea.Batch(sync, m.raceSave(), m.aiInfo(text))
	}
	return tea.Batch(sync, m.raceSave(), m.aiInfo(text), m.askRaceOthers(r, l))
}

func (m *Model) askRaceConflict(r *agentrace.Race, l *agentrace.Lane, files []string) tea.Cmd {
	m.raceS().target = [2]string{r.ID, l.ID}
	body := "You changed these files too since the race started:\n  " + strings.Join(files, "\n  ") +
		"\n\nNothing was written yet. Your files and the lane's work (branch " + l.Branch + ") are both safe." +
		"\n\nMerge with markers: writes the lane's other files and puts both versions with conflict markers (<<<<<<<) in these files for you to fix; the branch stays." +
		"\nKeep on branch: removes the worktree and keeps the work on the branch, to merge later with git."
	m.confirm = confirm.Risky("Merge conflict · "+l.Title, body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "markers", Title: "Merge with markers", Style: confirm.StylePrimary},
		confirm.Button{ID: "branch", Title: "Keep on branch"}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindRaceConflict
	return nil
}

// askRaceOthers asks, after the winner is merged, what happens to the
// other finished lanes: Keep (first and focused) or Discard them. A lane
// that still works is not offered.
func (m *Model) askRaceOthers(r *agentrace.Race, won *agentrace.Lane) tea.Cmd {
	var done, busy []string
	for i := range r.Lanes {
		l := &r.Lanes[i]
		switch {
		case l.ID == won.ID || l.Closed():
		case l.Finished():
			done = append(done, l.Title)
		case raceActive(l):
			busy = append(busy, l.Title)
		}
	}
	if len(done) == 0 {
		return nil
	}
	m.raceS().target = [2]string{r.ID, won.ID}
	what := "Discard the other lane"
	if len(done) > 1 {
		what = fmt.Sprintf("Discard the other %d lanes", len(done))
	}
	body := won.Title + " is merged into your files. The other finished lanes: " + strings.Join(done, ", ") + "." +
		"\n\nKeep the other lanes: they stay as they are, to compare or merge later." +
		"\n" + what + ": removes their worktrees; a branch with work on it stays (git branch), one without goes."
	if len(busy) > 0 {
		body += "\n\n" + strings.Join(busy, ", ") + " still works; it stays."
	}
	m.confirm = confirm.Risky("Merged "+won.Title+" · the other lanes?", body, confirm.Button{ID: "keep", Title: "Keep the other lanes"},
		confirm.Button{ID: "discard", Title: what, Style: confirm.StyleDestructive}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindRaceOthers
	return nil
}

// ── Discard ──────────────────────────────────────────────────────────────

func (m *Model) askRaceDiscard(r *agentrace.Race, l *agentrace.Lane) tea.Cmd {
	m.raceS().target = [2]string{r.ID, l.ID}
	files, _, _ := l.Totals()
	changed := fmt.Sprintf("%s changed %d file%s in its own worktree.", l.Title, files, plural(files))
	if !l.Stats {
		changed = l.Title + " worked in its own worktree."
	}
	body := changed + " Discard removes the worktree and the branch " + l.Branch + ". Your files do not change. This cannot be undone."
	m.confirm = confirm.Risky("Discard the lane of "+l.Title+"?", body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "discard", Title: "Discard", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindRaceDiscard
	return nil
}

// raceDiscardedMsg is the end of discarding lanes.
type raceDiscardedMsg struct {
	race string
	kept map[string]bool // lane → its branch stayed
	errs map[string]error
}

// raceDiscardCmd removes the lanes' worktrees. keepWork: a branch with
// work on it stays (the other lanes after a merge); else every branch
// goes (d, after its card).
func raceDiscardCmd(root, race string, lanes []string, keepWork bool) tea.Cmd {
	return func() tea.Msg {
		msg := raceDiscardedMsg{race: race, kept: map[string]bool{}, errs: map[string]error{}}
		for _, id := range lanes {
			drop := true
			if keepWork {
				ch, err := git.AgentChanges(root, id) // commits the work on the branch first
				if err != nil {
					msg.errs[id] = err
					continue
				}
				drop = len(ch) == 0
			}
			if err := git.RemoveAgentWorktree(root, id, drop); err != nil {
				msg.errs[id] = err
				continue
			}
			msg.kept[id] = !drop
		}
		return msg
	}
}

func (m *Model) onRaceDiscarded(msg raceDiscardedMsg) tea.Cmd {
	r := m.raceS().find(msg.race)
	if r == nil {
		return nil
	}
	n := 0
	for id, kept := range msg.kept {
		if l := r.Lane(id); l != nil {
			l.State, l.Note = agentrace.Discarded, ""
			if kept {
				l.Note = "kept on " + l.Branch
			}
			delete(m.raceS().bufs, id)
			n++
		}
	}
	m.raceRefreshPage()
	cmds := []tea.Cmd{m.raceSave()}
	for id, err := range msg.errs {
		cmds = append(cmds, m.toastErr("Could not discard "+id, err))
	}
	if n > 0 {
		cmds = append(cmds, m.aiInfo(fmt.Sprintf("Discarded %d lane%s", n, plural(n))))
	}
	return tea.Batch(cmds...)
}

// answerRaceCard runs the answer of a race card ("" or the safe answer:
// nothing happens).
func (m *Model) answerRaceCard(kind confirmKindEnum, id string) tea.Cmd {
	s := m.raceS()
	t := s.target
	s.target = [2]string{}
	r := s.find(t[0])
	if r == nil {
		return nil
	}
	root := aiWorkspaceRoot()
	switch {
	case kind == confirmKindRaceConflict && id == "markers":
		return raceMergeCmd(root, r.ID, t[1], true)
	case kind == confirmKindRaceConflict && id == "branch":
		return raceKeepCmd(root, r.ID, t[1])
	case kind == confirmKindRaceDiscard && id == "discard":
		if l := r.Lane(t[1]); l != nil && l.Finished() {
			return raceDiscardCmd(root, r.ID, []string{l.ID}, false)
		}
	case kind == confirmKindRaceJudgePick && id == "merge":
		if l := r.Lane(t[1]); l != nil && l.Finished() && r.Judge != nil && r.Judge.Pick == l.ID {
			return raceMergeCmd(root, r.ID, l.ID, false)
		}
	case kind == confirmKindRaceOthers && id == "discard":
		var ids []string
		for _, l := range r.Finished() {
			if l.ID != t[1] {
				ids = append(ids, l.ID)
			}
		}
		return raceDiscardCmd(root, r.ID, ids, true)
	}
	return nil
}

// ── Verify ───────────────────────────────────────────────────────────────

// raceVerify runs the race's verify command in every finished lane.
func (m *Model) raceVerify(r *agentrace.Race) tea.Cmd {
	if r.Verify == "" {
		m.raceMessage("This race has no verify command (set one in Agent: Race…)")
		return nil
	}
	s := m.raceS()
	n := 0
	var cmds []tea.Cmd
	for _, l := range r.Finished() {
		if l.Worktree == "" {
			continue
		}
		buf, err := raceSpawnVerify(m, *l, r.Verify)
		if err != nil {
			cmds = append(cmds, m.toastErr("Could not run the verify command in "+l.Title, err))
			continue
		}
		s.verify[buf] = [2]string{r.ID, l.ID}
		l.Verify = agentrace.Verify{State: agentrace.VerifyRunning}
		n++
	}
	if n == 0 && len(cmds) == 0 {
		m.raceMessage("No lane is done yet: verify runs in the finished lanes")
		return nil
	}
	m.raceRefreshPage()
	m.raceMessage(fmt.Sprintf("Verifying %d lane%s: %s", n, plural(n), r.Verify))
	return tea.Batch(append(cmds, m.raceSave())...)
}

// raceOnVerifyExit records a verify job's exit code (ok=false: not a
// verify job).
func (m *Model) raceOnVerifyExit(buf, code int) (tea.Cmd, bool) {
	if m.race == nil {
		return nil, false
	}
	t, ok := m.race.verify[buf]
	if !ok {
		return nil, false
	}
	delete(m.race.verify, buf)
	if r := m.race.find(t[0]); r != nil {
		if l := r.Lane(t[1]); l != nil {
			l.Verify = agentrace.VerifyResult(code)
		}
	}
	m.raceRefreshPage()
	return m.raceSave(), true
}

// ── Summary (AGX-6) ──────────────────────────────────────────────────────

// raceSummaryCap is how many bytes of the lanes' diffs go to the model.
const raceSummaryCap = 24 << 10

// raceQuickSlot is the model of the summary (a var for tests).
var raceQuickSlot = func(s *aiState) (aiSlot, error) { return s.resolveSlot(ai.SlotQuick) }

// raceSummaryMsg is the quick model's answer.
type raceSummaryMsg struct {
	race  string
	lines []string
	cost  string
	err   error
}

// raceSummary asks the quick model for a 3-line difference summary of
// the finished lanes (s on the page: the user asks, every time).
func (m *Model) raceSummary(r *agentrace.Race) tea.Cmd {
	var ids []string
	for _, l := range r.Lanes {
		if l.Finished() || l.State == agentrace.Merged {
			ids = append(ids, l.ID)
		}
	}
	if len(ids) < 2 {
		m.raceMessage("A summary needs two finished lanes to compare")
		return nil
	}
	s := m.aiS()
	slot, err := raceQuickSlot(s)
	if err != nil {
		m.raceMessage("To summarize the lanes, set a quick model (AI: Models); nothing was sent")
		return nil
	}
	pricing := ai.NewPricing(s.settings.Prices)
	root, race := aiWorkspaceRoot(), *r
	race.Lanes = append([]agentrace.Lane(nil), r.Lanes...)
	m.raceS().page.SetBusy("✦ summarizing with " + slot.cfg.Model + "…")
	return func() tea.Msg {
		diffs := map[string]string{}
		for _, id := range ids {
			if d, err := git.AgentDiff(root, id); err == nil {
				diffs[id] = d
			}
		}
		sys, user := agentrace.SummaryPrompt(race, diffs, raceSummaryCap, func(s string) string { return ai.Redact(ai.RedactSecrets(s)) })
		ctx, cancel := context.WithTimeout(context.Background(), aiMemoryTimeout)
		defer cancel()
		text, last := ai.Collect(slot.p.Stream(ctx, ai.Request{Model: slot.cfg.Model, Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: sys}, {Role: ai.RoleUser, Content: user}}}))
		if last.Err != nil {
			return raceSummaryMsg{race: race.ID, err: last.Err}
		}
		cost := "cost unknown"
		if last.Usage != nil {
			if usd, ok := pricing.Cost(slot.cfg.Kind, *last.Usage); ok {
				cost = aiDollars(usd)
			}
		}
		return raceSummaryMsg{race: race.ID, lines: agentrace.ParseSummary(ai.Redact(text)), cost: slot.cfg.Model + " · " + cost}
	}
}

func (m *Model) onRaceSummary(msg raceSummaryMsg) tea.Cmd {
	s := m.raceS()
	s.page.SetBusy("")
	r := s.find(msg.race)
	if r == nil {
		return nil
	}
	if msg.err != nil {
		m.raceMessage("The summary failed: " + aiOneLine(ai.Redact(msg.err.Error())))
		return nil
	}
	if len(msg.lines) == 0 {
		m.raceMessage("The model gave no summary")
		return nil
	}
	r.Summary, r.SummaryCost = msg.lines, msg.cost
	m.raceRefreshPage()
	return m.raceSave()
}

func (m *Model) handleRaceCompareMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case raceStatsMsg:
		r := m.raceS().find(msg.race)
		if r == nil {
			return nil, true
		}
		for id, fs := range msg.files {
			if l := r.Lane(id); l != nil {
				l.Files, l.Stats = fs, true
			}
		}
		for id, e := range msg.errs {
			if l := r.Lane(id); l != nil {
				l.Note = e
			}
		}
		m.raceRefreshPage()
		return m.raceSave(), true
	case raceMergedMsg:
		return m.onRaceMerged(msg), true
	case raceDiscardedMsg:
		return m.onRaceDiscarded(msg), true
	case raceRestackedMsg:
		return m.onRaceRestacked(msg), true
	case raceSummaryMsg:
		return m.onRaceSummary(msg), true
	case raceJudgeMsg:
		return m.onRaceJudge(msg), true
	}
	return nil, false
}
