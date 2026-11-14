package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The phone remote's queue (CMP-46, webproto/ops_chatqueue.go). While the
// laptop's AI answers in its open chat, chat.queue lines messages up; the
// first goes out, as if typed on the laptop, when the turn ends. {now}
// steers: the turn stops and that one goes next. Stop drops the queue.
// The queue belongs to the open chat: when another chat opens it is gone.

// aiQueued is one waiting message.
type aiQueued struct {
	id   string
	text string
}

// webChatQueueText is how much of a queued message the page sees.
const webChatQueueText = 200

func init() {
	webOps[webproto.OpChatQueue] = (*Model).webChatQueue
}

// webChatQueue handles chat.queue: {text} lines one up (sent at once while
// the AI is idle), {remove} takes one back, {now} steers with one.
func (m *Model) webChatQueue(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ChatQueueArgs)
	s := m.aiS()
	q := s.chatQueue()
	switch {
	case a.Text != "":
		switch {
		case !m.aiConfigured():
			call.fail(webproto.CodeConflict, "AI is not set up on the laptop.")
			return nil
		case s.home.busy:
			call.fail(webproto.CodeBusy, "The phone chat is answering. Stop it or wait.")
			return nil
		case len(q) >= webproto.MaxChatQueue:
			call.fail(webproto.CodeBusy, "Five messages are waiting already. Wait for the AI or take one back.")
			return nil
		}
		s.queueN++
		s.queue = append(q, aiQueued{id: "q-" + strconv.Itoa(s.queueN), text: a.Text})
		s.queueChat = s.conv.ID
		var cmd tea.Cmd
		if !s.streaming {
			cmd = m.aiSendQueued()
		}
		call.reply(s.webQueueList())
		return cmd
	case a.Remove != "":
		i := aiQueueIndex(q, a.Remove)
		if i < 0 {
			call.fail(webproto.CodeNotFound, "That message is not waiting any more.")
			return nil
		}
		s.queue = append(q[:i:i], q[i+1:]...)
		call.reply(s.webQueueList())
		return nil
	default:
		i := aiQueueIndex(q, a.Now)
		if i < 0 {
			call.fail(webproto.CodeNotFound, "That message is not waiting any more.")
			return nil
		}
		it := q[i]
		rest := append(q[:i:i], q[i+1:]...)
		s.queue = append([]aiQueued{it}, rest...)
		var cmd tea.Cmd
		if s.streaming {
			if s.chatCancel != nil { // the turn ends; its done sends this one next
				s.chatCancel()
				m.cancelAIReviewTurn()
			}
		} else {
			cmd = m.aiSendQueued()
		}
		call.reply(s.webQueueList())
		return cmd
	}
}

func aiQueueIndex(q []aiQueued, id string) int {
	for i := range q {
		if q[i].id == id {
			return i
		}
	}
	return -1
}

// chatQueue is the open chat's queue; a queue left from another chat is
// dropped.
func (s *aiState) chatQueue() []aiQueued {
	if len(s.queue) > 0 && s.queueChat != s.conv.ID {
		s.queue = nil
	}
	return s.queue
}

// dropChatQueue empties the queue (Stop).
func (s *aiState) dropChatQueue() {
	s.queue = nil
}

// webQueueList is the queue for the page.
func (s *aiState) webQueueList() webproto.ChatQueueList {
	l := webproto.ChatQueueList{Queue: []webproto.ChatQueued{}}
	for _, it := range s.chatQueue() {
		l.Queue = append(l.Queue, webproto.ChatQueued{ID: it.id, Text: truncRunes(it.text, webChatQueueText)})
	}
	return l
}

// webQueueKey sums up the queue to see a change cheaply.
func (s *aiState) webQueueKey() string {
	var b strings.Builder
	for _, it := range s.chatQueue() {
		b.WriteString(it.id + "\x00")
	}
	return b.String()
}

// aiSendQueued sends the first queued message in the open chat when the
// AI is free; nil when nothing waits or it cannot go yet.
func (m *Model) aiSendQueued() tea.Cmd {
	s := m.aiS()
	q := s.chatQueue()
	if len(q) == 0 || s.streaming || s.home.busy || !m.aiConfigured() {
		return nil
	}
	it := q[0]
	s.queue = q[1:]
	if len(s.queue) == 0 {
		s.queue = nil
	}
	if cmd, ok := m.aiMCPPromptSubmit(strings.TrimSpace(it.text)); ok { // as Enter on the laptop
		return cmd
	}
	return m.sendAIChat(it.text)
}
