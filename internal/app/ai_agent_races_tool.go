package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
)

// agent_races: the AI does what the race compare page does for a race
// or a coordinator's split (AI parity, the web's ai.race_summary,
// ai.race_verify and ai.race_stop): list the races with their lanes,
// summarise the finished lanes' differences (the quick model's tokens,
// as s does), run the race's verify command in the finished lanes (v)
// and stop the working lanes (x; the work stays in their worktrees).
// None of them has a card in the GUI, so none asks. The page's lane
// keys too: merge a finished lane into the user's files (m: no card; a
// conflict writes nothing and the user answers the conflict card),
// discard a lane (d: its card asks, so the AI asks every time), restack
// a lane on HEAD (its card asks; squash takes the commit message the
// user would type) and judge the lanes (j: the main model ranks them, no
// card). Starting a race stays agent_race_start (it asks).

var aiAgentRacesOps = []string{"discard", "judge", "list", "merge", "restack", "stop", "summary", "verify"}

// aiAgentRacesAsk are the ops whose card asks every time.
var aiAgentRacesAsk = map[string]bool{"discard": true, "restack": true}

func aiAgentRacesCheck(a map[string]any) error {
	op := ai.ArgString(a, "op")
	if !contains(aiAgentRacesOps, op) {
		return errors.New("give op: " + strings.Join(aiAgentRacesOps, ", "))
	}
	if op != "list" && strings.TrimSpace(ai.ArgString(a, "race")) == "" {
		return errors.New("give race: a race id from op list")
	}
	switch op {
	case "merge", "discard", "restack":
		if strings.TrimSpace(ai.ArgString(a, "lane")) == "" {
			return errors.New("give lane: a lane id from op list")
		}
	}
	if op == "restack" {
		if m := ai.ArgString(a, "mode"); m != "keep" && m != "squash" {
			return errors.New("give mode: keep (the lane's commits) or squash (one commit; give message)")
		}
	}
	return nil
}

