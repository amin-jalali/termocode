package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// termocode as an MCP server (M1 MS-1), the UI side. An outside agent
// connects to the workspace's socket (mcp_serve_socket.go); each
// connection gets an mcp.MCPServer over the shared mcpServeSource. A
// tools/call runs like a chat tool call: the tool comes from the same
// aiChatTools list (allow-listed, mcp_serve_tools.go), its Validate and
// Gate decide, a mutating call asks on the confirm card, Run reaches the
// Model through an aiIDECaller, the result is redacted and the call is
// one line in the AI audit channel with by=mcp:<client>.
//
// Calls reach Update over mcpServeHub.ch; waitMCPServe is re-armed after
// each message while the listener runs, and stops when it stops.

// mcpServeHub links the connection goroutines to Update.
type mcpServeHub struct {
	ch   chan tea.Msg
	done chan struct{}
	once sync.Once
}

func newMCPServeHub() *mcpServeHub {
	return &mcpServeHub{ch: make(chan tea.Msg, 16), done: make(chan struct{})}
}

// stop ends the hub: waiting callers get errMCPServeStopped.
func (h *mcpServeHub) stop() { h.once.Do(func() { close(h.done) }) }

var errMCPServeStopped = errors.New("termocode stopped serving MCP")

// send delivers msg to Update unless the hub stopped or ctx ended.
func (h *mcpServeHub) send(ctx context.Context, msg tea.Msg) error {
	select {
	case h.ch <- msg:
		return nil
	case <-h.done:
		return errMCPServeStopped
	case <-ctx.Done():
		return ctx.Err()
	}
}

// waitMCPServe reads the next hub message (nil once the hub stopped).
func waitMCPServe(h *mcpServeHub) tea.Cmd {
	return func() tea.Msg {
		select {
		case msg := <-h.ch:
			return msg
		case <-h.done:
			return nil
		}
	}
}

// mcpServeCallMsg runs fn on the Model for a connection goroutine.
type mcpServeCallMsg struct {
	hub   *mcpServeHub
	ctx   context.Context
	fn    aiIDEFunc
	reply chan aiIDEReply // buffered (1)
}

