package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// An MCP server's question on the phone (MOB-12). A form-mode question
// with plain fields is a cards entry with a form (mcp.FormOf): Deny
// first, then Send, which carries the answers (CardsAnswerArgs.Form).
// The laptop checks and converts them like its own prompts
// (mcp.FormValues), closes its own dialog and answers the server. A
// question that asks for a password or key, a URL-mode question and a
// form too big for the phone can only be denied on the phone: "Answer
// on the laptop". The audit line names the server and the field names,
// never the values.

// webElicitCard is the card of the open question.
func webElicitCard(p *aiMCPElicitPending, deny webproto.CardChoice) webproto.Card {
	r := p.req
	server := r.Server
	if server == "" {
		server = "server"
	}
	c := webproto.Card{Kind: webproto.CardMCP, Title: "MCP " + elicitText(server) + " asks for input",
		Body: elicitText(r.Message) + "\n\nWhat you send goes to this server."}
	if r.Mode == mcp.ElicitURL {
		c.Title = "MCP " + elicitText(server) + " asks you to open a page"
		c.Body = elicitText(r.Message) + "\n\nOpen the page on the laptop."
		c.Choices = []webproto.CardChoice{deny, {ID: "open", Label: "Open on laptop", NeedsLaptop: true}}
		return c
	}
	if b, ok := mcp.FormOf("elicit", r, aiCleanText); ok {
		c.Form = b.Form
		c.Choices = []webproto.CardChoice{deny, {ID: webproto.ChoiceSend, Label: "Send"}}
		return c
	}
	c.Body += "\nIt asks for something that stays on the laptop. Answer it there."
	c.Choices = []webproto.CardChoice{deny, {ID: "fill", Label: "Fill in on laptop", NeedsLaptop: true}}
	return c
}

// webAnswerElicit answers the open question from the phone: deny
// declines, send checks the answers and accepts.
func (m *Model) webAnswerElicit(call *webCall, ref *webCardRef, p *aiMCPElicitPending, a *webproto.CardsAnswerArgs, device string) tea.Cmd {
	var values map[string]any
	switch a.Choice {
	case webproto.ChoiceSend:
		if len(ref.card.Form) == 0 {
			call.fail(webproto.CodeForbidden, "Answer this on the laptop.")
			return nil
		}
		v, err := mcp.FormValues(p.req, a.Form)
		if err != nil {
			call.fail(webproto.CodeBadRequest, elicitText(err.Error()))
			return nil
		}
		values = v
	case webproto.ChoiceDeny:
	default:
		call.fail(webproto.CodeForbidden, "Answer this on the laptop.")
		return nil
	}
	// Close the laptop's own dialog of this question.
	if m.confirmOpen && (m.confirmKind == confirmKindMCPElicit || m.confirmKind == confirmKindMCPElicitURL || m.confirmKind == confirmKindMCPElicitSend) {
		m.confirmOpen = false
	}
	if m.promptOpen && m.promptKind == promptKindMCPElicit {
		m.promptOpen = false
	}
	word := "Denied"
	if values != nil {
		m.mcpElicitDone(mcp.ElicitAccept, values)
		word = "Sent"
	} else {
		m.mcpElicitDone(mcp.ElicitDecline, nil)
	}
	pub := &m.web.cards
	if pub.by == nil {
		pub.by = map[string]string{}
	}
	pub.by[a.ID] = device
	m.aiAudit(webNow(), "card:"+ref.card.Kind, webElicitAuditArgs(p), a.Choice, "web:"+device, word)
	var t tea.Cmd
	m.toast, t = m.toast.PushDetail(toast.Info, "Answered on "+device, word+": "+ref.card.Title)
	call.reply(nil)
	return t
}

// webElicitAuditArgs names the server and the fields, never the values.
func webElicitAuditArgs(p *aiMCPElicitPending) string {
	out := "server=" + p.req.Server
	if len(p.req.Fields) > 0 {
		out += " fields="
		for i, f := range p.req.Fields {
			if i > 0 {
				out += ","
			}
			out += f.Name
		}
	}
	return out
}
