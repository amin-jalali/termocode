package app

import (
	"os"
	"path/filepath"
	"testing"
)

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

// Bug (rule AH-1): "Overwrite?" (Save code block as… on a file that
// exists) had Overwrite first and focused, so Enter alone replaced the
// file.
func TestCovAIOverwriteCardIsSafeFirst(t *testing.T) {
	m := aiTestModel(t)
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.aiS().saveCode = "package other\n"
	m.onAISaveAsPath("main.go")
	checkCardRule(t, "overwrite", m, true)
	if got := m.confirm.Buttons()[0].ID; got != "cancel" {
		t.Errorf("first button %q, want cancel", got)
	}
}
