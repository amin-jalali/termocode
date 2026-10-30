package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Chat history management (2026-10-15, "how do I manage my history?"):
// AI: Delete Chat…, AI: Rename Chat… and AI: Clear Chat History.

const (
	pickerKindAIHistoryDelete pickerKindEnum = 970 + iota
	pickerKindAIHistoryRename
)

const promptKindAIHistoryRename promptKindEnum = 970

const (
	confirmKindAIHistoryDelete confirmKindEnum = 970 + iota
	confirmKindAIHistoryClear
)

// openAIHistoryFor opens the chat list for an action (delete / rename).
func (m *Model) openAIHistoryFor(kind pickerKindEnum, title string) tea.Cmd {
	list := m.aiS().store.List()
	if len(list) == 0 {
		return m.aiInfo("No saved AI chats yet")
	}
	items := make([]picker.Item, 0, len(list))
	for _, c := range list {
		items = append(items, picker.Item{ID: c.ID, Title: c.Title, Hint: aiAge(c.UpdatedAtMs)})
	}
	m.picker = picker.NewItems(title, items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = kind
	return nil
}

// onAIHistoryDeletePicked asks before deleting the chat.
func (m *Model) onAIHistoryDeletePicked(id string) tea.Cmd {
	s := m.aiS()
	c, err := s.store.Load(id)
	if err != nil {
		return m.aiToastErr("Could not open chat", err)
	}
	s.historyTarget = id
	m.confirm = confirm.Risky("Delete chat?", "Delete \""+c.Title+"\"? This cannot be undone.",
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "delete", Title: "Delete", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIHistoryDelete
	return nil
}

// onAIHistoryDeleteConfirm deletes the chat chosen above.
func (m *Model) onAIHistoryDeleteConfirm(id string) tea.Cmd {
	s := m.aiS()
	target := s.historyTarget
	s.historyTarget = ""
	if id != "delete" || target == "" {
		return nil
	}
	if err := s.store.Delete(target); err != nil {
		return m.aiToastErr("Could not delete chat", err)
	}
	m.dropOpenChatIf(func(c string) bool { return c == target })
	var cmd tea.Cmd
	m.toast, cmd = m.toast.Push(toast.Info, "Chat deleted")
	return cmd
}

// confirmAIHistoryClear asks before deleting every saved chat.
func (m *Model) confirmAIHistoryClear() tea.Cmd {
	n := len(m.aiS().store.List())
	if n == 0 {
		return m.aiInfo("No saved AI chats yet")
	}
	m.confirm = confirm.Risky("Clear chat history?", "Delete all "+itoa(n)+" saved chats? This cannot be undone.",
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "clear", Title: "Delete All", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIHistoryClear
	return nil
}

// onAIHistoryClearConfirm deletes every saved chat.
func (m *Model) onAIHistoryClearConfirm(id string) tea.Cmd {
	if id != "clear" {
		return nil
	}
	s := m.aiS()
	n := 0
	for _, c := range s.store.List() {
		if err := s.store.Delete(c.ID); err == nil {
			n++
		}
	}
	m.dropOpenChatIf(func(string) bool { return true })
	var cmd tea.Cmd
	m.toast, cmd = m.toast.Push(toast.Info, "Deleted "+itoa(n)+" chat"+plural(n))
	return cmd
}

// dropOpenChatIf replaces the open chat with a new one when drop(id) says
// its saved copy is gone, so the next save does not bring it back.
func (m *Model) dropOpenChatIf(drop func(id string) bool) {
	s := m.aiS()
	if s.conv.ID == "" || !drop(s.conv.ID) {
		return
	}
	if s.chatCancel != nil {
		s.chatCancel()
		m.cancelAIReviewTurn()
	}
	s.chatGen++
	s.streaming = false
	s.conv = ai.NewConversation()
	s.attachments = nil
	s.scroll = 0
	m.persistAIState()
}

// onAIHistoryRenamePicked asks for the new title.
func (m *Model) onAIHistoryRenamePicked(id string) tea.Cmd {
	s := m.aiS()
	c, err := s.store.Load(id)
	if err != nil {
		return m.aiToastErr("Could not open chat", err)
	}
	s.historyTarget = id
	m.prompt = prompt.New("Rename chat", "New title:", c.Title)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindAIHistoryRename
	return nil
}

// onAIHistoryRenamed saves the new title (the open chat too, when it is
// the one renamed).
func (m *Model) onAIHistoryRenamed(title string) tea.Cmd {
	s := m.aiS()
	target := s.historyTarget
	s.historyTarget = ""
	if target == "" || title == "" {
		return nil
	}
	c, err := s.store.Load(target)
	if err != nil {
		return m.aiToastErr("Could not open chat", err)
	}
	c.Title = title
	if err := s.store.Save(&c); err != nil {
		return m.aiToastErr("Could not rename chat", err)
	}
	if s.conv.ID == target {
		s.conv.Title = title
	}
	var cmd tea.Cmd
	m.toast, cmd = m.toast.Push(toast.Info, "Chat renamed")
	return cmd
}
