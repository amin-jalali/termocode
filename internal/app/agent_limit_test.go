package app

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/toast"
)

// limitScreen is a sample screen of internal/agents/testdata.
func limitScreen(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../agents/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// limitTestModel is a model with one Codex tab whose screen is *screen,
// at 10:00 UTC; typed text lands in *typed.
func limitTestModel(t *testing.T) (m *Model, screen *string, typed *[]string) {
	t.Helper()
	mm := aiTestModel(t)
	m = &mm
	m.terminalTabs = []terminalTab{{BufID: 7, Name: "codex", Agent: "codex", AgentState: agents.Working}}
	m.termOpen = true
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	t.Cleanup(clock.Set(func() time.Time { return now }))
	s := ""
	screen = &s
	oldRead := agentReadTail
	agentReadTail = func(_ *Model, buf int) (string, bool) { return *screen, buf == 7 }
	t.Cleanup(func() { agentReadTail = oldRead })
	var sent []string
	typed = &sent
	oldSend := agentChatSend
	agentChatSend = func(_ *Model, buf int, text string) { sent = append(sent, text) }
	t.Cleanup(func() { agentChatSend = oldSend })
	planned = nil
	oldTick := agentLimitTick
	agentLimitTick = func(d time.Duration, msg agentLimitResumeMsg) tea.Cmd {
		planned = append(planned, msg)
		return nil
	}
	t.Cleanup(func() { agentLimitTick = oldTick; planned = nil })
	return m, screen, typed
}

// writeSettings writes config.json.
func writeSettingsJSON(t *testing.T, body string) {
	t.Helper()
	path, err := settingsConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(path[:strings.LastIndex(path, "/")], 0o700)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Spec (AGX-11): a usage-limit message on an agent tab's screen is seen
// once: the tab is named "limit ↻12:05" until the reset, a toast names
// the agent and the reset time, and the OS notification text is generic.
// The same screen again does not toast twice. With agent_auto_continue
// off (the default) nothing is planned.
func TestAgentTabLimitSeen(t *testing.T) {
	m, screen, typed := limitTestModel(t)
	*screen = limitScreen(t, "limit/codex-usage.txt")
	cmd := m.onAgentOutput(7)
	tab := &m.terminalTabs[0]
	want := time.Date(2026, 11, 6, 12, 5, 0, 0, time.UTC)
	if !tab.LimitAt.Equal(want) || !strings.Contains(tab.LimitMsg, "usage limit") {
		t.Fatalf("limit %v %q", tab.LimitAt, tab.LimitMsg)
	}
	if got := m.panelEntryName(panelBarEntry{Kind: panelKindTerminal, Index: 0}); got != "limit ↻12:05" {
		t.Errorf("tab name %q", got)
	}
	if txt := toastText(*m); !strings.Contains(txt, "Codex hit its usage limit") || !strings.Contains(txt, "12:05") {
		t.Errorf("toast %q", txt)
	}
	if agentLimitNotice != "An agent hit its usage limit" {
		t.Errorf("OS text %q", agentLimitNotice)
	}
	_ = cmd
	if len(planned) != 0 {
		t.Error("auto-continue planned while off")
	}
	m.toast = toast.New()
	m.onAgentOutput(7)
	if txt := toastText(*m); strings.Contains(txt, "usage limit") {
		t.Errorf("toasted twice: %q", txt)
	}
	// After the reset the tab has its name back.
	restore := clock.Set(func() time.Time { return want.Add(time.Minute) })
	defer restore()
	if got := m.panelEntryName(panelBarEntry{Kind: panelKindTerminal, Index: 0}); got != "Codex" {
		t.Errorf("tab name after the reset %q", got)
	}
	if len(*typed) != 0 {
		t.Errorf("typed %q", *typed)
	}
}

// Spec (AGX-11): with agent_auto_continue on, the reset time + 1 min
// types "continue" (then Enter) once, and a toast says so. A second
// tick of the same limit does nothing (one shot).
func TestAgentAutoContinueOnce(t *testing.T) {
	m, screen, typed := limitTestModel(t)
	writeSettingsJSON(t, `{"agent_auto_continue": true}`)
	*screen = limitScreen(t, "limit/codex-usage.txt")
	cmd := m.onAgentOutput(7)
	msg := findLimitResume(t, cmd)
	if d := agentLimitResumeAfter(m.terminalTabs[0].LimitAt); d != 2*time.Hour+6*time.Minute {
		t.Errorf("delay %v", d)
	}
	*screen = limitScreen(t, "codex-done.txt")
	c := m.onAgentLimitResume(msg)
	if len(*typed) != 1 || (*typed)[0] != "continue" {
		t.Fatalf("typed %q", *typed)
	}
	if c == nil {
		t.Fatal("no Enter after the text")
	}
	runApply(m, c)
	if len(*typed) != 2 || (*typed)[1] != "\r" {
		t.Errorf("typed %q", *typed)
	}
	if txt := toastText(*m); !strings.Contains(txt, "Typed continue into Codex") {
		t.Errorf("toast %q", txt)
	}
	m.onAgentLimitResume(msg)
	if len(*typed) != 2 {
		t.Errorf("typed twice: %q", *typed)
	}
}

// planned is the auto-continue ticks the model planned (agentLimitTick).
var planned []agentLimitResumeMsg

// findLimitResume returns the last planned auto-continue tick.
func findLimitResume(t *testing.T, _ tea.Cmd) agentLimitResumeMsg {
	t.Helper()
	if len(planned) == 0 {
		t.Fatal("no auto-continue planned")
	}
	return planned[len(planned)-1]
}

// Spec (AGX-11): the guard. Auto-continue never types into a y/n or
// permission prompt, never after the agent moved on (it works again or
// a new limit came), never in a tab whose own toggle is off, and never
// for a tab that closed.
func TestAgentAutoContinueGuard(t *testing.T) {
	cases := []struct {
		name  string
		setup func(m *Model, screen *string)
	}{
		{"prompt on screen", func(m *Model, s *string) { *s = limitScreen(t, "codex-waiting-command.txt") }},
		{"working again", func(m *Model, s *string) {
			*s = limitScreen(t, "codex-done.txt")
			m.terminalTabs[0].AgentState = agents.Working
		}},
		{"tab toggle off", func(m *Model, s *string) {
			*s = limitScreen(t, "codex-done.txt")
			m.terminalTabs[0].AutoContinue = autoContinueOff
		}},
		{"tab closed", func(m *Model, s *string) {
			*s = limitScreen(t, "codex-done.txt")
			m.terminalTabs = nil
		}},
		{"a newer limit", func(m *Model, s *string) {
			*s = limitScreen(t, "codex-done.txt")
			m.terminalTabs[0].LimitAt = m.terminalTabs[0].LimitAt.Add(time.Hour)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, screen, typed := limitTestModel(t)
			writeSettingsJSON(t, `{"agent_auto_continue": true}`)
			*screen = limitScreen(t, "limit/codex-usage.txt")
			msg := findLimitResume(t, m.onAgentOutput(7))
			m.terminalTabs[0].AgentState = agents.Done
			c.setup(m, screen)
			m.onAgentLimitResume(msg)
			if len(*typed) != 0 {
				t.Errorf("typed %q", *typed)
			}
		})
	}
}

// Spec (AGX-11): Agent: Toggle Auto-Continue sets the active agent tab's
// own switch against the setting (and back); the AI may not run it, nor
// change the setting.
func TestAgentAutoContinueToggle(t *testing.T) {
	m, _, _ := limitTestModel(t)
	if p, ok := aiActionPolicyFor(agentAutoContinuePaletteID); !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v %v", p, ok)
	}
	if p, ok := aiSettingPolicies["agent_auto_continue"]; !ok || p.Gate != aiGateNever {
		t.Errorf("setting policy %+v %v", p, ok)
	}
	found := false
	for _, it := range tasksPaletteItems() {
		if it.ID == agentAutoContinuePaletteID && it.Title == "Agent: Toggle Auto-Continue" {
			found = true
		}
	}
	if !found {
		t.Error("no palette row")
	}
	m.dispatchTasksPalette(agentAutoContinuePaletteID)
	if m.terminalTabs[0].AutoContinue != autoContinueOn || !m.agentAutoContinueOn(&m.terminalTabs[0]) {
		t.Errorf("first toggle (setting off) %v", m.terminalTabs[0].AutoContinue)
	}
	if !strings.Contains(toastText(*m), "Auto-continue on for Codex") {
		t.Errorf("toast %q", toastText(*m))
	}
	m.dispatchTasksPalette(agentAutoContinuePaletteID)
	if m.agentAutoContinueOn(&m.terminalTabs[0]) {
		t.Error("second toggle still on")
	}
	m.terminalTabs[0].Agent = ""
	m.toast = toast.New()
	m.dispatchTasksPalette(agentAutoContinuePaletteID)
	if !strings.Contains(toastText(*m), "agent tab") {
		t.Errorf("no agent: %q", toastText(*m))
	}
}
