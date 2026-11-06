package app

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Agent: Sessions… (AGX-9, plan 2.6) ──────────────────────────────────
//
// The past Claude Code and Codex sessions of this project (its folder,
// the folders under it and its worktrees), from their own session files
// (agent_transcripts = read; asked first while off). Enter resumes one
// in a new terminal tab in its folder with the CLI's own resume command,
// checked against `<cli> --help` once per run; → reads it in the chat
// view. The index is built off the UI thread and cached by file time.

const (
	pickerKindAgentSessions  pickerKindEnum = 3741
	agentSessionsPaletteID                  = "agent-sessions"
	agentSessionsPfx                        = "sess:"
	agentSessionsWidth                      = 110
	agentSessionsNote                       = "Enter resumes it in a new terminal tab · → reads it here · / searches inside"
	agentChatPendingSessions                = -1 // agentChatPendingBuf: the card was for Agent: Sessions…
)

// agentSessionsRoot is the project whose sessions are listed (a var for
// tests).
var agentSessionsRoot = aiWorkspaceRoot

// agentSessionsRun types a resume line into a new terminal tab in dir and
// marks the tab as the agent's (a var for tests).
var agentSessionsRun = func(m *Model, dir, line, agentID string) {
	if m.nvim == nil {
		return
	}
	m.debugRunInTerminal(dir, line)
	if t := m.activeTerminalTab(); t != nil {
		t.Agent, t.AgentState = agentID, agents.StartState(agentID)
		if agents.HasScreenRules(agentID) {
			m.agentWatch(t.BufID, true)
		}
	}
}

// agentCLIAgent is the agent preset of a transcript format.
func agentCLIAgent(cli agentlog.CLI) (agents.Agent, bool) {
	return agents.ByID(string(cli))
}

var (
	agentResumeIDRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	agentCodexResumeRe  = regexp.MustCompile(`(?m)^\s+resume\b`)
	agentResumeHelpWait = 3 * time.Second
)

// agentResumeSupported reads a CLI's --help text for its resume command.
func agentResumeSupported(cli agentlog.CLI, help string) bool {
	switch cli {
	case agentlog.Claude:
		return strings.Contains(help, "--resume")
	case agentlog.Codex:
		return agentCodexResumeRe.MatchString(help)
	}
	return false
}

// agentResumeLine is the shell line that resumes session id; false for
// an id that is not a plain session id (never typed into a shell).
func agentResumeLine(cli agentlog.CLI, id string) (string, bool) {
	if !agentResumeIDRe.MatchString(id) {
		return "", false
	}
	switch cli {
	case agentlog.Claude:
		return "claude --resume " + id, true
	case agentlog.Codex:
		return "codex resume " + id, true
	}
	return "", false
}

// agentResumeCheck runs `<cli> --help` and looks for the resume command
// (a var for tests; runs off the UI thread).
var agentResumeCheck = func(cli agentlog.CLI) bool {
	a, ok := agentCLIAgent(cli)
	if !ok {
		return false
	}
	bin, err := agentLookPath(a.Bin)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), agentResumeHelpWait)
	defer cancel()
	out, _ := spawn.CommandContext(ctx, bin, "--help").CombinedOutput()
	return agentResumeSupported(cli, string(out))
}

// openAgentSessions opens the picker (asking first while off) and builds
// the index in the background.
func (m *Model) openAgentSessions() tea.Cmd {
	if !agentTranscriptsOn() {
		m.agentChatPendingBuf = agentChatPendingSessions
		m.openAgentTranscriptsCard("Claude Code and Codex", "~/.claude and ~/.codex")
		return nil
	}
	m.picker = picker.NewItems(" Agent sessions ", []picker.Item{{Header: true, Title: "Reading the sessions…", Group: "x"}})
	m.picker.SetNote([]string{agentSessionsNote})
	m.picker.SetWidth(agentSessionsWidth)
	m.picker.SetRowAction("→", "read")
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAgentSessions
	if m.agentSessIndex == nil {
		m.agentSessIndex = agentlog.NewIndex()
	}
	if m.agentSessionsResume == nil {
		m.agentSessionsResume = map[agentlog.CLI]bool{}
	}
	x, root, homes := m.agentSessIndex, agentSessionsRoot(), agentlogHomes()
	known := map[agentlog.CLI]bool{}
	for k := range m.agentSessionsResume {
		known[k] = true
	}
	return func() tea.Msg {
		sums := x.Build(homes, root)
		checks := map[agentlog.CLI]bool{}
		for _, s := range sums {
			if _, done := checks[s.CLI]; !done && !known[s.CLI] {
				checks[s.CLI] = agentResumeCheck(s.CLI)
			}
		}
		return applyMsg(func(m *Model) tea.Cmd {
			m.agentSessions = sums
			for k, v := range checks {
				m.agentSessionsResume[k] = v
			}
			if m.pickerOpen && m.pickerKind == pickerKindAgentSessions {
				m.picker.SetItems(m.agentSessionItems())
			}
			return nil
		})
	}
}

