package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

const (
	aiFamGPTLine   = "only a real question to the owner ends the turn"
	aiFamSmallLine = "use only the tools listed here"
)

// The lines a family adds: GPT finishes all steps in the turn, small
// models keep it short, Claude and the rest keep the default prompt.
func TestAIFamilyLines(t *testing.T) {
	if l := aiFamilyPrompt(ai.FamilyGPT); !strings.Contains(l, aiFamGPTLine) || !strings.Contains(l, "finish all steps in this turn") {
		t.Errorf("GPT line: %q", l)
	}
	if l := aiFamilyPrompt(ai.FamilySmall); !strings.Contains(l, aiFamSmallLine) {
		t.Errorf("small line: %q", l)
	}
	for _, f := range []string{ai.FamilyClaude, ""} {
		if l := aiFamilyPrompt(f); l != "" {
			t.Errorf("family %q adds %q; want the default prompt", f, l)
		}
	}
}

// A small model gets a smaller tool set: files, terminal, git,
// databases, tests and code intelligence stay; the rest goes.
func TestAISmallModelToolSet(t *testing.T) {
	all := aiDocTools("read_file", "edit_file", "run_command", "git_status", "db_query", "tests_run", "diagnostics",
		"open_file", "todo_write", "agent_run_start", "debug_start", "http_send", "kube_pods", "settings_set", "web_fetch")
	small := aiFamilyTools(ai.FamilySmall, all)
	names := map[string]bool{}
	for _, tl := range small {
		names[tl.Name] = true
	}
	for _, keep := range []string{"read_file", "edit_file", "run_command", "git_status", "db_query", "tests_run", "diagnostics", "open_file", "todo_write"} {
		if !names[keep] {
			t.Errorf("small tool set lost %s", keep)
		}
	}
	for _, drop := range []string{"agent_run_start", "debug_start", "http_send", "kube_pods", "settings_set", "web_fetch"} {
		if names[drop] {
			t.Errorf("small tool set keeps %s", drop)
		}
	}
	for _, f := range []string{ai.FamilyGPT, ai.FamilyClaude, ""} {
		if got := aiFamilyTools(f, all); len(got) != len(all) {
			t.Errorf("family %q narrows the tools: %d of %d", f, len(got), len(all))
		}
	}
}

// aiFamilyTurn runs one plain chat turn on model and returns the first
// request the model was sent.
func aiFamilyTurn(t *testing.T, model string) ai.Request {
	t.Helper()
	p := &recordingAIProvider{}
	m, _ := aiIDETestModel(t, p)
	m.ai.cfg.Model = model
	driveAIChat(t, m, m.sendAIChat("hello"), nil)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.reqs) == 0 {
		t.Fatal("no request")
	}
	return p.reqs[0]
}

// The chat picks the family from the turn's model id.
func TestAIChatTurnFollowsTheModelFamily(t *testing.T) {
	gpt := aiFamilyTurn(t, "gpt-5.1")
	if sys := gpt.Messages[0].Content; !strings.Contains(sys, aiFamGPTLine) {
		t.Errorf("GPT turn lacks the GPT line")
	}
	def := aiFamilyTurn(t, "fake-1")
	if sys := def.Messages[0].Content; strings.Contains(sys, aiFamGPTLine) || strings.Contains(sys, aiFamSmallLine) {
		t.Errorf("default turn has a family line")
	}
	small := aiFamilyTurn(t, "qwen2.5-coder:7b")
	sys := small.Messages[0].Content
	if !strings.Contains(sys, aiFamSmallLine) {
		t.Errorf("small turn lacks the small line")
	}
	if tok := len(sys) / 4; tok > ai.SmallPromptTokens {
		t.Errorf("small turn's system prompt is about %d tokens, cap %d", tok, ai.SmallPromptTokens)
	}
	if len(small.Tools) >= len(def.Tools) {
		t.Errorf("small turn has %d tools, default %d: want fewer", len(small.Tools), len(def.Tools))
	}
	for _, tl := range small.Tools {
		if tl.Name == "agent_run_start" {
			t.Errorf("small turn still offers agent_run_start")
		}
	}
}

// A Codex step runs a GPT model: its instructions carry the GPT line.
func TestAICodexStepGetsTheGPTLine(t *testing.T) {
	_, _, spec := stepMCPRun(t, agentdag.PresetReadOnly, nil, nil)
	if !strings.Contains(spec.Instructions, aiFamGPTLine) {
		t.Errorf("Codex step instructions lack the GPT line:\n%s", spec.Instructions)
	}
}
