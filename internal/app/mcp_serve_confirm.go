package app

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// The confirm card of outside agents (MS-1): one card at a time, first
// asked first shown (FIFO), over all connections. A card waits while
// another dialog is open. A call whose client went away or that waited
// longer than mcpServeConfirmTimeout leaves the queue (its card closes);
// the client gets an MCP error and nothing runs.

const confirmKindMCPServe confirmKindEnum = 2000

// mcpServeConfirmTimeout is the longest a call waits for the user.
var mcpServeConfirmTimeout = 2 * time.Minute

// mcpServeMaxQueue caps the calls waiting for a card; more are a no.
const mcpServeMaxQueue = 32

// mcpServeTick is how often a waiting queue looks again (only while a
// call waits: no tick when idle).
const mcpServeTick = 250 * time.Millisecond

// mcpServeState is the UI side of the MCP server (nil until it starts).
type mcpServeState struct {
	hub     *mcpServeHub
	queue   []*mcpServeAsk // waiting for the card, oldest first
	active  *mcpServeAsk   // the call whose card is open
	ticking bool
	srv     *mcp.SocketServer // the listener (mcp_serve_run.go)
	src     *mcpServeSource   // its tool source (mcp_serve_list.go)
	path    string            // its socket file
	clients []*mcpServeAgent  // connected agents (mcp_serve_status.go)
	// disconnectID is the agent the Disconnect card is about.
	disconnectID uint64
}

// mcpServeAsk is one call waiting for the user.
type mcpServeAsk struct {
	ctx    context.Context
	client string
	tool   ai.Tool
	human  string
	args   string    // the call's arguments, redacted (mcpServeCardInfo)
	always string    // the "Always allow" key, "" = not offered (mcp_serve_allow.go)
	reply  chan bool // buffered (1)
}

// mcpServeCardInfo is what Call tells the card besides the tool and the
// human text; it travels in the call's context so the confirm func keeps
// its shape.
type mcpServeCardInfo struct {
	args   string
	always string
}

type mcpServeCardKey struct{}

func withMCPServeCard(ctx context.Context, info mcpServeCardInfo) context.Context {
	return context.WithValue(ctx, mcpServeCardKey{}, info)
}

func mcpServeCardOf(ctx context.Context) mcpServeCardInfo {
	info, _ := ctx.Value(mcpServeCardKey{}).(mcpServeCardInfo)
	return info
}

// mcpServeGenericClient is the name Call gets for a client that sent none.
const mcpServeGenericClient = "client"

// mcpServeCardTitle names who asks and what for.
func mcpServeCardTitle(client, tool, human string) string {
	who := client
	if client == mcpServeGenericClient {
		who = "An outside agent"
	}
	return aiSpark() + " " + who + " wants to " + mcpServeActionPhrase(tool, human)
}

// mcpServeActionPhrase is what the agent wants, in words: the tool's own
// one-line description ("Run task: build" → run task "build"), or the
// tool id as words when it has none. Redacted like the card body.
func mcpServeActionPhrase(tool, human string) string {
	h := strings.TrimSpace(ai.Redact(aiCleanText(human)))
	if h == "" || h == tool {
		return "use " + strings.ReplaceAll(tool, "_", " ")
	}
	head, arg, ok := strings.Cut(h, ": ")
	if r := []rune(head); len(r) > 0 {
		head = strings.ToLower(string(r[:1])) + string(r[1:])
	}
	if !ok || strings.TrimSpace(arg) == "" {
		return head
	}
	if r := []rune(arg); len(r) > 40 {
		arg = string(r[:39]) + "…"
	}
	return head + ` "` + strings.TrimSpace(arg) + `"`
}

type mcpServeAskMsg struct {
	hub *mcpServeHub
	ask *mcpServeAsk
}

// mcpServeAskGoneMsg: the asking call ended (client gone, timeout).
type mcpServeAskGoneMsg struct {
	hub *mcpServeHub
	ask *mcpServeAsk
}

type mcpServeTickMsg struct{}

// confirmFunc asks on the card through Update.
func (h *mcpServeHub) confirmFunc() mcpServeConfirmFunc {
	return func(ctx context.Context, client string, t ai.Tool, human string) error {
		wait, cancel := context.WithTimeout(ctx, mcpServeConfirmTimeout)
		defer cancel()
		info := mcpServeCardOf(ctx)
		ask := &mcpServeAsk{ctx: wait, client: client, tool: t, human: human, args: info.args, always: info.always, reply: make(chan bool, 1)}
		declined := &mcp.CallError{Code: mcp.CodeDeclined, Message: "declined by the user"}
		if h.send(wait, mcpServeAskMsg{hub: h, ask: ask}) != nil {
			return mcpServeWaitErr(ctx, declined)
		}
		select {
		case ok := <-ask.reply:
			if ok {
				return nil
			}
			return declined
		case <-h.done:
			return &mcp.CallError{Code: mcp.CodeDeclined, Message: "declined: termocode stopped serving MCP"}
		case <-wait.Done():
			_ = h.send(context.Background(), mcpServeAskGoneMsg{hub: h, ask: ask})
			select { // the user may have answered just now
			case ok := <-ask.reply:
				if ok {
					return nil
				}
				return declined
			default:
			}
			return mcpServeWaitErr(ctx, declined)
		}
	}
}

// mcpServeWaitErr: a timeout when the client still waits, else declined.
func mcpServeWaitErr(ctx context.Context, declined error) error {
	if ctx.Err() == nil {
		return &mcp.CallError{Code: mcp.CodeTimeout, Message: "timed out waiting for the user"}
	}
	return declined
}

