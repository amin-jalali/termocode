package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentctl"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/clock"
)

const hibSessionID = "0199aaaa-bbbb-7ccc-8ddd-eeeeeeee0001"

// hibTestModel is limitTestModel with a movable clock, the idle ticks,
// the stops and the session lookup recorded.
type hibTest struct {
	m       *Model
	screen  *string
	typed   *[]string
	now     *time.Time
	ticks   []time.Duration
	stopped []int
	session string
}

func newHibTest(t *testing.T, minutes string) *hibTest {
	t.Helper()
	agentsUnicode(t)
	m, screen, typed := limitTestModel(t)
	if minutes != "" {
		writeSettingsJSON(t, `{"agent_hibernate_minutes": `+minutes+`}`)
	}
	h := &hibTest{m: m, screen: screen, typed: typed, session: hibSessionID}
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	h.now = &now
	t.Cleanup(clock.Set(func() time.Time { return *h.now }))
	oldTick := agentIdleTick
	agentIdleTick = func(d time.Duration, _ agentIdleMsg) tea.Cmd { h.ticks = append(h.ticks, d); return nil }
	t.Cleanup(func() { agentIdleTick = oldTick })
	oldStop := agentStopForeground
	agentStopForeground = func(_ *Model, buf int) error { h.stopped = append(h.stopped, buf); return nil }
	t.Cleanup(func() { agentStopForeground = oldStop })
	oldFind := agentHibernateSession
	agentHibernateSession = func(terminalTab) string { return h.session }
	t.Cleanup(func() { agentHibernateSession = oldFind })
	*screen = limitScreen(t, "codex-done.txt")
	return h
}

func (h *hibTest) at(min int) { *h.now = time.Date(2026, 11, 6, 10, min, 0, 0, time.UTC) }

func (h *hibTest) output(screen string) {
	*h.screen = screen
	h.m.onAgentOutput(7)
}

func (h *hibTest) fire() { h.m.onAgentIdle(agentIdleMsg{buf: 7}) }

// Spec (AGX-15): agent_hibernate_minutes 0 (the default) never plans an
// idle check.
func TestAgentHibernateOffByDefault(t *testing.T) {
	h := newHibTest(t, "")
	h.output(*h.screen)
	h.fire()
	if len(h.ticks) != 0 || len(h.stopped) != 0 || h.m.confirmOpen {
		t.Errorf("ticks %v stopped %v card %v", h.ticks, h.stopped, h.m.confirmOpen)
	}
}

// Spec (AGX-15): the idle clock. A done tab plans one check after N
// minutes; more output does not plan a second one; a check that comes
// early (output since) waits the rest; once idle long enough the card
// asks first, Keep running first and focused. Keep running stops
// nothing and asks again only after new output and a new idle time.
func TestAgentHibernateIdleClockAndCard(t *testing.T) {
	h := newHibTest(t, "10")
	h.output(*h.screen)
	if len(h.ticks) != 1 || h.ticks[0] != 10*time.Minute {
		t.Fatalf("ticks %v", h.ticks)
	}
	h.at(4)
	h.output(*h.screen + "\n")
	if len(h.ticks) != 1 {
		t.Errorf("a second check planned: %v", h.ticks)
	}
	h.at(10)
	h.fire()
	if len(h.ticks) != 2 || h.ticks[1] != 4*time.Minute || h.m.confirmOpen {
		t.Fatalf("early check: ticks %v card %v", h.ticks, h.m.confirmOpen)
	}
	h.at(14)
	h.fire()
	if !h.m.confirmOpen || h.m.confirmKind != confirmKindAgentHibernate || h.m.confirm.SafeID() != "keep" {
		t.Fatalf("card %v %v %q", h.m.confirmOpen, h.m.confirmKind, h.m.confirm.SafeID())
	}
	h.m.answerAgentHibernate("keep")
	if len(h.stopped) != 0 || h.m.terminalTabs[0].Hibernated {
		t.Errorf("Keep running stopped it: %v", h.stopped)
	}
	h.at(30)
	h.fire()
	if h.m.confirmOpen {
		t.Error("asked again without new output")
	}
}

// Spec (AGX-15): Hibernate stops the agent's process (the tab stays) and
// keeps its session id; the tab shows ☾. Always for this session skips
// the card next time.
func TestAgentHibernateStopsAndAlways(t *testing.T) {
	h := newHibTest(t, "10")
	h.output(*h.screen)
	h.at(10)
	h.fire()
	h.m.answerAgentHibernate("always")
	tab := h.m.terminalTabs[0]
	if len(h.stopped) != 1 || h.stopped[0] != 7 || !tab.Hibernated || tab.HibSession != hibSessionID {
		t.Fatalf("stopped %v tab %+v", h.stopped, tab)
	}
	if got := h.m.panelEntryName(panelBarEntry{Kind: panelKindTerminal, Index: 0}); got != "☾ Codex" {
		t.Errorf("tab name %q", got)
	}
	if len(*h.typed) != 0 {
		t.Errorf("typed %q", *h.typed)
	}
	// A second agent tab hibernates without a card.
	h.m.terminalTabs = append(h.m.terminalTabs, terminalTab{BufID: 8, Agent: "claude", AgentState: agents.Done})
	oldRead := agentReadTail
	agentReadTail = func(_ *Model, buf int) (string, bool) { return limitScreen(t, "claude-done.txt"), true }
	defer func() { agentReadTail = oldRead }()
	h.m.onAgentOutput(8)
	h.at(25)
	h.m.onAgentIdle(agentIdleMsg{buf: 8})
	if h.m.confirmOpen || len(h.stopped) != 2 || !h.m.terminalTabs[1].Hibernated {
		t.Errorf("always: card %v stopped %v", h.m.confirmOpen, h.stopped)
	}
}

