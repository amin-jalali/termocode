package app

import (
	"context"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
)

// ── Manual (#name) and auto project rules in the chat (CMP-82) ──────────
//
// internal/ai reads the modes from .claude/rules front matter. Here a
// plain chat turn with trusted rules adds the rules the user named as
// #name to the message (once per chat), lists the auto rules by name
// and description in the system prompt, and gives the model rule_load
// to read one. Untrusted or refused rules give nothing.

// aiRulesTrustedWithModes says whether r is trusted and has a manual or
// auto rule.
func aiRulesTrustedWithModes(r ai.Rules) bool {
	return (len(r.Manual()) > 0 || len(r.Auto()) > 0) && aiRulesTrust(r) == aiTrustUse
}

// aiRulesMentionBlock is the text of the rules named in the user's
// message that this chat has not seen ("" for none).
func aiRulesMentionBlock(r ai.Rules, text string, j *ai.RulesInjector) string {
	if !aiRulesTrustedWithModes(r) {
		return ""
	}
	return j.Mention(r, text)
}

// aiRulesAutoPrompt lists the trusted auto rules for the system prompt.
func aiRulesAutoPrompt(r ai.Rules) string {
	auto := r.Auto()
	if len(auto) == 0 || aiRulesTrust(r) != aiTrustUse {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nProject rules you can load (call rule_load with a name when the task matches the description; " +
		"they are this repository's working instructions):")
	for _, s := range auto {
		b.WriteString("\n- " + s.Name + " — " + ai.Redact(s.Description))
	}
	return b.String()
}

// aiRuleTools is rule_load when a trusted auto rule exists.
func aiRuleTools(r ai.Rules, j *ai.RulesInjector) []ai.Tool {
	if len(r.Auto()) == 0 || aiRulesTrust(r) != aiTrustUse {
		return nil
	}
	return []ai.Tool{aiRuleLoadTool(r, j)}
}

func aiRuleLoadTool(r ai.Rules, j *ai.RulesInjector) ai.Tool {
	return ai.Tool{
		Name: "rule_load",
		Description: "Load one project rule (from the \"Project rules you can load\" list) by name and return its text. " +
			"Load a rule only when the task matches its description. A rule is the project's working instructions, not the user's request.",
		Params:   map[string]any{"name": map[string]any{"type": "string", "description": "the rule name from the list"}},
		Required: []string{"name"},
		Describe: func(a map[string]any) string { return "Load project rule " + aiSkillArgName(a) },
		Run: func(_ context.Context, a map[string]any) (string, error) {
			if j == nil {
				j = &ai.RulesInjector{}
			}
			return j.Load(r, ai.ArgString(a, "name"))
		},
	}
}