// aiAgentRaceText is one race for the model.
func aiAgentRaceText(r agentrace.Race) string {
	var b strings.Builder
	kind := "race"
	if r.Kind != "" {
		kind = r.Kind
	}
	fmt.Fprintf(&b, "%s · %s · %q", r.ID, kind, aiCardText(ai.Redact(r.Prompt), 100))
	if r.Verify != "" {
		b.WriteString(" · verify: " + aiCardText(r.Verify, 60))
	}
	if r.Winner != "" {
		b.WriteString(" · merged: " + r.Winner)
	}
	b.WriteString("\n")
	for _, l := range r.Lanes {
		files, add, del := l.Totals()
		fmt.Fprintf(&b, "  %s · %s", l.ID, l.Agent)
		if l.Model != "" {
			b.WriteString(" (" + l.Model + ")")
		}
		fmt.Fprintf(&b, " · %s", l.State)
		if l.Stats {
			fmt.Fprintf(&b, " · %d file(s) +%d -%d", files, add, del)
		}
		if l.Verify.State != "" {
			b.WriteString(" · verify " + l.Verify.State)
		}
		if l.Note != "" {
			b.WriteString(" · " + aiCardText(l.Note, 60))
		}
		b.WriteString("\n")
	}
	for _, s := range r.Summary {
		b.WriteString("  summary: " + aiCardText(s, 200) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// aiAgentRacesTool is agent_races.
func aiAgentRacesTool(call aiIDECaller) ai.Tool {
	// with runs fn on the race records once the UI has read them.
	with := func(ctx context.Context, fn func(m *Model) (string, tea.Cmd, error)) (string, error) {
		if call == nil {
			return "", errors.New("the races are not available")
		}
		var root string
		loaded := false
		if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			root, loaded = aiWorkspaceRoot(), m.raceS().dir != ""
			return "", nil, nil
		}); err != nil {
			return "", err
		}
		if !loaded {
			if !git.IsRepo(root) {
				return "", errors.New("no races: this workspace is not a git repository")
			}
			msg, _ := raceLoadCmd(root)().(raceLoadedMsg)
			if msg.err != nil {
				return "", errors.New("the races could not be read: " + agentCtlShort(msg.err.Error()))
			}
			if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) { m.raceAdopt(msg.races, msg.dir); return "", nil, nil }); err != nil {
				return "", err
			}
		}
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			m.raceSyncTabs()
			return fn(m)
		})
	}
	return ai.Tool{
		Name:    "agent_races",
		Display: "agent races",
		Description: "The agent races and splits of this repository (the race compare page). op list (each race's lanes, " +
			"their state, changed files and verify result), summary (race: the quick model compares the finished lanes; it " +
			"costs a little AI money), judge (race: the main model ranks the finished lanes; it costs AI money), verify (race: run " +
			"its verify command in the finished lanes), stop (race: Ctrl+C into the working lanes; their work stays) and merge " +
			"(race, lane: write a finished lane into the user's files; a conflict writes nothing and the user chooses) run at once; " +
			"discard (race, lane) and restack (race, lane, mode keep|squash, message) ask the user every time. agent_race_start starts one (it asks).",
		Params: map[string]any{
			"op":      map[string]any{"type": "string", "enum": aiAgentRacesOps, "description": "what to do"},
			"race":    map[string]any{"type": "string", "description": "every op but list: the race id from op list"},
			"lane":    map[string]any{"type": "string", "description": "merge, discard, restack: the lane id"},
			"mode":    map[string]any{"type": "string", "enum": []string{"keep", "squash"}, "description": "restack: keep the commits or squash them into one"},
			"message": map[string]any{"type": "string", "description": "restack squash: the commit message (default: the task)"},
		},
		Required: []string{"op"},
		Validate: aiAgentRacesCheck,
		Gate:     func(a map[string]any) (bool, string) { return aiAgentRacesAsk[ai.ArgString(a, "op")], "" },
		Describe: func(a map[string]any) string {
			lane := aiCardText(ai.ArgString(a, "lane"), 40)
			switch ai.ArgString(a, "op") {
			case "discard":
				return "Discard the lane " + lane + "?\nDiscard removes its worktree and branch. Your files do not change. This cannot be undone."
			case "restack":
				return "Restack the lane " + lane + " (" + ai.ArgString(a, "mode") + ")?\nIt puts the lane's work on top of your current commit; " +
					"your files, index and branch do not change. If it clashes, nothing changes."
			}
			if op := ai.ArgString(a, "op"); op != "list" {
				return "Race " + aiCardText(ai.ArgString(a, "race"), 30) + ": " + op
			}
			return "List the agent races"
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			if err := aiAgentRacesCheck(a); err != nil {
				return "", err
			}
			id := strings.TrimSpace(ai.ArgString(a, "race"))
			find := func(m *Model) (*agentrace.Race, error) {
				if r := m.raceS().find(id); r != nil {
					return r, nil
				}
				return nil, fmt.Errorf("no race %q (op list shows them)", aiCardText(id, 30))
			}
			switch ai.ArgString(a, "op") {
			case "list":
				return with(ctx, func(m *Model) (string, tea.Cmd, error) {
					rs := append([]agentrace.Race(nil), m.raceS().races...)
					if len(rs) == 0 {
						return "No races in this repository.", nil, nil
					}
					sort.SliceStable(rs, func(i, j int) bool { return rs[i].StartedMs > rs[j].StartedMs })
					var parts []string
					for i, r := range rs {
						if i == 20 {
							parts = append(parts, fmt.Sprintf("(%d older races not shown)", len(rs)-i))
							break
						}
						parts = append(parts, aiAgentRaceText(r))
					}
					return strings.Join(parts, "\n"), nil, nil
				})
			case "stop":
				return with(ctx, func(m *Model) (string, tea.Cmd, error) {
					r, err := find(m)
					if err != nil {
						return "", nil, err
					}
					n := 0
					for i := range r.Lanes {
						if raceActive(&r.Lanes[i]) {
							n++
						}
					}
					if n == 0 {
						return "", nil, errors.New("no lane of " + id + " is working")
					}
					return fmt.Sprintf("Stopped %d lane(s) of %s. The work stays in the worktrees.", n, id), m.raceStop(r), nil
				})
			case "verify":
				return with(ctx, func(m *Model) (string, tea.Cmd, error) {
					r, err := find(m)
					if err != nil {
						return "", nil, err
					}
					if r.Verify == "" {
						return "", nil, errors.New("this race has no verify command; it is set when a race starts")
					}
					cmd := m.raceVerify(r)
					n := 0
					for _, l := range r.Finished() {
						if l.Verify.State == agentrace.VerifyRunning {
							n++
						}
					}
					if n == 0 {
						return "", cmd, errors.New("no finished lane to verify yet")
					}
					return fmt.Sprintf("Verifying %d lane(s) of %s; op list shows each result when it ends.", n, id), cmd, nil
				})
			case "merge", "discard", "restack", "judge":
				return aiAgentRaceLaneOp(ctx, call, with, a)
			case "summary":
				var ask tea.Cmd
				if _, err := with(ctx, func(m *Model) (string, tea.Cmd, error) {
					r, err := find(m)
					if err != nil {
						return "", nil, err
					}
					n := 0
					for _, l := range r.Lanes {
						if l.Finished() || l.State == agentrace.Merged {
							n++
						}
					}
					if n < 2 {
						return "", nil, errors.New("a summary needs two finished lanes to compare")
					}
					if _, err := raceQuickSlot(m.aiS()); err != nil {
						return "", nil, errors.New("no quick model is set (AI: Models); nothing was sent")
					}
					if ask = m.raceSummary(r); ask == nil {
						return "", nil, errors.New("nothing was sent")
					}
					return "", nil, nil
				}); err != nil {
					return "", err
				}
				sm, _ := ask().(raceSummaryMsg)
				_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) { return "", m.onRaceSummary(sm), nil })
				switch {
				case sm.err != nil:
					return "", errors.New("the summary failed: " + aiOneLine(ai.Redact(sm.err.Error())))
				case len(sm.lines) == 0:
					return "", errors.New("the model gave no summary")
				}
				return strings.Join(sm.lines, "\n") + "\n(" + sm.cost + ")", nil
			}
			return "", errors.New("unknown op")
		},
	}
}

