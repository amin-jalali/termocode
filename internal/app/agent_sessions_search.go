package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// ── Search inside sessions (AGX-10) ─────────────────────────────────────
//
// / in Agent: Sessions… (nothing typed yet) or Agent: Search Sessions…
// asks for words and searches this project's session files locally (Go,
// capped: 200 files, 50 MB, the first 3 hits per file). The text searched
// is already redacted. Owner decision D1b (default no): the AI gets no
// tool over these files.

const (
	pickerKindAgentSessionsHits   pickerKindEnum = 3742
	promptKindAgentSessionsSearch promptKindEnum = 3743
	agentSessionsSearchID                        = "agent-sessions-search"
	agentSearchPfx                               = "hit:"
)

// agentSessionsPickerKey is / in Agent: Sessions… with an empty filter.
func (m *Model) agentSessionsPickerKey(k tea.KeyMsg) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindAgentSessions || k.Type != tea.KeyRunes || string(k.Runes) != "/" || m.picker.Query() != "" {
		return nil, false
	}
	m.pickerOpen = false
	return m.openAgentSessionsSearch(), true
}

// openAgentSessionsSearch asks for the words (asking for the setting
// first while it is off).
func (m *Model) openAgentSessionsSearch() tea.Cmd {
	if !agentTranscriptsOn() {
		m.agentChatPendingBuf = agentChatPendingSessions
		m.openAgentTranscriptsCard("Claude Code and Codex", "~/.claude and ~/.codex")
		return nil
	}
	m.prompt = prompt.New(" Search agent sessions ", "Words to find in this project's Claude Code and Codex sessions:", "")
	m.prompt.SetNote("Searched here, on this computer; secrets are hidden first.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindAgentSessionsSearch
	return nil
}

// onAgentSessionsSearch runs the search off the UI thread and lists the
// hits.
func (m *Model) onAgentSessionsSearch(q string) tea.Cmd {
	q = strings.TrimSpace(q)
	if q == "" || !agentTranscriptsOn() {
		return nil
	}
	m.picker = picker.NewItems(" Sessions: "+q+" ", []picker.Item{{Header: true, Title: "Searching…", Group: "x"}})
	m.picker.SetNote([]string{"Enter reads the session in the chat view"})
	m.picker.SetWidth(agentSessionsWidth)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAgentSessionsHits
	if m.agentSessIndex == nil {
		m.agentSessIndex = agentlog.NewIndex()
	}
	x, root, homes := m.agentSessIndex, agentSessionsRoot(), agentlogHomes()
	return func() tea.Msg {
		hits, capped := agentlog.Search(x.Build(homes, root), q, agentlog.DefaultSearchLimits)
		return applyMsg(func(m *Model) tea.Cmd {
			m.agentHits, m.agentHitsCapped = hits, capped
			if m.pickerOpen && m.pickerKind == pickerKindAgentSessionsHits {
				m.picker.SetItems(m.agentSearchItems())
			}
			return nil
		})
	}
}

// agentSearchItems are the hit rows.
func (m *Model) agentSearchItems() []picker.Item {
	if len(m.agentHits) == 0 {
		return []picker.Item{{Header: true, Title: "No session has these words", Group: "x"}}
	}
	out := make([]picker.Item, 0, len(m.agentHits)+1)
	for i, h := range m.agentHits {
		name := string(h.Session.CLI)
		if a, ok := agentCLIAgent(h.Session.CLI); ok {
			name = a.Title
		}
		hint := h.Time.Local().Format("Jan 2 15:04") + " · " + name
		if fp := h.Session.FirstPrompt; fp != "" && fp != h.Snippet {
			hint += " · " + fp
		}
		out = append(out, picker.Item{ID: agentSearchPfx + strconv.Itoa(i), Title: h.Snippet, Hint: hint})
	}
	if m.agentHitsCapped {
		out = append(out, picker.Item{Header: true, Title: "Stopped at 200 sessions / 50 MB: use more words", Group: "x"})
	}
	return out
}

// onAgentSearchPicked reads the hit's session in the chat view.
func (m *Model) onAgentSearchPicked(id string) tea.Cmd {
	n, err := strconv.Atoi(strings.TrimPrefix(id, agentSearchPfx))
	if err != nil || n < 0 || n >= len(m.agentHits) {
		return nil
	}
	s := m.agentHits[n].Session
	title := string(s.CLI)
	if a, ok := agentCLIAgent(s.CLI); ok {
		title = a.Title
	}
	return m.openAgentChatFile(s.CLI, s.Path, title)
}

func init() {
	aiActionPolicies[agentSessionsSearchID] = aiNever(aiNoPicker + "; owner decision D1b: the AI gets no search over the agents' session files")
}
