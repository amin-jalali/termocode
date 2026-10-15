package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// K3: the agent uses the VISIBLE integrated terminal, as a user does.
//
//   - terminal_send (confirm, class run) types text into a terminal tab.
//     It opens the panel (and a tab) when needed and shows the tab, so the
//     user sees the command run. The editor keeps the keyboard.
//   - terminal_read (auto) returns the last lines of a tab. With wait_ms
//     it waits (max 30 s, Stop cancels) until the shell is idle again and
//     the output is quiet, so "run the tests and read the result" is two
//     calls. After a terminal_send it refreshes the IDE like run_command
//     (explorer, git, open buffers): the typed command may change files.
//   - terminal_tabs: list / new / focus run at once (they start an idle
//     shell or change the view only); close asks (class ide), and when
//     the tab's shell runs a command it always asks (no "Always allow").
//
// run_command stays the hidden one-shot run with captured output.

const (
	aiTermMaxWait  = 30 * time.Second
	aiTermMaxLines = 500
	aiTermMaxBytes = 16 * 1024
	aiTermPoll     = 150 * time.Millisecond
	// aiTermQuietIdle is how long the output must stay the same once the
	// shell is idle; aiTermQuietUnknown when idle can not be checked.
	aiTermQuietIdle    = 400 * time.Millisecond
	aiTermQuietUnknown = 1500 * time.Millisecond
)

var errAINoTerminal = errors.New(`no terminal is open; start one with terminal_send (it opens a tab) or terminal_tabs(op: "new")`)

// aiTermTurn is the terminal state of one chat turn: the buffer tick at
// each terminal_send (so a read waits for new output) and whether the IDE
// needs a refresh.
type aiTermTurn struct {
	mu    sync.Mutex
	sent  map[int]aiTermSent
	dirty bool
}

// aiTermSent is the last terminal_send to a buffer.
type aiTermSent struct {
	tick int    // changedtick just before the text was typed
	line string // the last line of the typed text
}

// aiTermStarted reports whether the shell took the typed text: the
// buffer changed and the last row is no longer the typed command line
// (the terminal echoes typed text before the shell reads it).
func aiTermStarted(s aiTermSent, ok bool, tick int, lines []string) bool {
	if !ok {
		return true
	}
	if tick == s.tick {
		return false
	}
	tail := aiTermTail(lines, 1)
	return s.line == "" || !strings.HasSuffix(tail, s.line)
}

// aiTermSnap is one look at a terminal tab (JSON between Update and the
// tool goroutine).
type aiTermSnap struct {
	Index  int      `json:"index"` // 1-based
	Name   string   `json:"name"`
	Buf    int      `json:"buf"`
	Pid    int      `json:"pid"`
	Tick   int      `json:"tick"`
	Lines  []string `json:"lines"`
	Hidden bool     `json:"hidden"`
}

// aiTermTabs returns the terminal tabs, also those of a hidden panel.
func (m *Model) aiTermTabs() (tabs []terminalTab, active int, hidden bool) {
	if m.termOpen && len(m.terminalTabs) > 0 {
		return m.terminalTabs, m.terminalActiveTab, false
	}
	if len(m.hiddenTerminalTabs) > 0 {
		return m.hiddenTerminalTabs, m.hiddenTerminalActive, true
	}
	return nil, 0, false
}

// aiTermResolve finds tab arg ("" = the active tab, "2" = the second tab,
// else a tab name) and returns its 0-based index.
func aiTermResolve(tabs []terminalTab, active int, arg string) (int, error) {
	arg = strings.TrimSpace(arg)
	if len(tabs) == 0 {
		return 0, errAINoTerminal
	}
	if arg == "" {
		if active < 0 || active >= len(tabs) {
			active = 0
		}
		return active, nil
	}
	if n, err := strconv.Atoi(arg); err == nil {
		if n < 1 || n > len(tabs) {
			return 0, fmt.Errorf("no terminal tab %d; there are %d (see terminal_tabs list)", n, len(tabs))
		}
		return n - 1, nil
	}
	found := -1
	for i, t := range tabs {
		if t.Name == arg {
			if found >= 0 {
				return 0, fmt.Errorf("two terminal tabs are named %q; use the tab number", arg)
			}
			found = i
		}
	}
	if found < 0 {
		return 0, fmt.Errorf("no terminal tab named %q (see terminal_tabs list)", arg)
	}
	return found, nil
}

