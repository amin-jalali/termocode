package app

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Hibernate idle agents, resume on return (AGX-15) ────────────────────
//
// With agent_hibernate_minutes > 0 (0, the default, is off) a Claude Code
// or Codex tab that is done or waiting with no output for that long is
// hibernated: after a card the first time in a session (Keep running
// first), its process is stopped (SIGTERM, 5 s, then SIGKILL, the tab's
// foreground job only: the shell and the tab stay) and its session id is
// kept (from its session file with agent_transcripts = read). Going back
// to the tab types the CLI's own resume command into its shell: the same
// session, or the CLI's last session of the folder when no id is known.
// Never a tab with a y/n or permission prompt on screen, never the tab
// the user is looking at. One idle check in flight per tab; no timer
// while the setting is off.

const (
	confirmKindAgentHibernate confirmKindEnum = 3747
	agentHibernatePaletteID                   = "agent-hibernate-toggle"
	agentStopGrace                            = 5 * time.Second
)

// agentIdleMsg: the idle check of a tab is due.
type agentIdleMsg struct{ buf int }

// agentIdleTick waits d, then runs the check (a var for tests).
var agentIdleTick func(d time.Duration, msg agentIdleMsg) tea.Cmd

func agentIdleTickDefault(d time.Duration, msg agentIdleMsg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return applyMsg(func(m *Model) tea.Cmd { return m.onAgentIdle(msg) })
	})
}

// agentStopForeground stops the program in the foreground of tab buf's
// terminal (a var for tests; agent_hibernate_unix.go).
var agentStopForeground = func(m *Model, buf int) error {
	if m.nvim == nil {
		return errors.New("no editor")
	}
	var pid int
	if err := m.nvim.ExecLuaResult(`local buf = ...
		local ok, chan = pcall(function() return vim.bo[buf].channel end)
		if not ok or not chan or chan <= 0 then return 0 end
		local ok2, pid = pcall(vim.fn.jobpid, chan)
		if ok2 then return pid end
		return 0`, &pid, buf); err != nil {
		return err
	}
	if pid <= 0 {
		return errors.New("the tab has no shell")
	}
	return agentKillForeground(pid, agentStopGrace)
}

// agentHibernateSession is the session id of tab t ("" when not known;
// a var for tests). It is read only with agent_transcripts = read.
var agentHibernateSession = func(t terminalTab) string {
	cli := agentCLI(t.Agent)
	if cli == "" || !agentTranscriptsOn() {
		return ""
	}
	cwd := t.Cwd
	if cwd == "" {
		cwd = aiWorkspaceRoot()
	}
	since := time.Time{}
	if !t.CmdAt.IsZero() {
		since = t.CmdAt.Add(-time.Minute)
	}
	ref, err := agentTranscriptFindAs(cli, t.Account, cwd, since)
	if err != nil {
		return ""
	}
	return agentSessionIDFromPath(ref.Path)
}

// agentSessionIDFromPath is the session id in a session file's name:
// <uuid>.jsonl (Claude Code), rollout-<time>-<uuid>.jsonl (Codex).
func agentSessionIDFromPath(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if len(base) < 36 {
		return ""
	}
	id := base[len(base)-36:]
	if strings.Count(id, "-") != 4 || !agentResumeIDRe.MatchString(id) {
		return ""
	}
	return id
}

// agentHibernateAfter is the idle time of the setting (0: off).
func agentHibernateAfter() time.Duration {
	v := loadSettings().AgentHibernateMinutes
	if v == nil || *v <= 0 {
		return 0
	}
	return time.Duration(*v) * time.Minute
}

// agentCanHibernate: the agent has a resume command.
func agentCanHibernate(t *terminalTab) bool { return agentCLI(t.Agent) != "" }

// agentIdleSeen notes output of tab t and plans its idle check.
func (m *Model) agentIdleSeen(t *terminalTab) tea.Cmd {
	t.lastOutput = clock.Now()
	t.idleAsked = false
	after := agentHibernateAfter()
	if after == 0 || t.idlePending || t.Hibernated || !agentCanHibernate(t) {
		return nil
	}
	t.idlePending = true
	return agentIdleTick(after, agentIdleMsg{buf: t.BufID})
}

