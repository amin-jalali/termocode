package ai

import (
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
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

// A synthetic slot resolves on first use: the kind's key (env, then the
// store, then main's) and the connected provider's base URL.
func TestSlotPoolSyntheticSlots(t *testing.T) {
	antKey := "sk-" + "ant-test-6666666666666666"
	orKey := "sk-" + "or-test-7777777777777777"
	creds := Credentials{Keys: map[string]string{KindAnthropic: antKey}}
	provs := map[string]ProviderEntry{
		KindAnthropic:  {Kind: KindAnthropic, Method: MethodKey},
		KindCompatible: {Kind: KindCompatible, Method: MethodURL, BaseURL: "http://localhost:8000/v1"},
		KindOllama:     {Kind: KindOllama, Method: MethodLocal, BaseURL: "http://box:11434/v1"},
	}
	env := map[string]string{"OPENROUTER_API_KEY": orKey}
	getenv := func(k string) string { return env[k] }
	var built atomic.Int32
	pool := NewSlotPool(map[string]Config{"gpt": {Kind: KindOpenAI, APIKey: "k", Model: "m"}},
		func(c Config) Provider { built.Add(1); return countProvider{cfg: c} }).
		WithSynthetic(SyntheticResolver(provs, Config{}, creds, getenv))

	cases := []struct {
		name               string
		kind, model, base  string
		key                string
		configured, exists bool
	}{
		{"anthropic/claude-sonnet-5-5", KindAnthropic, "claude-sonnet-5-5", DefaultBaseURL(KindAnthropic), antKey, true, true},
		{"openai-compatible/qwen3", KindCompatible, "qwen3", "http://localhost:8000/v1", "", true, true},
		{"ollama/llama3.2:3b", KindOllama, "llama3.2:3b", "http://box:11434/v1", "", true, true},
		// Not connected here: still runs on a key from the environment.
		{"openrouter/meta/llama-4:free", KindOpenRouter, "meta/llama-4:free", DefaultBaseURL(KindOpenRouter), orKey, true, true},
		{"openai/gpt-5-mini", KindOpenAI, "gpt-5-mini", DefaultBaseURL(KindOpenAI), "", false, true},
		{"gemini/x", "", "", "", "", false, false},
	}
	for _, c := range cases {
		cfg, ok := pool.Config(c.name)
		if ok != c.exists || pool.Has(c.name) != c.exists {
			t.Errorf("%s: exists %v, want %v", c.name, ok, c.exists)
			continue
		}
		if !ok {
			if _, _, err := pool.Provider(c.name); err == nil || !strings.Contains(err.Error(), "unknown model slot") {
				t.Errorf("%s: %v", c.name, err)
			}
			continue
		}
		if cfg.Kind != c.kind || cfg.Model != c.model || cfg.BaseURL != c.base || cfg.APIKey != c.key {
			t.Errorf("%s: cfg %+v", c.name, cfg)
		}
		_, _, err := pool.Provider(c.name)
		if (err == nil) != c.configured {
			t.Errorf("%s: Provider err %v, want configured %v", c.name, err, c.configured)
		}
		if err != nil && strings.Contains(err.Error(), antKey) {
			t.Errorf("%s: the error names a key", c.name)
		}
	}
	before := built.Load()
	_, _, _ = pool.Provider("anthropic/claude-sonnet-5-5")
	if built.Load() != before {
		t.Error("a synthetic slot's provider is built once")
	}
	if got := strings.Join(pool.Names(), ","); got != "gpt" {
		t.Errorf("names list config slots only: %q", got)
	}
}

// main's key serves a synthetic slot of main's kind (no stored key).
func TestSyntheticResolverUsesMainKey(t *testing.T) {
	main := Config{Kind: KindOpenAI, APIKey: "sk-" + "test-main-1"}
	cfg, ok := SyntheticResolver(nil, main, Credentials{}, func(string) string { return "" })("openai/o3")
	if !ok || cfg.APIKey != main.APIKey || cfg.Model != "o3" {
		t.Fatalf("%+v %v", cfg, ok)
	}
}
