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

// mcpServeNotListeningWhy says why nothing listens.
func (m *Model) mcpServeNotListeningWhy() string {
	switch mcpServeMode() {
	case "off":
		return "Not listening: mcp_server is off in config.json"
	case "on":
		if m.mcpServeErr != "" {
			return "Not listening: " + aiCleanText(m.mcpServeErr)
		}
		return "Not listening: mcp_server is on, but the server did not start (see errors.log)"
	}
	if m.mcpServeErr != "" {
		return "Not listening: " + aiCleanText(m.mcpServeErr)
	}
	return "Not listening yet: mcp_server is auto, it starts when you start an agent tab (F1 → Agent: Start …)"
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
		info(m.mcpServeNotListeningWhy())
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
				info(name + " · since " + a.since.Format("15:04") + " · " +
					mcpServePlural(a.calls, "call", "calls") + ", " + strconv.Itoa(a.asked) + " asked")
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
	items = append(items,
		picker.Item{ID: "copy", Title: "Copy termocode server config", Hint: "JSON for agent CLIs"},
		picker.Item{ID: "allow", Title: "Agent auto-approve…", Hint: "see and revoke Always allow"})
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
	m.picker = picker.NewItems(" ⇄ MCP server status (Enter on an action) ", m.mcpServeStatusItems())
	m.picker.SetWidth(mcpServeStatusWidth(m.w))
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindMCPServeStatus
	return nil
}

// onMCPServeStatusPicked runs an action row; Enter on a plain row looks
// again (the numbers change while the screen is shut).
func (m *Model) onMCPServeStatusPicked(id string) tea.Cmd {
	switch id {
	case "copy":
		return m.copyMCPServeConfig()
	case "allow":
		return m.openAIAutoApprovePicker()
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
		{ID: "mcp-serve-status", Title: "MCP: Server Status"},
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