// agentSessionItems are the picker rows of m.agentSessions.
func (m *Model) agentSessionItems() []picker.Item {
	if len(m.agentSessions) == 0 {
		return []picker.Item{{Header: true, Title: "No Claude Code or Codex sessions in this project yet", Group: "x"}}
	}
	root := agentSessionsRoot()
	out := make([]picker.Item, 0, len(m.agentSessions))
	for i, s := range m.agentSessions {
		name := string(s.CLI)
		if a, ok := agentCLIAgent(s.CLI); ok {
			name = a.Title
		}
		it := picker.Item{ID: agentSessionsPfx + strconv.Itoa(i)}
		if s.Err != "" {
			it.Title, it.Hint = "cannot read this session", name+" · "+filepath.Base(s.Path)
			out = append(out, it)
			continue
		}
		it.Title = s.FirstPrompt
		if it.Title == "" {
			it.Title = "(no prompt)"
		}
		p := "prompts"
		if s.Prompts == 1 {
			p = "prompt"
		}
		hint := s.Start.Local().Format("Jan 2 15:04") + " · " + name + " · " + strconv.Itoa(s.Prompts) + " " + p + " · " + kfmt(s.Tokens) + " tok"
		if s.Cwd != "" && filepath.Clean(s.Cwd) != filepath.Clean(root) {
			if rel, err := filepath.Rel(filepath.Dir(root), s.Cwd); err == nil {
				hint += " · ⑂ " + rel
			}
		}
		if a, ok := agentCLIAgent(s.CLI); !ok || !agentInstalled(a) {
			hint += " · not installed"
		} else if ok, known := m.agentSessionsResume[s.CLI]; known && !ok {
			hint += " · resume not supported"
		}
		it.Hint = hint
		out = append(out, it)
	}
	return out
}

// agentSessionAt is the summary of a picker row id.
func (m *Model) agentSessionAt(id string) (agentlog.Summary, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(id, agentSessionsPfx))
	if err != nil || !strings.HasPrefix(id, agentSessionsPfx) || n < 0 || n >= len(m.agentSessions) {
		return agentlog.Summary{}, false
	}
	return m.agentSessions[n], true
}

// onAgentSessionPicked is Enter: resume the session in a new tab.
func (m *Model) onAgentSessionPicked(id string) tea.Cmd {
	s, ok := m.agentSessionAt(id)
	if !ok {
		return nil
	}
	if s.Err != "" {
		return m.pushToastDetail(toast.Info, "Cannot resume this session", s.Err)
	}
	a, known := agentCLIAgent(s.CLI)
	if !known || !agentInstalled(a) {
		return m.pushToastDetail(toast.Info, string(s.CLI)+" is not installed", "F1 → Agent: Start… installs it")
	}
	if ok, checked := m.agentSessionsResume[s.CLI]; checked && !ok {
		return m.pushToastDetail(toast.Info, "Resume not supported", a.Title+"'s --help has no resume command; update "+a.Title+". → reads the session here.")
	}
	line, ok := agentResumeLine(s.CLI, s.ID)
	if !ok {
		return m.pushToastDetail(toast.Info, "Cannot resume this session", "its id is not a plain session id")
	}
	dir := agentSessionsRoot()
	if s.Cwd != "" {
		if st, err := os.Stat(s.Cwd); err == nil && st.IsDir() {
			dir = s.Cwd
		}
	}
	agentSessionsRun(m, dir, line, a.ID)
	return nil
}

// onAgentSessionsAction is →: read the session in the chat view.
func (m *Model) onAgentSessionsAction(id string) tea.Cmd {
	s, ok := m.agentSessionAt(id)
	if !ok {
		return nil
	}
	m.pickerOpen = false
	title := string(s.CLI)
	if a, ok := agentCLIAgent(s.CLI); ok {
		title = a.Title
	}
	return m.openAgentChatFile(s.CLI, s.Path, title)
}

func init() {
	aiActionPolicies[agentSessionsPaletteID] = aiNever(aiNoPicker + "; it reads the outside agents' own session files (owner decision D1)")
}
