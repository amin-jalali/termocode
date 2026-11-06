package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The chat history from the web (web parity, AI chat): rename, fork,
// export, and delete and clear after a laptop card (Cancel first). They
// do what the laptop's own AI: Rename Chat…, Fork Chat from Message…,
// Export Chat, Delete Chat… and Clear Chat History do, without the
// laptop's picker or prompt, and never move the laptop's focus.

func init() {
	webOps[webproto.OpChatRename] = (*Model).webChatRename
	webOps[webproto.OpChatFork] = (*Model).webChatFork
	webOps[webproto.OpChatExport] = (*Model).webChatExport
	webAskOps[webproto.OpChatDelete] = (*Model).webChatDelete
	webAskOps[webproto.OpChatClear] = (*Model).webChatClear
}

// webChatRename renames a stored chat (and the open one, when it is it).
func (m *Model) webChatRename(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ChatRenameArgs)
	title := strings.TrimSpace(a.Title)
	s := m.aiS()
	if a.ID == s.conv.ID {
		s.conv.Title = title
		if len(s.conv.Messages) > 0 {
			if err := s.store.Save(&s.conv); err != nil {
				call.fail(webproto.CodeInternal, "Could not rename the chat.")
				return nil
			}
		}
		call.reply(nil)
		return nil
	}
	c, err := s.store.Load(a.ID)
	if err != nil || c.ID != a.ID {
		call.fail(webproto.CodeNotFound, "That chat is gone.")
		return nil
	}
	c.Title = title
	if err := s.store.Save(&c); err != nil {
		call.fail(webproto.CodeInternal, "Could not rename the chat.")
		return nil
	}
	call.reply(nil)
	return nil
}

// webChatFork forks the laptop's open chat at message i, as the laptop's
// fork picker does. Refused while the AI answers: forking stops it.
func (m *Model) webChatFork(call *webCall) tea.Cmd {
	i := call.args.(*webproto.ChatForkArgs).I
	s := m.aiS()
	switch {
	case s.streaming || s.chatCancel != nil:
		call.fail(webproto.CodeBusy, "The AI is answering. Stop it first.")
		return nil
	case i >= len(s.conv.Messages):
		call.fail(webproto.CodeNotFound, "That message is gone.")
		return nil
	}
	from, focused := s.conv.ID, s.focused
	cmd := m.aiForkChat(i)
	s.focused = focused
	if s.conv.ID == from {
		call.fail(webproto.CodeConflict, "Pick a message to fork from.")
		return cmd
	}
	call.reply(webproto.ChatRef{ID: s.conv.ID})
	return cmd
}

// webChatExportName is the file name the browser saves an export as.
func webChatExportName() string {
	return "ai-chat-" + clock.Now().Format("2006-01-02") + ".md"
}

// webChatExport answers with a chat as markdown (registered secrets
// hidden): the open one from memory, a stored one read off the UI thread.
func (m *Model) webChatExport(call *webCall) tea.Cmd {
	id := call.args.(*webproto.ChatOpenArgs).ID
	s := m.aiS()
	if id == s.conv.ID {
		call.reply(webproto.ChatExport{Name: webChatExportName(), Text: webChatRedact(aiChatMarkdown(s.conv))})
		return nil
	}
	call.later()
	store := s.store
	crash.GoWith("web: exporting a stored chat", func() {
		c, err := store.Load(id)
		if err != nil || c.ID != id {
			call.fail(webproto.CodeNotFound, "That chat is gone.")
			return
		}
		call.reply(webproto.ChatExport{Name: webChatExportName(), Text: webChatRedact(aiChatMarkdown(c))})
	}, func() { call.fail(webproto.CodeInternal, "Something went wrong.") })
	return nil
}

// webChatDelete asks the laptop to delete one saved chat.
func (m *Model) webChatDelete(call *webCall) *webAsk {
	id := call.args.(*webproto.ChatOpenArgs).ID
	s := m.aiS()
	c, err := s.store.Load(id)
	if err != nil || c.ID != id {
		call.fail(webproto.CodeNotFound, "That chat is gone.")
		return nil
	}
	title := webName(webChatTitle(c))
	return &webAsk{
		summary: id,
		title:   "Delete chat?",
		body:    "Delete the chat “" + title + "”? This cannot be undone.",
		action:  "Delete",
		run: func(m *Model) tea.Cmd {
			if err := m.aiS().store.Delete(id); err != nil {
				return m.aiToastErr("Could not delete chat", err)
			}
			m.dropOpenChatIf(func(c string) bool { return c == id })
			var cmd tea.Cmd
			m.toast, cmd = m.toast.Push(toast.Info, "Chat deleted")
			return cmd
		},
	}
}

// webChatClear asks the laptop to delete every saved chat.
func (m *Model) webChatClear(call *webCall) *webAsk {
	n := len(m.aiS().store.List())
	if n == 0 {
		call.fail(webproto.CodeNotFound, "There are no saved chats.")
		return nil
	}
	return &webAsk{
		summary: itoa(n) + " chats",
		title:   "Clear chat history?",
		body:    "Delete all " + itoa(n) + " saved chats? This cannot be undone.",
		action:  "Delete All",
		run:     func(m *Model) tea.Cmd { return m.onAIHistoryClearConfirm("clear") },
	}
}
