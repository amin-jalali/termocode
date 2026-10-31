package app

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// MCP elicitation (MCPX-14): a server asks the user during a tool call.
//
//   - Form mode: a Deny-first card names the fields; "Fill in…" asks one
//     field at a time (a field that looks like a password or key is
//     masked, with a warning on the card); a last card shows what will
//     be sent (Cancel first). Nothing is filled in by termocode, and
//     never from the secret store.
//   - URL mode: a Deny-first card shows the full URL and its site;
//     "Open in browser" opens it.
//
// Deny answers decline, Esc and Cancel answer cancel.

const (
	confirmKindMCPElicit     confirmKindEnum = 2441
	confirmKindMCPElicitURL  confirmKindEnum = 2442
	confirmKindMCPElicitSend confirmKindEnum = 2443
	promptKindMCPElicit      promptKindEnum  = 2441
)

// aiMCPElicitMsg is a server's question, sent by the chat goroutine.
type aiMCPElicitMsg struct {
	gen   uint64
	req   mcp.ElicitRequest
	reply chan mcp.ElicitResult
	ch    chan tea.Msg
}

// aiMCPElicitPending is the open question.
type aiMCPElicitPending struct {
	req    mcp.ElicitRequest
	reply  chan mcp.ElicitResult
	field  int
	values map[string]any
}

// mcpChatElicitor sends a server's question to the UI and waits.
func mcpChatElicitor(gen uint64, ch chan tea.Msg) mcp.Elicitor {
	return func(ctx context.Context, r mcp.ElicitRequest) mcp.ElicitResult {
		reply := make(chan mcp.ElicitResult, 1)
		select {
		case ch <- aiMCPElicitMsg{gen: gen, req: r, reply: reply, ch: ch}:
		case <-ctx.Done():
			return mcp.ElicitResult{Action: mcp.ElicitCancel}
		}
		select {
		case a := <-reply:
			return a
		case <-ctx.Done():
			return mcp.ElicitResult{Action: mcp.ElicitCancel}
		}
	}
}

// mcpElicitCall makes every call of call ask the user through the chat.
func mcpElicitCall(call mcp.CallFunc, gen uint64, ch chan tea.Msg) mcp.CallFunc {
	elicit := mcpChatElicitor(gen, ch)
	return func(ctx context.Context, server, tool string, args map[string]any) (mcp.CallResult, error) {
		ctx = mcp.WithElicitor(ctx, func(ctx context.Context, r mcp.ElicitRequest) mcp.ElicitResult {
			r.Server = server
			return elicit(ctx, r)
		})
		return call(ctx, server, tool, args)
	}
}

func (m *Model) onAIMCPElicit(msg aiMCPElicitMsg) tea.Cmd {
	s := m.aiS()
	if msg.gen != s.chatGen || s.mcpx.elicit != nil {
		msg.reply <- mcp.ElicitResult{Action: mcp.ElicitCancel}
		return waitAIChat(msg.ch)
	}
	s.mcpx.elicit = &aiMCPElicitPending{req: msg.req, reply: msg.reply, values: map[string]any{}}
	m.mcpElicitCard()
	return waitAIChat(msg.ch)
}

func elicitText(s string) string { return ai.RedactSecrets(aiCleanText(s)) }

func (m *Model) mcpElicitCard() {
	p := m.aiS().mcpx.elicit
	r := p.req
	server := r.Server
	if server == "" {
		server = "server"
	}
	if r.Mode == mcp.ElicitURL {
		site := r.URL
		warn := ""
		if u, err := url.Parse(r.URL); err == nil && u.Host != "" {
			site = u.Host
			if u.Scheme != "https" {
				warn = "\nThis page does not use https: anyone on the way can read it."
			}
		} else {
			warn = "\nThis is not a normal web address."
		}
		m.confirm = confirm.Risky("MCP "+server+" asks you to open a page",
			elicitText(r.Message)+"\n\nSite: "+site+warn+"\nOnly open it if you trust this site. Never type a password you use elsewhere.",
			confirm.Button{ID: "deny", Title: "Deny"},
			confirm.Button{ID: "open", Title: "Open in browser"}).WithCode(r.URL, "")
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindMCPElicitURL
		return
	}
	var fields []string
	secret := false
	for _, f := range r.Fields {
		fields = append(fields, elicitText(f.Label())+" ("+elicitFieldKind(f)+")")
		secret = secret || f.Secret()
	}
	body := elicitText(r.Message)
	if len(fields) > 0 {
		body += "\n\nIt asks for: " + strings.Join(fields, ", ")
	}
	if secret {
		body += "\n\nIt asks for a password or key. Servers should not ask for these here. termocode never fills it in for you."
	}
	body += "\nWhat you type goes to this server."
	m.confirm = confirm.Risky("MCP "+server+" asks for input", body,
		confirm.Button{ID: "deny", Title: "Deny"},
		confirm.Button{ID: "fill", Title: "Fill in…"})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindMCPElicit
}

// elicitFieldKind is "text, required", "number", "red/blue" …
func elicitFieldKind(f mcp.ElicitField) string {
	k := "text"
	switch {
	case len(f.Enum) > 0:
		k = strings.Join(f.Enum, "/")
	case f.Type == "integer":
		k = "whole number"
	case f.Type == "number":
		k = "number"
	case f.Type == "boolean":
		k = "yes/no"
	case f.Format != "":
		k = f.Format
	}
	if f.Required {
		k += ", required"
	}
	return k
}

