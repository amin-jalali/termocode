package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Context compaction in the chat (OMX-3). Near the model's window (or
// when the provider says the context is too long) the turns before the
// new user message are summarised by the quick model. The summary is
// kept on that user message (StoredMessage.Summary): the transcript
// still shows every old message, the model gets the summary instead.
// Rules, skills, memory and todos are in the system prompt, which is
// never summarised. ai_compact "off" turns it off.

// aiCompactAt is the share of the context window (in percent) at which
// a turn summarises the older messages before it is sent.
const aiCompactAt = 80

// aiCompactMinMessages is the least number of older messages worth a
// summary.
const aiCompactMinMessages = 2

// aiLastSummary is the index of the last user message of msgs that holds
// a summary (-1: none).
func aiLastSummary(msgs []ai.StoredMessage) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == ai.StoredUser && msgs[i].Summary != "" {
			return i
		}
	}
	return -1
}

// aiCompactor is the summary call of a chat turn: the quick slot, or
// the turn's own slot when there is no quick one. nil when ai_compact is
// off.
func (m *Model) aiCompactor(turn aiSlot) func(ctx context.Context, older []ai.Message) (string, error) {
	s := m.aiS()
	if s.settings.Compact == "off" {
		return nil
	}
	slot, err := s.resolveSlot(ai.SlotQuick)
	if err != nil {
		slot = turn
	}
	return func(ctx context.Context, older []ai.Message) (string, error) {
		req := ai.Request{Model: slot.cfg.Model, Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: ai.CompactSystemPrompt},
			{Role: ai.RoleUser, Content: "The earlier chat:\n\n" + ai.CompactTranscript(older)},
		}}
		text, last := ai.Collect(slot.p.Stream(ctx, req))
		if last.Err != nil {
			return "", last.Err
		}
		return strings.TrimSpace(ai.Redact(ai.RedactSecrets(text))), nil
	}
}

// aiCompactEarly summarises the messages before the turn's user message
// (the last one of history) when history fills aiCompactAt percent of
// window. It returns the history to send and the agent event to show
// (Kind 0: nothing was done).
func aiCompactEarly(ctx context.Context, history []ai.Message, window int, compact func(context.Context, []ai.Message) (string, error)) ([]ai.Message, ai.Event) {
	n := len(history)
	if compact == nil || window <= 0 || n < 2+aiCompactMinMessages || ai.EstimateTokens(history)*100 < window*aiCompactAt {
		return history, ai.Event{}
	}
	sum, err := compact(ctx, history[1:n-1])
	if err != nil || sum == "" {
		return history, ai.Event{}
	}
	return ai.CompactedHistory(history[0], sum, history[n-1:]), ai.Event{Kind: ai.EventCompacted, Detail: sum, Attempt: n - 2}
}

// aiMarkCompacted keeps a summary on the turn's user message.
func (s *aiState) aiMarkCompacted(summary string) {
	at := s.turnMsg
	if at < 0 || at >= len(s.conv.Messages) || s.conv.Messages[at].Role != ai.StoredUser {
		return
	}
	prev := max(aiLastSummary(s.conv.Messages[:at]), 0)
	s.conv.Messages[at].Summary = ai.Redact(summary)
	s.conv.Messages[at].Summarised = at - prev
}

// aiSummaryLine is the transcript marker above a user message whose
// earlier messages were summarised.
func aiSummaryLine(n, width int) aiLine {
	t := "⋯ Earlier chat summarised (" + strconv.Itoa(n) + " message" + plural(n) + ") to fit the model"
	if lim := width - aiIndent - 1; runewidth.StringWidth(t) > lim && lim > 3 {
		t = runewidth.Truncate(t, lim, "…")
	}
	return aiLine{text: strings.Repeat(" ", aiIndent) + t, style: aiStyle(theme.Fg(theme.TextDim).Italic(true))}
}