// auditFunc writes the audit line on the UI goroutine.
func (h *mcpServeHub) auditFunc() mcpServeAuditFunc {
	call := h.caller()
	return func(at time.Time, client, tool, args, gate, result string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
			m.aiAudit(at, tool, args, gate, "mcp:"+client, result)
			return "", nil, nil
		})
	}
}

// newMCPServeSource is the ToolSource of a running hub.
func newMCPServeSource(h *mcpServeHub, root string) *mcpServeSource {
	h.allow.replace(mcpServeAllowLoad())
	return &mcpServeSource{call: h.caller(), root: root, specs: MCPServeToolSpecs(), confirm: h.confirmFunc(), audit: h.auditFunc(), allow: &h.allow, events: h.clientEventFunc(),
		dbSet: mcpServeDBSetOf(root)}
}

// mcpServeLive is the state when msg's hub is the running one.
func (m *Model) mcpServeLive(h *mcpServeHub) *mcpServeState {
	if st := m.mcpServe; st != nil && st.hub == h {
		return st
	}
	return nil
}

// onMCPServeAsk queues a call for the card.
func (m *Model) onMCPServeAsk(msg mcpServeAskMsg) tea.Cmd {
	next := waitMCPServe(msg.hub)
	st := m.mcpServeLive(msg.hub)
	if st == nil || len(st.queue) >= mcpServeMaxQueue {
		msg.ask.reply <- false
		return next
	}
	st.queue = append(st.queue, msg.ask)
	return tea.Batch(m.mcpServePump(), next)
}

// onMCPServeAskGone drops a call that stopped waiting.
func (m *Model) onMCPServeAskGone(msg mcpServeAskGoneMsg) tea.Cmd {
	next := waitMCPServe(msg.hub)
	st := m.mcpServeLive(msg.hub)
	if st == nil {
		return next
	}
	if st.active == msg.ask {
		st.active = nil
		if m.confirmOpen && m.confirmKind == confirmKindMCPServe {
			m.confirmOpen = false
		}
	}
	for i, a := range st.queue {
		if a == msg.ask {
			st.queue = append(st.queue[:i], st.queue[i+1:]...)
			break
		}
	}
	return tea.Batch(m.mcpServePump(), next)
}

// mcpServePump opens the next card when none is open. While calls wait it
// keeps one tick going, which also notices a card another dialog replaced.
func (m *Model) mcpServePump() tea.Cmd {
	st := m.mcpServe
	if st == nil {
		return nil
	}
	if st.active != nil && !(m.confirmOpen && m.confirmKind == confirmKindMCPServe) {
		st.active.reply <- false // the card was replaced or closed elsewhere: no
		st.active = nil
	}
	for st.active == nil && len(st.queue) > 0 && !m.confirmOpen {
		a := st.queue[0]
		st.queue = st.queue[1:]
		if a.ctx.Err() != nil {
			a.reply <- false
			continue
		}
		st.active = a
		body := ai.Redact(aiCleanText(a.human))
		if a.args != "" {
			body += "\n\nArguments: " + ai.Redact(aiCleanText(a.args))
		}
		body += "\n\nAsked by an outside agent over MCP (tool " + a.tool.Name + "). The name is the agent's own claim."
		buttons := []confirm.Button{{ID: "allow", Title: "Allow", Style: confirm.StylePrimary}}
		if a.always != "" {
			buttons = append(buttons, confirm.Button{ID: "always", Title: "Always allow"})
			body += "\nAlways allow: " + a.client + " may run " + mcpServeAlwaysWhat(a.always) +
				" without asking. Revoke it in AI: Agent Auto-Approve…"
		}
		// Deny is the safe answer, so it is plain (not red) and it is the
		// button Enter picks: a card can open while the user types, and an
		// Enter meant for something else must not allow an outside agent.
		buttons = append(buttons, confirm.Button{ID: "deny", Title: "Deny"})
		m.confirm = confirm.New(mcpServeCardTitle(a.client, a.tool.Name, a.human), body, buttons)
		for range buttons[1:] { // the cursor starts on the first button: move it to Deny
			m.confirm, _ = m.confirm.Update(tea.KeyMsg{Type: tea.KeyRight})
		}
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindMCPServe
	}
	if (st.active == nil && len(st.queue) == 0) || st.ticking {
		return nil
	}
	st.ticking = true
	return tea.Tick(mcpServeTick, func(time.Time) tea.Msg { return mcpServeTickMsg{} })
}

// onMCPServeTick looks at the queue again.
func (m *Model) onMCPServeTick() tea.Cmd {
	if m.mcpServe == nil {
		return nil
	}
	m.mcpServe.ticking = false
	return m.mcpServePump()
}

// answerMCPServeConfirm is the card's answer (Esc is "deny").
func (m *Model) answerMCPServeConfirm(id string) tea.Cmd {
	st := m.mcpServe
	if st == nil || st.active == nil {
		return nil
	}
	ask := st.active
	var toast tea.Cmd
	if id == "always" && ask.always != "" {
		// Remember first, so the next call (even one already waiting)
		// finds it; the answer for this call is yes even if the save fails.
		if err := m.mcpServeAllowAdd(ask.client, ask.always); err != nil {
			toast = m.aiToastErr("Could not save Always allow", err)
		} else {
			toast = m.aiInfo(ask.client + ": " + ask.always + " runs without asking now (revoke: AI: Agent Auto-Approve…)")
		}
	}
	ask.reply <- id == "allow" || id == "always"
	st.active = nil
	return tea.Batch(toast, m.mcpServePump())
}

// mcpServeDeclineAll answers no to every waiting call (stop, quit).
func (st *mcpServeState) declineAll() {
	if st.active != nil {
		st.active.reply <- false
		st.active = nil
	}
	for _, a := range st.queue {
		a.reply <- false
	}
	st.queue = nil
}
