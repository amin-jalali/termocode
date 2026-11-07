package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The phone's own chat (MOB-16, owner Q3): chat.home sends the phone's
// message to a background chat of this workspace, "Phone · <device>",
// instead of the laptop's open chat. The laptop's chat, panel and focus
// stay still. The main model slot answers from the phone chat's own
// history, with no tools (a background turn must never change files or
// open cards behind the laptop's back); the chat is a normal stored chat,
// so the laptop's history lists it and can open it.
// One AI turn at a time per workspace: chat.home is busy while the
// laptop's AI answers, and the laptop's send and chat.send are busy while
// the phone chat answers. Stop (laptop or chat.stop) stops it too.

func init() {
	webOps[webproto.OpChatHome] = (*Model).webChatHome
}

// aiHomeContext caps the phone chat messages a turn sends; aiHomeTimeout
// bounds one answer.
const aiHomeContext = 20

var aiHomeTimeout = 3 * time.Minute

const aiHomeSystem = "You are termocode's AI, answering in the user's phone chat about the project open on their laptop. " +
	"Answer short and concrete: the user reads on a small screen. You have no tools in this chat and change nothing; " +
	"when a task needs files, commands or changes, say so and suggest doing it in the laptop's chat."

// aiHomeChat is the phone chat turn that runs now (one per workspace) and
// the phone chats found so far, by device.
type aiHomeChat struct {
	busy   bool
	gen    int
	device string
	cancel context.CancelFunc
	call   *webCall
	conv   *ai.Conversation  // the chat the turn answers in
	ids    map[string]string // device → chat id
}

type aiHomeDoneMsg struct {
	gen      int
	id       string
	question string
	answer   string
	err      error
}

// webHomeTitle is the phone chat's title: "Phone · <device name>".
func webHomeTitle(c *webConn) string {
	name := strings.TrimSpace(webText(c.name))
	if name == "" {
		name = webText(c.device)
	}
	if r := []rune(name); len(r) > 30 {
		name = string(r[:30]) + "…"
	}
	return "Phone · " + name
}

// webHomeFind is the stored phone chat of c's device in this workspace
// (ok false: none yet). The id is kept, so the store is listed once.
func (m *Model) webHomeFind(c *webConn) (ai.Conversation, bool) {
	s := m.aiS()
	root := aiWorkspaceRoot()
	if id := s.home.ids[c.device]; id != "" {
		if conv, err := s.store.Load(id); err == nil && conv.ID == id {
			return conv, true
		}
		delete(s.home.ids, c.device) // deleted on the laptop
	}
	for _, conv := range s.store.List() {
		if conv.Home != nil && conv.Home.Device == c.device && conv.Home.Root == root {
			if full, err := s.store.Load(conv.ID); err == nil && full.ID == conv.ID {
				s.home.remember(c.device, full.ID)
				return full, true
			}
		}
	}
	return ai.Conversation{}, false
}

func (h *aiHomeChat) remember(device, id string) {
	if h.ids == nil {
		h.ids = map[string]string{}
	}
	h.ids[device] = id
}

// webHomeSnap is the phone chat for the page (an empty one before the
// first send: no id yet).
func (m *Model) webHomeSnap(c *webConn, conv *ai.Conversation) webproto.ChatSnap {
	snap := webproto.ChatSnap{Title: webHomeTitle(c), Messages: []webproto.ChatMsg{}}
	if conv != nil && conv.ID != "" {
		snap = webStoredChat(conv)
		snap.Title = webHomeTitle(c)
	}
	s := m.aiS()
	snap.Busy = s.home.busy && s.home.device == c.device
	return snap
}

