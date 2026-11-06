package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── External CLI agents in terminal tabs (U8, plan B4 detection) ────────
//
// A terminal tab runs Claude Code, Codex, Gemini CLI, OpenCode or Aider
// when the shell reports that command (OSC 133 C, term_integration.go)
// or when the palette started it ("Agent: Start …"). While it runs, nvim
// tells Go when the tab's text changes (nvim_buf_attach on_lines, at most
// one notification in flight); Go reads the last lines and asks
// internal/agents whether the agent works, waits for the user, or is
// done. No timer: an idle agent costs nothing. The tab shows a 1-cell
// mark in place of its shell glyph, and a toast says "<Agent> is waiting
// for you" when it starts waiting while its tab is not in focus.

// agentWatchLua turns the output watch of a buffer on or off.
const agentWatchLua = `local buf, on = ...
_G.termocode_agent_watch = _G.termocode_agent_watch or {}
local W = _G.termocode_agent_watch
if not on then
  if W[buf] then W[buf].on = false end
  return ''
end
if W[buf] then W[buf].on = true; W[buf].pend = false; return '' end
if not vim.api.nvim_buf_is_valid(buf) then return '' end
W[buf] = { on = true, pend = false }
vim.api.nvim_buf_attach(buf, false, {
  on_lines = function()
    local w = W[buf]
    if not w or not w.on then W[buf] = nil; return true end
    if w.pend then return end
    w.pend = true
    if _G.termocode_notify then _G.termocode_notify('termocode_agent_output', buf) end
  end,
  on_detach = function() W[buf] = nil end,
})
return ''`

// agentTailLua reads the last n lines of buf and re-arms the watch.
const agentTailLua = `local buf, n = ...
local W = _G.termocode_agent_watch
if W and W[buf] then W[buf].pend = false end
if not vim.api.nvim_buf_is_valid(buf) then return '' end
local count = vim.api.nvim_buf_line_count(buf)
return table.concat(vim.api.nvim_buf_get_lines(buf, math.max(count - n, 0), -1, false), '\n')`

// agentTailLines is how much of the screen the detection reads.
const agentTailLines = 60

func (m *Model) agentWatch(buf int, on bool) {
	if m.nvim == nil {
		return
	}
	_ = m.nvim.ExecLuaArgs(agentWatchLua, buf, on)
}

// onAgentOutput runs when an agent tab's text changed.
func (m *Model) onAgentOutput(buf int) tea.Cmd {
	t, shown := m.termTabByBuf(buf)
	if t == nil || t.Agent == "" || m.nvim == nil {
		m.agentWatch(buf, false)
		return nil
	}
	if t.AgentHook { // the agent reports its own state (agent_ctl.go)
		return nil
	}
	var raw string
	if err := m.nvim.ExecLuaResult(agentTailLua, &raw, buf, agentTailLines); err != nil {
		return nil
	}
	lines := strings.Split(raw, "\n")
	st := agents.Detect(t.Agent, lines)
	if task := agentFirstTaskDue(t, lines); task != "" {
		t.FirstTask = "" // at most once
		m.agentType(t.BufID, task)
	}
	if st == agents.Unknown || st == t.AgentState {
		return nil
	}
	t.AgentState = st
	m.agentRollupSync()     // OMX-35
	if m.raceLaneBuf(buf) { // a race lane says its own news (agent_race.go)
		return m.raceOnAgentState(buf, st)
	}
	if st == agents.Done {
		return m.notifyOS("Agent run ended")
	}
	if st != agents.Waiting {
		return nil
	}
	if m.inTerminal && m.termTabVisible(shown) {
		return m.notifyOS("Agent needs your input")
	}
	title := t.Agent
	if a, ok := agents.ByID(t.Agent); ok {
		title = a.Title
	}
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, title+" is waiting for you")
	return tea.Batch(c, m.notifyOS("Agent needs your input"))
}

// agentTitle is the tab name of an agent tab ("" for none).
func (t terminalTab) agentTitle() string {
	if t.Agent == "" {
		return ""
	}
	if a, ok := agents.ByID(t.Agent); ok {
		return a.Title
	}
	return t.Agent
}

// agentMark is the 1-cell state mark of an agent tab and its colour.
func agentMark(s agents.State) (string, theme.Color256) {
	switch s {
	case agents.Waiting:
		return theme.GlyphAgentWaiting.String(), theme.AccentLavender
	case agents.Done:
		return theme.GlyphAgentDone.String(), theme.AccentGreen
	}
	return theme.GlyphAgentWorking.String(), theme.TextSecondary
}

// ── Palette: "Agent: Start …" ────────────────────────────────────────────

const agentPaletteIDPfx = "agent-start-"

