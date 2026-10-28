package ai

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// AIP-2: a step may name a connected provider and a model instead of a
// slot; it runs on the synthetic slot "<provider>/<model>".
func TestParseProfilesProviderSteps(t *testing.T) {
	data := `{"profiles": {"mix": {"steps": [
  {"id": "plan", "provider": "anthropic", "model": "claude-sonnet-5-5"},
  {"id": "local", "provider": "ollama", "model": "llama3.2:3b"},
  {"id": "route", "provider": "openrouter", "model": "meta/llama-4:free"},
  {"id": "sub", "cli": "claude", "model": "opus"},
  {"id": "old", "slot": "main"}
], "loop": {"verify": "old", "fix": {"provider": "openai", "model": "gpt-5-mini"}}}}}`
	ps, err := ParseProfiles([]byte(data), "profiles.json", ProfileSourceUser, testSlots, 1)
	if err != nil {
		t.Fatal(err)
	}
	p := ps[0]
	wantSlots := []string{"anthropic/claude-sonnet-5-5", "ollama/llama3.2:3b", "openrouter/meta/llama-4:free", "", "main"}
	r := p.Run("r1")
	for i, want := range wantSlots {
		if r.Nodes[i].Slot != want {
			t.Errorf("node %d slot %q, want %q", i, r.Nodes[i].Slot, want)
		}
	}
	if r.Nodes[3].CLI != CLIClaude || r.Nodes[3].Model != "opus" {
		t.Errorf("CLI node %+v", r.Nodes[3])
	}
	if r.Nodes[0].Model != "" {
		t.Errorf("a provider node carries its model in the slot, not Model: %+v", r.Nodes[0])
	}
	if r.Loop.Fix.Slot != "openai/gpt-5-mini" {
		t.Errorf("fix slot %q", r.Loop.Fix.Slot)
	}
	if got := p.SlotFor(p.Steps[0]); got != "anthropic/claude-sonnet-5-5" {
		t.Errorf("SlotFor %q", got)
	}
	if got := p.StepLabel(p.Steps[0]); got != "claude-sonnet-5-5 · Anthropic" {
		t.Errorf("label %q", got)
	}
	if got := p.StepLabel(p.Steps[3]); got != "Claude Code (subscription) · opus" {
		t.Errorf("CLI label %q", got)
	}
	if got := p.StepLabel(p.Steps[4]); got != SlotMain {
		t.Errorf("slot label %q", got)
	}
}

func TestParseProfilesProviderErrors(t *testing.T) {
	cases := []struct{ name, step, field, want string }{
		{"provider and slot", `{"id":"a","provider":"anthropic","model":"m","slot":"main"}`, "provider", "leave out slot"},
		{"provider and category", `{"id":"a","provider":"anthropic","model":"m","category":"deep"}`, "provider", "leave out slot"},
		{"provider and cli", `{"id":"a","provider":"anthropic","model":"m","cli":"claude"}`, "provider", "not both"},
		{"unknown provider", `{"id":"a","provider":"gemini","model":"m"}`, "provider", `unknown provider "gemini"`},
		{"no model", `{"id":"a","provider":"openai"}`, "model", "needs a model"},
		{"dash model", `{"id":"a","provider":"openai","model":"--help"}`, "model", "model id"},
		{"space model", `{"id":"a","provider":"openai","model":"gpt 5"}`, "model", "model id"},
		{"dash cli model", `{"id":"a","cli":"codex","model":"-c"}`, "model", "model id"},
	}
	for _, c := range cases {
		data := `{"profiles": {"x": {"steps": [` + c.step + `]}}}`
		_, err := ParseProfiles([]byte(data), "profiles.json", ProfileSourceUser, testSlots, 1)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
		var doc struct {
			Profiles map[string]Profile `json:"profiles"`
		}
		if err := json.Unmarshal([]byte(data), &doc); err != nil {
			t.Fatal(err)
		}
		p := doc.Profiles["x"]
		p.Name = "x"
		var pe *ProfileError
		if err := ValidateProfile(p, testSlots, 1); !errors.As(err, &pe) || pe.Field != c.field {
			t.Errorf("%s: ValidateProfile = %#v, want field %q", c.name, err, c.field)
		}
	}
}

// Old profiles keep their bytes: a step without provider / model writes
// neither key.
func TestProfileStepOldJSONUnchanged(t *testing.T) {
	st := ProfileStep{ID: "a", Slot: "main", Category: "deep"}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"id":"a","slot":"main","category":"deep","tools":{}}`; string(b) != want {
		t.Fatalf("got %s, want %s", b, want)
	}
	// A stray "model" on a slot step was ignored before; it still is (the
	// file keeps loading and the step keeps its slot).
	data := `{"profiles": {"x": {"steps": [{"id":"a","slot":"main","model":"whatever"}]}}}`
	ps, err := ParseProfiles([]byte(data), "profiles.json", ProfileSourceUser, testSlots, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := ps[0].SlotFor(ps[0].Steps[0]); got != SlotMain {
		t.Fatalf("slot %q", got)
	}
}

func TestSyntheticSlotName(t *testing.T) {
	cases := []struct {
		name, kind, model string
		ok                bool
	}{
		{"anthropic/claude-opus-5-5", KindAnthropic, "claude-opus-5-5", true},
		{"openrouter/meta/llama-4:free", KindOpenRouter, "meta/llama-4:free", true},
		{"main", "", "", false},
		{"gemini/x", "", "", false},
		{"openai/", "", "", false},
	}
	for _, c := range cases {
		kind, model, ok := SplitSyntheticSlot(c.name)
		if kind != c.kind || model != c.model || ok != c.ok {
			t.Errorf("%q: %q %q %v", c.name, kind, model, ok)
		}
	}
}
