package ai

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ── Manual and auto project rules (CMP-82) ──────────────────────────────
//
// A .claude/rules file can say when it is used in its front matter:
//
//	inclusion: manual   the user names it in a message as #name
//	inclusion: auto     the model sees its name and description and
//	                    loads it with rule_load when the task matches
//
// Both are part of the one trust hash (rules.go) and are never in the
// prompt text nor added by file. Each rule's text enters a chat once
// (RulesInjector), inside the same RulesMaxBytes budget.

// Manual is the rules used only when the user names them, in File order.
func (r Rules) Manual() []ScopedRule { return r.byMode(RuleModeManual) }

// Auto is the rules offered to the model by description, in File order.
func (r Rules) Auto() []ScopedRule { return r.byMode(RuleModeAuto) }

func (r Rules) byMode(mode string) []ScopedRule {
	var out []ScopedRule
	for _, s := range r.Scoped {
		if s.Mode == mode {
			out = append(out, s)
		}
	}
	return out
}

// ruleMentionRe is "#name" at a word start.
var ruleMentionRe = regexp.MustCompile(`(?:^|[^\p{L}\p{N}_#/&])#([A-Za-z0-9][A-Za-z0-9_-]*)`)

// Mentioned is the manual and auto rules named in text as #name, in the
// order of their first mention; unknown names are skipped.
func (r Rules) Mentioned(text string) []ScopedRule {
	var out []ScopedRule
	seen := map[string]bool{}
	for _, mm := range ruleMentionRe.FindAllStringSubmatch(text, -1) {
		name := strings.ToLower(mm[1])
		if seen[name] {
			continue
		}
		seen[name] = true
		if s, ok := r.ruleNamed(name, ""); ok {
			out = append(out, s)
		}
	}
	return out
}

// ruleNamed is the first manual or auto rule called name (mode "" =
// either).
func (r Rules) ruleNamed(name, mode string) (ScopedRule, bool) {
	for _, s := range r.Scoped {
		if s.Mode != "" && s.Name == name && (mode == "" || s.Mode == mode) {
			return s, true
		}
	}
	return ScopedRule{}, false
}

// take marks rule s as in this chat and returns its capped text; ok is
// false when it was already in or the budget is used up. j.mu is held.
func (j *RulesInjector) take(s ScopedRule) (string, bool) {
	if j.seen[s.File] || j.used >= RulesMaxBytes {
		return "", false
	}
	if j.seen == nil {
		j.seen = map[string]bool{}
	}
	j.seen[s.File] = true
	text := s.Text
	if len(text) > RulesScopedMaxBytes {
		text = cutRules(text, RulesScopedMaxBytes)
	}
	j.used += len(text)
	return Redact(text), true
}

// Mention returns the text of the rules the user named in text that
// are not in this chat yet ("" for none). r must be rules the user
// trusts.
func (j *RulesInjector) Mention(r Rules, text string) string {
	j.mu.Lock()
	defer j.mu.Unlock()
	var parts []string
	for _, s := range r.Mentioned(text) {
		if body, ok := j.take(s); ok {
			parts = append(parts, "[Project rule #"+s.Name+", from "+s.File+", named by the user. Follow it unless it conflicts with the user's request.]\n"+body)
		}
	}
	return strings.Join(parts, "\n\n")
}

// Load returns the text of the auto rule called name for the model's
// rule_load call. A manual rule is refused (only the user names it); a
// rule already in this chat returns a short note. r must be trusted.
func (j *RulesInjector) Load(r Rules, name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "#")))
	s, ok := r.ruleNamed(name, RuleModeAuto)
	if !ok {
		var names []string
		for _, a := range r.Auto() {
			names = append(names, a.Name)
		}
		if len(names) == 0 {
			return "", errors.New("this project has no rules to load")
		}
		return "", fmt.Errorf("no rule %q to load. Rules: %s", name, strings.Join(names, ", "))
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.seen[s.File] {
		return "Rule " + s.Name + " is already in this chat.", nil
	}
	body, ok := j.take(s)
	if !ok {
		return "", errors.New("the project rules budget of this chat is used up")
	}
	return "Project rule " + s.Name + " (" + Redact(s.Description) + "), from " + s.File +
		". Follow it unless it conflicts with the user's request:\n\n" + body, nil
}
