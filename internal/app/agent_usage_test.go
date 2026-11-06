package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentlog"
)

// Spec (AGX-12): a limit seen on a Codex tab fills the plan pill of the
// status bar ("✦ Codex limit ↻12:05"); with nothing known there is no
// pill. The pill is a click target named in the hover labels.
func TestAgentUsagePillFromLimit(t *testing.T) {
	agentsUnicode(t)
	m, screen, _ := limitTestModel(t)
	if st := m.statusState(); st.Plan != "" || st.PlanShort != "" {
		t.Errorf("nothing known, pill %q %q", st.Plan, st.PlanShort)
	}
	*screen = limitScreen(t, "limit/codex-usage.txt")
	m.onAgentOutput(7)
	st := m.statusState()
	if st.Plan != "✦ Codex limit ↻12:05" || st.PlanShort != "✦limit" || !st.PlanLimited {
		t.Errorf("pill %q %q %v", st.Plan, st.PlanShort, st.PlanLimited)
	}
	found := false
	for _, s := range m.statusSpans() {
		if s.id == agentPlanStatusID {
			found = true
		}
	}
	if !found {
		t.Error("no status:plan span")
	}
	if _, ok := tooltipLabels[agentPlanStatusID]; !ok {
		t.Error("no hover label for the plan pill")
	}
	cmd := m.onStatusPlanClick()
	_ = cmd
	if !m.ai.agents.on || !m.ai.agents.usage {
		t.Error("a click does not open AI: Usage")
	}
}

// Spec (AGX-12, D1): with agent_transcripts on, a read session adds its
// plan share and tokens (source 2); "✦ Claude 62% ↻14:30".
func TestAgentUsageFromTranscript(t *testing.T) {
	agentsUnicode(t)
	m, _, _ := limitTestModel(t)
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	m.agentUsageSession("claude", "", agentlog.Session{CLI: agentlog.Claude, ID: "s1", End: now,
		Usage: agentlog.Usage{In: 1200, Out: 300, LimitPct: 62, LimitReset: now.Add(4*time.Hour + 30*time.Minute)}})
	if st := m.statusState(); st.Plan != "✦ Claude 62% ↻14:30" || st.PlanLimited {
		t.Errorf("pill %q %v", st.Plan, st.PlanLimited)
	}
}

// Spec (AGX-12): AI: Usage has a PLANS block: one row per agent account
// with the used share, the reset, the last limit and auto-continue;
// with nothing known it says where the numbers come from (never a vendor
// endpoint, owner D4). Agent: Usage Plans opens it; the AI cannot.
func TestAgentUsagePlansRows(t *testing.T) {
	agentsUnicode(t)
	m, screen, _ := limitTestModel(t)
	m.dispatchAIPalette("ai-usage")
	wantAll(t, agentsText(m, 100, 40), "PLANS", "No plan numbers yet", "!Codex")
	*screen = limitScreen(t, "limit/codex-usage.txt")
	m.onAgentOutput(7)
	text := agentsText(m, 100, 40)
	wantAll(t, text, "PLANS", "Codex", "limit", "↻12:05", "auto-continue off", "THIS SESSION")
	if strings.Index(text, "PLANS") > strings.Index(text, "THIS SESSION") {
		t.Error("PLANS is not first")
	}
	if narrow := agentsText(m, 50, 40); !strings.Contains(narrow, "Codex") {
		t.Errorf("narrow:\n%s", narrow)
	}
	if p, ok := aiActionPolicyFor(agentUsagePlansPaletteID); !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v %v", p, ok)
	}
	m.ai.agents.on, m.ai.agents.usage = false, false
	m.dispatchTasksPalette(agentUsagePlansPaletteID)
	if !m.ai.agents.on || !m.ai.agents.usage {
		t.Error("Agent: Usage Plans does not open the usage view")
	}
}

// Spec (AGX-12): the usage_summary tool gets a plans section: numbers,
// times and names only, never the limit message text.
func TestAgentUsageSummaryPlans(t *testing.T) {
	m, screen, _ := limitTestModel(t)
	*screen = limitScreen(t, "limit/codex-usage.txt")
	m.onAgentOutput(7)
	out, err := aiUsageInfo(m, itermocSnap{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	s := string(b)
	for _, w := range []string{`"plans"`, `"cli":"codex"`, `"limited":true`, `"reset":"2026-11-06T12:05:00Z"`} {
		if !strings.Contains(s, w) {
			t.Errorf("no %s in %s", w, s)
		}
	}
	if strings.Contains(s, "Upgrade") || strings.Contains(s, "hit your") {
		t.Errorf("the message text leaks: %s", s)
	}
}

var _ tea.Cmd
