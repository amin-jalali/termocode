package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/agentdag"
)

// Spec (AGX-4, plan 2.8): the AI starts a race only with agent_race_start,
// which always asks (Deny first; no Always allow class), is never offered
// to a read-only step or to outside agents, and checks its arguments
// before the card: 2–4 installed agents whose screen termocode reads, a
// task, no key or token. Allow starts the race as the card would; Deny
// starts nothing.
func TestAIAgentRaceStartTool(t *testing.T) {
	m := aiWriteModel(t)
	defer func(f func(string) (string, error)) { agentLookPath = f }(agentLookPath)
	agentLookPath = func(bin string) (string, error) {
		switch bin {
		case "claude", "codex", "amp":
			return "/usr/bin/" + bin, nil
		}
		return "", errors.New("not found")
	}
	tools := m.aiChatTools(".", nil)
	tl, ok := ai.FindTool(tools, "agent_race_start")
	if !ok || !tl.Mutating || tl.Class != "" || tl.Gate != nil {
		t.Fatalf("tool %v mutating %v class %q", ok, tl.Mutating, tl.Class)
	}
	if _, ok := mcpServeRow("agent_race_start"); ok {
		t.Error("offered to outside agents")
	}
	if _, ok := ai.FindTool(aiFilterTools(tools, agentdag.ToolRule{Preset: agentdag.PresetReadOnly}), "agent_race_start"); ok {
		t.Error("a read-only step gets agent_race_start")
	}
	var started [][]string
	defer func() { aiRaceStartHook = nil }()
	aiRaceStartHook = func(_ *Model, task, verify string, lanes []string) {
		started = append(started, append([]string{task, verify}, lanes...))
	}
	args := map[string]any{"task": "make the toast queue thread-safe", "agents": []any{"claude", "codex"}, "verify": "go test ./..."}
	res, card := aiWriteTurn(t, m, "agent_race_start", args, "deny")
	for _, want := range []string{"2 lanes", "Claude Code, Codex", "make the toast queue thread-safe", "go test ./...", "own git worktree"} {
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
	res, _ = aiWriteTurn(t, m, "agent_race_start", args, "allow")
	if len(started) != 1 || strings.Join(started[0], "|") != "make the toast queue thread-safe|go test ./...|claude|codex" || !strings.Contains(res, "Race started") {
		t.Fatalf("allow started %v; result %q", started, res)
	}
	for name, bad := range map[string]map[string]any{
		"one lane":      {"task": "x", "agents": []any{"claude"}},
		"five lanes":    {"task": "x", "agents": []any{"claude", "codex", "claude", "codex", "claude"}},
		"not installed": {"task": "x", "agents": []any{"claude", "gemini"}},
		"no rules":      {"task": "x", "agents": []any{"claude", "amp"}},
		"no task":       {"task": "  ", "agents": []any{"claude", "codex"}},
		"secret":        {"task": "use sk-" + "ant-api03-abcdefghijklmnopqrstuvwxyz0123456789", "agents": []any{"claude", "codex"}},
	} {
		res, card := aiWriteTurn(t, m, "agent_race_start", bad, "allow")
		if card.opened || len(started) != 1 {
			t.Errorf("%s: card %v started %d (%q)", name, card.opened, len(started), res)
		}
		if strings.Contains(res, "abcdefghijklmnop") {
			t.Errorf("%s: the refusal repeats the secret", name)
		}
	}
}