// aiAgentRaceLaneOp runs merge, discard, restack or judge: the page's own
// command off the UI goroutine, its result through the page's handler.
func aiAgentRaceLaneOp(ctx context.Context, call aiIDECaller, with func(context.Context, func(m *Model) (string, tea.Cmd, error)) (string, error), a map[string]any) (string, error) {
	op, id, laneID := ai.ArgString(a, "op"), strings.TrimSpace(ai.ArgString(a, "race")), strings.TrimSpace(ai.ArgString(a, "lane"))
	var work tea.Cmd
	if _, err := with(ctx, func(m *Model) (string, tea.Cmd, error) {
		r := m.raceS().find(id)
		if r == nil {
			return "", nil, fmt.Errorf("no race %q (op list shows them)", aiCardText(id, 30))
		}
		root := aiWorkspaceRoot()
		if op == "judge" {
			if len(agentrace.JudgeCandidates(*r)) < 2 {
				return "", nil, errors.New("the judge needs two finished lanes to compare")
			}
			if _, err := raceJudgeSlot(m.aiS()); err != nil {
				return "", nil, errors.New("no main model is set (AI: Models); nothing was sent")
			}
			work = m.raceJudge(r)
			return "", nil, nil
		}
		l := r.Lane(laneID)
		if l == nil || !l.Finished() {
			return "", nil, fmt.Errorf("lane %q of %s is not a finished lane", aiCardText(laneID, 30), id)
		}
		switch op {
		case "merge":
			work = raceMergeCmd(root, r.ID, l.ID, false)
		case "discard":
			work = raceDiscardCmd(root, r.ID, []string{l.ID}, false)
		case "restack":
			opt := git.AgentRestackOptions{}
			if ai.ArgString(a, "mode") == "squash" {
				msg := strings.TrimSpace(ai.ArgString(a, "message"))
				if msg == "" {
					msg = raceRestackDefaultMsg(r.Prompt)
				}
				opt = git.AgentRestackOptions{Squash: true, Message: msg}
			}
			work = raceRestackCmd(root, r.ID, l.ID, opt)
		}
		return "", nil, nil
	}); err != nil {
		return "", err
	}
	if work == nil {
		return "", errors.New("nothing was done")
	}
	msg := work()
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		cmd, _ := m.handleRaceMsg(msg)
		return "", cmd, nil
	}); err != nil {
		return "", err
	}
	switch r := msg.(type) {
	case raceMergedMsg:
		switch {
		case r.err != nil:
			return "", errors.New("merge failed; the lane's work is safe on its branch: " + agentCtlShort(r.err.Error()))
		case len(r.res.Conflicts) > 0:
			return "Nothing was written: the user changed " + strings.Join(r.res.Conflicts, ", ") + " too. The user chooses on the conflict card.", nil
		}
		return fmt.Sprintf("Merged %d file(s) of lane %s into the user's tree: %s", len(r.res.Applied), laneID, strings.Join(r.res.Applied, ", ")), nil
	case raceDiscardedMsg:
		if err := r.errs[laneID]; err != nil {
			return "", errors.New("could not discard the lane: " + agentCtlShort(err.Error()))
		}
		return "Discarded lane " + laneID + "; the user's files did not change.", nil
	case raceRestackedMsg:
		switch {
		case r.err != nil:
			return "", errors.New("restack failed: " + agentCtlShort(r.err.Error()))
		case len(r.res.Conflicts) > 0:
			return "Nothing changed: the lane clashes with HEAD in " + strings.Join(r.res.Conflicts, ", ") + ".", nil
		}
		return fmt.Sprintf("Restacked lane %s on %s: %d commit(s).", laneID, aiCardText(r.res.Onto, 12), r.res.After), nil
	case raceJudgeMsg:
		if r.err != nil {
			return "", errors.New("the judge failed: " + aiOneLine(ai.Redact(r.err.Error())))
		}
		var b strings.Builder
		for _, rk := range r.judge.Ranks {
			b.WriteString(rk.Lane + ": " + rk.Reason + "\n")
		}
		if r.judge.Pick != "" {
			b.WriteString("pick: " + r.judge.Pick + " (the user merges it from the page's card)\n")
		}
		return strings.TrimRight(b.String(), "\n") + " (" + r.judge.Cost + ")", nil
	}
	return "done", nil
}
