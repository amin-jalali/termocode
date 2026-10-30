package app

import (
	"os"
	"path/filepath"
	"testing"
)

// Bug (rule AH-1): "Undo AI changes?" overwrites the user's later edits,
// but its first, focused button was Restore, so Enter alone threw the
// edits away. Cancel must come first and be focused; Restore stays red.
func TestCovAIUndoTurnCardIsSafeFirst(t *testing.T) {
	m, dir, _ := undoTurnModel(t)
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("later\n"), 0o644)
	m.aiUndoTurn(-1)
	checkCardRule(t, "undo turn", *m, true)
	if got := m.confirm.Buttons()[0].ID; got != "cancel" {
		t.Errorf("first button %q, want cancel", got)
	}
}