func (m *Model) webChatHome(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ChatHomeArgs)
	text := strings.TrimSpace(a.Text)
	s := m.aiS()
	if text == "" { // a read: no tokens, no audit
		conv, _ := m.webHomeFind(call.conn)
		call.reply(m.webHomeSnap(call.conn, &conv))
		return nil
	}
	switch {
	case !m.aiConfigured():
		call.fail(webproto.CodeConflict, "AI is not set up on the laptop.")
		return nil
	case s.home.busy:
		call.fail(webproto.CodeBusy, "The phone chat is still answering. Wait or stop it.")
		return nil
	case s.streaming:
		call.fail(webproto.CodeBusy, "The laptop's AI is answering. Wait or stop it.")
		return nil
	}
	slot, err := s.resolveSlot(ai.SlotMain)
	if err != nil {
		call.fail(webproto.CodeConflict, "The AI profile on the laptop has no main model.")
		return nil
	}
	conv, ok := m.webHomeFind(call.conn)
	if !ok {
		conv = ai.NewConversation()
		conv.Title = webHomeTitle(call.conn)
		conv.Home = &ai.HomeChat{Device: call.conn.device, Root: aiWorkspaceRoot()}
	}
	if conv.ID == s.conv.ID {
		call.fail(webproto.CodeConflict, "This phone chat is open on the laptop. Go on there, or open another chat on the laptop.")
		return nil
	}
	if _, err := s.store.Claim(conv.ID); errors.Is(err, ai.ErrChatInUse) {
		call.fail(webproto.CodeConflict, "This phone chat is open in another termocode window.")
		return nil
	}
	msgs := []ai.Message{{Role: ai.RoleSystem, Content: aiHomeSystem}}
	hist := conv.Messages
	if len(hist) > aiHomeContext {
		hist = hist[len(hist)-aiHomeContext:]
	}
	for _, sm := range hist {
		switch {
		case sm.Role == ai.StoredUser:
			msgs = append(msgs, ai.Message{Role: ai.RoleUser, Content: sm.Text})
		case sm.Role == ai.StoredAssistant && sm.Text != "":
			msgs = append(msgs, ai.Message{Role: ai.RoleAssistant, Content: sm.Text})
		}
	}
	msgs = append(msgs, ai.Message{Role: ai.RoleUser, Content: text})
	req := ai.Request{Model: slot.cfg.Model, Messages: msgs}

	ctx, cancel := context.WithTimeout(context.Background(), aiHomeTimeout)
	s.home.gen++
	s.home.busy, s.home.device, s.home.cancel, s.home.call = true, call.conn.device, cancel, call
	s.home.remember(call.conn.device, conv.ID)
	gen, id := s.home.gen, conv.ID
	s.home.conv = &conv
	call.later()
	m.webAuditOp(call, strconv.Itoa(len(text))+" bytes", "asked")
	return func() tea.Msg {
		defer cancel()
		answer, last := ai.Collect(slot.p.Stream(ctx, req))
		return aiHomeDoneMsg{gen: gen, id: id, question: text, answer: strings.TrimSpace(ai.Redact(answer)), err: last.Err}
	}
}

// onAIHomeDone stores the answer in the phone chat and answers the page.
// A stopped or replaced turn is dropped.
func (m *Model) onAIHomeDone(msg aiHomeDoneMsg) tea.Cmd {
	s := m.aiS()
	if !s.home.busy || msg.gen != s.home.gen {
		return nil
	}
	call, conv := s.home.call, s.home.conv
	s.home.busy, s.home.cancel, s.home.call, s.home.conv = false, nil, nil, nil
	if conv == nil || conv.ID != msg.id {
		return nil
	}
	if msg.err != nil {
		call.fail(webproto.CodeConflict, webText("The phone chat failed: "+ai.Redact(msg.err.Error())))
		return nil
	}
	conv.Messages = append(conv.Messages,
		ai.StoredMessage{Role: ai.StoredUser, Text: msg.question},
		ai.StoredMessage{Role: ai.StoredAssistant, Text: msg.answer})
	if conv.ID == s.conv.ID { // the laptop opened it meanwhile: it is the laptop's to save
		s.conv.Messages = append(s.conv.Messages, conv.Messages[len(conv.Messages)-2:]...)
		m.persistAIState()
	} else if err := s.store.Save(conv); err != nil {
		recordError("[web] chat.home: save: " + err.Error())
	}
	call.reply(m.webHomeSnap(call.conn, conv))
	return nil
}

// aiHomeStop stops the phone chat's turn (Stop on the laptop or the
// page); true when one ran. Nothing of it is saved.
func (m *Model) aiHomeStop() bool {
	s := m.aiS()
	if !s.home.busy {
		return false
	}
	if s.home.cancel != nil {
		s.home.cancel()
	}
	if s.home.call != nil {
		s.home.call.fail(webproto.CodeConflict, "Stopped. Nothing was saved.")
	}
	s.home.busy, s.home.cancel, s.home.call, s.home.conv = false, nil, nil, nil
	s.home.gen++
	return true
}
