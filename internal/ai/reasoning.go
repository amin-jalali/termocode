package ai

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Reasoning level (OMX-5): how hard the model thinks before it answers.
// "" sends nothing (the model's own default). A slot sets it with
// ai_models.<slot>.reasoning, the main chat with ai_reasoning, and one
// request with Request.Reasoning (the chat's header chip, or "think" /
// "ultrathink" in a message).
const (
	ReasoningLow    = "low"
	ReasoningMedium = "medium"
	ReasoningHigh   = "high"
	ReasoningXHigh  = "xhigh"
	ReasoningMax    = "max"
)

// ReasoningLevels are the levels, lowest first.
var ReasoningLevels = []string{ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningXHigh, ReasoningMax}

// reasoningRank is the place of level in ReasoningLevels (1-based; 0 for
// "" or an unknown level).
func reasoningRank(level string) int {
	for i, l := range ReasoningLevels {
		if l == level {
			return i + 1
		}
	}
	return 0
}

// NormalizeReasoning is level lower-case and trimmed; "" when it is not a
// level.
func NormalizeReasoning(level string) string {
	l := strings.ToLower(strings.TrimSpace(level))
	if reasoningRank(l) == 0 {
		return ""
	}
	return l
}

// CheckReasoning reports why level is not a reasoning level ("" is fine).
func CheckReasoning(level string) error {
	if strings.TrimSpace(level) == "" || NormalizeReasoning(level) != "" {
		return nil
	}
	return fmt.Errorf("reasoning must be one of %s (it is %q)", strings.Join(ReasoningLevels, ", "), level)
}

// NextReasoning is the level after level in the header chip's cycle:
// default → low → … → max → default.
func NextReasoning(level string) string {
	r := reasoningRank(NormalizeReasoning(level))
	if r >= len(ReasoningLevels) {
		return ""
	}
	return ReasoningLevels[r]
}

// MaxReasoning is the higher of two levels.
func MaxReasoning(a, b string) string {
	if reasoningRank(NormalizeReasoning(b)) > reasoningRank(NormalizeReasoning(a)) {
		return NormalizeReasoning(b)
	}
	return NormalizeReasoning(a)
}

// reasoningOf is the level a request is sent with: the request's own,
// else the config's.
func reasoningOf(req Request, cfg Config) string {
	if l := NormalizeReasoning(req.Reasoning); l != "" {
		return l
	}
	return NormalizeReasoning(cfg.Reasoning)
}

var claudeVersionRe = regexp.MustCompile(`claude-(?:opus|sonnet|haiku|fable|mythos)-(\d+)(?:-(\d+))?`)

// claudeVersion is the major.minor of a Claude model id (ok=false when
// the id does not say: a proxy's own name).
func claudeVersion(model string) (major, minor int, ok bool) {
	m := claudeVersionRe.FindStringSubmatch(model)
	if m == nil {
		if strings.Contains(model, "claude-3") {
			return 3, 0, true
		}
		return 0, 0, false
	}
	major, _ = strconv.Atoi(m[1])
	if m[2] != "" && len(m[2]) < 3 { // not a date
		minor, _ = strconv.Atoi(m[2])
	}
	return major, minor, true
}

// anthropicThinkingBudgets are the thinking budgets of the models that
// take budget_tokens (before Claude 4.6).
var anthropicThinkingBudgets = map[string]int{
	ReasoningLow: 2048, ReasoningMedium: 8192, ReasoningHigh: 16384, ReasoningXHigh: 24576, ReasoningMax: 31999,
}

// addAnthropicReasoning sends level: adaptive thinking with an effort on
// Claude 4.6 and later (and on unknown ids), a thinking budget below
// max_tokens on older models (they reject effort).
func addAnthropicReasoning(body map[string]any, model, level string) {
	if level == "" {
		return
	}
	major, minor, ok := claudeVersion(model)
	if ok && (major < 4 || (major == 4 && minor < 6)) {
		budget := anthropicThinkingBudgets[level]
		maxTok, _ := body["max_tokens"].(int)
		if budget >= maxTok {
			budget = maxTok - 1
		}
		if budget < 1024 {
			return
		}
		body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
		return
	}
	if ok && major == 4 && minor == 6 && level == ReasoningXHigh {
		level = ReasoningHigh // 4.6 has no xhigh
	}
	body["thinking"] = map[string]any{"type": "adaptive"}
	body["output_config"] = map[string]any{"effort": level}
}

var openAIReasoningModelRe = regexp.MustCompile(`^(?:openai/)?(?:o[1-9]|gpt-[5-9])`)

// openAIEffort maps a level onto OpenAI's low / medium / high.
func openAIEffort(level string) string {
	switch level {
	case ReasoningXHigh, ReasoningMax:
		return ReasoningHigh
	}
	return level
}

// addOpenAIReasoning sends level to OpenAI reasoning models
// (reasoning_effort; other models reject it) and to OpenRouter
// (reasoning.effort; it drops it for models that do not reason). Local
// and custom servers get nothing.
func addOpenAIReasoning(body map[string]any, kind, model, level string) {
	if level == "" {
		return
	}
	switch kind {
	case KindOpenAI:
		if openAIReasoningModelRe.MatchString(model) {
			body["reasoning_effort"] = openAIEffort(level)
		}
	case KindOpenRouter:
		body["reasoning"] = map[string]any{"effort": openAIEffort(level)}
	}
}
