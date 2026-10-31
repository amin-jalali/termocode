package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Reasoning level of the chat (OMX-5). The level a turn is sent with:
//
//   - the chat's own level (AI: Next Reasoning Level, or a click on the
//     header chip "think <level>"; stored with the chat), else the
//     slot's (ai_reasoning for the main slot, ai_models.<slot>.reasoning);
//   - raised for one turn by "think" (high) or "ultrathink" (max) in the
//     message. The keyword shows as a chip above the input before
//     sending; its × keeps the message text but drops the raise.
//
// Nothing is hidden: the chip shows what the next turn will use.

// aiReasoningAuto is a chat's stored level when the user picked the
// slot's own level on purpose (the header chip stays).
const aiReasoningAuto = "auto"

// chatReasoning is the chat's chosen level ("" none or auto) and whether
// the user picked one in this chat.
func (s *aiState) chatReasoning() (level string, chosen bool) {
	switch s.conv.Reasoning {
	case "":
		return "", false
	case aiReasoningAuto:
		return "", true
	}
	return ai.NormalizeReasoning(s.conv.Reasoning), true
}

// shownReasoning is the level the header chip shows: the chat's, else
// ai_reasoning ("" with chosen=false: no chip).
func (s *aiState) shownReasoning() (level string, show bool) {
	if l, chosen := s.chatReasoning(); chosen {
		return l, true
	}
	l := ai.NormalizeReasoning(s.settings.Reasoning)
	return l, l != ""
}

// aiReasoningChipLabel is the header chip ("" when no level is set).
func (m Model) aiReasoningChipLabel() string {
	if m.ai == nil {
		return ""
	}
	l, show := m.ai.shownReasoning()
	if !show {
		return ""
	}
	if l == "" {
		l = aiReasoningAuto
	}
	return "think " + l
}

// cycleAIReasoning is AI: Next Reasoning Level (and a click on the
// header chip): auto → low → … → max → auto, for this chat.
func (m *Model) cycleAIReasoning() tea.Cmd {
	s := m.aiS()
	cur, _ := s.shownReasoning()
	next := ai.NextReasoning(cur)
	if next == "" {
		s.conv.Reasoning = aiReasoningAuto
	} else {
		s.conv.Reasoning = next
	}
	if len(s.conv.Messages) > 0 {
		if err := s.store.Save(&s.conv); err != nil {
			recordError("[ai] save chat: " + err.Error())
		}
	}
	if next == "" {
		return m.aiInfo("Reasoning: the model's default in this chat")
	}
	return m.aiInfo("Reasoning: " + next + " in this chat (more thinking costs more tokens)")
}

// aiInputThink is the level the input's keyword asks for and the
// keyword ("" when none, or when its chip was removed).
func (s *aiState) aiInputThink() (level, word string) {
	if s.thinkOff {
		return "", ""
	}
	level = ai.ReasoningKeyword(string(s.input))
	switch level {
	case ai.ReasoningMax:
		return level, "ultrathink"
	case ai.ReasoningHigh:
		return level, "think"
	}
	return "", ""
}

// takeTurnKeyword keeps the keyword level of the message being sent
// (none when its chip was removed) for the turn or run it starts, and
// clears the removed-chip mark for the next message.
func (s *aiState) takeTurnKeyword(text string) {
	s.turnThink = ""
	if !s.thinkOff {
		s.turnThink = ai.ReasoningKeyword(text)
	}
	s.thinkOff = false
}

// reasoningFor is the level of a turn on slot name (config cfg): the
// chat's chosen level, else the slot's (ai_reasoning for main), raised
// to think (the message's keyword level).
func (s *aiState) reasoningFor(name string, cfg ai.Config, think string) string {
	return s.reasoningFunc(think)(name, cfg)
}

// reasoningFunc is reasoningFor with the chat's state read now: the
// steps of a run ask for their level from their own goroutines.
func (s *aiState) reasoningFunc(think string) func(name string, cfg ai.Config) string {
	chat, chosen := s.chatReasoning()
	main := ai.NormalizeReasoning(s.settings.Reasoning)
	return func(name string, cfg ai.Config) string {
		base := ai.NormalizeReasoning(cfg.Reasoning)
		if base == "" && (name == "" || name == ai.SlotMain) {
			base = main
		}
		if chosen {
			base = chat
		}
		return ai.MaxReasoning(base, think)
	}
}
