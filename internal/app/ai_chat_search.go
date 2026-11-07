package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// ── AI: Search Chat History… (CMP-30) ───────────────────────────────────
//
// Full-text search over the saved AI chats of this computer: the words
// are looked for (any case) in each chat's title and messages, newest
// chat first, and each hit shows its matching line with secrets hidden.
// The rows are AI Chat History rows (the id is the chat id), so Enter
// opens the chat as there. The search reads files only and runs off the
// UI thread. The AI has chats_search (ai_session_tools.go) instead.

const (
	aiHistorySearchID                     = "ai-history-search"
	promptKindAIChatSearch promptKindEnum = 30301
	aiChatSearchMax                       = 50 // chats listed for one search
)

// openAIChatSearch asks for the words.
func (m *Model) openAIChatSearch() tea.Cmd {
	m.prompt = prompt.New(" Search AI chats ", "Words to find in your saved AI chats:", "")
	m.prompt.SetNote("Searched here, on this computer; secrets are hidden in the results.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindAIChatSearch
	return nil
}

// aiChatHit is one chat that has the words.
type aiChatHit struct {
	id, title, line string
	updatedMs       int64
}

// onAIChatSearch lists the chats that have q.
func (m *Model) onAIChatSearch(q string) tea.Cmd {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	m.aiChatHits, m.aiChatHitsCapped = nil, false
	m.picker = picker.NewItems(" AI chats: "+q+" ", []picker.Item{{Header: true, Title: "Searching…", Group: "x"}})
	m.picker.SetNote([]string{"Enter opens the chat"})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIHistory
	store := m.aiS().store
	return func() tea.Msg {
		var hits []aiChatHit
		capped := false
		for _, c := range store.List() {
			line, ok := aiChatMatch(c, q)
			if !ok {
				continue
			}
			if len(hits) == aiChatSearchMax {
				capped = true
				break
			}
			hits = append(hits, aiChatHit{id: c.ID, title: ai.Redact(ai.RedactSecrets(aiOneLine(c.Title))),
				line: ai.Redact(ai.RedactSecrets(line)), updatedMs: c.UpdatedAtMs})
		}
		return applyMsg(func(m *Model) tea.Cmd {
			m.aiChatHits, m.aiChatHitsCapped = hits, capped
			if m.pickerOpen && m.pickerKind == pickerKindAIHistory {
				m.picker.SetItems(m.aiChatSearchItems())
			}
			return nil
		})
	}
}

// aiChatSearchItems are the hit rows (ids are chat ids).
func (m *Model) aiChatSearchItems() []picker.Item {
	if len(m.aiChatHits) == 0 {
		return []picker.Item{{Header: true, Title: "No saved chat has these words", Group: "x"}}
	}
	out := make([]picker.Item, 0, len(m.aiChatHits)+1)
	for _, h := range m.aiChatHits {
		out = append(out, picker.Item{ID: h.id, Title: h.title, Hint: aiAge(h.updatedMs) + " · " + h.line})
	}
	if m.aiChatHitsCapped {
		out = append(out, picker.Item{Header: true, Title: "Showing the newest 50 chats: use more words", Group: "x"})
	}
	return out
}

func init() {
	aiActionPolicies[aiHistorySearchID] = aiNever(aiNoPicker + "; the AI has chats_search over the same chats")
}
