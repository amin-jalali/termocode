package agentrace

import (
	"errors"
	"fmt"
	"strings"
)

// ── Split (CMP-4) ────────────────────────────────────────────────────────
//
// A split is the coordinator's record: one goal cut into 2–4 parts, each
// part one lane (an outside agent in its own agent worktree, exactly as a
// race lane). It shares the race's file, numbering and lane ids; only the
// kind, the lanes' own parts and the merge rule differ: every part may be
// merged (there is no winner).

// KindSplit is Race.Kind of a split ("" is a race).
const KindSplit = "split"

// MaxPartChars caps one part's task.
const MaxPartChars = 2000

// Split reports whether r is a split.
func (r Race) Split() bool { return r.Kind == KindSplit }

// NewSplit makes split number n of goal: one lane per part, each with its
// own task (one line). 2–4 parts.
func NewSplit(n int, goal, verify string, parts []LaneSpec, nowMs int64) (Race, error) {
	goal = strings.Join(strings.Fields(goal), " ")
	if goal == "" {
		return Race{}, errors.New("say the goal the parts add up to")
	}
	if len(parts) < MinLanes || len(parts) > MaxLanes {
		return Race{}, fmt.Errorf("a split needs %d to %d parts", MinLanes, MaxLanes)
	}
	r := Race{ID: fmt.Sprintf("race-%d", n), Number: n, Kind: KindSplit, Name: "split/" + Slug(goal), Prompt: goal,
		Verify: strings.TrimSpace(verify), StartedMs: nowMs}
	for k, p := range parts {
		task := strings.Join(strings.Fields(p.Prompt), " ")
		switch {
		case !word(p.Agent):
			return Race{}, fmt.Errorf("agent %q is not known", p.Agent)
		case task == "":
			return Race{}, fmt.Errorf("part %d has no task", k+1)
		case len([]rune(task)) > MaxPartChars:
			return Race{}, fmt.Errorf("part %d is longer than %d characters", k+1, MaxPartChars)
		}
		r.Lanes = append(r.Lanes, Lane{ID: LaneID(n, k+1, p.Agent), Agent: p.Agent, Title: p.Title, Prompt: task, State: Starting})
	}
	return r, nil
}

// LaneTask is the one line typed into lane l: the race's task, or for a
// split the goal, which part this is and the part itself.
func (r Race) LaneTask(l Lane) string {
	if !r.Split() || l.Prompt == "" {
		return r.Prompt
	}
	k, n := 0, len(r.Lanes)
	for i := range r.Lanes {
		if r.Lanes[i].ID == l.ID {
			k = i + 1
		}
	}
	return fmt.Sprintf("Overall goal: %s. You do part %d of %d: %s. The other parts are done by other agents in their own copies of the repository, "+
		"so change only what your part needs and say briefly what you did when you are done.", r.Prompt, k, n, l.Prompt)
}

// StatusText is a split as the coordinator reads it back: the goal, then
// per lane its id, agent, state, part, files and branch.
func StatusText(r Race) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Split %s (%s): %s\n", r.ID, r.Name, r.Prompt)
	done := 0
	for i, l := range r.Lanes {
		if l.Finished() || l.State == Merged {
			done++
		}
		fmt.Fprintf(&b, "\n%d. %s [%s] %s", i+1, l.Title, l.ID, l.State)
		if l.Note != "" {
			fmt.Fprintf(&b, " (%s)", l.Note)
		}
		fmt.Fprintf(&b, "\n   part: %s\n", l.Prompt)
		if l.Stats {
			files, add, del := l.Totals()
			word := "files"
			if files == 1 {
				word = "file"
			}
			fmt.Fprintf(&b, "   %d %s +%d -%d", files, word, add, del)
			for _, f := range l.Files {
				fmt.Fprintf(&b, "\n     %s %s +%d -%d", f.Status, f.Path, f.Add, f.Del)
			}
			b.WriteString("\n")
		}
		if l.Branch != "" && (!l.Closed() || l.Note != "") { // a closed lane with a note kept its branch
			fmt.Fprintf(&b, "   branch %s\n", l.Branch)
		}
	}
	fmt.Fprintf(&b, "\n%d of %d parts done. The user reviews and merges each part on Agent: Race Compare (m per lane); "+
		"nothing reaches the user's files before that.\n", done, len(r.Lanes))
	return b.String()
}