// onAgentIdle is a tab's idle check.
func (m *Model) onAgentIdle(msg agentIdleMsg) tea.Cmd {
	t, shown := m.termTabByBuf(msg.buf)
	if t == nil {
		return nil
	}
	t.idlePending = false
	after := agentHibernateAfter()
	if after == 0 || t.Agent == "" || t.Hibernated || t.idleAsked || !agentCanHibernate(t) {
		return nil
	}
	if left := after - clock.Now().Sub(t.lastOutput); left > 0 {
		t.idlePending = true
		return agentIdleTick(left, msg)
	}
	if t.AgentState != agents.Done && t.AgentState != agents.Waiting {
		return nil // the next output plans a new check
	}
	if m.inTerminal && shown >= 0 && shown == m.terminalActiveTab {
		return nil // the user looks at it
	}
	if raw, ok := agentReadTail(m, msg.buf); !ok || agents.IsPrompt(t.Agent, strings.Split(raw, "\n")) {
		return nil
	}
	t.idleAsked = true
	if m.agentHibernateAlways {
		return m.agentHibernate(t)
	}
	if m.confirmOpen {
		return nil // another card is open: ask after the next idle time
	}
	m.agentHibernatePending = msg.buf
	mins := strconv.Itoa(int(after / time.Minute))
	m.confirm = confirm.Risky("Hibernate "+t.agentTitle()+"?",
		t.agentTitle()+" has had no output for "+mins+" min. termocode stops it to free memory and CPU; its session is kept. "+
			"Going back to the tab starts it again on the same session.",
		confirm.Button{ID: "keep", Title: "Keep running"},
		confirm.Button{ID: "hibernate", Title: "Hibernate", Style: confirm.StylePrimary},
		confirm.Button{ID: "always", Title: "Always for this session"}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentHibernate
	return nil
}

// answerAgentHibernate runs the card's answer; Keep running and Esc
// stop nothing.
func (m *Model) answerAgentHibernate(id string) tea.Cmd {
	buf := m.agentHibernatePending
	m.agentHibernatePending = 0
	m.confirmOpen = false
	if id != "hibernate" && id != "always" {
		return nil
	}
	if id == "always" {
		m.agentHibernateAlways = true
	}
	t, _ := m.termTabByBuf(buf)
	if t == nil || t.Agent == "" || t.Hibernated {
		return nil
	}
	return m.agentHibernate(t)
}

// agentHibernate keeps the session id and stops the agent.
func (m *Model) agentHibernate(t *terminalTab) tea.Cmd {
	if raw, ok := agentReadTail(m, t.BufID); ok && agents.IsPrompt(t.Agent, strings.Split(raw, "\n")) {
		return m.pushToast(toast.Info, t.agentTitle()+" asks something: not hibernated")
	}
	id := agentHibernateSession(*t)
	if err := agentStopForeground(m, t.BufID); err != nil {
		return m.pushToastDetail(toast.Warn, t.agentTitle()+" not hibernated", err.Error())
	}
	t.Hibernated, t.HibSession, t.idlePending = true, id, false
	m.agentRollupSync()
	return nil
}

// agentHibernatedLabel is the tab name of a hibernated tab.
func (t terminalTab) agentHibernatedLabel() string {
	if !t.Hibernated {
		return ""
	}
	moon := "☾ "
	if theme.CurrentIconMode == theme.IconModeASCII {
		moon = "zz "
	}
	return moon + t.agentTitle()
}

// agentWakeLine is the resume line of a hibernated tab and whether it
// names the session (else the CLI's last session of the folder).
func agentWakeLine(t terminalTab) (string, bool, error) {
	a, ok := agents.ByID(t.Agent)
	if !ok {
		return "", false, errors.New("unknown agent")
	}
	cli := agentCLI(t.Agent)
	line, exact := agentResumeLine(cli, t.HibSession)
	if !exact {
		switch cli {
		case agentlog.Claude:
			line = "claude --continue"
		case agentlog.Codex:
			line = "codex resume --last"
		default:
			return "", false, errors.New(a.Title + " has no resume command")
		}
	}
	if p, ok := agentFindProfile(t.Agent, t.Profile); ok { // CMP-21: the same model and mode
		var err error
		if line, err = agents.ProfileLine(a, p, line); err != nil {
			return "", false, err
		}
	}
	line, err := agentAccountLine(a, t.Account, line)
	return line, exact, err
}

// agentWake resumes a hibernated tab: its resume line in its shell.
func (m *Model) agentWake(t *terminalTab) {
	if t == nil || !t.Hibernated {
		return
	}
	line, exact, err := agentWakeLine(*t)
	if err != nil {
		m.agentLater(m.pushToastDetail(toast.Warn, t.agentTitle()+" not resumed", err.Error()))
		return
	}
	t.Hibernated, t.HibSession = false, ""
	t.AgentState = agents.StartState(t.Agent)
	t.LimitMsg, t.LimitAt = "", time.Time{}
	agentChatSend(m, t.BufID, line)
	m.agentLater(agentTypeLater(m, t.BufID, "\r"))
	msg := "Resuming " + t.agentTitle() + " (same session)"
	if !exact {
		msg = "Resuming " + t.agentTitle() + ": the last session of this folder (no session id was known)"
	}
	m.agentLater(m.pushToast(toast.Info, msg))
	m.agentRollupSync()
}

// agentLater queues a command from code that cannot return one (a tab
// switch); Update drains it.
func (m *Model) agentLater(c tea.Cmd) {
	if c != nil {
		m.agentLaterCmds = append(m.agentLaterCmds, c)
	}
}

// drainAgentLater returns the queued commands.
func (m *Model) drainAgentLater() tea.Cmd {
	if len(m.agentLaterCmds) == 0 {
		return nil
	}
	c := tea.Batch(m.agentLaterCmds...)
	m.agentLaterCmds = nil
	return c
}

// toggleAgentHibernate is Agent: Hibernate or Wake on the active tab.
func (m *Model) toggleAgentHibernate() tea.Cmd {
	t := m.activeTerminalTab()
	if t == nil || t.Agent == "" || !agentCanHibernate(t) {
		return m.pushToast(toast.Info, "Hibernate works on a Claude Code or Codex tab")
	}
	if t.Hibernated {
		m.agentWake(t)
		return m.drainAgentLater()
	}
	return m.agentHibernate(t)
}

func init() {
	agentIdleTick = agentIdleTickDefault
	aiActionPolicies[agentHibernatePaletteID] = aiNever(aiNoAIConfig + "; it stops or starts an outside agent's process")
	aiSettingPolicies["agent_hibernate_minutes"] = aiNever(aiNoAIConfig + "; it lets termocode stop idle outside agents")
}
