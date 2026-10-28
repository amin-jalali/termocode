package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// AIP-2: profile steps that name a connected provider and a model (and
// CLI steps that name a model). Nothing here reaches a network or a real
// CLI: providers are built by a fake, CLIs by fakeCLI.

// loadProfiles reads providers.json, so a provider step's synthetic slot
// resolves with the provider's base URL and the kind's stored key.
func TestAIProvidersLoadIntoSlots(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := ai.ProvidersPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := ai.SaveProvider(path, ai.ProviderEntry{Kind: ai.KindCompatible, Method: ai.MethodURL, BaseURL: "http://localhost:8000/v1"}); err != nil {
		t.Fatal(err)
	}
	key := "sk-" + "ant-test-8888888888888888"
	s := &aiState{}
	s.loadProfiles(ai.Credentials{Keys: map[string]string{ai.KindAnthropic: key}})
	if s.providers[ai.KindCompatible].BaseURL != "http://localhost:8000/v1" {
		t.Fatalf("providers %+v", s.providers)
	}
	cfg, ok := s.slots.Config("openai-compatible/qwen3")
	if !ok || cfg.BaseURL != "http://localhost:8000/v1" || cfg.Model != "qwen3" {
		t.Errorf("compatible slot %+v %v", cfg, ok)
	}
	cfg, ok = s.slots.Config("anthropic/claude-sonnet-5-5")
	if !ok || cfg.APIKey != key || cfg.Model != "claude-sonnet-5-5" {
		t.Errorf("anthropic slot kind %q model %q key set %v", cfg.Kind, cfg.Model, cfg.APIKey == key)
	}
}

// Without providers.json an old setup still resolves: the list is
// derived from it (ai_provider ollama with its base URL), and nothing
// else changes for the old slots.
func TestAIProvidersDerivedForOldSetup(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := &aiState{settings: ai.Settings{Provider: ai.KindOllama, BaseURL: "http://box:11434/v1",
		Models: map[string]ai.ModelSlot{"gpt": {Provider: ai.KindOpenAI, Model: "gpt-5"}}}}
	s.loadProfiles(ai.Credentials{})
	if p := s.providers[ai.KindOllama]; p.Method != ai.MethodLocal || p.BaseURL != "http://box:11434/v1" {
		t.Fatalf("derived %+v", s.providers)
	}
	if cfg, ok := s.slots.Config("ollama/llama3.2:3b"); !ok || cfg.BaseURL != "http://box:11434/v1" {
		t.Errorf("ollama slot %+v", cfg)
	}
	if cfg, ok := s.slots.Config("gpt"); !ok || cfg.Model != "gpt-5" {
		t.Errorf("an ai_models slot is unchanged: %+v %v", cfg, ok)
	}
	if !s.slotKnown["gpt"] || s.slotKnown["ollama/llama3.2:3b"] {
		t.Errorf("known slots %v", s.slotKnown)
	}
}

// A step with provider + model runs on that provider's model: the run
// resolves its synthetic slot, the card names the model, and the
// provider gets the step's request.
func TestAIProviderStepRuns(t *testing.T) {
	m, _ := aiIDETestModel(t, &recordingAIProvider{})
	step := &recordingAIProvider{replies: []ai.Chunk{{Delta: "planned it"}, {Delta: "checked it"}}}
	key := "sk-" + "ant-test-9999999999999999"
	var built []ai.Config
	var mu sync.Mutex
	m.ai.slots = ai.NewSlotPool(nil, func(c ai.Config) ai.Provider { mu.Lock(); built = append(built, c); mu.Unlock(); return step }).
		WithSynthetic(ai.SyntheticResolver(nil, ai.Config{}, ai.Credentials{Keys: map[string]string{ai.KindAnthropic: key}}, func(string) string { return "" }))
	m.ai.profFake = []ai.Profile{{Name: "pick", Source: ai.ProfileSourceUser,
		Steps: []ai.ProfileStep{
			{ID: "plan", Title: "Plan", Provider: ai.KindAnthropic, Model: "claude-sonnet-5-5"},
			{ID: "check", Title: "Check", Provider: ai.KindAnthropic, Model: "claude-haiku-4-5"},
		}}}
	m.ai.reloadProfiles()
	m.ai.profile = "pick"
	driveAIChat(t, m, m.sendAIChat("plan the change"), nil)

	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || rm.Text != "checked it" {
		t.Fatalf("run %+v text %q", rm.Run, rm.Text)
	}
	if s := rm.Steps[0]; s.Slot != "anthropic/claude-sonnet-5-5" || s.Model != "claude-sonnet-5-5" || s.State != string(agentdag.StateCompleted) {
		t.Errorf("step %+v", s)
	}
	if s := rm.Steps[1]; s.Slot != "anthropic/claude-haiku-4-5" || s.Model != "claude-haiku-4-5" {
		t.Errorf("step 2 %+v", s)
	}
	if len(built) != 2 || built[0].Kind != ai.KindAnthropic || built[0].Model != "claude-sonnet-5-5" || built[0].APIKey != key {
		t.Errorf("built %d providers", len(built))
	}
	if len(step.reqs) != 2 {
		t.Errorf("requests %d", len(step.reqs))
	}
}

