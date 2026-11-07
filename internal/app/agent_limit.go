package app

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Usage limits in agent tabs, auto-continue (AGX-11) ──────────────────
//
// The agent's own limit message on its screen (agents.LimitLine) names
// the tab "limit ↻14:30" until the reset and toasts once. With
// agent_auto_continue on (off by default) or the tab's own toggle on,
// termocode types "continue" once, at the reset + 1 min, and only when
// the agent still sits after that limit: not working, no y/n or
// permission prompt on screen, the same limit. Every auto-type goes
// through this one guard.

// autoContinueMode is a tab's own auto-continue switch.
type autoContinueMode int8

const (
	autoContinueDefault autoContinueMode = iota // follow agent_auto_continue
	autoContinueOn
	autoContinueOff
)

const agentAutoContinuePaletteID = "agent-auto-continue-toggle"

// agentLimitNotice is the OS notification: generic, no agent output.
const agentLimitNotice = "An agent hit its usage limit"

// agentLimitResumeMsg: the reset time of a tab's limit came.
type agentLimitResumeMsg struct {
	buf int
	at  int64 // LimitAt in ms: a tick of an older limit does nothing
}

// agentReadTail reads the last lines of an agent tab (a var for tests).
var agentReadTail = func(m *Model, buf int) (string, bool) {
	if m.nvim == nil {
		return "", false
	}
	var raw string
	if err := m.nvim.ExecLuaResult(agentTailLua, &raw, buf, agentTailLines); err != nil {
		return "", false
	}
	return raw, true
}

// agentLimitTick waits d, then sends msg (a var for tests).
var agentLimitTick = func(d time.Duration, msg agentLimitResumeMsg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return applyMsg(func(m *Model) tea.Cmd { return m.onAgentLimitResume(msg) })
	})
}

// agentTypeLater types text into buf after a short pause (the text,
// then Enter: not one paste). A var for tests.
var agentTypeLater = func(_ *Model, buf int, text string) tea.Cmd {
	return tea.Tick(agentChatEnterDelay, func(time.Time) tea.Msg {
		return applyMsg(func(m *Model) tea.Cmd { agentChatSend(m, buf, text); return nil })
	})
}

// agentLimitResumeAfter is the wait from now to a reset + 1 min.
func agentLimitResumeAfter(reset time.Time) time.Duration {
	return aiLimitResumeDelay(reset, clock.Now())
}

// agentLimitActive reports a limit whose reset has not come yet.
func (t terminalTab) agentLimitActive() bool {
	return t.LimitMsg != "" && !t.LimitAt.IsZero() && clock.Now().Before(t.LimitAt)
}

// agentLimitLabel is "limit ↻14:30" ("" when no limit is on).
func (t terminalTab) agentLimitLabel() string {
	if !t.agentLimitActive() {
		return ""
	}
	return "limit ↻" + t.LimitAt.In(clock.Now().Location()).Format("15:04")
}

// agentAutoContinueOn is the tab's switch, else the setting.
func (m *Model) agentAutoContinueOn(t *terminalTab) bool {
	switch t.AutoContinue {
	case autoContinueOn:
		return true
	case autoContinueOff:
		return false
	}
	v := loadSettings().AgentAutoContinue
	return v != nil && *v
}

// agentSeeLimit looks for a limit message on the tab's screen.
func (m *Model) agentSeeLimit(t *terminalTab, lines []string) tea.Cmd {
	line, ok := agents.LimitLine(t.Agent, lines)
	if !ok {
		return nil
	}
	now := clock.Now()
	msg := ai.Redact(line)
	if msg == t.LimitMsg && (t.LimitAt.IsZero() || now.Before(t.LimitAt.Add(2*aiLimitResumeMargin))) {
		return nil // the same message, still on screen
	}
	reset := clistep.LimitReset(line, now)
	t.LimitMsg, t.LimitAt, t.limitTyped = msg, reset, false
	m.agentUsageLimit(t, msg, reset, now) // AGX-12 (agent_usage.go)
	title := t.agentTitle()
	text := title + " hit its usage limit"
	if !reset.IsZero() {
		text += " · resets " + reset.In(now.Location()).Format("15:04")
	}
	if hint := m.agentSwitchHint(t); hint != "" { // CMP-13 (agent_switch.go)
		text += " · " + hint
	}
	var cmds []tea.Cmd
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Warn, text)
	cmds = append(cmds, c, m.notifyOS(agentLimitNotice))
	if !reset.IsZero() && m.agentAutoContinueOn(t) {
		cmds = append(cmds, agentLimitTick(agentLimitResumeAfter(reset), agentLimitResumeMsg{buf: t.BufID, at: reset.UnixMilli()}))
	}
	return tea.Batch(cmds...)
}

// onAgentLimitResume types "continue" once into the tab whose limit
// reset, behind the guard.
func (m *Model) onAgentLimitResume(msg agentLimitResumeMsg) tea.Cmd {
	t, _ := m.termTabByBuf(msg.buf)
	if t == nil || t.Agent == "" || t.limitTyped || t.LimitMsg == "" || t.LimitAt.UnixMilli() != msg.at ||
		!m.agentAutoContinueOn(t) || t.AgentState == agents.Working {
		return nil
	}
	raw, ok := agentReadTail(m, msg.buf)
	if !ok {
		return nil
	}
	if agents.IsPrompt(t.Agent, strings.Split(raw, "\n")) {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, t.agentTitle()+" asks something: auto-continue did not type")
		return c
	}
	t.limitTyped = true
	agentChatSend(m, msg.buf, "continue")
	enter := agentTypeLater(m, msg.buf, "\r")
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, "Typed continue into "+t.agentTitle()+" (its limit reset)")
	return tea.Batch(enter, c)
}

// toggleAgentAutoContinue is Agent: Toggle Auto-Continue on the active
// agent tab.
func (m *Model) toggleAgentAutoContinue() tea.Cmd {
	t := m.activeTerminalTab()
	if t == nil || t.Agent == "" {
		return m.pushToast(toast.Info, "Auto-continue works on an agent tab: open one first")
	}
	if m.agentAutoContinueOn(t) {
		t.AutoContinue = autoContinueOff
	} else {
		t.AutoContinue = autoContinueOn
	}
	state := "off"
	if t.AutoContinue == autoContinueOn {
		state = "on"
	}
	cmds := []tea.Cmd{m.pushToast(toast.Info, "Auto-continue "+state+" for "+t.agentTitle())}
	if t.AutoContinue == autoContinueOn && t.agentLimitActive() && !t.limitTyped {
		cmds = append(cmds, agentLimitTick(agentLimitResumeAfter(t.LimitAt), agentLimitResumeMsg{buf: t.BufID, at: t.LimitAt.UnixMilli()}))
	}
	return tea.Batch(cmds...)
}

func init() {
	aiActionPolicies[agentAutoContinuePaletteID] = aiNever(aiNoAIConfig + "; it lets termocode type into an outside agent's tab")
	aiSettingPolicies["agent_auto_continue"] = aiNever(aiNoAIConfig + "; it lets termocode type continue into an outside agent's tab after its usage limit resets")
}