// agentPaletteItems lists every palette agent (docs, AI policy); the
// palette itself drops the ones not on PATH (agentPaletteFilter).
func agentPaletteItems() []picker.Item {
	var out []picker.Item
	for _, a := range agents.Registry {
		if a.Palette {
			out = append(out, picker.Item{ID: agentPaletteIDPfx + a.ID, Title: "Agent: Start " + a.Title})
		}
	}
	return out
}

// agentLookPath finds an agent's program on PATH or in termocode's
// tools/bin (agent_install.go; a var for tests).
var agentLookPath = agentLookPathDefault

// agentPaletteFilter drops "Agent: Start …" rows whose program is not
// installed.
func agentPaletteFilter(items []picker.Item) []picker.Item {
	out := items[:0:0]
	for _, it := range items {
		if id, ok := strings.CutPrefix(it.ID, agentPaletteIDPfx); ok {
			a, known := agents.ByID(id)
			if !known {
				continue
			}
			if _, err := agentLookPath(a.Bin); err != nil {
				continue
			}
		}
		out = append(out, it)
	}
	return out
}

// dispatchAgentPalette handles "agent-start-<id>".
func (m *Model) dispatchAgentPalette(id string) (tea.Cmd, bool) {
	switch id {
	case agentPickerPaletteID: // Agent: Start… (agent_picker.go)
		return m.openAgentPicker(), true
	case agentRacePaletteID: // Agent: Race… (agent_race.go)
		return m.openRaceForm("", ""), true
	case agentRaceComparePaletteID:
		return m.openRaceCompare(), true
	}
	if id == agentChatPaletteID { // Agent: Chat View (agent_chat.go)
		return m.toggleAgentChatView(), true
	}
	if id == agentSessionsPaletteID { // Agent: Sessions… (agent_sessions.go)
		return m.openAgentSessions(), true
	}
	if id == agentSessionsSearchID { // Agent: Search Sessions… (agent_sessions_search.go)
		return m.openAgentSessionsSearch(), true
	}
	aid, ok := strings.CutPrefix(id, agentPaletteIDPfx)
	if !ok {
		return nil, false
	}
	a, ok := agents.ByID(aid)
	if !ok {
		return nil, false
	}
	return m.startAgent(a), true
}

// agentLaunchLine is the command line typed into the agent's tab: the
// program, plus the flags that give it termocode's MCP server for this
// launch when its recipe has them (MS-4). mcp_server off, a recipe
// without flags, or a failure to prepare the temp file: just the program,
// and a failure says so in a toast. The user's own config files are never
// touched.
func (m *Model) agentLaunchLine(a agents.Agent) (string, tea.Cmd) {
	return m.agentLaunchLineIn(a, "")
}

// agentLaunchLineIn is agentLaunchLine for an agent started in dir (""
// = the workspace): the MCP server gets that folder as --dir, and the
// line starts with a cd into it, so the agent runs there however the tab
// was opened (AGX-2).
func (m *Model) agentLaunchLineIn(a agents.Agent, dir string) (string, tea.Cmd) {
	line, cmd := m.agentLaunchLineAt(a, dir)
	if dir == "" {
		return line, cmd
	}
	q, ok := agents.ShellWord(dir)
	if !ok {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, a.Title+" starts in the workspace", "the folder's path has a character the shell line cannot quote")
		return line, tea.Batch(cmd, c)
	}
	return "cd " + q + " && " + line, cmd
}

func (m *Model) agentLaunchLineAt(a agents.Agent, dir string) (string, tea.Cmd) {
	if mcpServeMode() == "off" || a.MCP.Kind != agents.MCPFlag {
		return a.Bin, nil
	}
	exe, err := mcpServeExecutable()
	if err != nil || exe == "" {
		return a.Bin, nil
	}
	root := aiWorkspaceRoot()
	if dir != "" {
		root = dir
	}
	file := ""
	if a.MCP.TempFile != "" {
		if file, err = m.writeAgentMCPFile(a, exe, root); err != nil {
			recordError("[mcp-serve] temp config for " + a.ID + ": " + err.Error())
			var c tea.Cmd
			m.toast, c = m.toast.PushDetail(toast.Warn, a.Title+" starts without termocode's tools", err.Error())
			return a.Bin, c
		}
	}
	line, ok := a.MCPLine(exe, root, file)
	if !ok {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, a.Title+" starts without termocode's tools", "the path of termocode or of the folder has a character the shell line cannot quote")
		return a.Bin, c
	}
	return line, nil
}

// startAgent opens a terminal tab in the workspace and runs the agent in
// its shell, so the shell is back when the agent quits. A tab already
// running that agent is shown instead (no second copy by accident).
func (m *Model) startAgent(a agents.Agent) tea.Cmd { return m.startAgentIn(a, "", "") }

// agentStartedIn says whether tab t runs agent id in dir ("" = the
// workspace).
func agentStartedIn(t terminalTab, id, dir string) bool {
	return t.Agent == id && filepath.Clean(t.Worktree+"/") == filepath.Clean(dir+"/")
}

