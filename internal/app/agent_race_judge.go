package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
)

// ── The AI judge of a race (CMP-9) ──────────────────────────────────────
//
// p on the compare page sends the finished lanes' diffs (capped,
// secrets hidden), their numbers and verify results to the main model,
// which ranks them and picks one. The ranking, the pick and the cost are
// stored with the race and shown on the page. The judge only advises: a
// card offers to merge the pick with Cancel first, and Merge runs the
// normal merge (a conflict still stops first and asks). The user asks
// every time; nothing is sent on its own.

const confirmKindRaceJudgePick confirmKindEnum = 3724

// raceJudgeCap is how many bytes of the lanes' diffs go to the judge.
const raceJudgeCap = 48 << 10

// raceJudgeTimeout is how long the judge may take (a var for tests).
var raceJudgeTimeout = 90 * time.Second

// raceJudgeSlot is the judge's model (a var for tests).
var raceJudgeSlot = func(s *aiState) (aiSlot, error) { return s.resolveSlot(ai.SlotMain) }

// raceJudgeMsg is the judge's answer.
type raceJudgeMsg struct {
	race  string
	judge agentrace.Judgment
	err   error
}

// raceJudge asks the main model to rank the finished lanes of r.
func (m *Model) raceJudge(r *agentrace.Race) tea.Cmd {
	cands := agentrace.JudgeCandidates(*r)
	if len(cands) < 2 {
		m.raceMessage("The judge needs two finished lanes to compare")
		return nil
	}
	s := m.aiS()
	slot, err := raceJudgeSlot(s)
	if err != nil {
		m.raceMessage("To judge the lanes, set a main model (AI: Models); nothing was sent")
		return nil
	}
	pricing := ai.NewPricing(s.settings.Prices)
	root, race := aiWorkspaceRoot(), *r
	race.Lanes = append([]agentrace.Lane(nil), r.Lanes...)
	m.raceS().page.SetBusy("✦ judging the lanes with " + slot.cfg.Model + "…")
	return func() tea.Msg {
		diffs := map[string]string{}
		for _, l := range cands {
			if d, err := git.AgentDiff(root, l.ID); err == nil {
				diffs[l.ID] = d
			}
		}
		sys, user := agentrace.JudgePrompt(race, diffs, raceJudgeCap, func(s string) string { return ai.Redact(ai.RedactSecrets(s)) })
		ctx, cancel := context.WithTimeout(context.Background(), raceJudgeTimeout)
		defer cancel()
		text, last := ai.Collect(slot.p.Stream(ctx, ai.Request{Model: slot.cfg.Model, Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: sys}, {Role: ai.RoleUser, Content: user}}}))
		if last.Err != nil {
			return raceJudgeMsg{race: race.ID, err: last.Err}
		}
		j, err := agentrace.ParseJudge(ai.Redact(text), race)
		if err != nil {
			return raceJudgeMsg{race: race.ID, err: err}
		}
		cost := "cost unknown"
		if last.Usage != nil {
			if usd, ok := pricing.Cost(slot.cfg.Kind, *last.Usage); ok {
				cost = aiDollars(usd)
			}
		}
		j.Cost = slot.cfg.Model + " · " + cost
		return raceJudgeMsg{race: race.ID, judge: j}
	}
}

func (m *Model) onRaceJudge(msg raceJudgeMsg) tea.Cmd {
	s := m.raceS()
	s.page.SetBusy("")
	r := s.find(msg.race)
	if r == nil {
		return nil
	}
	if msg.err != nil {
		m.raceMessage("The judge failed: " + aiOneLine(ai.Redact(msg.err.Error())) + "; nothing changed")
		return nil
	}
	j := msg.judge
	r.Judge = &j
	m.raceRefreshPage()
	save := m.raceSave()
	l := j.PickLane(*r)
	switch {
	case l == nil:
		m.raceMessage("The judge says no lane does the task; nothing is offered to merge")
		return save
	case r.Winner != "":
		m.raceMessage("The judge picks " + l.Title + "; a lane is merged already")
		return save
	case !l.Finished():
		m.raceMessage("The judge picks " + l.Title + "; it is " + string(l.State) + ", so there is nothing to merge")
		return save
	}
	return tea.Batch(save, m.askRaceJudgePick(r, l))
}

// askRaceJudgePick offers to merge the judge's pick: Cancel first.
func (m *Model) askRaceJudgePick(r *agentrace.Race, l *agentrace.Lane) tea.Cmd {
	m.raceS().target = [2]string{r.ID, l.ID}
	reason := ""
	for _, rk := range r.Judge.Ranks {
		if rk.Lane == l.ID && rk.Reason != "" {
			reason = ": " + rk.Reason
		}
	}
	files, add, del := l.Totals()
	body := "The AI judge (" + r.Judge.Cost + ") picks " + l.Title + reason + "." +
		fmt.Sprintf("\n\n%s changed %d file%s (+%d -%d); verify: %s.", l.Title, files, plural(files), add, del, raceVerifyWord(*r, *l))
	if l.Verify.State != agentrace.VerifyPass {
		var passed []string
		for _, o := range agentrace.JudgeCandidates(*r) {
			if o.ID != l.ID && o.Verify.State == agentrace.VerifyPass {
				passed = append(passed, o.Title)
			}
		}
		if len(passed) > 0 {
			body += "\nCareful: the verify command passed in " + strings.Join(passed, ", ") + " but not in the pick."
		}
	}
	body += "\n\nThe judge only advises. Merge runs the normal merge of this lane into your files: a conflict stops first and asks." +
		"\nCancel: nothing changes; the ranking stays on the page."
	m.confirm = confirm.Risky("Merge the judge's pick · "+l.Title+"?", body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "merge", Title: "Merge " + l.Title, Style: confirm.StylePrimary}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindRaceJudgePick
	return nil
}

// raceVerifyWord is a lane's verify result in words.
func raceVerifyWord(r agentrace.Race, l agentrace.Lane) string {
	switch l.Verify.State {
	case agentrace.VerifyPass:
		return "passed"
	case agentrace.VerifyFail:
		return fmt.Sprintf("failed (exit %d)", l.Verify.Exit)
	case agentrace.VerifyRunning:
		return "still running"
	}
	if r.Verify == "" {
		return "no verify command"
	}
	return "not run yet (v)"
}
