package app

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/agents"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/cfgfile"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Agent: Chat View (AGX-8, plan 2.6) ──────────────────────────────────
//
// A Claude Code or Codex tab gets a second face in the bottom panel: its
// session file (agent_transcripts.go) drawn as a chat. Tool calls are one
// row each with their result's mark; results stay collapsed. Typing and
// Enter send the text to the tab (the user typed it, so no card); a
// permission prompt is answered in the terminal, never by termocode. v
// with an empty box (or the command again) shows the terminal. The file
// is read off the UI thread and followed with a 1 s poll while the view
// is open; nothing polls once it is closed.

const (
	confirmKindAgentTranscripts confirmKindEnum = 3740
	agentChatPaletteID                          = "agent-chat-view"
	agentChatPoll                               = time.Second
	agentChatEnterDelay                         = 80 * time.Millisecond // the text, then Enter (not one paste)
)

// agentChatState is the open chat view (one at a time).
type agentChatState struct {
	gen      int
	buf      int // the agent tab's terminal buffer (0: none)
	agent    string
	cli      agentlog.CLI
	title    string
	cwd      string
	since    time.Time
	readOnly bool   // opened from the history: no tab to type into
	account  string // the tab's agent account (agent_accounts.go)
	path     string

	tail    *agentlog.Tail // used only by the one poll in flight
	polling bool
	loaded  bool
	missing bool // no session file yet
	err     error
	sess    agentlog.Session

	input  string
	scroll int // rows up from the newest

	demo      bool
	demoState agents.State
}

var agentChatGen int

// agentCLI is the transcript format of an agent preset ("" none).
func agentCLI(agentID string) agentlog.CLI {
	switch agentID {
	case "claude":
		return agentlog.Claude
	case "codex":
		return agentlog.Codex
	}
	return ""
}

// agentChatSend types text into the terminal of buf (a var for tests).
var agentChatSend = func(m *Model, buf int, text string) {
	if m.nvim == nil || buf <= 0 {
		return
	}
	_ = m.nvim.ExecLuaArgs(`local buf, text = ...
		if vim.api.nvim_buf_is_loaded(buf) and vim.bo[buf].buftype == 'terminal' then
			local ok, chan = pcall(function() return vim.bo[buf].channel end)
			if ok and chan and chan > 0 then pcall(vim.fn.chansend, chan, text) end
		end`, buf, text)
}

// toggleAgentChatView is Agent: Chat View: the chat of the agent tab on
// show, or the terminal again when the chat is on show.
func (m *Model) toggleAgentChatView() tea.Cmd {
	if c := m.agentChat; c != nil && m.termOpen && m.panelActive == panelKindAgentChat {
		m.agentChatShowTerminal()
		return nil
	}
	return m.openAgentChatView()
}

// openAgentChatView opens the chat of the active terminal tab, asking
// first while agent_transcripts is off.
func (m *Model) openAgentChatView() tea.Cmd {
	t := m.activeTerminalTab()
	if t == nil || agentCLI(t.Agent) == "" {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "Chat View needs a Claude Code or Codex tab",
			"Show that tab first, or start one with F1 → Agent: Start…")
		return c
	}
	if !agentTranscriptsOn() {
		m.agentChatPendingBuf = t.BufID
		home := "~/.claude"
		if agentCLI(t.Agent) == agentlog.Codex {
			home = "~/.codex"
		}
		m.openAgentTranscriptsCard(t.agentTitle(), home)
		return nil
	}
	return m.openAgentChatFor(*t)
}

