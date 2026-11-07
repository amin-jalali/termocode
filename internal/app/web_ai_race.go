package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Agent races from the phone (MOB-62, mobile plan 2.6 and 3.9). Start is
// the laptop's agent_race_start behind a laptop card: the same checks
// (installed agents whose screen termocode can read, one verify line)
// run before the card, and Confirm starts the race as Agent: Race… does.
// Summarise, Verify and Stop are the compare page's s, v and x for one
// race: the summary spends the quick model's tokens only when tapped,
// verify runs the race's own command (from its start card) in the
// finished lanes, and stop sends Ctrl+C into the working lanes, the work
// staying in their worktrees. The phone sees the lanes change in the
// races topic (MOB-61). Merge and Discard stay laptop cards.

func init() {
	webAskOps[webproto.OpAIRaceStart] = (*Model).webAIRaceStart
	webOps[webproto.OpAIRaceSummary] = (*Model).webAIRaceSummary
	webOps[webproto.OpAIRaceVerify] = (*Model).webAIRaceVerify
	webOps[webproto.OpAIRaceStop] = (*Model).webAIRaceStop
}

// webAIRaceStart checks the race and asks the laptop to start it.
func (m *Model) webAIRaceStart(call *webCall) *webAsk {
	a := call.args.(*webproto.AIRaceStartArgs)
	if !git.IsRepo(aiWorkspaceRoot()) {
		call.fail(webproto.CodeBadRequest, "A race needs a git repository: each lane works in its own git worktree.")
		return nil
	}
	list := make([]any, len(a.Agents))
	for i, id := range a.Agents {
		list[i] = id
	}
	args := map[string]any{"task": a.Task, "agents": list, "verify": a.Verify}
	task, verify, lanes, err := aiRaceArgs(args)
	if err != nil {
		call.fail(webproto.CodeBadRequest, aiOneLine(ai.Redact(err.Error())))
		return nil
	}
	ids := make([]string, len(lanes))
	for i, l := range lanes {
		ids[i] = l.ID
	}
	return &webAsk{
		summary: fmt.Sprintf("race of %d lanes: %s", len(ids), strings.Join(ids, ", ")),
		title:   "Start an agent race?",
		body:    describeRaceStart(args),
		action:  "Start race",
		run:     func(m *Model) tea.Cmd { return m.startRace(task, verify, ids) },
	}
}

// webAIRaceWith runs fn on race id once the laptop has read the race
// records (read off the UI goroutine the first time; the laptop's compare
// page does not open).
func (m *Model) webAIRaceWith(call *webCall, id string, fn func(m *Model, r *agentrace.Race) tea.Cmd) tea.Cmd {
	s := m.raceS()
	if s.dir != "" {
		r := s.find(id)
		if r == nil {
			call.fail(webproto.CodeNotFound, "That race is gone.")
			return nil
		}
		m.raceSyncTabs()
		return fn(m, r)
	}
	if !git.IsRepo(aiWorkspaceRoot()) {
		call.fail(webproto.CodeNotFound, "No race: this workspace is not a git repository.")
		return nil
	}
	load := raceLoadCmd(aiWorkspaceRoot())
	call.later()
	return func() tea.Msg {
		msg, _ := load().(raceLoadedMsg)
		return applyMsg(func(m *Model) tea.Cmd {
			if msg.err != nil || msg.dir == "" {
				call.fail(webproto.CodeInternal, "The laptop could not read its races.")
				return nil
			}
			m.raceAdopt(msg.races, msg.dir)
			return m.webAIRaceWith(call, id, fn)
		})
	}
}

// webAIRaceSummary asks the quick model for the race's difference
// summary and answers with it once stored.
func (m *Model) webAIRaceSummary(call *webCall) tea.Cmd {
	return m.webAIRaceWith(call, call.args.(*webproto.AIRaceArgs).Race, func(m *Model, r *agentrace.Race) tea.Cmd {
		n := 0
		for _, l := range r.Lanes {
			if l.Finished() || l.State == agentrace.Merged {
				n++
			}
		}
		if n < 2 {
			call.fail(webproto.CodeBusy, "A summary needs two finished lanes to compare.")
			return nil
		}
		if _, err := raceQuickSlot(m.aiS()); err != nil {
			call.fail(webproto.CodeBadRequest, "Set a quick model on the laptop (AI: Models) to summarise. Nothing was sent.")
			return nil
		}
		ask := m.raceSummary(r)
		if ask == nil {
			call.fail(webproto.CodeInternal, "Nothing was sent.")
			return nil
		}
		call.later()
		return func() tea.Msg {
			sm, _ := ask().(raceSummaryMsg)
			return applyMsg(func(m *Model) tea.Cmd {
				cmd := m.onRaceSummary(sm)
				switch {
				case sm.err != nil:
					call.fail(webproto.CodeInternal, "The summary failed: "+aiOneLine(ai.Redact(sm.err.Error())))
				case len(sm.lines) == 0:
					call.fail(webproto.CodeInternal, "The model gave no summary.")
				default:
					lines := make([]string, len(sm.lines))
					for i, l := range sm.lines {
						lines[i] = webName(l)
					}
					call.reply(webproto.AIRaceSummary{Race: sm.race, Lines: lines, Cost: webName(sm.cost)})
				}
				return cmd
			})
		}
	})
}

// webAIRaceVerify runs the race's verify command in its finished lanes.
func (m *Model) webAIRaceVerify(call *webCall) tea.Cmd {
	return m.webAIRaceWith(call, call.args.(*webproto.AIRaceArgs).Race, func(m *Model, r *agentrace.Race) tea.Cmd {
		if r.Verify == "" {
			call.fail(webproto.CodeBadRequest, "This race has no verify command. It is set when a race starts.")
			return nil
		}
		ready := 0
		for _, l := range r.Finished() {
			if l.Worktree != "" {
				ready++
			}
		}
		if ready == 0 {
			call.fail(webproto.CodeBusy, "No lane is done yet: verify runs in the finished lanes.")
			return nil
		}
		cmd := m.raceVerify(r)
		n := 0
		for _, l := range r.Finished() {
			if l.Verify.State == agentrace.VerifyRunning {
				n++
			}
		}
		if n == 0 {
			call.fail(webproto.CodeInternal, "The verify command could not start. The laptop says why.")
			return cmd
		}
		call.reply(webproto.AIRaceDone{Race: r.ID, Lanes: n, Message: fmt.Sprintf("Verifying %d lane%s.", n, plural(n))})
		return cmd
	})
}

// webAIRaceStop stops the race's working lanes (their work stays).
func (m *Model) webAIRaceStop(call *webCall) tea.Cmd {
	return m.webAIRaceWith(call, call.args.(*webproto.AIRaceArgs).Race, func(m *Model, r *agentrace.Race) tea.Cmd {
		n := 0
		for i := range r.Lanes {
			if raceActive(&r.Lanes[i]) {
				n++
			}
		}
		if n == 0 {
			call.fail(webproto.CodeBusy, "No lane is working.")
			return nil
		}
		cmd := m.raceStop(r)
		call.reply(webproto.AIRaceDone{Race: r.ID, Lanes: n, Message: fmt.Sprintf("Stopped %d lane%s. The work stays in the worktrees.", n, plural(n))})
		return cmd
	})
}
