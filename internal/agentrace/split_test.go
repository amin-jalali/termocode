package agentrace

import (
	"strings"
	"testing"
)

// Spec (CMP-4): a split is a race record of kind "split": one goal cut
// into 2–4 parts, each lane with its own part (one line, at most
// MaxPartChars), named split/<slug of the goal>. Lane ids are the race's
// (race-<n>-<k>-<agent>), so worktrees and numbering are shared with races.
func TestNewSplit(t *testing.T) {
	r, err := NewSplit(4, "  Add dark mode\nto settings ", "go test ./...", []LaneSpec{
		{Agent: "claude", Title: "Claude Code", Prompt: " the theme tokens\nin internal/theme "},
		{Agent: "codex", Title: "Codex", Prompt: "the settings toggle"},
	}, 9)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Split() || r.Kind != KindSplit || r.ID != "race-4" || r.Name != "split/add-dark-mode-to-settings" {
		t.Errorf("split %+v", r)
	}
	if r.Prompt != "Add dark mode to settings" || r.Verify != "go test ./..." || r.StartedMs != 9 {
		t.Errorf("goal %q verify %q at %d", r.Prompt, r.Verify, r.StartedMs)
	}
	if len(r.Lanes) != 2 || r.Lanes[0].ID != "race-4-1-claude" || r.Lanes[0].Prompt != "the theme tokens in internal/theme" ||
		r.Lanes[1].Prompt != "the settings toggle" || r.Lanes[1].State != Starting {
		t.Errorf("lanes %+v", r.Lanes)
	}
	for name, parts := range map[string][]LaneSpec{
		"one part":   {{Agent: "a", Prompt: "x"}},
		"five parts": {{Agent: "a", Prompt: "x"}, {Agent: "b", Prompt: "x"}, {Agent: "c", Prompt: "x"}, {Agent: "d", Prompt: "x"}, {Agent: "e", Prompt: "x"}},
		"empty part": {{Agent: "a", Prompt: "x"}, {Agent: "b", Prompt: "  "}},
		"long part":  {{Agent: "a", Prompt: "x"}, {Agent: "b", Prompt: strings.Repeat("y", MaxPartChars+1)}},
		"bad agent":  {{Agent: "a", Prompt: "x"}, {Agent: "../b", Prompt: "y"}},
	} {
		if _, err := NewSplit(1, "goal", "", parts, 0); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := NewSplit(1, "  ", "", []LaneSpec{{Agent: "a", Prompt: "x"}, {Agent: "b", Prompt: "y"}}, 0); err == nil {
		t.Error("an empty goal was taken")
	}
	// A plain race is not a split, and its lanes all get the race's task.
	race, _ := New(1, "task", "", []LaneSpec{{Agent: "a"}, {Agent: "b"}}, 0)
	if race.Split() || race.LaneTask(race.Lanes[1]) != "task" {
		t.Errorf("race split %v task %q", race.Split(), race.LaneTask(race.Lanes[1]))
	}
}

// Spec (CMP-4): each split lane is typed its own part, with the goal and
// a note that the other parts are done by other agents, as one line (an
// Enter would send it early).
func TestSplitLaneTask(t *testing.T) {
	r, _ := NewSplit(1, "Add dark mode", "", []LaneSpec{{Agent: "a", Prompt: "theme tokens"}, {Agent: "b", Prompt: "settings toggle"}}, 0)
	got := r.LaneTask(r.Lanes[1])
	for _, want := range []string{"Add dark mode", "part 2 of 2", "settings toggle", "other agents"} {
		if !strings.Contains(got, want) {
			t.Errorf("task has no %q: %q", want, got)
		}
	}
	if strings.Contains(got, "theme tokens") || strings.ContainsAny(got, "\r\n") {
		t.Errorf("task %q", got)
	}
}

// Spec (CMP-4): the status text is what the coordinator reads back: the
// goal, each lane's id, agent, state, part, files and +/- once read, its
// branch, and how many parts are done; it says the user merges each
// part on the compare page.
func TestSplitStatusText(t *testing.T) {
	r, _ := NewSplit(2, "Add dark mode", "", []LaneSpec{{Agent: "claude", Title: "Claude Code", Prompt: "theme tokens"},
		{Agent: "codex", Title: "Codex", Prompt: "settings toggle"}}, 0)
	r.Lanes[0].State, r.Lanes[0].Stats, r.Lanes[0].Branch = Done, true, "termocode/agent-race-2-1-claude"
	r.Lanes[0].Files = []FileStat{{Status: "M", Path: "theme.go", Add: 4, Del: 1}}
	r.Lanes[1].State = Working
	got := StatusText(r)
	for _, want := range []string{"split/add-dark-mode", "Add dark mode", "race-2-1-claude", "Claude Code", "done", "theme tokens",
		"1 file +4 -1", "termocode/agent-race-2-1-claude", "race-2-2-codex", "working", "settings toggle", "1 of 2 parts done", "Race Compare"} {
		if !strings.Contains(got, want) {
			t.Errorf("status has no %q:\n%s", want, got)
		}
	}
}
