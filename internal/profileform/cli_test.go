package profileform

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// A step can run on a sign-in AI (Claude Code, Codex: cli + model). A
// CLI that is missing or signed out shows one line with a Fix pill that
// asks the app for the Add AI card at that AI (the form never touches a
// login, and never opens a terminal).

func cliProfile() ai.Profile {
	return ai.Profile{Steps: []ai.ProfileStep{
		{ID: "work", Title: "Work", CLI: ai.CLIClaude, Tools: agentdag.ToolRule{Preset: agentdag.PresetAll}},
		{ID: "review", Title: "Review", CLI: ai.CLICodex},
	}}
}

func cliCfg(claude, codex CLIState) Config {
	c := cfg(1)
	c.CLIs = map[string]CLIState{ai.CLIClaude: claude, ai.CLICodex: codex}
	return c
}

// Opening a profile with CLI steps and saving gives them back.
func TestFormCLIRoundTrip(t *testing.T) {
	m := New("sub", cliProfile(), cliCfg(CLIReady, CLIReady))
	p := m.Profile()
	if p.Steps[0].CLI != ai.CLIClaude || p.Steps[1].CLI != ai.CLICodex || p.Steps[0].Tools.Preset != agentdag.PresetAll {
		t.Fatalf("steps %+v", p.Steps)
	}
	if m.Dirty() {
		t.Error("dirty without a change")
	}
	m.SetSize(120, 40)
	m.FocusStep(1)
	if out := plain(m); !strings.Contains(out, "2  Review") || !strings.Contains(out, "Codex · its default model") || !strings.Contains(out, "uses your Codex sign-in") {
		t.Errorf("form:\n%s", out)
	}
	// The collapsed step names its CLI.
	if out := plain(m); !strings.Contains(out, "1  Work · Claude Code · its default model · all") {
		t.Errorf("collapsed:\n%s", out)
	}
}

func TestFormCLINeedsFix(t *testing.T) {
	m := New("sub", cliProfile(), cliCfg(CLIMissing, CLILoggedOut))
	m.SetSize(120, 40)
	m.FocusStep(0)
	out := plain(m)
	if !strings.Contains(out, "Claude Code is not installed") || !strings.Contains(out, "Fix") {
		t.Fatalf("missing row:\n%s", out)
	}
	if strings.Contains(out, "Install") || strings.Contains(out, "Log in") {
		t.Errorf("the old Install / Log in pills are gone:\n%s", out)
	}
	if !strings.Contains(out, "2  Review · Codex · its default model · read-only · ") || !strings.Contains(out, "not signed in") {
		t.Fatalf("a collapsed step says its CLI cannot run:\n%s", out)
	}
	m.FocusModel(0)
	m, _ = keys(m, "tab")
	if m.foc != (item{iSFix, 0}) {
		t.Fatalf("tab after Model goes to Fix, got %+v", m.foc)
	}
	m.FocusModel(1)
	m.FocusFix(0)
	if m.foc != (item{iSFix, 0}) || m.sel != 0 {
		t.Fatalf("FocusFix: %+v sel %d", m.foc, m.sel)
	}
	m, msgs := keys(m, "enter")
	if len(msgs) != 1 || msgs[0] != (AddAIMsg{Step: 0, Kind: ai.KindAnthropic, Method: ai.MethodCLI}) {
		t.Errorf("fix msgs %#v", msgs)
	}
	m = focusOn(m, item{iSFix, 1})
	_, msgs = keys(m, "enter")
	if len(msgs) != 1 || msgs[0] != (AddAIMsg{Step: 1, Kind: ai.KindOpenAI, Method: ai.MethodCLI}) {
		t.Errorf("fix msgs %#v", msgs)
	}
	// A ready CLI has no Fix: tab goes on to Tools.
	m.SetAIs(nil, map[string]CLIState{ai.CLIClaude: CLIReady, ai.CLICodex: CLIReady})
	m = focusOn(m, item{iSModel, 0})
	m, _ = keys(m, "tab")
	if m.foc != (item{iSTools, 0}) {
		t.Errorf("tab with a ready CLI: %+v", m.foc)
	}
	if out := plain(m); strings.Contains(out, "not installed") {
		t.Errorf("SetAIs did not refresh:\n%s", out)
	}
}

// A step on a provider that is not connected says so, and Fix asks for
// that provider.
func TestFormProviderNeedsFix(t *testing.T) {
	p := ai.Profile{Steps: []ai.ProfileStep{{ID: "a", Title: "A", Provider: ai.KindAnthropic, Model: "claude-sonnet-5-5"}}}
	m := New("p", p, cfg(1))
	m.SetSize(120, 40)
	m.FocusFix(0)
	if out := plain(m); !strings.Contains(out, "needs Anthropic") || m.foc != (item{iSFix, 0}) {
		t.Fatalf("foc %+v:\n%s", m.foc, out)
	}
	_, msgs := keys(m, "enter")
	if len(msgs) != 1 || msgs[0] != (AddAIMsg{Step: 0, Kind: ai.KindAnthropic}) {
		t.Errorf("fix msgs %#v", msgs)
	}
	// Connected: no Fix, the button shows the model and its cost tag.
	m.SetAIs([]Group{{Kind: ai.KindAnthropic, Name: "Anthropic", Tag: "API · pay per use", Models: []string{"claude-sonnet-5-5"}}}, nil)
	out := plain(m)
	if strings.Contains(out, "needs Anthropic") || !strings.Contains(out, "claude-sonnet-5-5 · Anthropic") || !strings.Contains(out, "API · pay per use") {
		t.Errorf("connected:\n%s", out)
	}
	if m.foc != (item{iSModel, 0}) {
		t.Errorf("focus leaves the gone Fix: %+v", m.foc)
	}
}

// A CLI step's problems show next to its fields (the rules of
// profiles.json).
func TestFormCLIPlannerError(t *testing.T) {
	p := cliProfile()
	p.Steps[1].Plan = true
	m := New("sub", p, cliCfg(CLIReady, CLIReady))
	if msg := m.Errors()["1:model"]; !strings.Contains(msg, "cannot be the planner") {
		t.Errorf("errors %v", m.Errors())
	}
}

func TestFormFixMouse(t *testing.T) {
	m := New("sub", cliProfile(), cliCfg(CLIMissing, CLIReady))
	m.SetSize(120, 40)
	m.FocusStep(0)
	_, tg := m.frame()
	for _, tt := range tg {
		if tt.it.kind == iSFix {
			ox, oy := m.Origin()
			_, cmd := m.HandleMouse(ox+1+tt.x, oy+1+tt.y, tea.MouseActionPress, tea.MouseButtonLeft)
			if cmd == nil || cmd() != (AddAIMsg{Step: 0, Kind: ai.KindAnthropic, Method: ai.MethodCLI}) {
				t.Fatal("a click on Fix asks the app")
			}
			return
		}
	}
	t.Fatal("no Fix target")
}
