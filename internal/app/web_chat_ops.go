package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The chat requests (WEB-13). They act on the chat open on the laptop:
// the page follows it (chat.pin only changes what the page shows; the
// hub keeps nothing for it). chat.send to a chat the laptop does not show
// is refused rather than switching the laptop's chat under the user.
// chat.new and chat.send never move the laptop's focus.

func init() {
	webOps[webproto.OpChatSend] = (*Model).webChatSend
	webOps[webproto.OpChatStop] = (*Model).webChatStop
	webOps[webproto.OpChatNew] = (*Model).webChatNew
	webOps[webproto.OpChatOpen] = (*Model).webChatOpen
	webOps[webproto.OpChatGetMessage] = (*Model).webChatGetMessage
	webOps[webproto.OpChatPin] = (*Model).webChatPin
}

// webChatSend sends a message in the laptop's open chat, as if typed
// there. A project profile, rules or skills file the user has not trusted
// yet opens its card on the laptop first (the cards topic shows it).
func (m *Model) webChatSend(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ChatSendArgs)
	if strings.TrimSpace(a.Text) == "" {
		call.fail(webproto.CodeBadRequest, "Type a message first.")
		return nil
	}
	s := m.aiS()
	switch {
	case a.ChatID != "" && a.ChatID != s.conv.ID:
		call.fail(webproto.CodeConflict, "This chat is not open on the laptop. Open it there first.")
		return nil
	case s.streaming:
		call.fail(webproto.CodeBusy, "The AI is still answering. Stop it or wait.")
		return nil
	case !m.aiConfigured():
		call.fail(webproto.CodeConflict, "AI is not set up on the laptop.")
		return nil
	}
	cmd := m.sendAIChat(a.Text)
	call.reply(webproto.ChatRef{ID: s.conv.ID})
	return cmd
}

// webChatStop stops the AI (the turn, a one-shot action, queued runs),
// like Stop on the laptop. Stopping is never harmful: it always answers ok.
func (m *Model) webChatStop(call *webCall) tea.Cmd {
	var cmd tea.Cmd
	if m.ai != nil {
		cmd = m.aiCancelAll()
	}
	call.reply(nil)
	return cmd
}

// webChatNew opens an empty chat on the laptop (the old one stays in the
// history). Refused while the AI answers: that would stop it.
func (m *Model) webChatNew(call *webCall) tea.Cmd {
	s := m.aiS()
	if s.streaming {
		call.fail(webproto.CodeBusy, "The AI is answering. Stop it first.")
		return nil
	}
	m.startNewAIChat()
	call.reply(webproto.ChatRef{ID: s.conv.ID})
	return nil
}

// webChatOpen answers with a whole chat to read: the open one from
// memory, a stored one read off the UI thread.
func (m *Model) webChatOpen(call *webCall) tea.Cmd {
	id := call.args.(*webproto.ChatOpenArgs).ID
	s := m.aiS()
	if id == s.conv.ID {
		call.reply(m.webChatBuild(&webChatPub{}))
		return nil
	}
	call.later()
	store := s.store
	go func() {
		c, err := store.Load(id)
		if err != nil || c.ID != id {
			call.fail(webproto.CodeNotFound, "That chat is gone.")
			return
		}
		call.reply(webStoredChat(&c))
	}()
	return nil
}

// webStoredChat is a stored chat for the page (nothing streams in it).
func webStoredChat(c *ai.Conversation) webproto.ChatSnap {
	snap := webproto.ChatSnap{ID: c.ID, Title: webChatTitle(*c), Messages: []webproto.ChatMsg{}}
	full := webChatFullFrom(c.Messages)
	for i := range c.Messages {
		cm, _ := webChatMsg(i, &c.Messages[i], false)
		if i < full {
			cm = webChatStub(cm)
		}
		snap.Messages = append(snap.Messages, cm)
	}
	return snap
}

// webChatGetMessage answers with one message of the open chat, cut at
// webproto.MaxChatMsgFull instead of MaxChatMsgText.
func (m *Model) webChatGetMessage(call *webCall) tea.Cmd {
	i := call.args.(*webproto.ChatGetMessageArgs).I
	s := m.aiS()
	if i >= len(s.conv.Messages) {
		call.fail(webproto.CodeNotFound, "That message is gone.")
		return nil
	}
	sm := s.conv.Messages[i]
	if webChatStreaming(s, i, len(s.conv.Messages)) {
		cm, _ := webChatMsg(i, &sm, true) // still coming: what may go out now
		call.reply(cm)
		return nil
	}
	sm.Steps = append([]ai.StoredStep(nil), sm.Steps...) // a run may change them on Update
	if sm.Run != nil {
		r := *sm.Run
		sm.Run = &r
	}
	call.later()
	go func() { // a big message's markdown takes longer than the call budget
		cm, _ := webChatMsgMax(i, &sm, webproto.MaxChatMsgFull)
		call.reply(cm)
	}()
	return nil
}

// webChatPin is the page saying it stops (On) or goes back to following
// the laptop's chat. The hub keeps nothing: the chat topic always follows
// the laptop, the page decides what it shows.
func (m *Model) webChatPin(call *webCall) tea.Cmd {
	call.reply(nil)
	return nil
}
