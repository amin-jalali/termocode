package app

import "testing"

// Bug (rule AH-1): "Delete chat?" had Delete first and focused, so
// Enter alone deleted the chat for good. Cancel must be first.
func TestCovAIDeleteChatCardIsSafeFirst(t *testing.T) {
	m, _ := historyModel(t)
	m.onAIHistoryDeletePicked("c1")
	checkCardRule(t, "delete chat", *m, true)
	if got := m.confirm.Buttons()[0].ID; got != "cancel" {
		t.Errorf("first button %q, want cancel", got)
	}
}

// Bug (rule AH-1): "Clear chat history?" had Delete All first and
// focused, so Enter alone deleted every saved chat.
func TestCovAIClearHistoryCardIsSafeFirst(t *testing.T) {
	m, _ := historyModel(t)
	m.confirmAIHistoryClear()
	checkCardRule(t, "clear history", *m, true)
	if got := m.confirm.Buttons()[0].ID; got != "cancel" {
		t.Errorf("first button %q, want cancel", got)
	}
}
