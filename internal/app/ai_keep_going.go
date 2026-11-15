package app

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Keep going while todos are open (OMX-7, OMX-45). It is on by default
// (ai_keep_going; AI: Toggle Keep Going for one chat): a plain turn that
// ends with open todos sends the next "keep going" message by itself. It
// stops when the list is done, after ai_keep_going_turns pushes, when the
// pushed turns cost the "work until done" budget, after
// aiKeepGoingStall pushes in a row with no change on the list, when the
// reply ends with a question to the user, when a turn failed, and at
// once when the user stops the AI. A turn that hit the step limit
// (ai_max_steps) is pushed on too, todos or not. Every pushed message is
// shown with an "auto" chip.

// aiKeepGoingStall is how many pushes in a row may leave the todos as
// they were before the chain stops.
const aiKeepGoingStall = 3

// aiKeepGoingRun is the state of one keep-going chain (it starts again
// with each message the user sends).
type aiKeepGoingRun struct {
	pushes int
	spent  float64   // dollars the pushed turns cost
	before []ai.Todo // the list when the last push was sent
	stale  int       // pushes in a row that did not change the list
	// stepLimit: the last turn stopped at the step limit, not with an
	// answer (it is pushed on even with no open todos).
	stepLimit bool
	stuck     bool // the repeat brake ended the last turn
	stopped   bool // the user stopped the AI: no more pushes
	pushing   bool // sendAIChat is called by the loop
}

// toggleAIKeepGoing turns keep going on or off for the open chat.
func (m *Model) toggleAIKeepGoing() tea.Cmd {
	s := m.aiS()
	on := !s.keepGoingOn()
	s.conv.KeepGoing, s.conv.KeepGoingOff = on, !on
	if len(s.conv.Messages) > 0 {
		if err := s.store.Save(&s.conv); err != nil {
			recordError("[ai] save chat: " + err.Error())
		}
	}
	if !on {
		return m.aiInfo("Keep going off in this chat")
	}
	rounds := s.settings.KeepGoingLimit()
	_, budget, _ := s.settings.UntilDone()
	text := fmt.Sprintf("Keep going on in this chat: up to %d more turn%s while todos are open", rounds, plural(rounds))
	if budget > 0 {
		text += fmt.Sprintf(" (budget $%.2f)", budget)
	}
	return m.aiInfo(text + " · Stop: Ctrl+C")
}

// keepGoingOn is whether keep going is on for the open chat: its own
// toggle, else ai_keep_going.
func (s *aiState) keepGoingOn() bool {
	if s.conv.KeepGoing || s.conv.KeepGoingOff {
		return s.conv.KeepGoing
	}
	return s.settings.KeepGoingOn()
}

// aiKeepGoingLabel is the todo card's " · keep going on" ("" when off).
func (m Model) aiKeepGoingLabel() string {
	if m.ai == nil || !m.ai.keepGoingOn() {
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
	if !s.keepGoingOn() || k.stopped || s.streaming {
		return nil
	}
	open := ai.TodosOpen(s.conv.Todos)
	if open == 0 && !k.stepLimit {
		*k = aiKeepGoingRun{}
		return nil
	}
	rounds := s.settings.KeepGoingLimit()
	_, budget, _ := s.settings.UntilDone()
	n := len(s.conv.Messages)
	if k.pushes > 0 {
		if slices.Equal(k.before, s.conv.Todos) {
			k.stale++
		} else {
			k.stale = 0
		}
	}
	why := ""
	switch {
	case n > 0 && s.conv.Messages[n-1].Role == ai.StoredError:
		why = "the last turn failed"
	case k.stuck:
		why = "the AI made the same call again and again"
	case aiAsksTheUser(s.conv.Messages):
		why = "the AI asked you a question"
	case k.stale >= aiKeepGoingStall:
		why = fmt.Sprintf("no progress on the todos in the last %d turns", k.stale)
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
	text := aiStepLimitPush
	if open > 0 {
		text = aiKeepGoingText(open)
	}
	k.pushes++
	k.before = slices.Clone(s.conv.Todos)
	k.pushing = true
	cmd := m.sendAIChat(text)
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

// aiStepLimitPush is the push after a turn that hit the step limit
// with no open todos.
const aiStepLimitPush = "Continue: the step limit paused your last turn before it was finished. Go on from where you stopped."

// aiKeepGoingChip is the "auto" chip of a pushed message.
func (s *aiState) aiKeepGoingChip(push int) string {
	return fmt.Sprintf("↻ auto · keep going %d/%d", push, s.settings.KeepGoingLimit())
}

// aiAsksTheUser is whether the last answer of the chat ends with a
// question (Latin or Arabic-script question mark, after any closing
// markdown or quotes): the user is to answer it, not a push.
func aiAsksTheUser(msgs []ai.StoredMessage) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		switch msgs[i].Role {
		case ai.StoredAssistant:
			t := strings.TrimRight(msgs[i].Text, " \t\r\n*_`\"')»”’")
			return strings.HasSuffix(t, "?") || strings.HasSuffix(t, "؟")
		case ai.StoredUser:
			return false
		}
	}
	return false
}

// aiNote adds a display-only line to the chat and saves it.
func (m *Model) aiNote(text string) {
	s := m.aiS()
	s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredNote, Text: text})
	if err := s.store.Save(&s.conv); err != nil {
		recordError("[ai] save chat: " + err.Error())
	}
}
