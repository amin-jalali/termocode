//go:build unix

package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/agents"
)

// Spec (MOBUX-8): the phone's task action sheet offers "Many agents", a
// race of 2–4 lanes. The ai topic lists the agents a race may use: the
// ones installed on the laptop whose screen termocode can read, by id and
// title, in the registry's order. An agent that is not installed is not
// listed; nothing else about the agent (its path, its login) is sent.
func TestWebAITopicRaceAgents(t *testing.T) {
	fakeAgentLookPath(t, "claude", "codex", "aider")
	m := webAITopicModel(t)
	s := m.webAIBuild()
	var want []string
	for _, a := range agents.Registry {
		if agents.HasScreenRules(a.ID) && (a.Bin == "claude" || a.Bin == "codex" || a.Bin == "aider") {
			want = append(want, a.ID)
		}
	}
	if len(s.RaceAgents) != len(want) {
		t.Fatalf("race agents %+v, want ids %v", s.RaceAgents, want)
	}
	for i, a := range s.RaceAgents {
		if a.ID != want[i] {
			t.Errorf("race agent %d = %q, want %q", i, a.ID, want[i])
		}
		ag, ok := agents.ByID(a.ID)
		if !ok || a.Title != ag.Title {
			t.Errorf("race agent %q title %q", a.ID, a.Title)
		}
	}
}

// No agent installed: the list is empty, and the sheet says so.
func TestWebAITopicRaceAgentsNone(t *testing.T) {
	fakeAgentLookPath(t)
	m := webAITopicModel(t)
	if s := m.webAIBuild(); len(s.RaceAgents) != 0 {
		t.Fatalf("race agents %+v", s.RaceAgents)
	}
}
