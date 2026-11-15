package app

import (
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The built-in chat keeps what its tools gave across turns. A turn's
// provider messages (the assistant messages with their tool calls, the
// tool results) are stored on its user message (StoredMessage.Native),
// redacted and cut; the next turns rebuild them as native messages for
// the last ai_tool_turns turns. Older turns, turns of another provider
// and chats saved before keep the one-line "[tools I ran]" rows.

// aiNativeResultBytes caps one stored tool result, aiNativeTurnBytes all
// of a turn's (each result is then cut to its share).
const (
	aiNativeResultBytes = 16 << 10
	aiNativeTurnBytes   = 96 << 10
	aiNativeResultMin   = 1 << 10
)

// aiNativeFor names the message form of a provider: its kind, and
// " tools" for native tool calls (the text protocol sends results as
// user messages).
func aiNativeFor(kind string, native bool) string {
	if native {
		return kind + " tools"
	}
	return kind
}

// aiStoreTurn is the stored form of a turn's provider messages (what
// agent.Run gave back): secrets redacted, tool results cut. It ends with
// an answer: a turn that stopped after its tools gets the chat's last
// text of the turn (end: the step limit note), else a short line. nil:
// nothing to keep.
func aiStoreTurn(msgs []ai.Message, end string) []ai.StoredNative {
	if len(msgs) == 0 {
		return nil
	}
	results, size := 0, 0
	for _, m := range msgs {
		if m.Role == ai.RoleTool {
			results++
			size += min(len(m.Content), aiNativeResultBytes)
		}
	}
	limit := aiNativeResultBytes
	if size > aiNativeTurnBytes && results > 0 {
		limit = max(aiNativeTurnBytes/results, aiNativeResultMin)
	}
	out := make([]ai.StoredNative, 0, len(msgs)+1)
	for _, m := range msgs {
		sn := ai.StoredNative{Role: string(m.Role), Content: ai.Redact(m.Content), CallID: m.ToolCallID, Tool: m.ToolName}
		if m.Role == ai.RoleTool && len(sn.Content) > limit {
			more := len(sn.Content) - limit
			sn.Content = aiCutBytes(sn.Content, limit) + "\n[cut: " + strconv.Itoa(more) + " more bytes; run the tool again for the rest]"
		}
		for _, c := range m.ToolCalls {
			sn.Calls = append(sn.Calls, ai.StoredCall{ID: c.ID, Name: c.Name, Args: aiRedactArgs(c.Args)})
		}
		if m.Role == ai.RoleAssistant && len(sn.Calls) == 0 && strings.TrimSpace(sn.Content) == "" {
			sn.Content = "(no answer)" // a provider refuses an empty message
		}
		out = append(out, sn)
	}
	if last := out[len(out)-1]; last.Role != string(ai.RoleAssistant) || len(last.Calls) > 0 {
		if end = strings.TrimSpace(end); end == "" {
			end = "(I stopped here, before a final answer.)"
		}
		out = append(out, ai.StoredNative{Role: string(ai.RoleAssistant), Content: ai.Redact(end)})
	}
	return out
}

// aiRedactArgs is args with secrets in its string values redacted.
func aiRedactArgs(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok {
			v = ai.Redact(s)
		}
		out[k] = v
	}
	return out
}

// aiNativeMessages are stored provider messages as provider messages.
func aiNativeMessages(sns []ai.StoredNative) []ai.Message {
	out := make([]ai.Message, 0, len(sns))
	for _, sn := range sns {
		m := ai.Message{Role: ai.Role(sn.Role), Content: sn.Content, ToolCallID: sn.CallID, ToolName: sn.Tool}
		for _, c := range sn.Calls {
			m.ToolCalls = append(m.ToolCalls, ai.ToolCall{ID: c.ID, Name: c.Name, Args: c.Args})
		}
		out = append(out, m)
	}
	return out
}

// aiChatTurnHistory is the history of a built-in chat turn (every stored
// message): the last ai_tool_turns turns stored for this provider form
// (kind, native) come back with their tool calls and results.
func (m *Model) aiChatTurnHistory(system string, native bool, kind string) []ai.Message {
	s := m.aiS()
	keep := aiNativeTurns(s.conv.Messages, s.settings.ToolTurnsKept())
	form := aiNativeFor(kind, native)
	return m.aiChatHistoryWith(system, len(s.conv.Messages), func(i int) []ai.Message {
		sm := s.conv.Messages[i]
		if !keep[i] || sm.NativeFor != form {
			return nil
		}
		return aiNativeMessages(sm.Native)
	})
}

// aiNativeTurns are the indexes of the last k user messages that have
// stored provider messages.
func aiNativeTurns(msgs []ai.StoredMessage, k int) map[int]bool {
	keep := map[int]bool{}
	for i := len(msgs) - 1; i >= 0 && len(keep) < k; i-- {
		if msgs[i].Role == ai.StoredUser && len(msgs[i].Native) > 0 {
			keep[i] = true
		}
	}
	return keep
}

// aiKeepTurn stores turn (agent.Run's messages, in form) on user message
// i (when it is still the message text) and drops the stored messages of turns older than the last k, so
// the chat file stays small.
func aiKeepTurn(msgs []ai.StoredMessage, i int, text string, turn []ai.Message, form string, k int) {
	if i < 0 || i >= len(msgs) || msgs[i].Role != ai.StoredUser || msgs[i].Text != text {
		return
	}
	if k > 0 {
		end := ""
		for j := i + 1; j < len(msgs) && msgs[j].Role != ai.StoredUser; j++ {
			if msgs[j].Role == ai.StoredAssistant && strings.TrimSpace(msgs[j].Text) != "" {
				end = msgs[j].Text
			}
		}
		msgs[i].Native, msgs[i].NativeFor = aiStoreTurn(turn, end), form
	}
	keep := aiNativeTurns(msgs, k)
	for j := range msgs {
		if len(msgs[j].Native) > 0 && !keep[j] {
			msgs[j].Native, msgs[j].NativeFor = nil, ""
		}
	}
}