// startAgentWithTask starts a in dir and types task once the agent shows
// its input box (WTX-8: Create + Start).
func (m *Model) startAgentWithTask(a agents.Agent, dir, task string) tea.Cmd {
	return m.startAgentIn(a, dir, task)
}

// startAgentIn is startAgent in folder dir ("" = the workspace), with a
// first task to type ("" = none).
func (m *Model) startAgentIn(a agents.Agent, dir, task string) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	if _, err := agentLookPath(a.Bin); err != nil {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, a.Title+" is not installed", "`"+a.Bin+"` is not on PATH. Install "+a.Title+", then start it again. Guide: guides/agents.md")
		return c
	}
	if !m.termOpen && len(m.hiddenTerminalTabs) > 0 {
		m.toggleTerminalPanel() // bring the hidden tabs back first
	}
	for i, t := range m.terminalTabs {
		if agentStartedIn(t, a.ID, dir) {
			m.showPanelTab(panelKindTerminal)
			m.switchTerminalTab(i)
			return nil
		}
	}
	if card, c := m.agentMCPCardIn(a, dir); card != nil {
		card.task = task
		m.openAgentMCPCard(card) // OpenCode: a project file, ask first
		return nil
	} else if c != nil {
		return tea.Batch(c, m.launchAgentIn(a, dir, task))
	}
	return m.launchAgentIn(a, dir, task)
}

// launchAgent opens the agent's tab and types its command line.
func (m *Model) launchAgent(a agents.Agent) tea.Cmd { return m.launchAgentIn(a, "", "") }

// launchAgentIn is launchAgent in folder dir with a first task.
func (m *Model) launchAgentIn(a agents.Agent, dir, task string) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	m.newTerminalTabIn(dir)
	t := m.activeTerminalTab()
	if t == nil {
		return nil
	}
	// Without shell integration the mark stays until the tab closes;
	// with it, the shell's command-end clears it (term_integration.go).
	t.Agent, t.AgentState = a.ID, agents.StartState(a.ID)
	t.Worktree = dir
	var taskCmd tea.Cmd
	if task != "" {
		if agents.HasScreenRules(a.ID) {
			t.FirstTask = task
		} else {
			taskCmd = m.pushToastDetail(toast.Info, "Type the first task yourself", a.Title+"'s screen cannot be read, so termocode does not know when it is ready.")
		}
	}
	// The listener first, then the line: the agent may call at once.
	serveCmd := m.ensureMCPServeForAgent() // mcp_server auto: the agent may use the IDE's tools
	line, launchCmd := m.agentLaunchLineIn(a, dir)
	_ = m.nvim.ExecLua(fmt.Sprintf(`
		local buf = %d
		local ok, chan = pcall(function() return vim.bo[buf].channel end)
		if ok and chan and chan > 0 then pcall(vim.fn.chansend, chan, %q) end
	`, t.BufID, line+"\n"))
	if agents.HasScreenRules(a.ID) { // a preset without rules: nothing to read
		m.agentWatch(t.BufID, true)
	}
	m.applyLayout()
	m.resizeTerminalSplit()
	return tea.Batch(serveCmd, launchCmd, taskCmd, m.agentMCPHandTip(a))
}

func init() {
	registerNotifyHandler("termocode_agent_output", func(m *Model, args []any) tea.Cmd {
		return m.onAgentOutput(notifyArgInt(args, 0))
	})
	// AI policies (ai_actions_policy.go) of this file's and term_fix.go's
	// palette rows and setting, kept here so the shared map stays as is.
	for _, it := range agentPaletteItems() {
		aiActionPolicies[it.ID] = aiConfirm(ai.ClassRun) // runs a program in a new tab
	}
	aiActionPolicies["terminal-fix-ai"] = aiNever(aiNoNested)
	aiSettingPolicies["terminal_shell_integration"] = aiNever("changes how the user's shell starts; a file-only switch the user edits in config.json")
}

// agentFirstTaskDue is the first task to type now: the tab has one and
// the agent shows its input box (Done), never a question or a y/n prompt
// (WTX-8). "" = not now.
func agentFirstTaskDue(t *terminalTab, lines []string) string {
	if t.FirstTask == "" || !agents.InputReady(t.Agent, lines) {
		return ""
	}
	return t.FirstTask
}

// agentType types text into the agent's tab and presses Enter.
func (m *Model) agentType(buf int, text string) {
	if m.nvim == nil {
		return
	}
	_ = m.nvim.ExecLua(fmt.Sprintf(`
		local buf = %d
		local ok, chan = pcall(function() return vim.bo[buf].channel end)
		if ok and chan and chan > 0 then pcall(vim.fn.chansend, chan, %q) end
	`, buf, agentCleanTask(text)+"\r"))
}

// agentCleanTask is the task as one line (no control characters: a
// newline would send it early, an escape could drive the agent's UI).
func agentCleanTask(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