// aiTermLabel is "tab 2 (name)".
func aiTermLabel(i int, name string) string {
	return fmt.Sprintf("tab %d (%s)", i+1, name)
}

// aiTermKeepFocus remembers where the keys go and returns a func that
// puts them back: opening or showing a tab focuses the shell, but the
// agent must not steal the keyboard from the user.
func (m *Model) aiTermKeepFocus() func() {
	if m.inTerminal || m.nvim == nil {
		return func() {}
	}
	focus, panelFocused := m.focus, m.panelFocused
	win, _ := m.nvim.EvalLuaString(`return tostring(vim.api.nvim_get_current_win())`)
	return func() {
		ok, _ := m.nvim.EvalLuaStringArgs(`local w = ...
			if w > 0 and vim.api.nvim_win_is_valid(w) and not vim.w[w].termocode_panel
				and vim.bo[vim.api.nvim_win_get_buf(w)].buftype ~= 'terminal' then
				vim.api.nvim_set_current_win(w)
				return '1'
			end
			return ''`, parseInt(win))
		if ok != "1" {
			m.ensureEditorWindowCurrent()
		}
		m.inTerminal = false
		m.focus, m.panelFocused = focus, panelFocused
	}
}

// aiTermNewTab starts a new shell tab (opening the panel when needed) and
// returns its index.
func (m *Model) aiTermNewTab() (int, error) {
	if m.nvim == nil {
		return 0, errors.New("the editor is not running")
	}
	before := map[int]bool{}
	tabs, _, _ := m.aiTermTabs()
	for _, t := range tabs {
		before[t.BufID] = true
	}
	switch {
	case !m.termOpen && len(m.hiddenTerminalTabs) > 0:
		m.openTerminalPanel() // shows the hidden tabs
		if m.termOpen {
			m.newTerminalTab()
		}
	default:
		m.newTerminalTab() // opens the panel with a first tab when closed
	}
	t := m.activeTerminalTab()
	if !m.termOpen || t == nil || before[t.BufID] {
		msg := "could not start a shell"
		if m.err != "" {
			msg += ": " + m.err
		}
		return 0, errors.New(msg)
	}
	m.aiTermUnminimize()
	return m.terminalActiveTab, nil
}

// aiTermShow makes the tab with buffer buf the visible panel tab without
// focusing it, opening a hidden panel first. It returns the tab index.
func (m *Model) aiTermShow(buf int) (int, error) {
	if !m.termOpen {
		m.openTerminalPanel()
	}
	idx := -1
	for i, t := range m.terminalTabs {
		if t.BufID == buf {
			idx = i
		}
	}
	if idx < 0 {
		return 0, errors.New("that terminal tab was closed")
	}
	if idx != m.terminalActiveTab || m.panelActive != panelKindTerminal {
		if m.terminalWinID <= 0 {
			m.terminalWinID = m.probeTerminalWindowID()
		}
		_ = m.nvim.ExecLuaArgs(`local win, buf = ...
			if vim.api.nvim_win_is_valid(win) and vim.api.nvim_buf_is_loaded(buf) then
				pcall(vim.api.nvim_win_set_buf, win, buf)
			end`, m.terminalWinID, buf)
		m.terminalActiveTab = idx
		m.panelActive = panelKindTerminal
		m.terminalCwd = m.terminalTabs[idx].Cwd
		m.terminalCwdAt = time.Now()
	}
	m.aiTermUnminimize()
	return idx, nil
}

