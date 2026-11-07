package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Switch account after a usage limit (CMP-13) ─────────────────────────
//
// Auto-continue (agent_limit.go) waits for the reset. Agent: Switch
// Account… goes on now instead: on an agent tab of a CLI with accounts
// (agent_accounts.go), a card (Cancel first) offers every other account
// of that CLI whose own limit is not on. The pick starts the same agent
// as that account in the tab's folder, in a new tab, and types a short
// handover task once the agent is ready. The old tab is not touched, and
// no sign-in file is moved or read: each account keeps its own folder.

const (
	agentSwitchPaletteID                          = "agent-account-switch"
	confirmKindAgentAccountSwitch confirmKindEnum = 3881
	agentSwitchBtnPfx                             = "as:"
)

// agentSwitchHandover is the first task of the new tab (one line).
const agentSwitchHandover = "My last session in this folder hit its usage limit. Read git status, git diff and the recent commits, then go on with the unfinished work."

// agentSwitchStart starts agent a as label in dir with a first task (a
// var for tests).
var agentSwitchStart = func(m *Model, a agents.Agent, dir, task, label string) tea.Cmd {
	return m.startAgentRun(a, dir, task, label)
}

// agentSwitchTargets are the accounts tab t may switch to: the CLI's
// default folder (when t runs as a label) and its saved labels, without
// t's own account and without an account whose limit is on.
func (m *Model) agentSwitchTargets(t *terminalTab) []string {
	if t == nil || t.Agent == "" {
		return nil
	}
	a, ok := agents.ByID(t.Agent)
	if !ok || a.ConfigEnv == "" {
		return nil
	}
	limited := map[string]bool{}
	now := clock.Now()
	for _, w := range m.agentUsageWindows() {
		if w.CLI == t.Agent && w.Limited(now) {
			limited[w.Account] = true
		}
	}
	var out []string
	for _, l := range append([]string{""}, agentAccountLabels(t.Agent)...) {
		if l == t.Account || limited[l] || (l != "" && agentAccountLabelOK(l) != nil) {
			continue
		}
		out = append(out, l)
	}
	return out
}

// agentSwitchName is how a label shows on a card ("" = the default).
func agentSwitchName(label string) string {
	if label == "" {
		return "default"
	}
	return label
}

// agentSwitchHint is the limit toast's tail when another account is
// free ("" when none).
func (m *Model) agentSwitchHint(t *terminalTab) string {
	ts := m.agentSwitchTargets(t)
	if len(ts) == 0 {
		return ""
	}
	return "Agent: Switch Account… goes on as " + agentSwitchName(ts[0])
}

// openAgentSwitch is Agent: Switch Account… on the active agent tab.
func (m *Model) openAgentSwitch() tea.Cmd {
	t := m.activeTerminalTab()
	if t == nil || t.Agent == "" {
		return m.pushToast(toast.Info, "Switch Account works on an agent tab: open one first")
	}
	a, ok := agents.ByID(t.Agent)
	if !ok || a.ConfigEnv == "" {
		return m.pushToastDetail(toast.Info, t.agentTitle()+" has no accounts", "only Claude Code and Codex have a config-folder variable")
	}
	ts := m.agentSwitchTargets(t)
	if len(ts) == 0 {
		return m.pushToast(toast.Info, "No other free "+a.Title+" account: add one with Agent: Accounts…, or wait for the reset")
	}
	if len(ts) > 4 {
		ts = ts[:4]
	}
	dir := t.Worktree
	if dir == "" {
		dir = "the workspace"
	}
	var b strings.Builder
	b.WriteString(a.Title + " (" + agentSwitchName(t.Account) + ")")
	if t.agentLimitActive() {
		b.WriteString(" hit its usage limit; it resets " + t.LimitAt.In(clock.Now().Location()).Format("15:04") + ".")
	} else {
		b.WriteString(" runs in this tab.")
	}
	b.WriteString("\n\nA new tab starts " + a.Title + " as the account you pick, in " + dir +
		", and types this task once it is ready:\n\n    " + agentSwitchHandover +
		"\n\nThis tab stays as it is. Each account keeps its own sign-in folder; termocode never reads or moves it.")
	btns := make([]confirm.Button, 0, len(ts))
	for i, l := range ts {
		bt := confirm.Button{ID: agentSwitchBtnPfx + l, Title: "Go on as " + agentSwitchName(l)}
		if i == 0 {
			bt.Style = confirm.StylePrimary
		}
		btns = append(btns, bt)
	}
	m.aiS().agentSwitchBuf = t.BufID
	m.confirm = confirm.Risky("Switch "+a.Title+" to another account?", b.String(),
		confirm.Button{ID: "cancel", Title: "Cancel"}, btns...).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentAccountSwitch
	return nil
}

// answerAgentSwitch starts the picked account; Cancel and Esc run
// nothing. The tab and the account are checked again.
func (m *Model) answerAgentSwitch(id string) tea.Cmd {
	buf := m.aiS().agentSwitchBuf
	m.aiS().agentSwitchBuf = 0
	m.confirmOpen = false
	label, ok := strings.CutPrefix(id, agentSwitchBtnPfx)
	if !ok {
		return nil
	}
	t, _ := m.termTabByBuf(buf)
	if t == nil {
		return nil
	}
	a, known := agents.ByID(t.Agent)
	if !known {
		return nil
	}
	for _, l := range m.agentSwitchTargets(t) {
		if l == label {
			return agentSwitchStart(m, a, t.Worktree, agentSwitchHandover, label)
		}
	}
	return nil
}

func init() {
	aiActionPolicies[agentSwitchPaletteID] = aiNever(aiNoAIConfig + "; it starts an outside agent as another of the user's accounts")
}
