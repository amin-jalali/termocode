package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Spec (AGX-1): a preset without screen rules (Amp) started from the
// shell is an agent tab (its name on the tab) whose state is Unknown, and
// the tab keeps its shell glyph: no working / waiting / done mark that
// could be wrong. A preset with rules (Codex) still starts as working.
func TestPresetWithoutRulesTabHasNoMark(t *testing.T) {
	m := termTestModel(100)
	m.onTermOSC(5, "\x1b]133;C;cmdline_url=amp")
	tab := m.terminalTabs[0]
	if tab.Agent != "amp" || tab.AgentState != agents.Unknown {
		t.Fatalf("amp tab: agent %q state %s", tab.Agent, tab.AgentState)
	}
	plain := stripANSITerminal(m.renderTerminalTabBar(m.editorPaneWidth()))
	if !strings.Contains(plain, " Amp ") {
		t.Errorf("no Amp tab name: %q", plain)
	}
	for _, st := range []agents.State{agents.Working, agents.Waiting, agents.Done} {
		if mark, _ := agentMark(st); strings.Contains(plain, " "+mark+" Amp ") {
			t.Errorf("Amp shows the %s mark: %q", st, plain)
		}
	}
	if strings.Contains(plain, " "+theme.GlyphAgentWorking.String()+" Amp ") {
		t.Errorf("Amp shows a working mark: %q", plain)
	}

	m.onTermOSC(7, "\x1b]133;C;cmdline_url=codex")
	if tab := m.terminalTabs[1]; tab.Agent != "codex" || tab.AgentState != agents.Working {
		t.Errorf("codex tab: agent %q state %s", tab.Agent, tab.AgentState)
	}
}
