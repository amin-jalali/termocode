package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
)

// agent_runs: the AI does what the Runs view and the agent worktrees do
// (AI parity), over every chat's runs, not only the ones it started:
// list the runs of the last week, read one run's step answers, pause a
// live run before its next step and let a paused one go on (a stopped
// run resumes only as its chat's last turn: the user does that), and merge or discard the
// work of an agent step's own git worktree.
//
// It asks exactly where the GUI asks: Merge and Discard are cards on the
// page (ai.worktree_merge, ai.worktree_discard; the laptop asks Discard
// too), so they ask every time with no "Always allow"; a merge that meets
// the user's own changes writes nothing and the user answers the conflict
// card, as on the page. list, output, pause (p) and resume (r) have no
// card in the Runs view and run at once. Stop stays agent_run_stop: the
// Runs view asks before it stops a run ("Keep running" first).

// aiAgentRunsOps are the ops; true: the op asks every time.
var aiAgentRunsOps = map[string]bool{
	"list": false, "output": false, "pause": false, "resume": false,
	"merge": true, "discard": true,
}

func aiAgentRunsOpList() []string {
	ops := make([]string, 0, len(aiAgentRunsOps))
	for op := range aiAgentRunsOps {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	return ops
}

// aiAgentRunsMax caps the list and the text of one step.
const (
	aiAgentRunsMax     = 40
	aiAgentRunsStepMax = 4000
)

func aiAgentRunsCheck(a map[string]any) error {
	op := ai.ArgString(a, "op")
	if _, ok := aiAgentRunsOps[op]; !ok {
		return errors.New("give op: " + strings.Join(aiAgentRunsOpList(), ", "))
	}
	switch op {
	case "output", "pause", "resume":
		if strings.TrimSpace(ai.ArgString(a, "key")) == "" {
			return errors.New("give key: the run's key from op list")
		}
	case "merge", "discard":
		if !aiAgentWorktreeIDOK(strings.TrimSpace(ai.ArgString(a, "worktree"))) {
			return errors.New("give worktree: an agent worktree id (worktrees_list shows them)")
		}
	}
	return nil
}

// aiAgentRunsKind is a run's state word.
func aiAgentRunsKind(k runsKind) string {
	if w, ok := webRunsState[k]; ok {
		return w
	}
	return "unknown"
}

// aiAgentRunsLine is one run of the list.
func aiAgentRunsLine(it runsItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s · %s · %q", it.key, aiAgentRunsKind(it.kind), it.run.Profile, aiCardText(it.task, 80))
	if it.chatTitle != "" {
		fmt.Fprintf(&b, " · chat %q", aiCardText(it.chatTitle, 40))
	}
	if it.open {
		b.WriteString(" (this chat)")
	}
	fmt.Fprintf(&b, " · %d step(s)", len(it.steps))
	if it.cur >= 0 && it.cur < len(it.steps) && it.kind.live() {
		fmt.Fprintf(&b, ", now %d %s", it.cur+1, aiCardText(it.steps[it.cur].Title, 30))
	}
	if it.cost > 0 {
		b.WriteString(" · " + aiDollars(it.cost))
	}
	if it.why != "" {
		b.WriteString(" · " + aiCardText(it.why, 80))
	}
	if aiRunByChip(&it.run) != "" {
		b.WriteString(" · by AI")
	}
	return b.String()
}

// aiAgentRunsOutput is one run in full: each step's state and answer.
func aiAgentRunsOutput(it runsItem) string {
	var b strings.Builder
	b.WriteString(aiAgentRunsLine(it) + "\n")
	for i, st := range it.steps {
		fmt.Fprintf(&b, "\nStep %d %s (%s", i+1, aiCleanText(st.Title), st.State)
		if st.Model != "" {
			b.WriteString(", " + st.Model)
		}
		b.WriteString(")")
		if st.Worktree != nil && st.Worktree.Open() {
			b.WriteString(" · worktree " + st.Worktree.ID + " on " + st.Worktree.Branch + " waits for merge or discard")
		}
		b.WriteString("\n")
		if st.Error != "" {
			b.WriteString("error: " + aiRunClip(st.Error, 300) + "\n")
		}
		if t := strings.TrimSpace(st.Text); t != "" && agentdag.State(st.State) != agentdag.StatePending {
			b.WriteString(aiRunClip(ai.Redact(t), aiAgentRunsStepMax) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// aiAgentRunsFind is the run with key.
func (m *Model) aiAgentRunsFind(key string) (runsItem, error) {
	for _, it := range m.runsCollect(runsNow()) {
		if it.key == key {
			return it, nil
		}
	}
	return runsItem{}, fmt.Errorf("no run %q in the last week (op list shows the keys)", aiCardText(key, 60))
}

// aiAgentRunsResume lets a paused run go on (the Runs view's r). A
// stopped run resumes only as the last turn of its chat, and while the AI
// answers its own turn is the last one: the user resumes it (r).
func (m *Model) aiAgentRunsResume(it runsItem) (string, tea.Cmd, error) {
	switch {
	case (it.kind == runsPaused || it.kind == runsPausing) && it.open && m.aiS().run != nil:
		return "The run goes on.", m.runsResume(it), nil
	case it.kind == runsPaused || it.kind == runsPausing:
		return "", nil, errors.New("this paused run is in another chat; the user resumes it there (the Runs view, r)")
	case it.kind.live():
		return "", nil, errors.New("this run is going; nothing to resume")
	case it.kind == runsResumable:
		return "", nil, errors.New("a stopped run resumes only as the last turn of its chat, and your answer is that turn now: " +
			"tell the user to press r on it in the Runs view, or start the steps left with agent_run_start")
	}
	return "", nil, errors.New("this run cannot resume: " + runsNoResumeWhy(it))
}

// aiAgentRunsWorktree finds the agent worktree id for Merge or Discard:
// a step of this chat owns it, or it is left over.
func (m *Model) aiAgentRunsWorktree(id string) (aiWorktreeRef, error) {
	s := m.aiS()
	key := "left:" + id
	if st, ok := s.webAIWorktreeSteps()[id]; ok {
		if st.running {
			return aiWorktreeRef{}, errors.New("its step still runs; wait until it ends")
		}
		key = st.key
	} else {
		left, err := git.LeftoverAgentWorktrees(aiWorkspaceRoot())
		found := false
		for _, wt := range left {
			found = found || wt.ID == id
		}
		if err != nil || !found {
			return aiWorktreeRef{}, fmt.Errorf("no agent worktree %s (worktrees_list shows them)", id)
		}
		m.webAIWorktreeLeft(key, id)
	}
	r, ok := s.aiWorktreeRef(key)
	if !ok {
		return r, fmt.Errorf("the worktree %s is closed already", id)
	}
	return r, nil
}

// aiAgentWorktreeIDOK: an agent worktree id (git.AgentWorktreeID: lower
// case letters, digits and dashes).
func aiAgentWorktreeIDOK(id string) bool {
	if id == "" || len(id) > 120 {
		return false
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// aiAgentRunsTool is agent_runs.
func aiAgentRunsTool(call aiIDECaller) ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	ui := func(ctx context.Context, fn func(m *Model) (string, tea.Cmd, error)) (string, error) {
		if call == nil {
			return "", errors.New("the Runs view is not available")
		}
		return call(ctx, fn)
	}
	return ai.Tool{
		Name:    "agent_runs",
		Display: "agent runs",
		Description: "The agent runs of every chat (the Runs view) and the agent worktrees. op list (runs of the last week " +
			"with their key and state), output (key: each step's answer), pause (key: before its next step) and resume (key: a " +
			"paused run of this chat) run at once; " +
			"merge (worktree: write an agent step's work into the user's files) and discard (worktree: remove it and its branch) " +
			"ask the user every time. agent_run_stop stops a run.",
		Params: map[string]any{
			"op":       map[string]any{"type": "string", "enum": aiAgentRunsOpList(), "description": "what to do"},
			"key":      str("output, pause, resume: the run's key from op list"),
			"worktree": str("merge, discard: the agent worktree id (worktrees_list)"),
		},
		Required: []string{"op"},
		Validate: aiAgentRunsCheck,
		Describe: func(a map[string]any) string {
			if err := aiAgentRunsCheck(a); err != nil {
				return "agent_runs (" + aiCardText(err.Error(), 80) + ")"
			}
			id := strings.TrimSpace(ai.ArgString(a, "worktree"))
			branch := git.AgentBranchPrefix + id
			switch ai.ArgString(a, "op") {
			case "merge":
				return "Merge the agent's work?\nMerge writes the files the agent changed on branch " + branch +
					" into your tree. If you changed one of them too, nothing is written and you choose on the conflict card."
			case "discard":
				return "Discard the agent's work?\nDiscard removes the agent's worktree and the branch " + branch +
					". Your files do not change. This cannot be undone."
			case "pause":
				return "Pause the run " + aiCardText(ai.ArgString(a, "key"), 60) + " before its next step"
			case "resume":
				return "Resume the run " + aiCardText(ai.ArgString(a, "key"), 60)
			case "output":
				return "Read the run " + aiCardText(ai.ArgString(a, "key"), 60)
			}
			return "List the agent runs"
		},
		Gate: func(a map[string]any) (bool, string) {
			ask, ok := aiAgentRunsOps[ai.ArgString(a, "op")]
			return ask || !ok, ""
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			if err := aiAgentRunsCheck(a); err != nil {
				return "", err
			}
			key := strings.TrimSpace(ai.ArgString(a, "key"))
			id := strings.TrimSpace(ai.ArgString(a, "worktree"))
			switch op := ai.ArgString(a, "op"); op {
			case "list":
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					items := runsNest(m.runsCollect(runsNow()))
					if len(items) == 0 {
						return "No agent runs in the last week.", nil, nil
					}
					var b strings.Builder
					for i, it := range items {
						if i == aiAgentRunsMax {
							fmt.Fprintf(&b, "(%d more not shown)\n", len(items)-i)
							break
						}
						b.WriteString(strings.Repeat("  ", it.depth) + aiAgentRunsLine(it) + "\n")
					}
					return strings.TrimRight(b.String(), "\n"), nil, nil
				})
			case "output":
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					it, err := m.aiAgentRunsFind(key)
					if err != nil {
						return "", nil, err
					}
					return aiAgentRunsOutput(it), nil, nil
				})
			case "pause":
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					it, err := m.aiAgentRunsFind(key)
					switch {
					case err != nil:
						return "", nil, err
					case it.kind == runsPaused || it.kind == runsPausing:
						return "", nil, errors.New("this run is paused already (op resume goes on)")
					case it.kind != runsRunning && it.kind != runsStalled:
						return "", nil, errors.New("only a run that is going can pause")
					case !it.open || m.aiS().run == nil:
						return "", nil, errors.New("only the open chat's run in front can pause (a background run stops with agent_run_stop)")
					}
					return "The run pauses after the step that works now (op resume goes on).", m.runsPause(it), nil
				})
			case "resume":
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					it, err := m.aiAgentRunsFind(key)
					if err != nil {
						return "", nil, err
					}
					return m.aiAgentRunsResume(it)
				})
			case "merge", "discard":
				var r aiWorktreeRef
				if _, err := ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					var err error
					if r, err = m.aiAgentRunsWorktree(id); err != nil {
						return "", nil, err
					}
					if c := r.wt.Critic; op == "merge" && c != nil && c.State == ai.CriticIssues {
						return "", nil, errors.New("the critic found issues in this work; the user merges it from its card after reading them")
					}
					return "", nil, nil
				}); err != nil {
					return "", err
				}
				root, wt := aiWorkspaceRoot(), *r.wt
				if op == "discard" {
					msg := aiWorktreeDiscardedMsg{key: r.key, err: git.RemoveAgentWorktree(root, wt.ID, true)}
					_, _ = ui(ctx, func(m *Model) (string, tea.Cmd, error) { return "", m.onAIWorktreeDiscarded(msg), nil })
					if msg.err != nil {
						return "", errors.New("could not discard the worktree: " + agentCtlShort(msg.err.Error()))
					}
					return "Discarded the worktree " + wt.ID + " and its branch " + wt.Branch + "; the user's files did not change.", nil
				}
				msg, _ := aiWorktreeMergeCmd(root, r.key, wt, false)().(aiWorktreeMergedMsg)
				_, _ = ui(ctx, func(m *Model) (string, tea.Cmd, error) { return "", m.onAIWorktreeMerged(msg), nil })
				switch {
				case msg.err != nil:
					return "", errors.New("merge failed; the agent's work is safe on " + wt.Branch + ": " + agentCtlShort(msg.err.Error()))
				case len(msg.res.Conflicts) > 0:
					return "Nothing was written: the user changed " + strings.Join(msg.res.Conflicts, ", ") +
						" too. The user chooses on the conflict card (merge with markers, or keep the work on branch " + wt.Branch + ").", nil
				}
				return fmt.Sprintf("Merged %d file(s) from %s into the user's tree: %s", len(msg.res.Applied), wt.Branch, strings.Join(msg.res.Applied, ", ")), nil
			}
			return "", errors.New("unknown op")
		},
	}
}
