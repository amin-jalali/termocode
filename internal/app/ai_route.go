package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/keymap"
)

// handleAIMsg routes the Group A async messages (called early in
// updateInner so streaming continues under any overlay).
func (m *Model) handleAIMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case aiInlineTriggerMsg:
		if !msg.manual && !m.aiInlineEnabled() {
			return nil, true
		}
		return m.startAIInline(msg.manual, msg.settled), true
	case aiInlineResultMsg:
		return m.onAIInlineResult(msg), true
	case aiNESTriggerMsg: // ai_nextedit.go
		return m.startAINES(), true
	case aiNESResultMsg:
		return m.onAINESResult(msg), true
	case aiChatEventMsg:
		return m.onAIChatEvent(msg), true
	case aiRunEventMsg:
		return m.onAIRunEvent(msg), true
	case aiWorktreeMsg: // ai_worktrees.go
		return m.onAIWorktree(msg), true
	case aiWorktreeReviewMsg: // ai_worktree_actions.go
		return m.onAIWorktreeReview(msg), true
	case aiWorktreeMergedMsg:
		return m.onAIWorktreeMerged(msg), true
	case aiWorktreeDiscardedMsg:
		return m.onAIWorktreeDiscarded(msg), true
	case aiWorktreesListMsg: // ai_worktree_list.go
		return m.onAIWorktreesList(msg), true
	case aiLimitResumeMsg: // ai_cli_steps.go
		return m.onAILimitResume(msg), true
	case aiCLIStatusMsg: // ai_cli_form.go
		return m.onAICLIStatus(msg), true
	case aiChatConfirmMsg:
		return m.onAIChatConfirm(msg), true
	case aiIDECallMsg:
		return m.onAIIDECall(msg), true
	case aiMCPTrustMsg:
		return m.onAIMCPTrust(msg), true
	case mcpConnectDoneMsg:
		return m.onMCPConnectDone(msg), true
	case mcpCatalogLoadedMsg: // mcp_catalog.go
		return m.onMCPCatalogLoaded(msg), true
	case aiChatDoneMsg:
		return m.onAIChatDone(msg), true
	case aiMemoryDoneMsg: // ai_memory_extract.go
		return m.onAIMemoryDone(msg), true
	case aiActivityTickMsg:
		return m.onAIActivityTick(), true
	case aiAgentsSnapMsg:
		return m.onAIAgentsSnap(msg), true
	case aiActionResultMsg:
		return m.onAIActionResult(msg), true
	case diffview.FinishMsg:
		return m.onAIReviewFinish(msg), true
	case diffview.HideMsg:
		m.reviewOpen = false
		if m.ai != nil && m.ai.review != nil && !m.ai.review.card.Review() {
			m.ai.review = nil // a read-only compare card is done
		}
		return nil, true
	case compareReadyMsg:
		return m.openCompareCard(msg), true
	case aiOAuthDoneMsg:
		return m.onAIOAuthDone(msg), true
	}
	if cmd, ok := m.handleAddAIMsg(msg); ok { // ai_add.go
		return cmd, true
	}
	if cmd, ok := m.handleItermocProvMsg(msg); ok { // itermoc_providers.go
		return cmd, true
	}
	if cmd, ok := m.handleAIProfileFormMsg(msg); ok { // ai_profile_form.go
		return cmd, true
	}
	return m.handleMCPMsg(msg)
}

// editorHasKeys reports whether typed keys go to the editor: focus is on
// the editor, not in the integrated terminal and not in the AI chat. The
// active tab's accent is lit only then.
func (m Model) editorHasKeys() bool {
	if m.focus != FocusEditor || m.inTerminal {
		return false
	}
	return m.ai == nil || !m.ai.focused || !m.actionsPanelVisible()
}

// aiFocusSwap puts the chat panel into the F6 cycle (explorer → editor →
// AI panel → explorer). ok=false: let the default swap run.
func (m *Model) aiFocusSwap() bool {
	s := m.aiS()
	if s.focused {
		s.focused = false
		m.focus = FocusExplorer
		if !m.showExp {
			m.focus = FocusEditor
		}
		return true
	}
	if m.focus == FocusEditor && m.actionsPanelVisible() {
		s.focused = true
		return true
	}
	return false
}

// aiActionForKey maps the Group A keymap actions to palette IDs.
func aiActionForKey(a keymap.Action) string {
	switch a {
	case keymap.ActionAITriggerInline:
		return "ai-trigger-inline"
	case keymap.ActionAIEditSelection:
		return "ai-edit"
	case keymap.ActionAIToggleInline:
		return "ai-toggle-inline"
	case keymap.ActionAINextProfile:
		return "ai-profile-next"
	case keymap.ActionAIToggleAgents:
		return "ai-agents"
	}
	return ""
}

// aiOverflowMenuItems adds AI rows to the ⋮ menu (Group A).
func aiOverflowMenuItems(hints map[keymap.Action]string) []overflowMenuItem {
	run := func(id string) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			cmd, _ := m.dispatchAIPalette(id)
			return cmd
		}
	}
	hasFile := func(c overflowMenuContext) bool { return !c.InTerminal && c.EditorPath != "" }
	return []overflowMenuItem{
		{label: "✦ AI Chat", group: groupGeneral, shortcut: hints[keymap.ActionToggleActionsPanel], action: run("ai-open-chat")},
		{label: "✦ Edit with AI…", group: groupContext, shortcut: hints[keymap.ActionAIEditSelection], visible: hasFile, action: run("ai-edit")},
		{label: "✦ Explain with AI", group: groupContext, visible: hasFile, action: run("ai-explain")},
	}
}