// caller is the aiIDECaller of the MCP tools: like the chat's, but over
// the hub, with no chat generation (the listener's life is the limit).
func (h *mcpServeHub) caller() aiIDECaller {
	return func(ctx context.Context, fn aiIDEFunc) (string, error) {
		reply := make(chan aiIDEReply, 1)
		if err := h.send(ctx, mcpServeCallMsg{hub: h, ctx: ctx, fn: fn, reply: reply}); err != nil {
			return "", err
		}
		select {
		case r := <-reply:
			return r.out, r.err
		case <-h.done:
			return "", errMCPServeStopped
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// onMCPServeCall runs one call on the real Model.
func (m *Model) onMCPServeCall(msg mcpServeCallMsg) tea.Cmd {
	next := waitMCPServe(msg.hub)
	if msg.ctx != nil && msg.ctx.Err() != nil {
		msg.reply <- aiIDEReply{err: msg.ctx.Err()}
		return next
	}
	out, cmd, err := m.mcpServeRunFn(msg.fn)
	msg.reply <- aiIDEReply{out: out, err: err}
	return tea.Batch(cmd, next)
}

// mcpServeRunFn runs fn; a panic becomes an error (an outside client
// must not crash the editor).
func (m *Model) mcpServeRunFn(fn aiIDEFunc) (out string, cmd tea.Cmd, err error) {
	defer func() {
		if r := recover(); r != nil {
			recordError(fmt.Sprintf("[mcp-serve] panic: %v", r))
			out, cmd, err = "", nil, errors.New("internal error")
		}
	}()
	return fn(m)
}

// handleMCPServeMsg routes the hub messages. ok=false: not ours.
func (m *Model) handleMCPServeMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case mcpServeCallMsg:
		return m.onMCPServeCall(msg), true
	case mcpServeAskMsg:
		return m.onMCPServeAsk(msg), true
	case mcpServeAskGoneMsg:
		return m.onMCPServeAskGone(msg), true
	case mcpServeTickMsg:
		return m.onMCPServeTick(), true
	}
	return nil, false
}

// mcpServeClient cleans a clientInfo.name for the audit line and the
// card: letters, digits, . _ - only (spaces become -), at most 24; an
// empty name is "client". The name is the client's own claim.
func mcpServeClient(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if b.Len() >= 24 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-.")
	if name == "" {
		return "client"
	}
	return name
}

// mcpServeConfirmFunc asks the user about one mutating call: nil when
// allowed, a *mcp.CallError (declined / timeout) when not.
type mcpServeConfirmFunc func(ctx context.Context, client string, t ai.Tool, human string) error

// mcpServeAuditFunc writes one audit line.
type mcpServeAuditFunc func(at time.Time, client, tool, args, gate, result string)

// mcpServeSource is the ToolSource of every connection.
type mcpServeSource struct {
	call    aiIDECaller
	root    string
	specs   []mcp.Tool
	confirm mcpServeConfirmFunc
	audit   mcpServeAuditFunc
}

func (s *mcpServeSource) List() []mcp.Tool { return s.specs }

// Call runs one tools/call through the allow-list, Validate, the gate
// and the confirm card.
func (s *mcpServeSource) Call(ctx context.Context, name string, args map[string]any) (mcp.CallResult, error) {
	client := mcpServeClient(mcp.ClientName(ctx))
	at := clock.Now()
	argText := aiAuditArgs(args)
	audit := func(gate, result string) {
		if s.audit != nil {
			s.audit(at, client, name, argText, gate, result)
		}
	}
	if _, ok := mcpServeRow(name); !ok { // never tools, mcp__* and typos alike
		audit("refused", "not an exposed tool")
		return mcp.CallResult{}, &mcp.CallError{Code: mcp.CodeInvalidParams, Message: fmt.Sprintf("unknown tool %q", name)}
	}
	var tool ai.Tool
	found := false
	if _, err := s.call(ctx, func(m *Model) (string, tea.Cmd, error) {
		tool, found = ai.FindTool(mcpServeTools(m.aiChatTools(s.root, s.call), s.call), name)
		return "", nil, nil
	}); err != nil {
		audit("-", "error: "+err.Error())
		return mcp.CallResult{}, err
	}
	if !found { // listed, but not here now (no editor, no LSP)
		audit("-", "not available now")
		return mcp.CallResult{}, &mcp.CallError{Code: mcp.CodeInvalidParams, Message: name + " is not available right now (is the editor running?)"}
	}
	if tool.Validate != nil {
		if err := tool.Validate(args); err != nil {
			msg := ai.Redact(err.Error())
			audit(ai.GateInvalid, "error: "+msg)
			return mcp.CallResult{}, &mcp.CallError{Code: mcp.CodeInvalidParams, Message: msg}
		}
	}
	gate := ai.GateAuto
	if t, ask := mcpServeGate(tool, args); ask {
		gate = ai.GateConfirm
		if err := s.ask(ctx, client, t, t.Human(args)); err != nil {
			audit(gate, "declined: "+err.Error())
			return mcp.CallResult{}, err
		}
		gate = ai.GateConfirm + ":allowed"
	}
	out, err := mcpServeRunTool(ctx, tool, args)
	if err != nil {
		text := "error: " + ai.Redact(err.Error())
		audit(gate, text)
		return mcp.CallResult{Content: []mcp.Content{{Type: "text", Text: text}}, IsError: true}, nil
	}
	out = ai.RedactSecrets(out) // the tools redact already; once more for safety
	audit(gate, out)
	return mcp.CallResult{Content: []mcp.Content{{Type: "text", Text: out}}}, nil
}

// mcpServeGate decides whether a call asks the user first: the tool's
// per-call Gate, else its Mutating flag, as in the chat (no auto-approve
// for outside agents). The returned tool carries the call's class.
func mcpServeGate(t ai.Tool, args map[string]any) (ai.Tool, bool) {
	if t.Gate != nil {
		t.Mutating, t.Class = t.Gate(args)
	}
	return t, t.Mutating
}

// ask runs the confirm func; without one every mutating call is declined.
func (s *mcpServeSource) ask(ctx context.Context, client string, t ai.Tool, human string) error {
	if s.confirm == nil {
		return &mcp.CallError{Code: mcp.CodeDeclined, Message: "declined: nobody can confirm"}
	}
	return s.confirm(ctx, client, t, human)
}

// mcpServeRunTool runs a tool; a panic is an error.
func mcpServeRunTool(ctx context.Context, t ai.Tool, args map[string]any) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			recordError(fmt.Sprintf("[mcp-serve] %s panic: %v", t.Name, r))
			out, err = "", errors.New("internal error")
		}
	}()
	return t.Run(ctx, args)
}
