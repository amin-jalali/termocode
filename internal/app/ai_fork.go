package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// G-FORK: fork the chat from one message. The fork opens as the chat
// in the panel; the original stays saved as it was (Chat History has
// both). Entry points: AI: Fork Chat from Message… (palette), Alt+K in
// the focused chat (both open a message picker) and "Fork Chat from
// Here" in the transcript's right-click menu.

const pickerKindAIFork pickerKindEnum = 975

// aiForkLimit is the last message a fork may start at: while a reply
// streams, the running turn's question (its answer is not finished).
func aiForkLimit(s *aiState) int {
	last := len(s.conv.Messages) - 1
	if s.streaming && s.turnMsg >= 0 && s.turnMsg < last {
		return s.turnMsg
	}
	return last
}

// aiForkItems are the picker rows: questions and answers, newest first.
func aiForkItems(s *aiState) []picker.Item {
	limit := aiForkLimit(s)
	num := 0
	var items []picker.Item
	for i, msg := range s.conv.Messages {
		if msg.Role != ai.StoredUser && msg.Role != ai.StoredAssistant {
			continue
		}
		num++
		if i > limit {
			break
		}
		who := "AI: "
		if msg.Role == ai.StoredUser {
			who = "You: "
		}
		line := strings.TrimSpace(msg.Text)
		if msg.Run != nil && line == "" {
			line = "run (" + msg.Run.State + ")"
		}
		line = strings.TrimSpace(strings.SplitN(line, "\n", 2)[0])
		if r := []rune(line); len(r) > 60 {
			line = string(r[:60]) + "…"
		}
		items = append(items, picker.Item{ID: strconv.Itoa(i), Title: who + line, Hint: "message " + strconv.Itoa(num)})
	}
	for a, b := 0, len(items)-1; a < b; a, b = a+1, b-1 {
		items[a], items[b] = items[b], items[a]
	}
	return items
}

// openAIForkPicker asks which message the fork starts at.
func (m *Model) openAIForkPicker() tea.Cmd {
	items := aiForkItems(m.aiS())
	if len(items) == 0 {
		return m.aiInfo("Nothing to fork yet: send a message first")
	}
	m.picker = picker.NewItems(" Fork Chat from Message ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIFork
	return nil
}

// onAIForkPicked forks at the picked message index.
func (m *Model) onAIForkPicked(id string) tea.Cmd {
	i, err := strconv.Atoi(id)
	if err != nil {
		return nil
	}
	return m.aiForkChat(i)
}

// aiForkChat makes a new chat from the open one up to message i and
// opens it. A running reply is stopped first and the original is saved
// with its turn so far (like Ctrl+C); the fork starts at the last
// finished message.
func (m *Model) aiForkChat(i int) tea.Cmd {
	s := m.aiS()
	if len(s.conv.Messages) == 0 {
		return m.aiInfo("Nothing to fork yet: send a message first")
	}
	if limit := aiForkLimit(s); i > limit {
		i = limit
	}
	fork, input, ok := ai.ForkConversation(s.conv, i)
	if !ok {
		return m.aiInfo("Pick a message to fork from")
	}
	aiMarkInterrupted(&fork) // no running run or pending step is copied
	if s.streaming || s.chatCancel != nil {
		if s.chatCancel != nil {
			s.chatCancel()
			m.cancelAIReviewTurn()
		}
		orig := s.conv
		if n := len(orig.Messages); n > 0 && orig.Messages[n-1].Role == ai.StoredAssistant && orig.Messages[n-1].Run == nil && strings.TrimSpace(orig.Messages[n-1].Text) == "" {
			orig.Messages = orig.Messages[:n-1]
		}
		aiMarkInterrupted(&orig)
		if err := s.store.Save(&orig); err != nil {
			recordError("[ai] save chat before fork: " + err.Error())
		}
	}
	s.chatGen++
	s.streaming = false
	s.chatCancel = nil
	s.run = nil
	s.conv = fork
	s.input = []rune(input)
	s.caret = len(s.input)
	s.attachments = nil
	s.scroll = 0
	s.restored = true
	s.sel.clear()
	if err := s.store.Save(&s.conv); err != nil {
		return m.aiToastErr("Could not save the forked chat", err)
	}
	m.persistAIState()
	open := m.openAIPanel(true)
	msg := "Forked at message " + strconv.Itoa(fork.ForkedFrom.Message) + ": the original chat is kept in Chat History"
	if input != "" {
		msg = "Forked at message " + strconv.Itoa(fork.ForkedFrom.Message) + ": edit the question and press Enter"
	}
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, msg)
	return tea.Batch(open, c)
}
