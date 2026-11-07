package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (CMP-82): with trusted rules, "#release" in a message adds that
// manual rule to the message sent (not to the system prompt); the
// system prompt lists the auto rules by name and description, and the
// turn has the rule_load tool. Manual rules are never listed.
func TestAIRulesManualMentionAndAutoList(t *testing.T) {
	m, rec, dir := aiRulesModel(t, "Base rule.\n")
	writeAIRules(t, dir, ".claude/rules/release.md", "---\ninclusion: manual\n---\nTag the release by hand.\n")
	writeAIRules(t, dir, ".claude/rules/db.md", "---\ninclusion: auto\ndescription: database migrations\n---\nNever drop a column.\n")
	trustAIRules(t, m, "please ship it, see #release")
	if len(rec.reqs) != 1 {
		t.Fatalf("requests %d", len(rec.reqs))
	}
	req := rec.reqs[0]
	sys := req.Messages[0].Content
	if !strings.Contains(sys, "db — database migrations") || !strings.Contains(sys, "rule_load") {
		t.Errorf("system prompt lacks the auto list:\n%s", sys)
	}
	if strings.Contains(sys, "Tag the release") || strings.Contains(sys, "Never drop a column") || strings.Contains(sys, "release —") {
		t.Errorf("rule text or manual rule in the system prompt:\n%s", sys)
	}
	if user := lastUserContent(req); !strings.Contains(user, "Tag the release by hand.") || !strings.Contains(user, "please ship it") {
		t.Errorf("user message %q", user)
	}
	found := false
	for _, tl := range req.Tools {
		found = found || tl.Name == "rule_load"
	}
	if !found {
		t.Error("no rule_load tool")
	}
}

// Spec (CMP-82): rule_load returns the auto rule's text (redacted) and
// refuses a manual one; untrusted rules give no list and no tool.
func TestAIRuleLoadTool(t *testing.T) {
	_, _, dir := aiRulesModel(t, "")
	writeAIRules(t, dir, ".claude/rules/db.md", "---\ninclusion: auto\ndescription: database migrations\n---\nNever drop a column.\n")
	rules := ai.LoadProjectRules(dir)
	if aiRulesAutoPrompt(rules) != "" || len(aiRuleTools(rules, &ai.RulesInjector{})) != 0 {
		t.Error("untrusted rules offered")
	}
	sess := loadSession()
	sess.AIRulesTrusted = rules.Hash
	saveSession(sess)
	tools := aiRuleTools(rules, &ai.RulesInjector{})
	if len(tools) != 1 || tools[0].Name != "rule_load" || tools[0].Mutating {
		t.Fatalf("tools %+v", tools)
	}
	out, err := tools[0].Run(t.Context(), map[string]any{"name": "db"})
	if err != nil || !strings.Contains(out, "Never drop a column.") {
		t.Errorf("load %q %v", out, err)
	}
	found := false
	for _, d := range AIToolCatalog() {
		found = found || d.Name == "rule_load"
	}
	if !found {
		t.Error("rule_load is not in the tool docs")
	}
}
