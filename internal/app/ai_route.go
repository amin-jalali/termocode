package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/keymap"
)

// handleAIMsg routes the Group A async messages (called early in
// updateInner so streaming continues under any overlay).
func (m *Model) handleAIMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case aiInlineTriggerMsg:
		if !msg.manual && !m.aiInlineEnabled() {
			return nil, true
		}
		return m.startAIInline(msg.manual), true
	case aiInlineResultMsg:
		return m.onAIInlineResult(msg), true
	case aiChatEventMsg:
		return m.onAIChatEvent(msg), true
	case aiChatConfirmMsg:
		return m.onAIChatConfirm(msg), true
	case aiChatDoneMsg:
		return m.onAIChatDone(msg), true
	case aiActionResultMsg:
		return m.onAIActionResult(msg), true
	case aiOAuthDoneMsg:
		return m.onAIOAuthDone(msg), true
	}
	return nil, false
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
