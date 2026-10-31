package agentrun

import (
	"errors"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/ai"
)

// SendPrefix starts a message Send hands to a step's agent, so the model
// knows it is not part of its task text.
const SendPrefix = "[A message sent to you while you work on this step]\n"

// SendMax caps a message (runes).
const SendMax = 4000

// mailbox holds the messages for the agents that run now, by node id.
type mailbox struct {
	mu   sync.Mutex
	open map[string][]string // a running agent's waiting messages
}

func (b *mailbox) openNode(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.open == nil {
		b.open = map[string][]string{}
	}
	b.open[id] = nil
}

// closeNode ends the node's box; left gets each message it never read.
func (b *mailbox) closeNode(id string, left func(string)) {
	b.mu.Lock()
	rest := b.open[id]
	delete(b.open, id)
	b.mu.Unlock()
	for _, t := range rest {
		left(t)
	}
}

func (b *mailbox) put(id, text string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	q, ok := b.open[id]
	if !ok {
		return false
	}
	b.open[id] = append(q, text)
	return true
}

func (b *mailbox) take(id string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	q := b.open[id]
	if len(q) > 0 {
		b.open[id] = nil
	}
	return q
}

// Send queues text for the agent of running node id: it reads it after
// its current tool calls, or before its turn would end (NodeMessage).
// It refuses a node that does not run an agent now (pending, ended, a
// CLI step).
func (h *Handle) Send(id, text string) error {
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return errors.New("the message is empty")
	case len([]rune(text)) > SendMax:
		return errors.New("the message is too long (at most 4000 characters)")
	}
	if !h.mail.put(id, text) {
		return errors.New("the step does not run now")
	}
	return nil
}

// inbox is the Agent.Inbox of node id: Send's messages, then next's.
func (h *Handle) inbox(id string, next func() []ai.Message) func() []ai.Message {
	return func() []ai.Message {
		var out []ai.Message
		for _, t := range h.mail.take(id) {
			out = append(out, ai.Message{Role: ai.RoleUser, Content: SendPrefix + t})
			h.emit(Event{NodeID: id, Kind: NodeMessage, Output: t})
		}
		if next != nil {
			out = append(out, next()...)
		}
		return out
	}
}
