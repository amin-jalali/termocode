package app

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// AI: Side Question… from the page (WEBP-9): chat.side asks the laptop's
// quick model beside its open chat (ai_side.go); the answer goes back to
// the page, never into the main chat, and opens no card on the laptop.

func init() {
	webOps[webproto.OpChatSide] = (*Model).webChatSide
	webOps[webproto.OpAIMCPItems] = (*Model).webAIMCPItems
}

// webAIMCPItems is AI: MCP Prompts and Resources… for the page: the list
// the laptop's picker shows (servers start, so off the UI thread). The
// servers' names, titles and hints are data from outside: cleaned and
// redacted; the page only puts a pick in its chat draft.
func (m *Model) webAIMCPItems(call *webCall) tea.Cmd {
	mgr := m.aiMCP()
	call.later()
	return func() tea.Msg {
		items := aiMCPInsertItems(mgr)
		if len(items) > webproto.MaxMCPItems {
			items = items[:webproto.MaxMCPItems]
		}
		out := make([]webproto.MCPItem, 0, len(items))
		for _, it := range items {
			out = append(out, webproto.MCPItem{Text: webClean(it.ID), Title: webText(it.Title), Hint: webText(it.Hint)})
		}
		call.reply(webproto.MCPItems{Items: out})
		return nil
	}
}

func (m *Model) webChatSide(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ChatSendArgs)
	s := m.aiS()
	switch {
	case len(a.Attach) > 0: // MOB-5: attaches go with chat.send only
		call.fail(webproto.CodeBadRequest, "A side question takes no attached items.")
		return nil
	case a.ChatID != "" && a.ChatID != s.conv.ID:
		call.fail(webproto.CodeConflict, "This chat is not open on the laptop. Open it there first.")
		return nil
	case !m.aiConfigured():
		call.fail(webproto.CodeConflict, "AI is not set up on the laptop.")
		return nil
	case s.side.busy:
		call.fail(webproto.CodeBusy, "The side question is still being answered.")
		return nil
	}
	cmd := m.aiSideAsk(a.Text, call)
	if cmd == nil {
		call.fail(webproto.CodeBadRequest, "Type a question first.") // a no-op when it was answered
		return nil
	}
	call.later()
	m.webAuditOp(call, strconv.Itoa(len(a.Text))+" bytes", "asked")
	return cmd
}
