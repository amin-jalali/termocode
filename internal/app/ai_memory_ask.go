package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/ai"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Memory asks once (OM-2, owner decision): memory is off by default. The
// first time a turn could give lessons in a workspace that never decided,
// a quiet card under the last answer asks: "Save lessons for later
// chats?  Turn on  Not now". It is no dialog: it takes no focus and the
// palette stays free. Not now is kept per workspace and the card never
// comes back; AI: Memory: Turn On/Off (palette) can still decide, and
// then the card goes. Turn on turns memory on (and makes git ignore the
// folder) and saves the lessons of the turn that asked. Nothing is saved
// while undecided.

// aiMemoryAskPending is the turn the card in the chat is for.
type aiMemoryAskPending struct {
	convID string
	from   int // the turn's user message
}

// askAIMemory shows the ask-once card in the chat for the turn at from
// (a later turn moves it to that turn). Without a quick model slot no
// lesson could be saved, so it does not ask.
func (m *Model) askAIMemory(from int) tea.Cmd {
	s := m.aiS()
	if _, err := s.resolveSlot(ai.SlotQuick); err != nil {
		return nil
	}
	s.memAsk = &aiMemoryAskPending{convID: s.conv.ID, from: from}
	s.runGen++ // the transcript cache must redraw
	return nil
}

// aiMemoryAskLines is the card under the last answer: a question, a row
// of two chips and a hint for the palette way.
func (m Model) aiMemoryAskLines(width, inner int) []aiLine {
	s := m.ai
	if s == nil || s.memAsk == nil || s.memAsk.convID != s.conv.ID || s.streaming {
		return nil
	}
	ind := strings.Repeat(" ", aiIndent)
	out := []aiLine{{}}
	out = append(out, aiLine{text: ind + "Save lessons for later chats?", style: theme.Fg(theme.TextSecondary).Bold(true)})
	chip := func(label string, t float64) (string, int) {
		return lipgloss.NewStyle().Background(theme.Mix(theme.BgPanel, theme.AIAccent, t)).
			Foreground(theme.LG(theme.TextPrimary)).Render(" " + label + " "), runewidth.StringWidth(label) + 2
	}
	on, onW := chip("Turn on", 0.30)
	no, noW := chip("Not now", 0.10)
	row := theme.Bg(theme.BgPanel).Render(ind) + on + theme.Bg(theme.BgPanel).Render(" ") + no
	out = append(out, aiLine{styled: row, buttons: []aiLineButton{
		{aiIndent, aiIndent + onW, "memask-on"},
		{aiIndent + onW + 1, aiIndent + onW + 1 + noW, "memask-notnow"}}})
	out = append(out, aiLine{text: ind + "Or: palette, AI: Memory…", style: theme.Fg(theme.TextDim)})
	return out
}

// aiDemoMemoryOff keeps the ask-once card out of the run demos (the
// screenshots show the run, not the memory question).
func aiDemoMemoryOff() {
	if sess := loadSession(); sess.AIMemory == aiMemoryUndecided {
		sess.AIMemory = aiMemoryOff
		saveSession(sess)
	}
}

// answerAIMemoryAsk stores the card's answer ("on" or Not now).
func (m *Model) answerAIMemoryAsk(id string) tea.Cmd {
	s := m.aiS()
	p := s.memAsk
	s.memAsk = nil
	s.runGen++
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
