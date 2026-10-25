package app

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/picker"
)

// What the user sees of termocode as an MCP server (M1 MS-3): the agents
// that are connected (a count for the status bar, a list on the status
// screen), the screen itself, and the config snippet for agent CLIs.
// Everything follows events: a connection starts, is named, makes a call
// or ends. There is no timer for any of it.

// pickerKindMCPServeStatus is the status screen.
const pickerKindMCPServeStatus pickerKindEnum = 2000

// The two small cards of the status screen (stop, disconnect).
const (
	confirmKindMCPServeStop       confirmKindEnum = 2001
	confirmKindMCPServeDisconnect confirmKindEnum = 2002
)

// mcpServeStatusLines is how many audit lines the screen shows.
const mcpServeStatusLines = 5

// mcpServeAgent is one connected outside agent.
type mcpServeAgent struct {
	id    uint64 // the connection's (mcp.ConnID)
	name  string // cleaned clientInfo.name, "" until initialize
	since time.Time
	calls int
	asked int // calls that showed a card
}

type mcpServeClientKind int

const (
	mcpClientConnected mcpServeClientKind = iota
	mcpClientNamed
	mcpClientGone
	mcpClientCalled
)

// mcpServeClientMsg is a connection event on its way to Update.
type mcpServeClientMsg struct {
	hub   *mcpServeHub
	kind  mcpServeClientKind
	id    uint64
	name  string
	asked bool // mcpClientCalled: a card was shown
}

// ClientConnected, ClientNamed and ClientGone make mcpServeSource an
// mcp.ClientObserver: the socket server tells it about each connection.
func (s *mcpServeSource) ClientConnected(id uint64) {
	s.event(mcpServeClientMsg{kind: mcpClientConnected, id: id})
}

func (s *mcpServeSource) ClientNamed(id uint64, name string) {
	s.event(mcpServeClientMsg{kind: mcpClientNamed, id: id, name: mcpServeClient(name)})
}

func (s *mcpServeSource) ClientGone(id uint64) {
	s.event(mcpServeClientMsg{kind: mcpClientGone, id: id})
}

// noteCall counts one tools/call of ctx's connection.
func (s *mcpServeSource) noteCall(ctx context.Context, asked bool) {
	s.event(mcpServeClientMsg{kind: mcpClientCalled, id: mcp.ConnID(ctx), asked: asked})
}

func (s *mcpServeSource) event(msg mcpServeClientMsg) {
	if s.events != nil {
		s.events(msg)
	}
}

// clientEventFunc delivers connection events to Update. It waits a moment
// at most: a stopped hub or a busy UI must not hold a connection forever.
func (h *mcpServeHub) clientEventFunc() func(mcpServeClientMsg) {
	return func(msg mcpServeClientMsg) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		msg.hub = h
		_ = h.send(ctx, msg)
	}
}

// onMCPServeClient applies one connection event.
func (m *Model) onMCPServeClient(msg mcpServeClientMsg) tea.Cmd {
	next := waitMCPServe(msg.hub)
	st := m.mcpServeLive(msg.hub)
	if st == nil {
		return next
	}
	find := func() *mcpServeAgent {
		for _, a := range st.clients {
			if a.id == msg.id {
				return a
			}
		}
		return nil
	}
	switch msg.kind {
	case mcpClientConnected:
		if find() == nil {
			st.clients = append(st.clients, &mcpServeAgent{id: msg.id, since: clock.Now()})
		}
	case mcpClientNamed:
		if a := find(); a != nil {
			a.name = msg.name
		}
	case mcpClientCalled:
		if a := find(); a != nil {
			a.calls++
			if msg.asked {
				a.asked++
			}
		}
	case mcpClientGone:
		for i, a := range st.clients {
			if a.id == msg.id {
				st.clients = append(st.clients[:i], st.clients[i+1:]...)
				break
			}
		}
	}
	return next
}

// mcpServeAgentCount is how many outside agents are connected now.
func (m Model) mcpServeAgentCount() int {
	if m.mcpServe == nil {
		return 0
	}
	return len(m.mcpServe.clients)
}

