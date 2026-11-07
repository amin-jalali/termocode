package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (CMP-30): Agent: Search All Sessions… searches the agent sessions
// of every project, not only this one: from another folder the hits are
// still found, each row names its project folder, and Enter reads the
// session. The AI cannot open it.
func TestAgentSessionsSearchAll(t *testing.T) {
	m := sessionsTestModel(t)
	setAgentTranscripts(t, "read")
	agentSessionsRoot = func() string { return "/work/other" }
	m.dispatchTasksPalette(agentSessionsSearchID)
	m.promptOpen = false
	runApply(m, cmp30Cmd(m.handleAIPromptSubmit("queue")))
	if rows := agentPickRowsText(m.agentSearchItems()); !strings.Contains(rows, "No session has these words") {
		t.Fatalf("project search from another folder:\n%s", rows)
	}
	m.pickerOpen = false
	m.dispatchTasksPalette(agentSessionsSearchAllID)
	if !m.promptOpen || m.promptKind != promptKindAgentSessionsSearch || !m.agentSearchAll {
		t.Fatalf("palette: prompt %v kind %v all %v", m.promptOpen, m.promptKind, m.agentSearchAll)
	}
	m.promptOpen = false
	runApply(m, cmp30Cmd(m.handleAIPromptSubmit("queue")))
	rows := agentPickRowsText(m.agentSearchItems())
	if !strings.Contains(rows, "Fix the failing test in queue.go") || !strings.Contains(rows, "demo") {
		t.Fatalf("all hits:\n%s", rows)
	}
	m.handleAIPickerSelect(m.agentSearchItems()[0].ID)
	if c := m.agentChat; c == nil || !c.readOnly {
		t.Errorf("Enter on a hit: %+v", m.agentChat)
	}
	if p, ok := aiActionPolicyFor(agentSessionsSearchAllID); !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v %v", p, ok)
	}
	found := false
	for _, it := range tasksPaletteItems() {
		found = found || it.ID == agentSessionsSearchAllID && it.Title == "Agent: Search All Sessions…"
	}
	if !found {
		t.Error("not in the palette")
	}
}

// Spec (CMP-30): AI: Search Chat History… asks for words and lists the
// saved AI chats whose title or messages have them (any case), newest
// first, with the matching line redacted; Enter opens the chat. It needs
// no provider; the AI has chats_search instead of this picker.
func TestAIChatHistorySearch(t *testing.T) {
	mm := aiTestModel(t)
	m := &mm
	s := m.aiS()
	deploy := saveTestChat(t, s, "", ai.StoredMessage{Role: ai.StoredUser, Text: "how do we ship the api?"},
		ai.StoredMessage{Role: ai.StoredAssistant, Text: "Run make ship, key " + aiTestKey})
	saveTestChat(t, s, "", ai.StoredMessage{Role: ai.StoredUser, Text: "rename a var"})
	if _, ok := m.dispatchAIPalette(aiHistorySearchID); !ok || !m.promptOpen || m.promptKind != promptKindAIChatSearch {
		t.Fatalf("palette: ok %v prompt %v kind %v", ok, m.promptOpen, m.promptKind)
	}
	m.promptOpen = false
	runApply(m, cmp30Cmd(m.handleAIPromptSubmit("MAKE SHIP")))
	if !m.pickerOpen || m.pickerKind != pickerKindAIHistory {
		t.Fatalf("picker %v kind %v", m.pickerOpen, m.pickerKind)
	}
	rows := agentPickRowsText(m.aiChatSearchItems())
	if !strings.Contains(rows, deploy.ID+" | ") || strings.Contains(rows, "rename a var") || strings.Contains(rows, "0123456789abcdef") {
		t.Fatalf("hits:\n%s", rows)
	}
	m.handleAIPickerSelect(deploy.ID)
	if m.aiS().conv.ID != deploy.ID {
		t.Errorf("Enter opened %q, want %q", m.aiS().conv.ID, deploy.ID)
	}
	m.promptOpen = false
	runApply(m, cmp30Cmd(m.handleAIPromptSubmit("nothing-like-this")))
	if rows := agentPickRowsText(m.aiChatSearchItems()); !strings.Contains(rows, "No saved chat has these words") {
		t.Errorf("no hits:\n%s", rows)
	}
	if p, ok := aiActionPolicyFor(aiHistorySearchID); !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v %v", p, ok)
	}
}

// cmp30Cmd keeps the command of a (cmd, ok) pair.
func cmp30Cmd(c tea.Cmd, _ bool) tea.Cmd { return c }