// A CLI step with a model asks its CLI for it; the card names it.
func TestAICLIStepPassesModel(t *testing.T) {
	f := newFakeCLI(t, map[string][]fakeCLIReply{"work": {{text: "done"}}}, "")
	m, _ := aiIDETestModel(t, &recordingAIProvider{})
	m.ai.profFake = []ai.Profile{{Name: "sub", Source: ai.ProfileSourceUser,
		Steps: []ai.ProfileStep{{ID: "work", Title: "Work", CLI: ai.CLIClaude, Model: "opus", System: "[step:work]"}}}}
	m.ai.reloadProfiles()
	m.ai.profile = "sub"
	driveAIChat(t, m, m.sendAIChat("go"), nil)

	_, specs := f.calls("work")
	if len(specs) != 1 || specs[0].Model != "opus" || specs[0].CLI != ai.CLIClaude {
		t.Fatalf("specs %+v", specs)
	}
	if s := aiRunMsg(t, m).Steps[0]; s.CLI != ai.CLIClaude || s.Model != "Claude Code · opus" {
		t.Errorf("step %+v", s)
	}
}

// A CLI that termocode installed into its tools/bin (not on PATH) passes
// the run's check.
func TestAICLIFoundInToolsBin(t *testing.T) {
	newFakeCLI(t, map[string][]fakeCLIReply{"work": {{text: "done"}}}, "")
	agentLookPath = func(bin string) (string, error) { return "", os.ErrNotExist }
	tools := t.TempDir()
	t.Setenv("TERMOCODE_TOOLS_DIR", tools)
	if err := os.MkdirAll(filepath.Join(tools, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, _ := aiIDETestModel(t, &recordingAIProvider{})
	m.ai.profFake = []ai.Profile{{Name: "sub", Source: ai.ProfileSourceUser,
		Steps: []ai.ProfileStep{{ID: "work", Title: "Work", CLI: ai.CLICodex, System: "[step:work]"}}}}
	m.ai.reloadProfiles()
	m.ai.profile = "sub"
	run, _ := m.ai.profiles.Get("sub")
	if _, err := m.ai.aiRunPreflight(run.Run("r"), run.CategorySlots()); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("no codex anywhere: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tools, "bin", "codex"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ai.aiRunPreflight(run.Run("r"), run.CategorySlots()); err != nil {
		t.Fatalf("codex in tools/bin: %v", err)
	}
}

// The badge and the pipeline list name a CLI step's model when it has one.
func TestAICLIStepModelLabels(t *testing.T) {
	s := &aiState{}
	p := ai.Profile{Name: "sub", Steps: []ai.ProfileStep{{ID: "a", CLI: ai.CLICodex, Model: "o3"}}}
	if got := s.profileModelLabel(p); got != "Codex · o3" {
		t.Errorf("badge %q", got)
	}
	if got := s.aiProfileSteps(p); len(got) != 1 || got[0].slot != "Codex" || got[0].model != "o3 · subscription" {
		t.Errorf("pipeline steps %+v", got)
	}
	p.Steps[0].Model = ""
	if got := s.profileModelLabel(p); got != "Codex" {
		t.Errorf("badge without model %q", got)
	}
	if got := s.aiProfileSteps(p); got[0].model != "subscription" {
		t.Errorf("pipeline without model %+v", got)
	}
}