// answerMCPElicit handles the cards' buttons ("" is Esc).
func (m *Model) answerMCPElicit(kind confirmKindEnum, id string) tea.Cmd {
	s := m.aiS()
	p := s.mcpx.elicit
	if p == nil {
		return nil
	}
	switch {
	case id == "fill" && kind == confirmKindMCPElicit:
		if len(p.req.Fields) == 0 {
			return m.mcpElicitReview()
		}
		p.field = 0
		m.mcpElicitAskField("")
		return nil
	case id == "open" && kind == confirmKindMCPElicitURL:
		if err := osOpen(p.req.URL); err != nil {
			m.mcpElicitDone(mcp.ElicitCancel, nil)
			return m.aiMCPWarn("Could not open the page", err.Error())
		}
		m.mcpElicitDone(mcp.ElicitAccept, nil)
		return m.aiInfo("Opened the page of MCP " + p.req.Server)
	case id == "send" && kind == confirmKindMCPElicitSend:
		m.mcpElicitDone(mcp.ElicitAccept, p.values)
		return nil
	case id == "deny":
		m.mcpElicitDone(mcp.ElicitDecline, nil)
		return nil
	}
	m.mcpElicitDone(mcp.ElicitCancel, nil) // Esc, Cancel
	return nil
}

func (m *Model) mcpElicitDone(action string, content map[string]any) {
	s := m.aiS()
	p := s.mcpx.elicit
	s.mcpx.elicit = nil
	if p != nil {
		p.reply <- mcp.ElicitResult{Action: action, Content: content}
	}
}

// mcpElicitAskField opens the prompt of the current field (errText: the
// last value was not valid).
func (m *Model) mcpElicitAskField(errText string) {
	p := m.aiS().mcpx.elicit
	f := p.req.Fields[p.field]
	initial := ""
	if f.Default != nil && !f.Secret() {
		initial = fmt.Sprint(f.Default)
	}
	m.prompt = prompt.New(fmt.Sprintf("MCP %s · %d of %d", p.req.Server, p.field+1, len(p.req.Fields)), elicitText(f.Label())+":", initial)
	note := elicitText(f.Description)
	if k := elicitFieldKind(f); k != "text" {
		note = strings.TrimSpace(note + " (" + k + ")")
	}
	if !f.Required {
		note = strings.TrimSpace(note + " Leave empty to skip.")
		m.prompt.SetAllowEmpty(true)
	}
	if f.Secret() {
		m.prompt.SetMasked(true)
		note = strings.TrimSpace(note + " Goes to the server; termocode does not save it.")
	}
	m.prompt.SetNote(note)
	if errText != "" {
		m.prompt.SetError(errText)
	}
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindMCPElicit
}

// elicitValue checks and converts one typed value ("" skips).
func elicitValue(f mcp.ElicitField, v string) (any, bool, error) {
	if !f.Secret() {
		v = strings.TrimSpace(v)
	}
	if v == "" {
		if f.Required {
			return nil, false, fmt.Errorf("%s is required", f.Label())
		}
		return nil, false, nil
	}
	if len(f.Enum) > 0 {
		for _, e := range f.Enum {
			if strings.EqualFold(e, v) {
				return e, true, nil
			}
		}
		return nil, false, fmt.Errorf("one of: %s", strings.Join(f.Enum, ", "))
	}
	switch f.Type {
	case "integer":
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, false, fmt.Errorf("a whole number, please")
		}
		return n, true, nil
	case "number":
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, false, fmt.Errorf("a number, please")
		}
		return n, true, nil
	case "boolean":
		switch strings.ToLower(v) {
		case "y", "yes", "true", "1":
			return true, true, nil
		case "n", "no", "false", "0":
			return false, true, nil
		}
		return nil, false, fmt.Errorf("yes or no, please")
	}
	return v, true, nil
}

// onMCPElicitField takes one field's value and asks the next.
func (m *Model) onMCPElicitField(v string) tea.Cmd {
	p := m.aiS().mcpx.elicit
	if p == nil || p.field >= len(p.req.Fields) {
		return nil
	}
	f := p.req.Fields[p.field]
	val, set, err := elicitValue(f, v)
	if err != nil {
		m.mcpElicitAskField(err.Error())
		return nil
	}
	if set {
		p.values[f.Name] = val
	}
	if p.field++; p.field < len(p.req.Fields) {
		m.mcpElicitAskField("")
		return nil
	}
	return m.mcpElicitReview()
}

// mcpElicitReview shows what will be sent (secrets as dots).
func (m *Model) mcpElicitReview() tea.Cmd {
	p := m.aiS().mcpx.elicit
	var lines []string
	for _, f := range p.req.Fields {
		v, ok := p.values[f.Name]
		shown := "(empty)"
		switch {
		case ok && f.Secret():
			shown = "••••••"
		case ok:
			shown = elicitText(fmt.Sprint(v))
		}
		lines = append(lines, elicitText(f.Label())+": "+shown)
	}
	body := "Nothing to send but your OK."
	if len(lines) > 0 {
		body = strings.Join(lines, "\n")
	}
	m.confirm = confirm.Risky("Send this to MCP "+p.req.Server+"?", body,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "send", Title: "Send"})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindMCPElicitSend
	return nil
}

// aiMCPElicitPromptClosed: Esc on a field cancels the question.
func (m *Model) aiMCPElicitPromptClosed() tea.Cmd {
	if m.promptKind == promptKindMCPElicit && m.aiS().mcpx.elicit != nil {
		m.mcpElicitDone(mcp.ElicitCancel, nil)
		return m.aiInfo("Cancelled: nothing was sent")
	}
	return nil
}
