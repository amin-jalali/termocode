package profileform

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// CS-5: a step can run on "Claude Code (subscription)" or "Codex
// (subscription)". A CLI that is missing or not logged in shows a clear
// row with an Install / Log in pill; the form only asks the app (it
// never touches a login).

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

func TestFormCLIPicker(t *testing.T) {
	m := New("sub", ai.Profile{Steps: []ai.ProfileStep{{ID: "a", Title: "A"}}}, cliCfg(CLIReady, CLIReady))
	m.SetSize(120, 40)
	m = focusOn(m, item{iSModel, 0})
	var labels []string
	for i := 0; i < len(m.opts); i++ {
		m, _ = keys(m, "right")
		labels = append(labels, m.opts[m.steps[0].model].label)
	}
	got := strings.Join(labels, " | ")
	if !strings.Contains(got, "gpt | Claude Code (subscription) | Codex (subscription) | category quick") {
		t.Fatalf("options %s", got)
	}
	m.steps[0].model = m.cliIndex(ai.CLICodex)
	st := m.Profile().Steps[0]
	if st.CLI != ai.CLICodex || st.Slot != "" || st.Category != "" {
		t.Errorf("step %+v", st)
	}
	if out := plain(m); !strings.Contains(out, "Codex (subscription)") || !strings.Contains(out, "uses your Codex login") {
		t.Errorf("form:\n%s", out)
	}
	if !m.Valid() {
		t.Errorf("errors %v", m.Errors())
	}
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
	if out := plain(m); !strings.Contains(out, "2  Review") || !strings.Contains(out, "Codex (subscription)") {
		t.Errorf("form:\n%s", out)
	}
	// The collapsed step names its CLI.
	if out := plain(m); !strings.Contains(out, "1  Work · Claude Code (subscription) · all") {
		t.Errorf("collapsed:\n%s", out)
	}
}

func TestFormCLIMissingRow(t *testing.T) {
	m := New("sub", cliProfile(), cliCfg(CLIMissing, CLILoggedOut))
	m.SetSize(120, 40)
	m.FocusStep(0)
	out := plain(m)
	if !strings.Contains(out, "Claude Code is not installed") || !strings.Contains(out, "Install") {
		t.Fatalf("missing row:\n%s", out)
	}
	if !strings.Contains(out, "2  Review · Codex (subscription) · read-only · ") || !strings.Contains(out, "not logged in") {
		t.Fatalf("a collapsed step says its CLI cannot run:\n%s", out)
	}
	m.FocusModel(0)
	m, _ = keys(m, "tab")
	if m.foc != (item{iSCLI, 0}) {
		t.Fatalf("tab after Model goes to the pill, got %+v", m.foc)
	}
	m.FocusModel(1)
	m.FocusCLI(0)
	if m.foc != (item{iSCLI, 0}) || m.sel != 0 {
		t.Fatalf("FocusCLI: %+v sel %d", m.foc, m.sel)
	}
	m, msgs := keys(m, "enter")
	if len(msgs) != 1 || msgs[0] != (CLIActionMsg{CLI: ai.CLIClaude}) {
		t.Errorf("install msgs %#v", msgs)
	}
	m.FocusStep(1)
	out = plain(m)
	if !strings.Contains(out, "Codex is not logged in") || !strings.Contains(out, "Log in") {
		t.Fatalf("login row:\n%s", out)
	}
	m = focusOn(m, item{iSCLI, 1})
	_, msgs = keys(m, "enter")
	if len(msgs) != 1 || msgs[0] != (CLIActionMsg{CLI: ai.CLICodex, Login: true}) {
		t.Errorf("login msgs %#v", msgs)
	}
	// A ready CLI has no pill: tab goes on to Tools.
	m.SetCLIs(map[string]CLIState{ai.CLIClaude: CLIReady, ai.CLICodex: CLIReady})
	m = focusOn(m, item{iSModel, 0})
	m, _ = keys(m, "tab")
	if m.foc != (item{iSTools, 0}) {
		t.Errorf("tab with a ready CLI: %+v", m.foc)
	}
	if out := plain(m); strings.Contains(out, "not installed") {
		t.Errorf("SetCLIs did not refresh:\n%s", out)
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

func TestFormCLIMouse(t *testing.T) {
	m := New("sub", cliProfile(), cliCfg(CLIMissing, CLIReady))
	m.SetSize(120, 40)
	m.FocusStep(0)
	_, tg := m.frame()
	for _, tt := range tg {
		if tt.it.kind == iSCLI {
			ox, oy := m.Origin()
			_, cmd := m.HandleMouse(ox+1+tt.x, oy+1+tt.y, tea.MouseActionPress, tea.MouseButtonLeft)
			if cmd == nil || cmd() != (CLIActionMsg{CLI: ai.CLIClaude}) {
				t.Fatal("a click on Install asks the app")
			}
			return
		}
	}
	t.Fatal("no Install target")
}
