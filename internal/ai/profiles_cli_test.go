package ai

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
)

// CS-1: a profile step may run an official CLI with the user's
// subscription login ("cli": "claude" | "codex") instead of a model slot.
func TestParseProfilesCLISteps(t *testing.T) {
	data := `{"profiles": {"sub": {"steps": [
  {"id": "work", "cli": "claude", "tools": {"preset": "all"}},
  {"id": "review", "cli": "codex"}
], "loop": {"verify": "review", "fix": {"id": "fix", "cli": "claude", "tools": {"preset": "all"}}}}}}`
	ps, err := ParseProfiles([]byte(data), "profiles.json", ProfileSourceUser, testSlots, 1)
	if err != nil {
		t.Fatal(err)
	}
	p := ps[0]
	if !p.HasCLI() {
		t.Fatal("HasCLI = false")
	}
	r := p.Run("r1")
	if r.Nodes[0].CLI != CLIClaude || r.Nodes[1].CLI != CLICodex || r.Loop.Fix.CLI != CLIClaude {
		t.Fatalf("nodes %+v loop %+v", r.Nodes, r.Loop.Fix)
	}
	if r.Nodes[0].Slot != "" || r.Nodes[1].Slot != "" {
		t.Errorf("a CLI step has no model slot: %q %q", r.Nodes[0].Slot, r.Nodes[1].Slot)
	}
	if got := p.StepLabel(p.Steps[1]); got != "Codex (subscription)" {
		t.Errorf("label %q", got)
	}
	if got := p.StepLabel(ProfileStep{Category: agentdag.CategoryQuick}); got != SlotQuick {
		t.Errorf("label of a slot step %q", got)
	}
	if StepTools(p.Steps[1]) != agentdag.PresetReadOnly || StepTools(p.Steps[0]) != agentdag.PresetAll || StepTools(ProfileStep{}) != agentdag.PresetAll {
		t.Error("a CLI step without a preset reads only; a model step gets all")
	}
	if BuiltinProfiles()[0].HasCLI() {
		t.Error("default has no CLI step")
	}
}

func TestParseProfilesCLIErrors(t *testing.T) {
	cases := []struct{ name, data, want string }{
		{"unknown cli", `{"profiles": {"x": {"steps": [{"id":"a","cli":"gemini"}]}}}`, `unknown cli "gemini" (claude or codex)`},
		{"cli and slot", `{"profiles": {"x": {"steps": [{"id":"a","cli":"claude","slot":"main"}]}}}`, `runs on its CLI; leave out slot and category`},
		{"cli planner", `{"profiles": {"x": {"steps": [{"id":"a","cli":"codex","plan":true}]}}}`, `a CLI step cannot be the planner`},
		{"cli fix planner", `{"profiles": {"x": {"steps": [{"id":"a"}], "loop": {"verify": "a", "fix": {"cli": "nope"}}}}}`, `unknown cli "nope"`},
	}
	for _, c := range cases {
		_, err := ParseProfiles([]byte(c.data), "profiles.json", ProfileSourceUser, testSlots, 1)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	var pe *ProfileError
	err := ValidateProfile(Profile{Name: "x", Steps: []ProfileStep{{ID: "a", CLI: "zz"}}}, testSlots, 1)
	if pe, _ = err.(*ProfileError); pe == nil || pe.Step != "a" || pe.Field != "cli" {
		t.Errorf("field error %#v", err)
	}
}

// CLILabel names the CLIs the way the form and the cards show them.
func TestCLILabel(t *testing.T) {
	if CLILabel(CLIClaude) != "Claude Code (subscription)" || CLILabel(CLICodex) != "Codex (subscription)" {
		t.Errorf("labels %q %q", CLILabel(CLIClaude), CLILabel(CLICodex))
	}
	if CLITitle(CLIClaude) != "Claude Code" || CLITitle(CLICodex) != "Codex" {
		t.Error("titles")
	}
	if len(CLIs) != 2 || CLIs[0] != CLIClaude {
		t.Errorf("CLIs %v", CLIs)
	}
}
