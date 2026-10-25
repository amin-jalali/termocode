package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
)

// Memory asks once (OM-2, owner decision): memory is off by default. The
// first time a turn could give lessons in a workspace that never decided,
// one card asks. Not now (first, focused; Esc too) is kept per workspace
// and the card never comes back; AI: Memory: Turn On/Off can still turn
// memory on later. Turn on memory turns it on (and makes git ignore the
// folder) and saves the lessons of the turn that asked.

const confirmKindAIMemoryAsk confirmKindEnum = 1005

// aiMemoryAskPending is the turn waiting for the card's answer.
type aiMemoryAskPending struct {
	convID string
	from   int // the turn's user message
}

// askAIMemory opens the ask-once card for the turn at from. Another open
// card wins: the question waits for a later turn. Without a quick model
// slot no lesson could be saved, so it does not ask.
func (m *Model) askAIMemory(from int) tea.Cmd {
	s := m.aiS()
	if m.confirmOpen {
		return nil
	}
	if _, err := s.resolveSlot(ai.SlotQuick); err != nil {
		return nil
	}
	s.memAsk = &aiMemoryAskPending{convID: s.conv.ID, from: from}
	msg := "termocode can keep short lessons from your chats (for example \"run go test ./... before saying a fix works\") " +
		"and give them to the AI in later chats in this project.\n\n" +
		"  • A quick model writes them after turns that used tools or failed (a small cost).\n" +
		"  • They go to " + aiMemoryRel + "; termocode adds .termocode/memory/ to .gitignore.\n" +
		"  • Secrets are removed before and after the model.\n\n" +
		"You can change this later: AI: Memory: Turn On/Off."
	m.confirm = confirm.Risky("Save lessons for later chats?", msg,
		confirm.Button{ID: "notnow", Title: "Not now"},
		confirm.Button{ID: "on", Title: "Turn on memory", Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIMemoryAsk
	return nil
}

// aiDemoMemoryOff keeps the ask-once card out of the run demos (the
// screenshots show the run, not the memory question).
func aiDemoMemoryOff() {
	if sess := loadSession(); sess.AIMemory == aiMemoryUndecided {
		sess.AIMemory = aiMemoryOff
		saveSession(sess)
	}
}

// answerAIMemoryAsk stores the answer; "" (Esc) is Not now.
func (m *Model) answerAIMemoryAsk(id string) tea.Cmd {
	s := m.aiS()
	p := s.memAsk
	s.memAsk = nil
	if id != "on" {
		sess := loadSession()
		sess.AIMemory = aiMemoryOff
		saveSession(sess)
		return m.aiInfo("Memory stays off. Turn it on any time: AI: Memory: Turn On/Off")
	}
	cmd := m.turnOnAIMemory()
	if p == nil || p.convID != s.conv.ID || p.from < 0 || p.from >= len(s.conv.Messages) || s.streaming {
		return cmd
	}
	return tea.Batch(cmd, m.startAIMemory(p.from))
}