func (m *Model) aiTermUnminimize() {
	m.terminalMinimized = false
	m.applyLayout()
	m.resizeTerminalSplit()
}

// aiTermSendLua types text into the terminal of buf and returns the
// buffer's changedtick from just before.
const aiTermSendLua = `local buf, text = ...
if not vim.api.nvim_buf_is_valid(buf) or vim.bo[buf].buftype ~= 'terminal' then return -1 end
local ok, chan = pcall(function() return vim.bo[buf].channel end)
if not ok or not chan or chan <= 0 then return -1 end
local tick = vim.api.nvim_buf_get_changedtick(buf)
vim.fn.chansend(chan, text)
return tick`

// aiTermPick runs on Update: pick or make the tab for terminal_send and
// show it. created is true for a new shell (the caller waits for its
// prompt before typing).
func (m *Model) aiTermPick(tabArg string, newTab bool) (buf int, created bool, err error) {
	if m.nvim == nil {
		return 0, false, errors.New("the editor is not running")
	}
	restore := m.aiTermKeepFocus()
	defer restore()
	if !newTab {
		tabs, active, _ := m.aiTermTabs()
		if len(tabs) > 0 || tabArg != "" {
			i, err := aiTermResolve(tabs, active, tabArg)
			if err != nil {
				return 0, false, err
			}
			if _, err := m.aiTermShow(tabs[i].BufID); err != nil {
				return 0, false, err
			}
			return tabs[i].BufID, false, nil
		}
	}
	idx, err := m.aiTermNewTab()
	if err != nil {
		return 0, false, err
	}
	return m.terminalTabs[idx].BufID, true, nil
}

// aiTermType runs on Update: type text into the tab with buffer buf.
func (m *Model) aiTermType(buf int, text string, turn *aiTermTurn) (string, error) {
	idx := -1
	for i, t := range m.terminalTabs {
		if t.BufID == buf {
			idx = i
		}
	}
	if idx < 0 || m.nvim == nil {
		return "", errors.New("that terminal tab was closed")
	}
	tab := m.terminalTabs[idx]
	var tick int
	if err := m.nvim.ExecLuaResult(aiTermSendLua, &tick, buf, text); err != nil {
		return "", err
	}
	if tick < 0 {
		return "", errors.New("the shell of terminal " + aiTermLabel(idx, tab.Name) + " has exited")
	}
	turn.mu.Lock()
	lines := strings.Split(strings.TrimRight(text, "\r\n"), "\n")
	turn.sent[buf] = aiTermSent{tick: tick, line: strings.TrimSpace(lines[len(lines)-1])}
	turn.dirty = true
	turn.mu.Unlock()
	return "sent to terminal " + aiTermLabel(idx, tab.Name) +
		"; the user sees it run. Read the output with terminal_read (wait_ms waits until the command is done).", nil
}

// aiTermSend is terminal_send on the tool goroutine. A new shell gets up
// to aiTermStartWait to draw its prompt first, so the typed command shows
// after the prompt as if the user typed it.
func aiTermSend(ctx context.Context, call aiIDECaller, turn *aiTermTurn, tabArg string, newTab bool, text string) (string, error) {
	var buf int
	var created bool
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		var err error
		buf, created, err = m.aiTermPick(tabArg, newTab)
		return "", nil, err
	}); err != nil {
		return "", err
	}
	if created {
		aiTermWaitPrompt(ctx, call, buf)
	}
	return call(ctx, func(m *Model) (string, tea.Cmd, error) {
		out, err := m.aiTermType(buf, text, turn)
		return out, nil, err
	})
}

const aiTermStartWait = 3 * time.Second

