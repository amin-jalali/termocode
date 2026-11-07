package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (MOB-63, mobile plan 3.9): Compare models is a race of one agent
// CLI on 2–4 different models. The AI asks with agent_race_models: it
// always asks (Deny first, no Always allow class), is never given to a
// read-only step or to outside agents, and checks its arguments before
// the card: an agent whose CLI takes --model (Claude Code, Codex),
// installed, 2–4 different safe model ids, a task, no key.
func TestAIAgentRaceModelsTool(t *testing.T) {
	m := aiWriteModel(t)
	fakeAgentLookPath(t, "claude", "codex", "amp")
	tools := m.aiChatTools(".", nil)
	tl, ok := ai.FindTool(tools, "agent_race_models")
	if !ok || !tl.Mutating || tl.Class != "" || tl.Gate != nil {
		t.Fatalf("tool %v mutating %v class %q", ok, tl.Mutating, tl.Class)
	}
	if _, ok := mcpServeRow("agent_race_models"); ok {
		t.Error("offered to outside agents")
	}
	if _, ok := ai.FindTool(aiFilterTools(tools, agentdag.ToolRule{Preset: agentdag.PresetReadOnly}), "agent_race_models"); ok {
		t.Error("a read-only step gets agent_race_models")
	}
	var started [][]string
	defer func() { aiRaceModelsHook = nil }()
	aiRaceModelsHook = func(_ *Model, task, verify, agent string, models []string) {
		started = append(started, append([]string{task, verify, agent}, models...))
	}
	args := map[string]any{"task": "make the toast queue thread-safe", "agent": "claude", "models": []any{"opus", "sonnet"}, "verify": "go test ./..."}
	res, card := aiWriteTurn(t, m, "agent_race_models", args, "deny")
	for _, want := range []string{"Compare 2 models", "Claude Code", "opus, sonnet", "make the toast queue thread-safe", "go test ./...", "own git worktree"} {
		if !strings.Contains(card.body, want) {
			t.Errorf("card has no %q: %q", want, card.body)
		}
	}
	if strings.Join(card.buttons, " · ") != "Deny · Start race" {
		t.Errorf("buttons %v", card.buttons)
	}
	if len(started) != 0 || !strings.Contains(res, "declined") {
		t.Fatalf("deny started %v; result %q", started, res)
	}
	res, _ = aiWriteTurn(t, m, "agent_race_models", args, "allow")
	if len(started) != 1 || strings.Join(started[0], "|") != "make the toast queue thread-safe|go test ./...|claude|opus|sonnet" || !strings.Contains(res, "Race started") {
		t.Fatalf("allow started %v; result %q", started, res)
	}

	for name, bad := range map[string]map[string]any{
		"one model":     {"task": "x", "agent": "claude", "models": []any{"opus"}},
		"five models":   {"task": "x", "agent": "codex", "models": []any{"a", "b", "c", "d", "e"}},
		"same twice":    {"task": "x", "agent": "claude", "models": []any{"opus", "opus"}},
		"flag model":    {"task": "x", "agent": "claude", "models": []any{"opus", "--dangerously-skip-permissions"}},
		"shell model":   {"task": "x", "agent": "claude", "models": []any{"opus", "a;rm -rf ~"}},
		"no --model":    {"task": "x", "agent": "amp", "models": []any{"a", "b"}},
		"not installed": {"task": "x", "agent": "gemini", "models": []any{"a", "b"}},
		"empty task":    {"task": "  ", "agent": "claude", "models": []any{"opus", "sonnet"}},
		"two lines":     {"task": "x", "agent": "claude", "models": []any{"opus", "sonnet"}, "verify": "a\nb"},
		"key":           {"task": "use sk-ant-api03-" + strings.Repeat("a", 40), "agent": "claude", "models": []any{"opus", "sonnet"}},
	} {
		if err := checkRaceModels(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Spec (MOB-63): a lane of a Compare models race starts its agent on its
// model (claude --model opus); a plain race lane starts the bare program
// as before. The lane's title names the model, so the compare page, the
// summary and the races topic tell the lanes apart.
func TestRaceModelsLanes(t *testing.T) {
	if got := raceAgentLine("claude", ""); got != "claude" {
		t.Errorf("bare lane %q", got)
	}
	if got := raceAgentLine("codex", "gpt-6-astra"); got != "codex --model gpt-6-astra" {
		t.Errorf("model lane %q", got)
	}
	e := newRaceEnv(t, "claude", "codex")
	e.feed(e.m.startRaceModels("fix the login", "", "codex", []string{"gpt-6-astra", "gpt-6-luna"}))
	r := e.race(t)
	if len(r.Lanes) != 2 {
		t.Fatalf("lanes %+v", r.Lanes)
	}
	for i, want := range []string{"gpt-6-astra", "gpt-6-luna"} {
		l := r.Lanes[i]
		if l.Agent != "codex" || l.Model != want || l.Title != "Codex · "+want || l.Worktree == "" || l.State == agentrace.Failed {
			t.Errorf("lane %d %+v", i, l)
		}
	}
}

// Spec (MOB-63): the phone's picker lists the agents that can compare
// models (their CLI takes --model), with the built-in model list of each
// and whether it is installed; nothing else is offered.
func TestRaceModelChoices(t *testing.T) {
	fakeAgentLookPath(t, "claude")
	got := raceModelChoices()
	if len(got) != 2 || got[0].ID != "claude" || got[1].ID != "codex" {
		t.Fatalf("choices %+v", got)
	}
	if !got[0].Installed || got[1].Installed {
		t.Errorf("installed %v %v", got[0].Installed, got[1].Installed)
	}
	if len(got[0].Models) != len(ai.CLIModels(ai.CLIClaude)) || got[0].Models[0].ID != "opus" || got[1].Models[0].ID == "" {
		t.Errorf("models %+v / %+v", got[0].Models, got[1].Models)
	}
	for _, c := range got {
		if a, ok := agents.ByID(c.ID); !ok || c.Title != a.Title {
			t.Errorf("%s title %q", c.ID, c.Title)
		}
	}
}
