package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/agentusage"
	"github.com/amin-jalali/termocode/internal/clock"
)

// switchTestModel is a model with one Claude Code tab (buf 7, in folder
// /w/feat, as account) at 10:00 UTC; switch starts are recorded.
func switchTestModel(t *testing.T, account string) (m *Model, started *[]string) {
	t.Helper()
	mm := aiTestModel(t)
	m = &mm
	m.terminalTabs = []terminalTab{{BufID: 7, Name: "claude", Agent: "claude", AgentState: agents.Done, Worktree: "/w/feat", Account: account}}
	m.termOpen = true
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	t.Cleanup(clock.Set(func() time.Time { return now }))
	var s []string
	old := agentSwitchStart
	agentSwitchStart = func(_ *Model, a agents.Agent, dir, task, label string) tea.Cmd {
		s = append(s, a.ID+"@"+label+" in "+dir+" task="+task)
		return nil
	}
	t.Cleanup(func() { agentSwitchStart = old })
	return m, &s
}

// Spec (CMP-13): the accounts to switch to are the other accounts of the
// same CLI (the default folder too, when the tab runs as a label), in
// saved order, without the tab's own account and without an account
// whose limit is still on.
func TestAgentSwitchTargets(t *testing.T) {
	m, _ := switchTestModel(t, "")
	writeSettingsJSON(t, `{"agent_accounts": {"claude": ["work", "home"], "codex": ["x"]}}`)
	now := clock.Now()
	m.agentUsageBook().Limit(agentusage.Key{CLI: "claude", Account: "home"}, "", now.Add(time.Hour), now)
	if got := strings.Join(m.agentSwitchTargets(&m.terminalTabs[0]), ","); got != "work" {
		t.Errorf("default tab targets %q, want work", got)
	}
	m.terminalTabs[0].Account = "work"
	if got := m.agentSwitchTargets(&m.terminalTabs[0]); len(got) != 1 || got[0] != "" {
		t.Errorf("work tab targets %q, want the default only", got)
	}
	gem := terminalTab{BufID: 8, Agent: "gemini"}
	if got := m.agentSwitchTargets(&gem); len(got) != 0 {
		t.Errorf("gemini targets %q", got)
	}
}

// Spec (CMP-13): Agent: Switch Account… on an agent tab opens a card
// whose first, focused answer is Cancel (Esc too); one button per free
// account. The AI can never run it.
func TestAgentSwitchCard(t *testing.T) {
	m, started := switchTestModel(t, "")
	writeSettingsJSON(t, `{"agent_accounts": {"claude": ["work"]}}`)
	if p, ok := aiActionPolicyFor(agentSwitchPaletteID); !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v %v", p, ok)
	}
	found := false
	for _, it := range tasksPaletteItems() {
		found = found || it.ID == agentSwitchPaletteID
	}
	if !found {
		t.Error("no palette row")
	}
	if _, ok := m.dispatchAgentPalette(agentSwitchPaletteID); !ok {
		t.Fatal("not dispatched")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindAgentAccountSwitch {
		t.Fatalf("card open %v kind %v", m.confirmOpen, m.confirmKind)
	}
	bs := m.confirm.Buttons()
	if len(bs) != 2 || bs[0].ID != "cancel" || m.confirm.SafeID() != "cancel" || bs[1].ID != "as:work" {
		t.Fatalf("buttons %+v safe %q", bs, m.confirm.SafeID())
	}
	if !strings.Contains(bs[1].Title, "work") {
		t.Errorf("button title %q", bs[1].Title)
	}
	if cmd, ok := m.handleAIConfirm("cancel"); !ok || cmd != nil || m.confirmOpen || len(*started) != 0 {
		t.Errorf("cancel: ok %v open %v started %v", ok, m.confirmOpen, *started)
	}
}

// Spec (CMP-13): the account button starts the same agent as that
// account in the tab's folder, with a one-line handover task; the old
// tab stays as it is.
func TestAgentSwitchAnswer(t *testing.T) {
	m, started := switchTestModel(t, "")
	writeSettingsJSON(t, `{"agent_accounts": {"claude": ["work"]}}`)
	m.openAgentSwitch()
	m.handleAIConfirm("as:work")
	if len(*started) != 1 || (*started)[0] != "claude@work in /w/feat task="+agentSwitchHandover {
		t.Fatalf("started %v", *started)
	}
	if strings.ContainsAny(agentSwitchHandover, "\n\r") {
		t.Error("handover is not one line")
	}
	if len(m.terminalTabs) != 1 || m.terminalTabs[0].Account != "" {
		t.Errorf("old tab changed: %+v", m.terminalTabs)
	}
	// An answer for an account that is not a target any more does nothing.
	m.openAgentSwitch()
	m.handleAIConfirm("as:gone")
	if len(*started) != 1 {
		t.Errorf("started %v", *started)
	}
}

// Spec (CMP-13): no agent tab, a CLI without accounts, or no free other
// account: a toast says why, and no card opens.
func TestAgentSwitchNothingToSwitch(t *testing.T) {
	m, _ := switchTestModel(t, "")
	writeSettingsJSON(t, `{}`)
	m.openAgentSwitch()
	if txt := strings.Join(strings.Fields(toastText(*m)), " "); m.confirmOpen || !strings.Contains(txt, "Agent: Accounts") {
		t.Errorf("no accounts: open %v toast %q", m.confirmOpen, txt)
	}
	m.terminalTabs[0].Agent = "gemini"
	m.openAgentSwitch()
	if m.confirmOpen {
		t.Error("card for a CLI without accounts")
	}
	m.terminalTabs[0].Agent = ""
	m.openAgentSwitch()
	if m.confirmOpen {
		t.Error("card without an agent tab")
	}
}

// Spec (CMP-13): when an agent tab hits its limit and another account of
// that CLI is free, the limit toast names Agent: Switch Account….
func TestAgentLimitToastNamesSwitch(t *testing.T) {
	m, screen, _ := limitTestModel(t)
	writeSettingsJSON(t, `{"agent_accounts": {"codex": ["work"]}}`)
	*screen = limitScreen(t, "limit/codex-usage.txt")
	m.onAgentOutput(7)
	if txt := strings.Join(strings.Fields(toastText(*m)), " "); !strings.Contains(txt, "Agent: Switch Account") || !strings.Contains(txt, "work") {
		t.Errorf("toast %q", txt)
	}
}