// aiTermWaitPrompt waits until the new shell of buf printed something and
// then stayed quiet for a moment (its prompt), at most aiTermStartWait.
func aiTermWaitPrompt(ctx context.Context, call aiIDECaller, buf int) {
	deadline := time.Now().Add(aiTermStartWait)
	lastTick, stableSince := -1, time.Now()
	for time.Now().Before(deadline) {
		raw, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			if m.nvim == nil {
				return "", nil, errors.New("the editor is not running")
			}
			var raw string
			err := m.nvim.ExecLuaResult(aiTermSnapLua, &raw, buf, 5)
			return raw, nil, err
		})
		var s aiTermSnap
		if err != nil || raw == "" || json.Unmarshal([]byte(raw), &s) != nil {
			return
		}
		if s.Tick != lastTick {
			lastTick, stableSince = s.Tick, time.Now()
		} else if aiTermTail(s.Lines, 5) != "" && time.Since(stableSince) >= 200*time.Millisecond {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// aiTermSnapLua reads the tail of a terminal buffer, its job pid and tick.
const aiTermSnapLua = `local buf, n = ...
if not vim.api.nvim_buf_is_valid(buf) then return '' end
local count = vim.api.nvim_buf_line_count(buf)
-- The terminal pads the screen with blank rows below the prompt.
local from = math.max(count - n - 300, 0)
local pid = 0
local ok, chan = pcall(function() return vim.bo[buf].channel end)
if ok and chan and chan > 0 then
  local okp, p = pcall(vim.fn.jobpid, chan)
  if okp then pid = p end
end
return vim.json.encode({ buf = buf, pid = pid,
  tick = vim.api.nvim_buf_get_changedtick(buf),
  lines = vim.api.nvim_buf_get_lines(buf, from, -1, false) })`

// aiTermSnapshot runs on Update: one look at tab tabArg.
func (m *Model) aiTermSnapshot(tabArg string, n int) (string, error) {
	if m.nvim == nil {
		return "", errors.New("the editor is not running")
	}
	tabs, active, hidden := m.aiTermTabs()
	i, err := aiTermResolve(tabs, active, tabArg)
	if err != nil {
		return "", err
	}
	var raw string
	if err := m.nvim.ExecLuaResult(aiTermSnapLua, &raw, tabs[i].BufID, n); err != nil {
		return "", err
	}
	if raw == "" {
		return "", errors.New("the shell of terminal " + aiTermLabel(i, tabs[i].Name) + " has exited")
	}
	var s aiTermSnap
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return "", err
	}
	s.Index, s.Name, s.Hidden = i+1, tabs[i].Name, hidden
	b, err := json.Marshal(s)
	return string(b), err
}

// aiTermBusy says whether the shell of job pid runs a command: 1 yes,
// 0 no, -1 unknown. It asks ps for the terminal's foreground process
// group: the shell itself (idle) or a command it started (busy). The job
// may be "$SHELL -c $SHELL" (termopen with a string), so the interactive
// shell can also be a child of pid with the same name.
func aiTermBusy(ctx context.Context, pid int) int {
	if pid <= 0 {
		return -1
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ps := func(fields string, p int) []string {
		out, err := spawn.CommandContext(ctx, "ps", "-o", fields, "-p", strconv.Itoa(p)).Output()
		if err != nil {
			return nil
		}
		return strings.Fields(string(out))
	}
	job := ps("tpgid=,comm=", pid)
	if len(job) < 2 {
		return -1
	}
	fg := parseInt(job[0])
	if fg <= 0 {
		return -1
	}
	if fg == pid {
		return 0
	}
	lead := ps("ppid=,comm=", fg) // the foreground group leader
	if len(lead) < 2 {
		return -1
	}
	if parseInt(lead[0]) == pid && lead[1] == job[1] {
		return 0
	}
	return 1
}

func aiTermBusyWord(b int) string {
	switch b {
	case 1:
		return "running a command"
	case 0:
		return "idle"
	}
	return "state unknown"
}

// aiTermTail is the last n lines of lines, without trailing spaces and
// blank rows, at most aiTermMaxBytes (the end is kept).
func aiTermTail(lines []string, n int) string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, strings.TrimRight(l, " \t\r"))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	s := strings.Join(out, "\n")
	if len(s) > aiTermMaxBytes {
		s = "… [cut]\n" + s[len(s)-aiTermMaxBytes:]
	}
	return s
}

