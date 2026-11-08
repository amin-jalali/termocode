package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// Every step on every provider knows who it is: the AI inside termocode
// on the owner's computer, in this project, with termocode's tools (a
// short list by kind, and when to use them). The project's AGENTS.md
// rules come after that. A Codex step must not go to its own docs or
// skills to learn what it is (the owner's "what can you do?" ran `cat
// ~/.codex/skills/…/SKILL.md` and a web search about Codex).

// gptProfile is the owner's GPT profile: one Codex step, Tools: all.
func gptProfile() ai.Profile {
	return ai.Profile{Name: "gpt", Title: "GPT", Source: ai.ProfileSourceUser,
		Steps: []ai.ProfileStep{{ID: "s1", Title: "GPT", CLI: ai.CLICodex, Model: "gpt-6-sol",
			Tools: agentdag.ToolRule{Preset: agentdag.PresetAll}, System: "[step:s1]"}}}
}

func gptRunModel(t *testing.T) (*Model, string) {
	t.Helper()
	m, dir := cliRunModel(t)
	m.ai.profFake = []ai.Profile{gptProfile()}
	m.ai.reloadProfiles()
	m.ai.profile = "gpt"
	return m, dir
}

// checkIdentity checks the termocode context at the start of a step's
// instructions, before the project rules.
func checkIdentity(t *testing.T, what, text, dir, rule string) {
	t.Helper()
	for _, want := range []string{
		"You are the AI inside termocode",
		"on the owner's own computer",
		"project " + filepath.Base(dir),
		"Do not read your own docs, skills or config",
		"Databases:", "db_query",
		"Files:", "read_file",
		"Git:", "git_status",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("%s misses %q:\n%s", what, want, text)
		}
	}
	who, rules := strings.Index(text, "You are the AI inside termocode"), strings.Index(text, rule)
	if who < 0 || rules < 0 || rules < who {
		t.Errorf("%s: the rules (at %d) must come after termocode's context (at %d)", what, rules, who)
	}
}

func trustRulesForTest(t *testing.T, dir string) {
	t.Helper()
	sess := loadSession()
	sess.AIRulesTrusted = ai.LoadProjectRules(dir).Hash
	saveSession(sess)
}

// A Codex step's prompt starts with termocode's context.
func TestAIIdentityCodexStep(t *testing.T) {
	f := newFakeCLI(t, map[string][]fakeCLIReply{"s1": {{text: "I am termocode's AI."}}}, "")
	m, dir := gptRunModel(t)
	writeAIRules(t, dir, "AGENTS.md", "RULE-OWNER-42: answer in short lines.\n")
	trustRulesForTest(t, dir)
	m.output = newOutputStore()
	driveAIChat(t, m, m.sendAIChat("what can you do?"), nil)
	prompts, _ := f.calls("s1")
	if len(prompts) != 1 {
		t.Fatalf("%d prompts", len(prompts))
	}
	checkIdentity(t, "codex prompt", prompts[0], dir, "RULE-OWNER-42")
}

// A model step (Claude, GPT by API, …) gets the same context in its
// system prompt.
func TestAIIdentityModelStep(t *testing.T) {
	m, rec, dir := aiRulesModel(t, "RULE-OWNER-42: answer in short lines.\n")
	trustRulesForTest(t, dir)
	driveAIChat(t, m, m.sendAIChat("what can you do?"), nil)
	if len(rec.reqs) == 0 {
		t.Fatal("no request")
	}
	checkIdentity(t, "system prompt", rec.reqs[0].Messages[0].Content, dir, "RULE-OWNER-42")
}
