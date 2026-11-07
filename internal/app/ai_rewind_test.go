package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
)

// CMP-62: rewind the chat and the code to any turn; code checkpoints
// for every agent (also outside CLI agents).

// Spec: Rewind (code + chat) to the first question puts every file of
// the later turns back, drops the messages from there on and puts the
// question back in the input to edit.
func TestAIRewindCodeAndChat(t *testing.T) {
	m, dir, _ := undoTurnModel(t)
	m.aiRewindTo(0, true)
	if got := readFile(filepath.Join(dir, "a.txt")); got != "mine\n" {
		t.Errorf("a.txt %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err == nil {
		t.Error("new.txt still there")
	}
	s := m.aiS()
	if len(s.conv.Messages) != 0 {
		t.Errorf("messages left: %+v", s.conv.Messages)
	}
	if string(s.input) != "change things" {
		t.Errorf("input %q, want the question back", string(s.input))
	}
	if m.aiRestoreFiles(0) != 0 || m.aiLastRestoreTurn() != -1 {
		t.Error("restore points of the rewound turns are kept")
	}
}

// Spec: Rewind (code only) keeps the chat and records a tool line.
func TestAIRewindCodeOnly(t *testing.T) {
	m, dir, _ := undoTurnModel(t)
	n := len(m.aiS().conv.Messages)
	m.aiRewindTo(0, false)
	if got := readFile(filepath.Join(dir, "a.txt")); got != "mine\n" {
		t.Errorf("a.txt %q", got)
	}
	msgs := m.aiS().conv.Messages
	if len(msgs) != n+1 {
		t.Fatalf("messages %d, want %d", len(msgs), n+1)
	}
	if last := msgs[len(msgs)-1]; last.Role != ai.StoredTool || !strings.Contains(last.Text, "Rewind") {
		t.Errorf("chat line %+v", last)
	}
}

// Spec: the picker lists your questions with the files a rewind puts
// back; the card has Cancel first (focused) and Cancel keeps everything.
func TestAIRewindPickerAndCard(t *testing.T) {
	m, dir, _ := undoTurnModel(t)
	items := aiRewindItems(m)
	if len(items) == 0 || items[0].ID != "0" || !strings.Contains(items[0].Title, "change things") || !strings.Contains(items[0].Hint, "2 files") {
		t.Fatalf("items %+v", items)
	}
	m.onAIRewindPicked("0")
	if !m.confirmOpen || m.confirmKind != confirmKindAIRewind {
		t.Fatalf("no rewind card (open %v kind %v)", m.confirmOpen, m.confirmKind)
	}
	b := m.confirm.Buttons()
	if len(b) != 3 || b[0].ID != "cancel" || b[2].Style != confirm.StyleDestructive {
		t.Fatalf("buttons %+v", b)
	}
	m.confirmOpen = false
	m.handleAIConfirm("cancel")
	if got := readFile(filepath.Join(dir, "a.txt")); got != "agent\n" {
		t.Errorf("cancel changed a.txt: %q", got)
	}
	m.onAIRewindPicked("0")
	m.confirmOpen = false
	m.handleAIConfirm("code")
	if got := readFile(filepath.Join(dir, "a.txt")); got != "mine\n" {
		t.Errorf("code only: a.txt %q", got)
	}
}

// Spec: an outside agent's turn starts when its hook says "working"
// after another state; more "working" reports in the same turn do not
// save again.
func TestAgentTurnStarts(t *testing.T) {
	for _, c := range []struct {
		was, now agents.State
		want     bool
	}{
		{agents.Unknown, agents.Working, true},
		{agents.Done, agents.Working, true},
		{agents.Waiting, agents.Working, true},
		{agents.Working, agents.Working, false},
		{agents.Working, agents.Done, false},
	} {
		if got := agentTurnStarts(c.was, c.now); got != c.want {
			t.Errorf("%v → %v: %v, want %v", c.was, c.now, got, c.want)
		}
	}
}

// Spec: a tab's checkpoint folder is its agent worktree, else its shell
// folder, else the workspace.
func TestCheckpointDirForTab(t *testing.T) {
	if got := checkpointDirFor(&terminalTab{Worktree: "/w", Cwd: "/c"}); got != "/w" {
		t.Errorf("got %q", got)
	}
	if got := checkpointDirFor(&terminalTab{Cwd: "/c"}); got != "/c" {
		t.Errorf("got %q", got)
	}
	if got := checkpointDirFor(&terminalTab{}); got != aiWorkspaceRoot() {
		t.Errorf("got %q", got)
	}
}

// Spec: the palette has the three commands, each with an AI policy: the
// AI may save a checkpoint, only the user rolls code back.
func TestCheckpointPaletteAndPolicy(t *testing.T) {
	ids := map[string]bool{}
	for _, e := range aiPaletteItems() {
		ids[e.id] = true
	}
	for _, id := range []string{"ai-rewind-turn", "ai-checkpoint-save", "ai-checkpoint-restore"} {
		if !ids[id] {
			t.Errorf("%s not in the palette", id)
		}
	}
	if p := aiActionPolicies["ai-checkpoint-save"]; p.Gate != aiGateAuto {
		t.Errorf("save gate %v", p.Gate)
	}
	for _, id := range []string{"ai-rewind-turn", "ai-checkpoint-restore"} {
		if p := aiActionPolicies[id]; p.Gate != aiGateNever {
			t.Errorf("%s gate %v", id, p.Gate)
		}
	}
}