// aiTermRead is terminal_read on the tool goroutine.
func aiTermRead(ctx context.Context, call aiIDECaller, turn *aiTermTurn, tabArg string, n int, wait time.Duration) (string, error) {
	if n <= 0 {
		n = 50
	}
	if n > aiTermMaxLines {
		n = aiTermMaxLines
	}
	if wait < 0 {
		wait = 0
	}
	if wait > aiTermMaxWait {
		wait = aiTermMaxWait
	}
	deadline := time.Now().Add(wait)
	var snap aiTermSnap
	busy := -1
	lastTick, stableSince := -1, time.Now()
	timedOut := false
	for {
		raw, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			out, err := m.aiTermSnapshot(tabArg, n)
			return out, nil, err
		})
		if err != nil {
			return "", err
		}
		if err := json.Unmarshal([]byte(raw), &snap); err != nil {
			return "", err
		}
		busy = aiTermBusy(ctx, snap.Pid)
		now := time.Now()
		if snap.Tick != lastTick {
			lastTick, stableSince = snap.Tick, now
		}
		if wait == 0 {
			break
		}
		turn.mu.Lock()
		sent, ok := turn.sent[snap.Buf]
		turn.mu.Unlock()
		started := aiTermStarted(sent, ok, snap.Tick, snap.Lines)
		quiet := now.Sub(stableSince)
		if started && ((busy == 0 && quiet >= aiTermQuietIdle) || (busy < 0 && quiet >= aiTermQuietUnknown)) {
			break
		}
		if now.After(deadline) {
			timedOut = true
			break
		}
		select {
		case <-ctx.Done():
			return "", errAIChatStopped
		case <-time.After(aiTermPoll):
		}
	}
	head := fmt.Sprintf("terminal %s, shell %s", aiTermLabel(snap.Index-1, snap.Name), aiTermBusyWord(busy))
	if snap.Hidden {
		head += ", panel hidden"
	}
	if timedOut {
		head += fmt.Sprintf(", still busy after %s", wait)
	}
	out := head + ":\n" + aiTermTail(snap.Lines, n)
	turn.mu.Lock()
	dirty := turn.dirty
	turn.dirty = false
	turn.mu.Unlock()
	if dirty { // a typed command may have changed files
		out += aiSyncAfter(ctx, call, "", aiSyncDisk)
	}
	return out, nil
}

// aiTermListLua returns the job pid of each buffer (0 when none).
const aiTermListLua = `local out = {}
for _, buf in ipairs({...}) do
  local pid = 0
  if vim.api.nvim_buf_is_valid(buf) then
    local ok, chan = pcall(function() return vim.bo[buf].channel end)
    if ok and chan and chan > 0 then
      local okp, p = pcall(vim.fn.jobpid, chan)
      if okp then pid = p end
    end
  end
  table.insert(out, pid)
end
return vim.json.encode(out)`

type aiTermRow struct {
	Name   string
	Cwd    string
	Pid    int
	Active bool
}

// aiTermRows runs on Update: the tabs with their shell pids.
func (m *Model) aiTermRows() ([]aiTermRow, bool, error) {
	tabs, active, hidden := m.aiTermTabs()
	if len(tabs) == 0 || m.nvim == nil {
		return nil, false, nil
	}
	args := make([]any, len(tabs))
	for i, t := range tabs {
		args[i] = t.BufID
	}
	var raw string
	if err := m.nvim.ExecLuaResult(aiTermListLua, &raw, args...); err != nil {
		return nil, false, err
	}
	var pids []int
	_ = json.Unmarshal([]byte(raw), &pids)
	rows := make([]aiTermRow, len(tabs))
	for i, t := range tabs {
		rows[i] = aiTermRow{Name: t.Name, Cwd: t.Cwd, Active: i == active}
		if i < len(pids) {
			rows[i].Pid = pids[i]
		}
	}
	return rows, hidden, nil
}

