package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/ai"
)

// importModel is a model with an empty chat store and a read-only agent
// chat view of a two-turn Claude Code session.
func importModel(t *testing.T) (*Model, ai.ChatStore) {
	t.Helper()
	m := &Model{w: 120, h: 40}
	s := m.aiS()
	s.store = ai.ChatStore{Dir: t.TempDir()}
	m.agentChat = &agentChatState{readOnly: true, loaded: true, cli: agentlog.Claude, title: "Claude Code",
		sess: agentlog.Session{CLI: agentlog.Claude, ID: "0123456789abcdef", Turns: []agentlog.Turn{
			{Kind: agentlog.User, Text: "fix the parser test"},
			{Kind: agentlog.Assistant, Text: "Fixed it."},
		}}}
	return m, s.store
}

// Spec (CMP-87): Agent: Import Session into AI Chat is in the palette and
// the AI cannot run it (it copies an outside agent's own session file,
// owner decision D1).
func TestAgentImportPaletteAndPolicy(t *testing.T) {
	if p, ok := aiActionPolicyFor(agentImportPaletteID); !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v %v", p, ok)
	}
	found := false
	for _, it := range tasksPaletteItems() {
		if it.ID == agentImportPaletteID && it.Title == "Agent: Import Session into AI Chat" {
			found = true
		}
	}
	if !found {
		t.Error("no palette row")
	}
}

// Spec (CMP-87): the import asks first (Cancel first) and says what is
// copied; Cancel writes nothing; Import saves a new AI chat with the
// prompts and answers and opens it, and the session file is not touched.
func TestAgentImportSessionCard(t *testing.T) {
	m, store := importModel(t)
	m.dispatchTasksPalette(agentImportPaletteID)
	if !m.confirmOpen || m.confirmKind != confirmKindAgentImport || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no card: open %v kind %v", m.confirmOpen, m.confirmKind)
	}
	box := strings.Join(strings.Fields(ansi.Strip(m.confirm.Box())), " ")
	for _, want := range []string{"Claude Code", "2 messages", "not changed"} {
		if !strings.Contains(box, want) {
			t.Errorf("card has no %q: %s", want, box)
		}
	}
	m.answerAIRulesForTest("cancel")
	if n := len(store.List()); n != 0 {
		t.Fatalf("Cancel saved %d chats", n)
	}
	m.dispatchTasksPalette(agentImportPaletteID)
	m.answerAIRulesForTest("import")
	list := store.List()
	if len(list) != 1 {
		t.Fatalf("%d chats saved, want 1", len(list))
	}
	got, err := store.Load(list[0].ID)
	if err != nil || got.ImportedFrom == nil || got.ImportedFrom.Session != "0123456789abcdef" || len(got.Messages) != 2 {
		t.Fatalf("saved %+v %v", got, err)
	}
	if s := m.aiS(); s.conv.ID != got.ID || s.conv.Messages[1].Text != "Fixed it." {
		t.Errorf("open chat %q %+v", s.conv.ID, s.conv.Messages)
	}
}

// Spec (CMP-87): i in a read-only session view opens the same card; with
// no session view open (or an empty session) nothing is imported and a
// toast says what to do.
func TestAgentImportKeyAndNoSession(t *testing.T) {
	m, store := importModel(t)
	if _, ok := agentChatKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")}); !ok || !m.confirmOpen || m.confirmKind != confirmKindAgentImport {
		t.Fatalf("i: handled %v card %v kind %v", ok, m.confirmOpen, m.confirmKind)
	}
	m.confirmOpen = false
	m.agentChat = nil
	m.dispatchTasksPalette(agentImportPaletteID)
	if m.confirmOpen || len(store.List()) != 0 {
		t.Fatal("a card or a chat with no session view")
	}
	if got := ansi.Strip(strings.Join(m.toast.View(200, 40), " ")); !strings.Contains(got, "Agent: Sessions…") {
		t.Errorf("toast %q", got)
	}
	m2, store2 := importModel(t)
	m2.agentChat.sess.Turns = nil
	m2.dispatchTasksPalette(agentImportPaletteID)
	if m2.confirmOpen || len(store2.List()) != 0 {
		t.Fatal("an empty session opened a card")
	}
}
