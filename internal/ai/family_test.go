package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// The prompt gets a few lines for the model's family, picked from the
// model id (its size too: up to 14B parameters is small): GPT models are told to finish every step in the turn, small
// models get fewer tools and a capped prompt, Claude keeps the default.
func TestModelFamily(t *testing.T) {
	for _, c := range []struct {
		override, kind, model, _ string
		want                     string
	}{
		{"", KindOpenAI, "gpt-5.1", "", FamilyGPT},
		{"", KindOpenAI, "gpt-6-astra", "", FamilyGPT},
		{"", KindOpenAI, "o3-mini", "", FamilyGPT},
		{"", KindOpenRouter, "openai/gpt-5-codex", "", FamilyGPT},
		{"", KindAnthropic, "claude-opus-5-5", "", FamilyClaude},
		{"", KindOpenRouter, "anthropic/claude-sonnet-4.5", "", FamilyClaude},
		{"", KindOllama, "qwen2.5-coder:7b", "", FamilySmall},
		{"", KindLMStudio, "llama-3.2-3b-instruct", "", FamilySmall},
		{"", KindCompatible, "phi-4-mini-14b", "", FamilySmall},
		{"", KindOllama, "gpt-oss:20b", "", FamilyGPT},    // 20B: not small, and a GPT model
		{"", KindOllama, "llama3.3:70b", "", ""},          // big local model: default
		{"", KindOllama, "fake-1", "", ""},                // no size in the id: default
		{"", KindOpenAI, "deepseek-chat", "", ""},         // other family: default
		{"small", KindOpenAI, "gpt-5.1", "", FamilySmall}, // the owner's setting wins
		{"other", KindOllama, "qwen2.5-coder:7b", "", ""},
		{"claude", KindOpenAI, "my-proxy-model", "", FamilyClaude},
	} {
		if got := ModelFamily(c.override, c.model); got != c.want {
			t.Errorf("ModelFamily(%q, %q) = %q, want %q (kind %s)", c.override, c.model, got, c.want, c.kind)
		}
	}
}

// A CLI step's family: Codex runs GPT models, Claude Code runs Claude,
// unless the step names a model of another family.
func TestCLIModelFamily(t *testing.T) {
	for _, c := range []struct{ override, cli, model, want string }{
		{"", CLICodex, "", FamilyGPT},
		{"", CLIClaude, "", FamilyClaude},
		{"", CLICodex, "gpt-5.1-codex", FamilyGPT},
		{"", CLIClaude, "opus", FamilyClaude},
		{"other", CLICodex, "", ""},
	} {
		if got := CLIModelFamily(c.override, c.cli, c.model); got != c.want {
			t.Errorf("CLIModelFamily(%q, %q, %q) = %q, want %q", c.override, c.cli, c.model, got, c.want)
		}
	}
}

// ai_model_family is read from the settings file.
func TestModelFamilySetting(t *testing.T) {
	var s Settings
	if err := json.Unmarshal([]byte(`{"ai_model_family":"small"}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.ModelFamily != "small" {
		t.Errorf("ai_model_family = %q", s.ModelFamily)
	}
	if err := CheckModelFamily("tiny"); err == nil {
		t.Error("CheckModelFamily accepts an unknown family")
	}
	for _, ok := range []string{"", "auto", "gpt", "claude", "small", "other"} {
		if err := CheckModelFamily(ok); err != nil {
			t.Errorf("CheckModelFamily(%q): %v", ok, err)
		}
	}
}

// A small model's system prompt is cut to the cap at a line end, with a
// note; a prompt under the cap is left as it is.
func TestCapSystemPrompt(t *testing.T) {
	short := "line one\nline two\n"
	if got := CapSystemPrompt(short, 100); got != short {
		t.Errorf("short prompt changed: %q", got)
	}
	long := ""
	for range 2000 {
		long += "a rule line of the system prompt\n"
	}
	got := CapSystemPrompt(long, 1000)
	if len(got)/4 > 1000 {
		t.Errorf("capped prompt is about %d tokens, want <= 1000", len(got)/4)
	}
	if got[:len("a rule line")] != "a rule line" || !strings.Contains(got, SystemPromptCutNote) {
		t.Errorf("capped prompt lost its head or the note: …%q", got[len(got)-120:])
	}
	if CapSystemPrompt(long, 0) != long {
		t.Error("cap 0 should mean no cap")
	}
}