// aiTermTabPid runs on the tool goroutine: the shell pid of tab arg.
func aiTermTabPid(ctx context.Context, call aiIDECaller, arg string) (int, error) {
	out, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		tabs, active, _ := m.aiTermTabs()
		i, err := aiTermResolve(tabs, active, arg)
		if err != nil {
			return "", nil, err
		}
		if m.nvim == nil {
			return "", nil, errors.New("the editor is not running")
		}
		var raw string
		if err := m.nvim.ExecLuaResult(aiTermListLua, &raw, tabs[i].BufID); err != nil {
			return "", nil, err
		}
		var pids []int
		_ = json.Unmarshal([]byte(raw), &pids)
		if len(pids) == 0 {
			return "0", nil, nil
		}
		return strconv.Itoa(pids[0]), nil, nil
	})
	if err != nil {
		return 0, err
	}
	return parseInt(out), nil
}

var aiTermOps = []string{"list", "new", "focus", "close"}

// aiTerminalTools are terminal_send, terminal_read and terminal_tabs.
func aiTerminalTools(call aiIDECaller) []ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	num := func(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }
	boolean := func(d string) map[string]any { return map[string]any{"type": "boolean", "description": d} }
	tabParam := str("terminal tab: its number (1 = first) or name; default the active tab")
	turn := &aiTermTurn{sent: map[int]aiTermSent{}}
	return []ai.Tool{
		{
			Name: "terminal_send",
			Description: "Type text into the user's VISIBLE integrated terminal (opens the panel and a tab when needed) and press Enter. " +
				"Use it for interactive or long-running commands the user wants to watch: dev servers, watchers, test runs, REPLs, " +
				"answering a prompt. Then read the output with terminal_read. For a quick command whose output only you need, use run_command.",
			Params: map[string]any{
				"text":    str("text to type, e.g. a shell command"),
				"tab":     tabParam,
				"new_tab": boolean("open a new terminal tab for it"),
				"enter":   boolean("press Enter after the text (default true)"),
			},
			Required: []string{"text"},
			Mutating: true, Class: ai.ClassRun,
			Describe: func(a map[string]any) string {
				s := "Run in terminal: " + ai.ArgString(a, "text")
				if ai.ArgBool(a, "new_tab") {
					s += " (new tab)"
				} else if t := ai.ArgString(a, "tab"); t != "" {
					s += " (tab " + t + ")"
				}
				return s
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				text := ai.ArgString(a, "text")
				enter := true
				if v, ok := a["enter"]; ok && v != nil {
					enter = ai.ArgBool(a, "enter")
				}
				if enter {
					text = strings.TrimRight(text, "\r\n") + "\r"
				}
				if text == "" {
					return "", errors.New("text is empty")
				}
				tab, newTab := ai.ArgString(a, "tab"), ai.ArgBool(a, "new_tab")
				return aiTermSend(ctx, call, turn, tab, newTab, text)
			},
		},
		{
			Name: "terminal_read",
			Description: "Read the last lines of an integrated terminal tab and whether its shell is idle or running a command. " +
				"wait_ms (max 30000) first waits until the command is done and the output is quiet — e.g. after terminal_send of a test run.",
			Params: map[string]any{
				"tab":     tabParam,
				"lines":   num("how many last lines (default 50, max 500)"),
				"wait_ms": num("wait up to this long for the command to finish (default 0, max 30000)"),
			},
			Describe: func(a map[string]any) string { return "Read terminal " + ai.ArgString(a, "tab") },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				wait := time.Duration(ai.ArgInt(a, "wait_ms")) * time.Millisecond
				return aiTermRead(ctx, call, turn, ai.ArgString(a, "tab"), ai.ArgInt(a, "lines"), wait)
			},
		},
		{
			Name: "terminal_tabs",
			Description: "Manage the integrated terminal tabs: list (number, name, idle / running), new (open a shell tab), " +
				"focus (show a tab), close (ends its shell; asks the user).",
			Params: map[string]any{
				"op":  map[string]any{"type": "string", "enum": aiTermOps, "description": "what to do"},
				"tab": str("tab number or name, for focus / close"),
			},
			Required: []string{"op"},
			Mutating: true, Class: ai.ClassIDE, // Gate decides per op
			Describe: func(a map[string]any) string {
				switch ai.ArgString(a, "op") {
				case "close":
					return "Close terminal tab " + ai.ArgString(a, "tab")
				case "new":
					return "Open a terminal tab"
				case "focus":
					return "Show terminal tab " + ai.ArgString(a, "tab")
				}
				return "List terminal tabs"
			},
			Validate: func(a map[string]any) error {
				op := ai.ArgString(a, "op")
				if !contains(aiTermOps, op) {
					return fmt.Errorf("unknown op %q; use one of %s", op, strings.Join(aiTermOps, ", "))
				}
				if op == "close" && strings.TrimSpace(ai.ArgString(a, "tab")) == "" {
					return errors.New("close needs tab (its number or name)")
				}
				return nil
			},
			Gate: func(a map[string]any) (bool, string) {
				if ai.ArgString(a, "op") != "close" {
					return false, "" // list / new / focus: an idle shell or a view change
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				pid, err := aiTermTabPid(ctx, call, ai.ArgString(a, "tab"))
				if err != nil || aiTermBusy(ctx, pid) != 0 {
					return true, "" // a running command would be killed: always ask
				}
				return true, ai.ClassIDE
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				op, tab := ai.ArgString(a, "op"), ai.ArgString(a, "tab")
				if op == "list" {
					return aiTermList(ctx, call)
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					out, err := m.aiTermTabOp(op, tab)
					return out, nil, err
				})
			},
		},
	}
}

