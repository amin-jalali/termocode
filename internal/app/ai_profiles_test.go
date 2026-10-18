package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

func writeAIConfig(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, "termocode", rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A broken profiles.json never breaks the AI state: the built-ins stay
// and the error names the file and line.
func TestAIStateBrokenProfilesKeepBuiltins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("TERMOCODE_AI_PROVIDER", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	writeAIConfig(t, dir, "ai/profiles.json", "{\n\"profiles\": {\n\"x\": [\n")
	s := newAIState()
	if got := s.profiles.Names(); len(got) != len(ai.BuiltinProfiles()) || got[0] != ai.ProfileDefault {
		t.Fatalf("names %v", got)
	}
	if len(s.profiles.Errors) != 1 || !strings.Contains(s.profiles.Errors[0], "profiles.json line") {
		t.Errorf("errors %v", s.profiles.Errors)
	}
}

// Profiles and model slots load with the AI state; a slot's provider is
// built on first use, "main" is the chat's own provider.
func TestAIStateProfilesAndSlots(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("TERMOCODE_AI_PROVIDER", "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key-12345678")
	t.Setenv("OPENAI_API_KEY", "")
	writeAIConfig(t, dir, "config.json", `{"ai_models": {"gpt": {"provider": "openai", "model": "gpt-5-mini"}, "bad": {"provider": "nope"}}}`)
	writeAIConfig(t, dir, "ai/credentials.json", `{"keys": {"openai": "sk-stored-openai-key-1234"}}`)
	writeAIConfig(t, dir, "ai/profiles.json", `{"profiles": {"duo": {"steps": [{"id": "a"}, {"id": "b", "slot": "gpt"}]}}}`)
	s := newAIState()
	if _, ok := s.profiles.Get("duo"); !ok {
		t.Fatalf("user profile missing: %v %v", s.profiles.Names(), s.profiles.Errors)
	}
	if len(s.profiles.Errors) != 1 || !strings.Contains(s.profiles.Errors[0], `slot "bad"`) {
		t.Errorf("errors %v", s.profiles.Errors)
	}
	p, cfg, err := s.slotProvider(ai.SlotMain)
	if err != nil || p != s.provider || cfg.Kind != ai.KindAnthropic {
		t.Errorf("main slot: %v %v", cfg.Kind, err)
	}
	p1, cfg, err := s.slotProvider("gpt")
	if err != nil || cfg.Kind != ai.KindOpenAI || cfg.Model != "gpt-5-mini" {
		t.Fatalf("gpt slot: %+v %v", cfg, err)
	}
	if p2, _, _ := s.slotProvider("gpt"); p2 != p1 {
		t.Error("the slot provider must be built once")
	}
	if _, _, err := s.slotProvider("nope"); err == nil {
		t.Error("unknown slot")
	}
}
