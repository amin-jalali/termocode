package agentrace

import (
	"strings"
	"testing"
)

// Spec (CMP-18): a fan-out count says how many lanes run each agent,
// written "claude:3,codex:2" (commas or spaces; a bare id is one lane).
// Counts are 1 to MaxLanes, the total is at most MaxLanes, ids are words.
func TestParseFanout(t *testing.T) {
	for spec, want := range map[string]string{
		"claude:3,codex:2":   "claude claude claude codex codex",
		" claude:2 , codex ": "claude claude codex",
		"claude codex:1":     "claude codex",
		"claude,claude":      "claude claude",
	} {
		got, err := ParseFanout(spec)
		if err != nil || strings.Join(got, " ") != want {
			t.Errorf("%q: %v %v, want %s", spec, got, err, want)
		}
	}
	for _, bad := range []string{"", "  , ", "claude:0", "claude:x", "claude:9", "claude:5,codex:4", "../x:2", "claude:", ":2"} {
		if got, err := ParseFanout(bad); err == nil {
			t.Errorf("%q: no error (%v)", bad, got)
		}
	}
}

// Spec (CMP-18): the fan-out text is the counts in first-seen order; a
// count of one is the bare id.
func TestFanoutText(t *testing.T) {
	if got := FanoutText([]string{"claude", "codex", "claude", "claude", "aider"}); got != "claude:3,codex,aider" {
		t.Errorf("text %q", got)
	}
	cs := Counts([]string{"codex", "claude", "codex"})
	if len(cs) != 2 || cs[0] != (Count{Agent: "codex", N: 2}) || cs[1] != (Count{Agent: "claude", N: 1}) {
		t.Errorf("counts %+v", cs)
	}
	if FanoutText(nil) != "" {
		t.Error("no lanes: not empty")
	}
}

// Spec (CMP-18): a race has up to 8 lanes; an agent that runs more than
// once gets a number in each of its lane titles, so the compare page tells
// them apart; an agent that runs once keeps its title.
func TestNewFanoutRace(t *testing.T) {
	var specs []LaneSpec
	for i := 0; i < 5; i++ {
		specs = append(specs, LaneSpec{Agent: "claude", Title: "Claude Code"})
	}
	specs = append(specs, LaneSpec{Agent: "codex", Title: "Codex"}, LaneSpec{Agent: "codex", Title: "Codex"}, LaneSpec{Agent: "aider", Title: "Aider"})
	r, err := New(2, "task", "", specs, 0)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, l := range r.Lanes {
		titles = append(titles, l.Title)
	}
	want := "Claude Code #1|Claude Code #2|Claude Code #3|Claude Code #4|Claude Code #5|Codex #1|Codex #2|Aider"
	if got := strings.Join(titles, "|"); got != want {
		t.Errorf("titles %s", got)
	}
	if r.Lanes[7].ID != "race-2-8-aider" {
		t.Errorf("last lane id %s", r.Lanes[7].ID)
	}
	if _, err := New(2, "task", "", append(specs, LaneSpec{Agent: "aider"}), 0); err == nil {
		t.Error("nine lanes: no error")
	}
}