// aiTermList is terminal_tabs list.
func aiTermList(ctx context.Context, call aiIDECaller) (string, error) {
	var rows []aiTermRow
	var hidden bool
	_, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		var err error
		rows, hidden, err = m.aiTermRows()
		return "", nil, err
	})
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return `no terminal tabs; terminal_send or terminal_tabs(op: "new") opens one`, nil
	}
	var b strings.Builder
	if hidden {
		b.WriteString("(the terminal panel is hidden; terminal_send or focus shows it)\n")
	}
	for i, r := range rows {
		fmt.Fprintf(&b, "%d. %s", i+1, r.Name)
		if r.Active {
			b.WriteString(" (active)")
		}
		fmt.Fprintf(&b, " — %s", aiTermBusyWord(aiTermBusy(ctx, r.Pid)))
		if r.Cwd != "" {
			b.WriteString(" — " + r.Cwd)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// aiTermTabOp runs terminal_tabs new / focus / close on Update.
func (m *Model) aiTermTabOp(op, tab string) (string, error) {
	if m.nvim == nil {
		return "", errors.New("the editor is not running")
	}
	restore := m.aiTermKeepFocus()
	defer restore()
	switch op {
	case "new":
		i, err := m.aiTermNewTab()
		if err != nil {
			return "", err
		}
		return "opened terminal " + aiTermLabel(i, m.terminalTabs[i].Name), nil
	case "focus", "close":
		tabs, active, _ := m.aiTermTabs()
		i, err := aiTermResolve(tabs, active, tab)
		if err != nil {
			return "", err
		}
		name := tabs[i].Name
		idx, err := m.aiTermShow(tabs[i].BufID)
		if err != nil {
			return "", err
		}
		if op == "focus" {
			return "showing terminal " + aiTermLabel(idx, name), nil
		}
		m.closeTerminalTab(idx)
		return "closed terminal " + aiTermLabel(idx, name), nil
	}
	return "", fmt.Errorf("unknown op %q", op)
}
