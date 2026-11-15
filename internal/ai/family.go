package ai

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// Model families: the system prompt gets a few lines for the family of
// the turn's model (app.aiFamilyPrompt). "" is the default prompt.
const (
	FamilyGPT    = "gpt"
	FamilyClaude = "claude"
	FamilySmall  = "small" // a model of up to SmallModelParams billion parameters
)

// SmallModelParams is the largest model size (billions of parameters,
// read from the model id: "qwen2.5-coder:7b") that counts as small.
const SmallModelParams = 14

// SmallPromptTokens caps the system prompt of a small model (about 4
// bytes a token): a small model loses the task in a long prompt.
const SmallPromptTokens = 8000

// SystemPromptCutNote ends a system prompt cut by CapSystemPrompt.
const SystemPromptCutNote = "[The rest of this system prompt was cut to fit a small model.]\n"

var modelSizeRe = lazyre.New(`(?:^|[^a-z0-9.])(\d+(?:\.\d+)?)b(?:$|[^a-z0-9])`)

// CheckModelFamily checks an ai_model_family value.
func CheckModelFamily(v string) error {
	switch v {
	case "", "auto", FamilyGPT, FamilyClaude, FamilySmall, "other":
		return nil
	}
	return fmt.Errorf("ai_model_family must be auto, gpt, claude, small or other (it is %q)", v)
}

// ModelFamily is the family of model: override is ai_model_family
// ("" or auto: from the model id; other: the default prompt). A model
// whose id names up to SmallModelParams billion parameters is small;
// else gpt-*, o1…o9 and codex models are GPT, and claude, opus, sonnet,
// haiku and fable models are Claude.
func ModelFamily(override, model string) string {
	switch override {
	case FamilyGPT, FamilyClaude, FamilySmall:
		return override
	case "other":
		return ""
	}
	id := strings.ToLower(strings.TrimSpace(model))
	if m := modelSizeRe.FindStringSubmatch(id); m != nil {
		if n, err := strconv.ParseFloat(m[1], 64); err == nil && n <= SmallModelParams {
			return FamilySmall
		}
	}
	if i := strings.LastIndexByte(id, '/'); i >= 0 { // openrouter: "openai/gpt-5"
		id = id[i+1:]
	}
	switch {
	case strings.HasPrefix(id, "gpt"), strings.Contains(id, "codex"),
		len(id) > 1 && id[0] == 'o' && id[1] >= '1' && id[1] <= '9':
		return FamilyGPT
	case strings.Contains(id, "claude"), strings.HasPrefix(id, "opus"), strings.HasPrefix(id, "sonnet"),
		strings.HasPrefix(id, "haiku"), strings.HasPrefix(id, "fable"):
		return FamilyClaude
	}
	return ""
}

// CLIModelFamily is the family of a CLI step: the family of its model
// when the id tells, else Codex runs GPT models and Claude Code Claude.
func CLIModelFamily(override, cli, model string) string {
	if override != "" && override != "auto" {
		return ModelFamily(override, model)
	}
	if f := ModelFamily("", model); f != "" {
		return f
	}
	switch cli {
	case CLICodex:
		return FamilyGPT
	case CLIClaude:
		return FamilyClaude
	}
	return ""
}

// CapSystemPrompt cuts sys to about tokens tokens (4 bytes a token) at a
// line end and adds SystemPromptCutNote; tokens <= 0 or a shorter sys
// leaves it as it is. The head (who the AI is, its tools, how it works)
// stays; the tail goes.
func CapSystemPrompt(sys string, tokens int) string {
	limit := tokens*4 - len(SystemPromptCutNote)
	if tokens <= 0 || len(sys) <= tokens*4 || limit <= 0 {
		return sys
	}
	cut := strings.LastIndexByte(sys[:limit], '\n') + 1
	if cut == 0 { // one long line: cut at a rune start
		cut = limit
		for cut > 0 && !utf8.RuneStart(sys[cut]) {
			cut--
		}
	}
	return sys[:cut] + SystemPromptCutNote
}