// ── The status screen ──────────────────────────────────────────────────

// mcpServeAuditLines are the newest audit lines of outside agents,
// oldest first. The audit channel holds them redacted already.
func (m *Model) mcpServeAuditLines(n int) []string {
	if m.output == nil {
		return nil
	}
	var out []string
	lines := m.output.Lines(aiAuditChannel)
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if strings.Contains(lines[i], "by=mcp:") {
			out = append(out, aiCleanText(lines[i]))
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// mcpServeCallRow turns one audit line of an outside agent
// ("09:41:12  tasks_run  label=x  gate=confirm:allowed  by=mcp:claude-code
// → started") into a row a person reads: "09:41 claude-code · tasks_run ·
// asked, you allowed". The arguments and the result text stay in the
// audit log (Output: AI). ok is false for a line of another shape.
func mcpServeCallRow(line string) (string, bool) {
	at, rest, found := strings.Cut(line, "  ")
	if !found || len(at) < 5 {
		return "", false
	}
	tool, _, _ := strings.Cut(rest, "  ")
	_, afterGate, found := strings.Cut(rest, "gate=")
	if !found {
		return "", false
	}
	gate, afterGate, _ := strings.Cut(afterGate, "  ")
	by, result, _ := strings.Cut(strings.TrimPrefix(afterGate, "by=mcp:"), "  → ")
	if !strings.HasPrefix(afterGate, "by=mcp:") || tool == "" || by == "" {
		return "", false
	}
	var what string
	switch {
	case strings.HasPrefix(result, "declined") && strings.Contains(result, "timed out"):
		what = "asked, no answer in time"
	case strings.HasPrefix(result, "declined"):
		what = "asked, you denied"
	case gate == "confirm:allowed":
		what = "asked, you allowed"
	case gate == "confirm:always":
		what = "ran (always allowed)"
	case gate == "auto":
		what = "ran without asking"
	default:
		what = "not run"
	}
	return at[:5] + " " + by + " · " + tool + " · " + what, true
}

// mcpServeNotListeningWhy says why nothing listens, in plain words: a
// title and one sentence (the error state of the screen, C-5).
func (m *Model) mcpServeNotListeningWhy() (title, sentence string) {
	switch mcpServeMode() {
	case "off":
		return "Not listening", "The MCP server is turned off."
	}
	if m.mcpServeStopped {
		return "Not listening", "You stopped it for this session."
	}
	switch mcpServeMode() {
	case "on":
		if m.mcpServeErr != "" {
			return "Not listening", "It could not start: " + aiCleanText(m.mcpServeErr)
		}
		return "Not listening", "It did not start."
	}
	if m.mcpServeErr != "" {
		return "Not listening", "It could not start: " + aiCleanText(m.mcpServeErr)
	}
	return "Not listening yet", "It starts when you start an agent tab."
}

// mcpServeNotListeningAction is the row that fixes it: turn it on when it
// is off, else start it now (or try again after an error).
func (m *Model) mcpServeNotListeningAction() picker.Item {
	switch {
	case mcpServeMode() == "off":
		return picker.Item{ID: "serve-on", Title: "Turn on", Hint: "Enter · let outside agents connect"}
	case m.mcpServeErr != "":
		return picker.Item{ID: "serve-start", Title: "Try again", Hint: "Enter · start listening"}
	}
	return picker.Item{ID: "serve-start", Title: "Start listening now", Hint: "Enter · for this session"}
}

func mcpServePlural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// mcpServeStatusItems are the rows of the status screen.
func (m *Model) mcpServeStatusItems() []picker.Item {
	var items []picker.Item
	info := func(title string) { items = append(items, picker.Item{Title: title}) }
	head := func(title string) { items = append(items, picker.Item{Header: true, Title: title}) }
	st := m.mcpServe
	if st == nil {
		// The error state: a title, one sentence, then the button with its
		// key (the screen opens on it, so Enter runs it).
		title, sentence := m.mcpServeNotListeningWhy()
		head(title)
		info(sentence)
		items = append(items, m.mcpServeNotListeningAction())
	} else {
		n := len(st.clients)
		who := "no agent connected"
		if n > 0 {
			who = mcpServePlural(n, "agent", "agents") + " connected"
		}
		info("Listening · " + who)
		if n > 0 {
			head("Connected agents")
			for _, a := range st.clients {
				name := a.name
				if name == "" {
					name = "(no name yet)"
				}
				items = append(items, picker.Item{ID: "agent:" + strconv.FormatUint(a.id, 10), Hint: "Enter to disconnect",
					Title: name + " · since " + a.since.Format("15:04") + " · " +
						mcpServePlural(a.calls, "call", "calls") + ", " + strconv.Itoa(a.asked) + " asked"})
			}
		}
	}
	if lines := m.mcpServeAuditLines(mcpServeStatusLines); len(lines) > 0 {
		head("Last calls of outside agents")
		for _, l := range lines {
			if row, ok := mcpServeCallRow(l); ok {
				info(row)
			}
		}
	}
	head("Actions")
	if st != nil {
		items = append(items, picker.Item{ID: "serve-stop", Title: "Stop listening…", Hint: "for this session"})
	}
	items = append(items,
		picker.Item{ID: "copy", Title: "Copy termocode server config", Hint: "JSON for agent CLIs"},
		picker.Item{ID: "allow", Title: "AI: Agent Auto-Approve…", Hint: "see and revoke Always allow"})
	return items
}

// mcpServeStatusWidth is the status box width: it follows the window
// (8 cells less) between the picker's default and a cap, so call rows and
// action hints are not cut in a wide terminal.
func mcpServeStatusWidth(w int) int {
	return max(56, min(96, w-8))
}

// openMCPServeStatus shows the status screen.
func (m *Model) openMCPServeStatus() tea.Cmd {
	m.picker = picker.NewItems(" MCP server status ", m.mcpServeStatusItems())
	m.picker.SetWidth(mcpServeStatusWidth(m.w))
	m.picker.SetSize(m.w, m.h)
	if m.mcpServe == nil { // not listening: the cursor is on the button that fixes it
		m.picker.SetCursorID(m.mcpServeNotListeningAction().ID)
	}
	m.pickerOpen = true
	m.pickerKind = pickerKindMCPServeStatus
	return nil
}

// mcpServeStatusKey handles "?" on the status screen: the key card. The
// screen comes back when the card closes.
func (m *Model) mcpServeStatusKey(k tea.KeyMsg) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindMCPServeStatus || k.String() != "?" {
		return nil, false
	}
	m.pickerOpen = false
	return m.openKeyCard("MCP server status keys", keyRows(
		[]string{"Up / Down", "move to a row"},
		[]string{"Enter", "run the row: disconnect an agent, start or stop listening, copy the config"},
		[]string{"Type", "search the rows"},
		[]string{"Esc", "close the screen"},
	), help.Loc{Page: "guides/agents.md", Anchor: "see-what-is-connected"}, func(m *Model) tea.Cmd { return m.openMCPServeStatus() }), true
}

// onMCPServeStatusPicked runs an action row; Enter on a plain row looks
// again (the numbers change while the screen is shut).
func (m *Model) onMCPServeStatusPicked(id string) tea.Cmd {
	switch id {
	case "copy":
		return m.copyMCPServeConfig()
	case "allow":
		return m.openAIAutoApprovePicker()
	case "serve-on":
		path, err := settingsConfigPath()
		if err == nil {
			err = mcp.SaveSetting(path, "mcp_server", "on")
		}
		if err != nil {
			return m.aiToastErr("Could not turn the MCP server on", err)
		}
		return tea.Batch(m.syncMCPServe(), m.openMCPServeStatus())
	case "serve-start":
		return tea.Batch(m.ensureMCPServeForAgent(), m.openMCPServeStatus())
	case "serve-stop":
		m.openMCPServeConfirm(confirmKindMCPServeStop, "Stop listening?",
			"Connected agents lose their connection and waiting cards are denied.\n\n"+
				"This is for this session only. Your settings do not change. "+
				"It starts again when you start an agent tab or choose Start listening now.",
			"Stop listening")
		return nil
	}
	if rest, ok := strings.CutPrefix(id, "agent:"); ok {
		if n, err := strconv.ParseUint(rest, 10, 64); err == nil && m.mcpServe != nil {
			for _, a := range m.mcpServe.clients {
				if a.id == n {
					m.mcpServe.disconnectID = n
					name := a.name
					if name == "" {
						name = "this agent"
					}
					m.openMCPServeConfirm(confirmKindMCPServeDisconnect, "Disconnect "+name+"?",
						"It loses its connection to termocode. It can connect again by itself.",
						"Disconnect")
					return nil
				}
			}
		}
	}
	return m.openMCPServeStatus()
}

// openMCPServeConfirm shows a small card of the status screen: Cancel
// first (the focus), then the action.
func (m *Model) openMCPServeConfirm(kind confirmKindEnum, title, body, action string) {
	m.pickerOpen = false
	m.confirm = confirm.Risky(title, body,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "ok", Title: action, Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = kind
}

// answerMCPServeStatusConfirm runs "Stop listening" or "Disconnect"; both
// come back to the status screen.
func (m *Model) answerMCPServeStatusConfirm(kind confirmKindEnum, id string) tea.Cmd {
	if id == "ok" {
		switch kind {
		case confirmKindMCPServeStop:
			m.mcpServeForAgent = false
			m.stopMCPServe()
			m.mcpServeStopped = true
		case confirmKindMCPServeDisconnect:
			if st := m.mcpServe; st != nil {
				if st.srv != nil {
					st.srv.Disconnect(st.disconnectID)
				}
				for i, a := range st.clients { // at once; the end event finds nothing left
					if a.id == st.disconnectID {
						st.clients = append(st.clients[:i], st.clients[i+1:]...)
						break
					}
				}
			}
		}
	}
	return m.openMCPServeStatus()
}

// ── The config snippet ─────────────────────────────────────────────────

// mcpServeExecutable is the path of this termocode. Tests swap it.
var mcpServeExecutable = os.Executable

// mcpServeConfigJSON is the "mcpServers" entry agent CLIs take (Claude
// Code, Codex, Cursor …): they start `termocode mcp --dir <workspace>`.
func mcpServeConfigJSON(exe, dir string) string {
	q := func(s string) string {
		b, _ := json.Marshal(s)
		return string(b)
	}
	return "{\n" +
		"  \"mcpServers\": {\n" +
		"    \"termocode\": {\n" +
		"      \"command\": " + q(exe) + ",\n" +
		"      \"args\": [\"mcp\", \"--dir\", " + q(dir) + "]\n" +
		"    }\n" +
		"  }\n" +
		"}\n"
}

// copyMCPServeConfig puts the snippet on the clipboard.
func (m *Model) copyMCPServeConfig() tea.Cmd {
	exe, err := mcpServeExecutable()
	if err != nil || exe == "" {
		exe = "termocode" // found through PATH
	}
	return m.copyWithToast(mcpServeConfigJSON(exe, aiWorkspaceRoot()), "Copied the termocode server config: paste it into your agent's MCP settings")
}

// ── Palette ────────────────────────────────────────────────────────────

func mcpServePaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "mcp-serve-status", Title: "MCP: Server Status…"},
		{ID: "mcp-serve-copy-config", Title: "MCP: Copy termocode Server Config"},
	}
}

// dispatchMCPServePalette runs the MCP server's palette ids.
func (m *Model) dispatchMCPServePalette(id string) (tea.Cmd, bool) {
	switch id {
	case "mcp-serve-status":
		return m.openMCPServeStatus(), true
	case "mcp-serve-copy-config":
		return m.copyMCPServeConfig(), true
	}
	return nil, false
}