// Spec (AGX-15): never a tab in a y/n or permission prompt (the user may
// be about to answer), never the tab the user looks at in the focused
// terminal, never an agent without a resume command.
func TestAgentHibernateNever(t *testing.T) {
	t.Run("prompt", func(t *testing.T) {
		h := newHibTest(t, "10")
		h.output(limitScreen(t, "codex-waiting-command.txt"))
		h.at(10)
		h.fire()
		if h.m.confirmOpen || len(h.stopped) != 0 {
			t.Error("hibernates a prompt")
		}
	})
	t.Run("on show", func(t *testing.T) {
		h := newHibTest(t, "10")
		h.output(*h.screen)
		h.m.inTerminal, h.m.terminalActiveTab = true, 0
		h.at(10)
		h.fire()
		if h.m.confirmOpen || len(h.stopped) != 0 {
			t.Error("hibernates the tab on show")
		}
	})
	t.Run("no resume", func(t *testing.T) {
		h := newHibTest(t, "10")
		h.m.terminalTabs[0].Agent = "gemini"
		*h.screen = limitScreen(t, "gemini-done.txt")
		h.m.onAgentOutput(7)
		h.at(10)
		h.fire()
		if h.m.confirmOpen || len(h.stopped) != 0 || len(h.ticks) != 0 {
			t.Error("plans or hibernates an agent without resume")
		}
	})
}

// Spec (AGX-15): going back to a hibernated tab resumes it with the
// same session (the CLI's own resume command); without a known id it
// uses the CLI's last-session flag and says so. An account tab keeps
// its folder.
func TestAgentHibernateResume(t *testing.T) {
	h := newHibTest(t, "10")
	h.output(*h.screen)
	h.at(10)
	h.fire()
	h.m.answerAgentHibernate("hibernate")
	h.m.terminalTabs = append(h.m.terminalTabs, terminalTab{BufID: 9, Name: "zsh"})
	h.m.switchTerminalTab(1)
	h.m.switchTerminalTab(0)
	if len(*h.typed) != 2 || (*h.typed)[0] != "codex resume "+hibSessionID || (*h.typed)[1] != "\r" {
		t.Fatalf("typed %q", *h.typed)
	}
	tab := h.m.terminalTabs[0]
	if tab.Hibernated || tab.AgentState != agents.Working {
		t.Errorf("tab %+v", tab)
	}
	if c := h.m.drainAgentLater(); c == nil || !strings.Contains(toastText(*h.m), "Resuming Codex") {
		t.Errorf("toast %q", toastText(*h.m))
	}
	// No id: the last session of this folder.
	*h.typed = nil
	h.session = ""
	h.at(30)
	h.output(*h.screen + "\n\n")
	h.at(45)
	h.fire()
	h.m.answerAgentHibernate("hibernate")
	h.m.terminalTabs[0].Account = "work"
	h.m.switchTerminalTab(1)
	h.m.switchTerminalTab(0)
	if len(*h.typed) == 0 || !strings.HasPrefix((*h.typed)[0], "env CODEX_HOME=") || !strings.HasSuffix((*h.typed)[0], " codex resume --last") {
		t.Errorf("typed %q", *h.typed)
	}
	if !strings.Contains(toastText(*h.m), "last session") {
		t.Errorf("toast %q", toastText(*h.m))
	}
}

// Spec (AGX-15): Agent: Hibernate or Wake works on the active agent tab;
// the AI may not run it, nor change agent_hibernate_minutes.
func TestAgentHibernatePolicy(t *testing.T) {
	h := newHibTest(t, "")
	if p, ok := aiActionPolicyFor(agentHibernatePaletteID); !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v %v", p, ok)
	}
	if p, ok := aiSettingPolicies["agent_hibernate_minutes"]; !ok || p.Gate != aiGateNever {
		t.Errorf("setting policy %+v %v", p, ok)
	}
	h.m.terminalActiveTab = 0
	h.m.dispatchTasksPalette(agentHibernatePaletteID)
	if !h.m.terminalTabs[0].Hibernated || len(h.stopped) != 1 {
		t.Errorf("hibernate now: %v %v", h.m.terminalTabs[0].Hibernated, h.stopped)
	}
	h.m.dispatchTasksPalette(agentHibernatePaletteID)
	if h.m.terminalTabs[0].Hibernated || len(*h.typed) == 0 {
		t.Errorf("wake: %v %q", h.m.terminalTabs[0].Hibernated, *h.typed)
	}
}

// Spec (AGX-15): a prompt sent to a hibernated tab (termocode agent
// prompt, the outside agents' control socket) is never typed into the
// bare shell: the tab resumes and the sender is told to send it again.
func TestAgentHibernatePromptWakesFirst(t *testing.T) {
	h := newHibTest(t, "")
	tab := &h.m.terminalTabs[0]
	tab.Hibernated, tab.HibSession = true, hibSessionID
	rep := h.m.agentCtlSend(agentCtlTab{t: tab, id: "1"}, agentctl.Request{Text: "rm -rf build"})
	if !strings.Contains(rep.Error, "hibernated") {
		t.Errorf("reply %+v", rep)
	}
	for _, s := range *h.typed {
		if strings.Contains(s, "rm -rf") {
			t.Fatalf("the prompt went into the shell: %q", *h.typed)
		}
	}
	if tab.Hibernated || len(*h.typed) == 0 || (*h.typed)[0] != "codex resume "+hibSessionID {
		t.Errorf("not resumed: %v %q", tab.Hibernated, *h.typed)
	}
}