// openAgentTranscriptsCard asks to turn agent_transcripts on (Not now
// first; Esc and Enter keep it off).
func (m *Model) openAgentTranscriptsCard(who, home string) {
	body := "termocode can show the session files that " + who + " write for themselves in " + home + ": as a chat (Agent: Chat View), as a history (Agent: Sessions…) and in a search.\n\n" +
		"Read only: termocode never writes there. Secrets are hidden before anything is drawn. Nothing goes to the AI or the web panel.\n\n" +
		"Turn on sets agent_transcripts to read in config.json. Set it to off to stop."
	m.confirm = confirm.Risky("Read "+who+"'s session files?", body,
		confirm.Button{ID: "cancel", Title: "Not now"},
		confirm.Button{ID: "on", Title: "Turn on", Style: confirm.StylePrimary}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentTranscripts
}

// answerAgentTranscripts is the card's answer: Turn on writes the
// setting and opens the view; Not now and Esc do nothing.
func (m *Model) answerAgentTranscripts(id string) tea.Cmd {
	buf := m.agentChatPendingBuf
	m.agentChatPendingBuf = 0
	if id != "on" {
		return nil
	}
	if err := saveAgentTranscripts("read"); err != nil {
		return m.pushToastDetail(toast.Errr, "agent_transcripts not saved", err.Error())
	}
	if buf == agentChatPendingSessions {
		return m.openAgentSessions()
	}
	if t, _ := m.termTabByBuf(buf); t != nil {
		return m.openAgentChatFor(*t)
	}
	return nil
}

// saveAgentTranscripts writes agent_transcripts to config.json.
func saveAgentTranscripts(v string) error {
	path, err := settingsConfigPath()
	if err != nil {
		return err
	}
	return cfgfile.Update(path, func(cfg map[string]any) error {
		cfg["agent_transcripts"] = v
		return nil
	})
}

// openAgentChatFor shows the chat of tab t (the same one if it is open).
func (m *Model) openAgentChatFor(t terminalTab) tea.Cmd {
	if c := m.agentChat; c != nil && c.buf == t.BufID && !c.readOnly {
		m.showAgentChatPanel()
		return m.agentChatPollCmd(c.gen)
	}
	cwd := terminalDir(t.Cwd) // not nvim's term:// name (view.go)
	if cwd == "" {
		cwd = aiWorkspaceRoot()
	}
	since := time.Time{}
	if !t.CmdAt.IsZero() {
		since = t.CmdAt.Add(-time.Minute)
	}
	agentChatGen++
	m.agentChat = &agentChatState{gen: agentChatGen, buf: t.BufID, agent: t.Agent, cli: agentCLI(t.Agent),
		title: t.agentTitle(), cwd: cwd, since: since, account: t.Account}
	m.showAgentChatPanel()
	return m.agentChatPollCmd(agentChatGen)
}

// openAgentChatFile opens a session file read only (Agent: Sessions… o).
func (m *Model) openAgentChatFile(cli agentlog.CLI, path, title string) tea.Cmd {
	agentChatGen++
	m.agentChat = &agentChatState{gen: agentChatGen, cli: cli, title: title, readOnly: true, path: path,
		tail: agentlog.NewTail(cli, path)}
	m.showAgentChatPanel()
	return m.agentChatPollCmd(agentChatGen)
}

func (m *Model) showAgentChatPanel() {
	m.showPanelTab(panelKindAgentChat)
	m.panelActive = panelKindAgentChat
	m.panelFocused = true
	m.inTerminal = false
}

// closeAgentChat closes the view; its poll ends at the next tick.
func (m *Model) closeAgentChat() {
	for _, k := range m.panelTabs {
		if k == panelKindAgentChat {
			m.closePanelTab(panelKindAgentChat)
			break
		}
	}
	m.agentChat = nil
}

// agentChatShowTerminal shows the agent's terminal tab (the chat tab
// stays in the bar).
func (m *Model) agentChatShowTerminal() {
	c := m.agentChat
	if c == nil || c.buf <= 0 {
		return
	}
	for i, t := range m.terminalTabs {
		if t.BufID == c.buf {
			m.switchTerminalTab(i)
			if m.nvim != nil {
				m.applyLayout()
				m.resizeTerminalSplit()
			}
			return
		}
	}
}

// agentChatPollCmd reads the file (finding it first) off the UI thread
// and schedules the next poll; nil once the view is closed.
func (m *Model) agentChatPollCmd(gen int) tea.Cmd {
	c := m.agentChat
	if c == nil || c.gen != gen || c.demo || c.polling {
		return nil
	}
	if !agentTranscriptsOn() && !c.readOnly {
		c.err = errAgentTranscriptsOff
		c.loaded = true
		return nil
	}
	c.polling = true
	tail, cli, cwd, since, account := c.tail, c.cli, c.cwd, c.since, c.account
	return func() tea.Msg {
		var sess agentlog.Session
		var err error
		missing := false
		if tail == nil {
			ref, ferr := agentTranscriptFindAs(cli, account, cwd, since)
			if ferr != nil {
				missing = true
			} else {
				tail = agentlog.NewTail(cli, ref.Path)
			}
		}
		if tail != nil {
			_, err = tail.Poll()
			sess = tail.Session()
		}
		return applyMsg(func(m *Model) tea.Cmd {
			c := m.agentChat
			if c == nil || c.gen != gen {
				return nil
			}
			c.polling, c.loaded, c.missing, c.err = false, true, missing, err
			if tail != nil {
				c.tail, c.sess, c.path = tail, sess, tail.Path()
				if err == nil {
					m.agentUsageSession(c.agent, m.agentChatAccount(c), sess) // AGX-12 (agent_usage.go)
				}
			}
			return agentChatTick(gen)
		})
	}
}

// agentChatTick polls again in a second while the view is on show.
func agentChatTick(gen int) tea.Cmd {
	return tea.Tick(agentChatPoll, func(time.Time) tea.Msg {
		return applyMsg(func(m *Model) tea.Cmd {
			c := m.agentChat
			if c == nil || c.gen != gen {
				return nil
			}
			if !m.termOpen || m.panelActive != panelKindAgentChat {
				return agentChatTick(gen) // hidden: wait, read nothing
			}
			return m.agentChatPollCmd(gen)
		})
	})
}

// agentChatKey handles keys while the chat has focus.
func agentChatKey(m *Model, msg tea.KeyMsg) (tea.Cmd, bool) {
	c := m.agentChat
	if c == nil {
		return nil, false
	}
	switch msg.Type {
	case tea.KeyEnter:
		text := strings.TrimSpace(c.input)
		if c.readOnly || text == "" {
			return nil, true
		}
		c.input, c.scroll = "", 0
		buf := c.buf
		agentChatSend(m, buf, text)
		return tea.Tick(agentChatEnterDelay, func(time.Time) tea.Msg {
			return applyMsg(func(m *Model) tea.Cmd { agentChatSend(m, buf, "\r"); return nil })
		}), true
	case tea.KeyBackspace:
		if r := []rune(c.input); len(r) > 0 {
			c.input = string(r[:len(r)-1])
		}
		return nil, true
	case tea.KeyCtrlU:
		c.input = ""
		return nil, true
	case tea.KeyUp:
		c.scroll++
		return nil, true
	case tea.KeyDown:
		if c.scroll > 0 {
			c.scroll--
		}
		return nil, true
	case tea.KeyPgUp:
		c.scroll += 10
		return nil, true
	case tea.KeyPgDown:
		c.scroll = max(c.scroll-10, 0)
		return nil, true
	case tea.KeySpace:
		if !c.readOnly && c.input != "" {
			c.input += " "
		}
		return nil, true
	case tea.KeyRunes:
		if msg.Alt || msg.Paste && c.readOnly {
			return nil, false
		}
		s := agentChatClean(string(msg.Runes))
		if c.input == "" && s == "v" {
			m.agentChatShowTerminal()
			return nil, true
		}
		if c.readOnly && s == "i" { // CMP-87: import it into the AI chats (agent_import.go)
			return m.openAgentImport(), true
		}
		if !c.readOnly {
			c.input += s
		}
		return nil, true
	}
	return nil, false
}

// agentChatClean drops control characters from typed text.
func agentChatClean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// ── Drawing ──────────────────────────────────────────────────────────────

// kfmt is a token count: 440, 9 k, 102 k.
func kfmt(n int) string {
	if n < 1000 {
		return strconv.Itoa(n)
	}
	return strconv.Itoa((n+500)/1000) + " k"
}

// agentChatStatus is the line under the chat: what the agent does and
// the session's numbers.
func agentChatStatus(s agentlog.Session, st agents.State, title string) string {
	var parts []string
	switch st {
	case agents.Waiting:
		parts = append(parts, "◐ "+title+" is waiting for you · answer in the terminal (v)")
	case agents.Working:
		parts = append(parts, "● working")
	case agents.Done:
		parts = append(parts, "✓ done")
	}
	u := s.Usage
	if u.Context > 0 {
		ctx := "context " + kfmt(u.Context)
		if u.Window > 0 {
			ctx += " of " + kfmt(u.Window)
		}
		parts = append(parts, ctx)
	}
	if u.In > 0 || u.Out > 0 {
		parts = append(parts, kfmt(u.In)+" in", kfmt(u.Out)+" out")
	}
	if u.LimitPct > 0 {
		lim := "limit " + strconv.FormatFloat(u.LimitPct, 'f', -1, 64) + "%"
		if !u.LimitReset.IsZero() {
			lim += " ↻ " + u.LimitReset.Local().Format("15:04")
		}
		parts = append(parts, lim)
	}
	return strings.Join(parts, " · ")
}

// agentChatBody is the chat rows of s at width w.
func agentChatBody(s agentlog.Session, w int) []string {
	if w < 20 {
		w = 20
	}
	narrow := w < 80
	bg := theme.BgEditor
	dim := theme.FgBg(theme.TextDim, bg)
	text := theme.FgBg(theme.TextPrimary, bg)
	// Each call gets the mark of the next result of the same tool.
	marks := make([]string, len(s.Turns))
	pending := map[string][]int{}
	for i, t := range s.Turns {
		switch t.Kind {
		case agentlog.ToolCall:
			marks[i] = "…"
			pending[t.Tool] = append(pending[t.Tool], i)
		case agentlog.ToolResult:
			if q := pending[t.Tool]; len(q) > 0 {
				marks[q[0]] = "✓"
				if t.Failed {
					marks[q[0]] = "✘"
				}
				pending[t.Tool] = q[1:]
			}
		}
	}
	var rows []string
	speak := func(label string, style func(string) string, body string) {
		lines := strings.Split(ansi.Wrap(body, w-6, " "), "\n")
		for i, ln := range lines {
			lead := "      "
			if i == 0 {
				lead = style(label) + strings.Repeat(" ", max(6-runewidth.StringWidth(label), 1))
			}
			rows = append(rows, lead+text.Render(ln))
		}
	}
	you := theme.FgBg(theme.AccentBlue, bg).Bold(true)
	spark := theme.FgBg(theme.AccentLavender, bg)
	for i, t := range s.Turns {
		switch t.Kind {
		case agentlog.User:
			speak("you", func(x string) string { return you.Render(x) }, t.Text)
		case agentlog.Assistant:
			speak(aiSpark(), func(x string) string { return spark.Render(x) }, t.Text)
		case agentlog.ToolCall:
			args := agentChatArgs(t.Args, s.Cwd, narrow)
			gap := "  "
			if narrow {
				gap = " "
			}
			mark := marks[i]
			mc := theme.TextDim
			switch mark {
			case "✓":
				mc = theme.AccentGreen
			case "✘":
				mc = theme.DiagError
			}
			lead := "      ▸ " + t.Tool + gap
			room := w - runewidth.StringWidth(lead) - len(gap) - 1
			if room < 4 {
				room = 4
			}
			if runewidth.StringWidth(args) > room {
				args = runewidth.Truncate(args, room, "…")
			}
			if args == "" {
				gap = ""
			}
			rows = append(rows, dim.Render("      ▸ ")+text.Render(t.Tool)+dim.Render(gap+args+gap)+theme.FgBg(mc, bg).Render(mark))
		}
	}
	return rows
}

// agentChatArgs makes a call summary shorter: paths relative to the
// session's folder; narrow, the first path's base name.
func agentChatArgs(args, cwd string, narrow bool) string {
	args = strings.ReplaceAll(args, "\n", " ")
	if cwd != "" {
		args = strings.ReplaceAll(args, strings.TrimSuffix(cwd, "/")+"/", "")
	}
	if narrow {
		head, rest, _ := strings.Cut(args, " ")
		if strings.Contains(head, "/") && !strings.HasPrefix(head, "./") {
			args = strings.TrimSpace(filepath.Base(head) + " " + rest)
		}
	}
	return args
}

// agentChatState is the agent tab's screen state (Unknown read only).
func (m *Model) agentChatAgentState(c *agentChatState) agents.State {
	if c.demo {
		return c.demoState
	}
	if c.readOnly {
		return agents.Unknown
	}
	if t, _ := m.termTabByBuf(c.buf); t != nil {
		return t.AgentState
	}
	return agents.Unknown
}

// agentChatRender is the panel content: header, chat, status, input.
func (m *Model) agentChatRender(w, h int) []string {
	c := m.agentChat
	bg := theme.BgEditor
	dim := theme.FgBg(theme.TextDim, bg)
	muted := theme.FgBg(theme.TextMuted, bg)
	if c == nil {
		return []string{dim.Render("  No agent chat is open: F1 → Agent: Chat View in a Claude Code or Codex tab.")}
	}
	// Header: what this is, and the face switch.
	head := " " + aiSpark() + " " + c.title
	if c.sess.Branch != "" {
		head += " · " + c.sess.Branch
	}
	if id := c.sess.ID; id != "" {
		if len(id) > 8 {
			id = id[:8] + "…"
		}
		head += " · session " + id
	}
	if n := c.sess.Prompts(); c.loaded && n > 0 {
		head += " · " + strconv.Itoa(n) + " turn"
		if n != 1 {
			head += "s"
		}
	}
	right := "[chat] terminal "
	if c.readOnly {
		right = "read only "
	}
	if room := w - runewidth.StringWidth(right) - 1; runewidth.StringWidth(head) > room {
		head = runewidth.Truncate(head, max(room, 1), "…")
	}
	pad := max(w-runewidth.StringWidth(head)-runewidth.StringWidth(right), 1)
	rule := dim.Render(strings.Repeat("─", w))
	rows := []string{theme.FgBg(theme.TextPrimary, bg).Bold(true).Render(head) + strings.Repeat(" ", pad) + muted.Render(right), rule}
	// Body.
	var body []string
	switch {
	case c.err != nil:
		msg := c.err.Error()
		if !strings.HasPrefix(msg, "cannot read this session") {
			msg = "cannot read this session: " + msg
		}
		body = append(body, theme.FgBg(theme.DiagWarning, bg).Render("  "+msg),
			dim.Render("  The terminal shows the agent as it is: v terminal."))
	case !c.loaded:
		body = append(body, dim.Render("  Reading the session…"))
	case c.missing:
		body = append(body, dim.Render("  No session file yet: "+c.title+" writes it after your first prompt."))
	default:
		if c.sess.Truncated {
			body = append(body, dim.Render("  ⋯ earlier turns are not shown (only the newest part of a long session is read)"))
		}
		body = append(body, agentChatBody(c.sess, w)...)
	}
	bodyH := h - 5
	if bodyH < 1 {
		bodyH = 1
	}
	maxScroll := max(len(body)-bodyH, 0)
	if c.scroll > maxScroll {
		c.scroll = maxScroll
	}
	end := len(body) - c.scroll
	start := max(end-bodyH, 0)
	shown := body[start:end]
	for i := len(shown); i < bodyH; i++ {
		rows = append(rows, "")
	}
	rows = append(rows, shown...)
	status := agentChatStatus(c.sess, m.agentChatAgentState(c), c.title)
	if runewidth.StringWidth(status)+1 > w {
		status = runewidth.Truncate(status, w-1, "…")
	}
	rows = append(rows, muted.Render(" "+status), rule)
	// Input.
	switch {
	case c.readOnly:
		rows = append(rows, dim.Render(" read only · ↑↓ scroll · i import into AI chat · Esc leaves"))
	case c.input == "":
		hint := " > type a message (Enter sends it to the terminal) · v terminal"
		if w < 80 {
			hint = " > type a message · Enter sends · v terminal"
		}
		rows = append(rows, dim.Render(hint))
	default:
		in := c.input
		if room := w - 4; runewidth.StringWidth(in) > room {
			in = "…" + string([]rune(in)[len([]rune(in))-max(room-1, 1):])
		}
		rows = append(rows, theme.FgBg(theme.TextPrimary, bg).Render(" > "+in)+theme.FgBg(theme.AccentBlue, bg).Render("▏"))
	}
	if len(rows) > h {
		rows = rows[len(rows)-h:]
	}
	return rows
}

func init() {
	registerPanelKind(panelKindAgentChat, panelKindSpec{
		Title: "AGENT CHAT",
		Icon:  "✦",
		TitleFn: func(m *Model) string {
			if c := m.agentChat; c != nil {
				return "CHAT · " + c.title
			}
			return ""
		},
		Render: func(m *Model, w, h int) []string { return m.agentChatRender(w, h) },
		Key:    agentChatKey,
		Scroll: func(m *Model, delta int) {
			if c := m.agentChat; c != nil {
				c.scroll = max(c.scroll-delta, 0)
			}
		},
		OnClose: func(m *Model) { m.agentChat = nil },
	})
	aiActionPolicies[agentChatPaletteID] = aiNever("shows an outside agent's own session file; only the user opens it (owner decision D1)")
}
