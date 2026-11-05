package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/profileform"
)

// fakeAgentLookPath makes bins the installed agent programs.
func fakeAgentLookPath(t *testing.T, bins ...string) {
	t.Helper()
	old := agentLookPath
	t.Cleanup(func() { agentLookPath = old })
	agentLookPath = func(bin string) (string, error) {
		for _, b := range bins {
			if b == bin {
				return "/usr/bin/" + bin, nil
			}
		}
		return "", errors.New("not found")
	}
}

func agentPickRowsText(items []picker.Item) string {
	var b strings.Builder
	for _, it := range items {
		if it.Header {
			fmt.Fprintf(&b, "# %s\n", it.Title)
			continue
		}
		fmt.Fprintf(&b, "%s | %s | %s\n", it.ID, it.Title, it.Hint)
	}
	return b.String()
}

// Spec (AGX-1, plan 2.4): one list in three groups. INSTALLED: the
// agents on PATH, with the CLI's sign-in state where termocode can check
// it (Claude Code, Codex). BUILT-IN: termocode's own AI profiles.
// NOT INSTALLED: the rest, saying what Enter does (install with npm / pip,
// or that the CLI installs from its own site).
func TestAgentPickerItemsGroups(t *testing.T) {
	in := agentPickInput{
		agents:    agents.Registry,
		installed: func(a agents.Agent) bool { return a.ID == "claude" || a.ID == "codex" || a.ID == "amp" },
		cli:       map[string]profileform.CLIState{ai.CLIClaude: profileform.CLIReady, ai.CLICodex: profileform.CLILoggedOut},
		profiles:  []agentPickProfile{{name: "default", steps: 1}, {name: "plan", steps: 3}},
	}
	got := agentPickRowsText(agentPickerItems(in))
	head := "# INSTALLED\n" +
		"agent:claude | ◉ Claude Code | claude · signed in\n" +
		"agent:codex | ◦ Codex | codex · not signed in · Enter to sign in\n" +
		"agent:amp | ◦ Amp | amp\n" +
		"# BUILT-IN\n" +
		"profile:default | ✦ default | termocode AI · 1 step\n" +
		"profile:plan | ✦ plan | termocode AI · 3 steps\n" +
		"# NOT INSTALLED\n" +
		"agent:gemini |   Gemini CLI | gemini · Enter to install (npm)\n"
	if !strings.HasPrefix(got, head) {
		t.Fatalf("rows:\n%s\nwant prefix:\n%s", got, head)
	}
	for _, want := range []string{
		"agent:aider |   Aider | aider · Enter to install (pip)\n",
		"agent:cursor |   Cursor CLI | cursor-agent · install it from its own site\n",
		"agent:pi |   Pi | pi · Enter to install (npm)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no row %q in:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "agent:"); n != len(agents.Registry) {
		t.Errorf("%d agent rows, want %d", n, len(agents.Registry))
	}
	// Not checked yet: no sign-in word at all (never a wrong one).
	in.cli = nil
	if got := agentPickRowsText(agentPickerItems(in)); !strings.Contains(got, "agent:claude | ◦ Claude Code | claude\n") {
		t.Errorf("unchecked claude:\n%s", got)
	}
}

// Spec (AGX-1): F1 → Agent: Start… opens the picker; the AI cannot open
// it (a picker only the user answers; it has outside_agent_start). Enter
// on an installed agent starts it like Agent: Start <Title>; Enter on a
// profile makes it the active profile and opens the chat.
func TestAgentPickerPaletteAndEnter(t *testing.T) {
	fakeAgentLookPath(t, "codex", "amp")
	m := aiTestModel(t)
	if p, ok := aiActionPolicyFor("agent-start"); !ok || p.Gate != aiGateNever {
		t.Errorf("agent-start policy %+v %v", p, ok)
	}
	found := false
	for _, it := range tasksPaletteItems() {
		if it.ID == "agent-start" && it.Title == "Agent: Start…" {
			found = true
		}
	}
	if !found {
		t.Error("no Agent: Start… palette row")
	}
	if _, ok := m.dispatchTasksPalette("agent-start"); !ok || !m.pickerOpen || m.pickerKind != pickerKindAgentPicker {
		t.Fatalf("picker open %v kind %v", m.pickerOpen, m.pickerKind)
	}
	var started []string
	old := agentPickStart
	agentPickStart = func(_ *Model, a agents.Agent) tea.Cmd { started = append(started, a.ID); return nil }
	t.Cleanup(func() { agentPickStart = old })
	if _, ok := m.handleAIPickerSelect("agent:amp"); !ok {
		t.Fatal("picker kind not routed")
	}
	if strings.Join(started, ",") != "amp" {
		t.Errorf("started %v", started)
	}
	m.pickerKind = pickerKindAgentPicker
	m.handleAIPickerSelect("profile:" + ai.ProfileDefault)
	if m.aiS().profile != ai.ProfileDefault || !m.actionsOpen || !m.aiS().focused {
		t.Errorf("profile %q, chat open %v focused %v", m.aiS().profile, m.actionsOpen, m.aiS().focused)
	}
}
