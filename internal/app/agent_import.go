package app

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Agent: Import Session into AI Chat (CMP-87) ─────────────────────────
//
// The session in the agent chat view (Agent: Sessions… → reads one, or a
// live Claude Code / Codex tab's chat view) becomes a new termocode AI
// chat in Chat History: its prompts and answers, its tool calls as one
// line each, no tool results. A card asks first (Cancel first). The
// session file is only read; the chat is a copy, so the built-in AI can
// go on from it. i in a read-only session view opens the same card.

const (
	confirmKindAgentImport confirmKindEnum = 3748
	agentImportPaletteID                   = "agent-import-session"
)

// openAgentImport shows the import card of the open session view.
func (m *Model) openAgentImport() tea.Cmd {
	c := m.agentChat
	if c == nil || !c.loaded || c.err != nil || c.missing {
		return m.pushToastDetail(toast.Info, "No session to import", "Agent: Sessions…, → reads one, i imports it")
	}
	title := c.title
	if a, ok := agentCLIAgent(c.cli); ok && title == "" {
		title = a.Title
	}
	conv := agentlog.ImportConversation(c.sess, title)
	if len(conv.Messages) == 0 {
		return m.pushToastDetail(toast.Info, "Nothing to import", "this session has no prompt or answer yet")
	}
	m.agentImportPending = &conv
	n := conv.ImportedFrom.Messages
	body := "Copies " + strconv.Itoa(n) + " messages of this " + title + " session into a new AI chat:\n\n" +
		"    " + conv.Title +
		"\n\nPrompts and answers are copied, tool calls as one line each; tool results are not. " +
		"Secrets are hidden. The session file is not changed; the new chat is in Chat History."
	if c.sess.Truncated {
		body += "\nOnly the newest part of this long session was read."
	}
	m.confirm = confirm.Risky("Import into an AI chat?", body,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "import", Title: "Import", Style: confirm.StylePrimary}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentImport
	return nil
}

// answerAgentImport runs the card's answer: Import saves the chat and
// opens it; Cancel and Esc do nothing.
func (m *Model) answerAgentImport(id string) tea.Cmd {
	conv := m.agentImportPending
	m.agentImportPending = nil
	if id != "import" || conv == nil {
		return nil
	}
	s := m.aiS()
	if s.streaming || s.chatCancel != nil {
		return m.aiInfo("Stop the AI reply first (Esc), then import again")
	}
	if err := s.store.Save(conv); err != nil {
		return m.aiToastErr("Could not save the imported chat", err)
	}
	s.chatGen++
	s.run = nil
	s.conv = *conv
	s.input, s.caret = nil, 0
	s.attachments = nil
	s.scroll = 0
	s.restored = true
	s.sel.clear()
	m.persistAIState()
	open := m.openAIPanel(true)
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, "Imported into an AI chat", "type to go on with termocode's AI")
	return tea.Batch(open, c)
}

func init() {
	aiActionPolicies[agentImportPaletteID] = aiNever("copies an outside agent's own session file; only the user does it (owner decision D1)")
}
