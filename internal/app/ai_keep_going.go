package app

import (
	"fmt"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Keep going while todos are open (OMX-7, OMX-45). When it is on for a
// chat (AI: Toggle Keep Going), a plain turn that ends with open todos
// sends the next "keep going" message by itself. It stops when the list
// is done, after the "work until done" rounds (ai_until_done_rounds),
// when the pushed turns cost the "work until done" budget, when a turn
// made no progress on the list, when a turn failed, and at once when the
// user stops the AI. Every pushed message is shown with an "auto" chip.

// aiKeepGoingRun is the state of one keep-going chain (it starts again
// with each message the user sends).
type aiKeepGoingRun struct {
	pushes  int
	spent   float64   // dollars the pushed turns cost
	before  []ai.Todo // the list when the last push was sent
	stopped bool      // the user stopped the AI: no more pushes
	pushing bool      // sendAIChat is called by the loop
}

// toggleAIKeepGoing turns keep going on or off for the open chat.
func (m *Model) toggleAIKeepGoing() tea.Cmd {
	s := m.aiS()
	s.conv.KeepGoing = !s.conv.KeepGoing
	if len(s.conv.Messages) > 0 {
		if err := s.store.Save(&s.conv); err != nil {
			recordError("[ai] save chat: " + err.Error())
		}
	}
	if !s.conv.KeepGoing {
		return m.aiInfo("Keep going off in this chat")
	}
	rounds, budget, _ := s.settings.UntilDone()
	text := fmt.Sprintf("Keep going on in this chat: up to %d more turn%s while todos are open", rounds, plural(rounds))
	if budget > 0 {
		text += fmt.Sprintf(" (budget $%.2f)", budget)
	}
	return m.aiInfo(text + " · Stop: Ctrl+C")
}

// aiKeepGoingLabel is the todo card's " · keep going on" ("" when off).
func (m Model) aiKeepGoingLabel() string {
	if m.ai == nil || !m.ai.conv.KeepGoing {
		return ""
	}
	return " · keep going on"
}

// aiKeepGoingUsage adds the cost of a pushed turn to the chain.
func (s *aiState) aiKeepGoingUsage(u *ai.Usage) {
	if u == nil || s.keep.pushes == 0 {
		return
	}
	if c, ok := ai.NewPricing(s.settings.Prices).Cost(s.cfg.Kind, *u); ok {
		s.keep.spent += c
	}
}

// aiKeepGoingNext runs after a plain turn: it sends the next push, or
// says why the chain stops. nil when nothing is to do.
func (m *Model) aiKeepGoingNext() tea.Cmd {
	s := m.aiS()
	k := &s.keep
	if !s.conv.KeepGoing || k.stopped || s.streaming {
		return nil
	}
	open := ai.TodosOpen(s.conv.Todos)
	if open == 0 {
		*k = aiKeepGoingRun{}
		return nil
	}
	rounds, budget, _ := s.settings.UntilDone()
	n := len(s.conv.Messages)
	why := ""
	switch {
	case n > 0 && s.conv.Messages[n-1].Role == ai.StoredError:
		why = "the last turn failed"
	case k.pushes > 0 && slices.Equal(k.before, s.conv.Todos):
		why = "no progress on the todos in the last turn"
	case k.pushes >= rounds:
		why = fmt.Sprintf("%d of %d turns used", k.pushes, rounds)
	case budget > 0 && k.spent >= budget:
		why = fmt.Sprintf("budget $%.2f reached ($%.2f spent)", budget, k.spent)
	}
	if why != "" {
		*k = aiKeepGoingRun{}
		m.aiNote(fmt.Sprintf("↻ Keep going stopped: %s · %d todo%s open", why, open, plural(open)))
		return nil
	}
	k.pushes++
	k.before = slices.Clone(s.conv.Todos)
	k.pushing = true
	cmd := m.sendAIChat(aiKeepGoingText(open))
	k.pushing = false
	if s.streaming && s.turnMsg < len(s.conv.Messages) {
		s.conv.Messages[s.turnMsg].KeepGoing = k.pushes
	}
	return cmd
}

// aiKeepGoingText is the message a push sends.
func aiKeepGoingText(open int) string {
	verb := "are"
	if open == 1 {
		verb = "is"
	}
	return fmt.Sprintf("Keep going: %d todo%s %s still open. Work on the next one and update the list with todo_write. "+
		"If you are blocked or need me, say so and stop.", open, plural(open), verb)
}

// aiKeepGoingChip is the "auto" chip of a pushed message.
func (s *aiState) aiKeepGoingChip(push int) string {
	rounds, _, _ := s.settings.UntilDone()
	return fmt.Sprintf("↻ auto · keep going %d/%d", push, rounds)
}

// aiNote adds a display-only line to the chat and saves it.
func (m *Model) aiNote(text string) {
	s := m.aiS()
	s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredNote, Text: text})
	if err := s.store.Save(&s.conv); err != nil {
		recordError("[ai] save chat: " + err.Error())
	}
}
