package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/aimemory"
)

// OM-2, owner decision: memory is off by default and asks once. The
// first time a lesson could be saved in a workspace that never decided,
// one card asks (Not now first and focused); the answer is kept per
// workspace.

func (m *Model) answerAIMemoryAskForTest(id string) tea.Cmd {
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm(id)
	return cmd
}

// Spec: a tool turn in an undecided workspace opens the card once;
// nothing is sent to the quick model before the answer. Not now is
// remembered: the next tool turn neither asks nor extracts.
func TestAIMemoryAskOnceNotNow(t *testing.T) {
	main := &recordingAIProvider{replies: []ai.Chunk{aiListDirCall("c1"), {Delta: "one"}, aiListDirCall("c2"), {Delta: "two"}}}
	quick := &recordingAIProvider{replies: []ai.Chunk{{Delta: aiMemoryReplyOne}}}
	m, dir := aiMemoryModel(t, main, quick, aiMemoryUndecided)
	if msg := aiMemoryMsgFrom(driveAIChatMemory(t, m, m.sendAIChat("first")), 300*time.Millisecond); msg != nil {
		t.Fatal("extracted before the answer")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindAIMemoryAsk {
		t.Fatal("no ask-once card after the first tool turn")
	}
	if got := m.confirm.SafeID(); got != "notnow" {
		t.Errorf("safe answer %q, want notnow", got)
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"Save lessons for later chats?", ".termocode/memory/lessons.md", ".gitignore", "Not now", "Turn on memory", "AI: Memory: Turn On/Off"} {
		if !strings.Contains(box, want) {
			t.Errorf("card lacks %q:\n%s", want, box)
		}
	}
	if aiMemoryMsgFrom(m.answerAIMemoryAskForTest("notnow"), 300*time.Millisecond) != nil {
		t.Fatal("Not now extracted")
	}
	if got := loadSession().AIMemory; got != aiMemoryOff {
		t.Errorf("session %q after Not now", got)
	}
	if msg := aiMemoryMsgFrom(driveAIChatMemory(t, m, m.sendAIChat("second")), 300*time.Millisecond); msg != nil || m.confirmOpen {
		t.Fatalf("asked or extracted again after Not now (card %v)", m.confirmOpen)
	}
	if len(quick.reqs) != 0 {
		t.Errorf("quick requests %d", len(quick.reqs))
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err == nil {
		t.Error(".gitignore made after Not now")
	}
}

// Spec: Esc is the safe answer (as Not now).
func TestAIMemoryAskOnceEsc(t *testing.T) {
	main := &recordingAIProvider{replies: []ai.Chunk{aiListDirCall("c1"), {Delta: "one"}}}
	m, _ := aiMemoryModel(t, main, &recordingAIProvider{}, aiMemoryUndecided)
	driveAIChatMemory(t, m, m.sendAIChat("first"))
	if !m.confirmOpen || m.confirmKind != confirmKindAIMemoryAsk {
		t.Fatal("no card")
	}
	m.confirmOpen = false
	m.onAIConfirmClosed()
	if got := loadSession().AIMemory; got != aiMemoryOff {
		t.Errorf("session %q after Esc", got)
	}
}

// Spec: Turn on memory turns it on, adds .termocode/memory/ to the
// existing .gitignore exactly once (old lines byte-equal) and saves the
// lessons of the turn that asked.
func TestAIMemoryAskOnceTurnOn(t *testing.T) {
	main := &recordingAIProvider{replies: []ai.Chunk{aiListDirCall("c1"), {Delta: "There is a.txt."}}}
	quick := &recordingAIProvider{replies: []ai.Chunk{{Delta: aiMemoryReplyOne}}}
	m, dir := aiMemoryModel(t, main, quick, aiMemoryUndecided)
	old := "# local\r\nbin/\n.env"
	_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(old), 0o644)
	driveAIChatMemory(t, m, m.sendAIChat("what files are here?"))
	msg := aiMemoryMsgFrom(m.answerAIMemoryAskForTest("on"), 5*time.Second)
	if msg == nil {
		t.Fatal("Turn on did not save the turn's lessons")
	}
	m.handleAIMsg(*msg)
	if got := loadSession().AIMemory; got != aiMemoryOn {
		t.Errorf("session %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); string(b) != old+"\n.termocode/memory/\n" {
		t.Errorf(".gitignore %q", b)
	}
	if !strings.Contains(aiToastText(m), "added .termocode/memory/ to .gitignore") {
		t.Errorf("toast %q", aiToastText(m))
	}
	if ls, _ := aimemory.Load(dir); len(ls) != 1 {
		t.Errorf("lessons %+v", ls)
	}
	if got := aiTranscriptText(m, 100); !strings.Contains(got, "memory: 1 lesson saved") {
		t.Errorf("transcript:\n%s", got)
	}
}

// Spec: a plain turn never asks; nor does a tool turn without a quick
// model slot (no lesson could be saved).
func TestAIMemoryAskOncePlainTurnNoCard(t *testing.T) {
	m, _ := aiMemoryModel(t, &recordingAIProvider{}, &recordingAIProvider{}, aiMemoryUndecided)
	driveAIChatMemory(t, m, m.sendAIChat("hello"))
	if m.confirmOpen {
		t.Fatal("card after a plain turn")
	}
	main := &recordingAIProvider{replies: []ai.Chunk{aiListDirCall("c1"), {Delta: "one"}}}
	m2, _ := aiMemoryModel(t, main, nil, aiMemoryUndecided)
	driveAIChatMemory(t, m2, m2.sendAIChat("list"))
	if m2.confirmOpen || loadSession().AIMemory != aiMemoryUndecided {
		t.Fatal("asked without a quick slot")
	}
}
