package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Group R3: restore points of an agent turn.

// undoTurnModel runs one agent turn that writes a.txt (open in a tab) and
// creates new.txt, with fileWrite auto-approved (no review).
func undoTurnModel(t *testing.T) (*Model, string, *scriptedAIProvider) {
	t.Helper()
	two := ai.Chunk{ToolCalls: []ai.ToolCall{
		{ID: "c1", Name: "write_file", Args: map[string]any{"path": "a.txt", "content": "agent\n"}},
		{ID: "c2", Name: "create_file", Args: map[string]any{"path": "new.txt", "content": "fresh\n"}},
	}}
	p := &scriptedAIProvider{replies: []ai.Chunk{two}}
	m, _, dir := aiSyncModel(t, p, map[string]string{"a.txt": "mine\n"})
	m.openFileAtLine(filepath.Join(dir, "a.txt"), 1)
	driveAIChat(t, m, m.sendAIChat("change things"), nil)
	if readFile(filepath.Join(dir, "a.txt")) != "agent\n" || readFile(filepath.Join(dir, "new.txt")) != "fresh\n" {
		t.Fatalf("the turn did not write: %v", p.toolResults())
	}
	return m, dir, p
}

// Spec: AI: Undo Last Turn puts back every file of the turn: the written
// file gets its old text (the open tab too), the created file is gone,
// and the chat records it for the model.
func TestAIUndoLastTurnRestores(t *testing.T) {
	m, dir, _ := undoTurnModel(t)
	if n := m.aiRestoreFiles(0); n != 2 {
		t.Fatalf("restore point has %d files", n)
	}
	m.aiUndoTurn(-1)
	if m.confirmOpen {
		t.Fatal("asked although nothing changed after the turn")
	}
	if got := readFile(filepath.Join(dir, "a.txt")); got != "mine\n" {
		t.Errorf("a.txt %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err == nil {
		t.Error("new.txt still there")
	}
	if got := bufText(t, m.nvim, filepath.Join(dir, "a.txt")); got != "mine" {
		t.Errorf("tab %q", got)
	}
	last := m.ai.conv.Messages[len(m.ai.conv.Messages)-1]
	if last.Role != ai.StoredTool || !strings.Contains(last.Text, "Undo AI changes") || !strings.Contains(last.Text, "a.txt") {
		t.Errorf("chat line %+v", last)
	}
	if m.aiRestoreFiles(0) != 0 || m.aiLastRestoreTurn() != -1 {
		t.Error("the restore point was not dropped")
	}
}

// Spec: when a file changed after the turn, Undo asks first; Cancel keeps
// everything, Restore puts the old text back.
func TestAIUndoTurnAsksWhenChanged(t *testing.T) {
	m, dir, _ := undoTurnModel(t)
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("later\n"), 0o644)
	m.aiUndoTurn(-1)
	if !m.confirmOpen || m.confirmKind != confirmKindAIUndoTurn || !strings.Contains(m.confirm.Box(), "a.txt") {
		t.Fatalf("no undo dialog naming a.txt (open %v)", m.confirmOpen)
	}
	m.confirmOpen = false
	m.handleAIConfirm("cancel")
	if got := readFile(filepath.Join(dir, "a.txt")); got != "later\n" {
		t.Errorf("cancel changed a.txt: %q", got)
	}
	m.aiUndoTurn(-1)
	m.confirmOpen = false
	m.handleAIConfirm("restore")
	if got := readFile(filepath.Join(dir, "a.txt")); got != "mine\n" {
		t.Errorf("restore: a.txt %q", got)
	}
}

// Spec: restore points are saved: a new session of the same chat can
// still undo the turn.
func TestAIRestorePointsPersist(t *testing.T) {
	m, dir, _ := undoTurnModel(t)
	conv := m.ai.conv
	m.ai.restore = nil // as after a restart: read from disk
	m.ai.conv = conv
	if m.aiLastRestoreTurn() != 0 {
		t.Fatal("restore point not on disk")
	}
	m.aiUndoTurn(-1)
	if got := readFile(filepath.Join(dir, "a.txt")); got != "mine\n" {
		t.Errorf("a.txt %q", got)
	}
}

// Spec: the chat shows an "Undo AI changes" button under a turn with a
// restore point; it runs the undo.
func TestAIUndoTurnChatButton(t *testing.T) {
	m, dir, _ := undoTurnModel(t)
	found := ""
	for _, l := range m.buildAITranscript(60) {
		for _, b := range l.buttons {
			if strings.HasPrefix(b.id, "undo-turn:") {
				found = b.id
			}
		}
	}
	if found != "undo-turn:0" {
		t.Fatalf("button %q", found)
	}
	m.onAIButton(found)
	if got := readFile(filepath.Join(dir, "a.txt")); got != "mine\n" {
		t.Errorf("a.txt %q", got)
	}
}
