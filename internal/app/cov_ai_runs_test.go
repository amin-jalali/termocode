package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Agent runs in the chat (ai_runs.go): when Retry is refused, and a chat
// file that can not be written while a run goes on.

// Retry from a failed step: refused while answering, for a message that
// is not a run, when another message came after it, when its profile is
// gone and without a provider.
func TestCovAIRunRetryRefusals(t *testing.T) {
	rev := &recordingAIProvider{replies: []ai.Chunk{{Delta: "found it"}}}
	m, _ := aiRunModel(t, rev, failingAIProvider{})
	driveAIChat(t, m, m.sendAIChat("go"), nil)
	s := m.aiS()
	i := len(s.conv.Messages) - 1
	if s.aiRetryStep(i) != 2 {
		t.Fatalf("retry step = %d, want 2", s.aiRetryStep(i))
	}
	s.streaming = true
	if s.aiRetryStep(i) != 0 || m.aiRetryRun(i) == nil || !strings.Contains(covToastText(m), "still answering") {
		t.Errorf("while answering: %q", covToastText(m))
	}
	s.streaming = false
	if m.aiRetryRun(0) != nil {
		t.Error("retry of the user's message did something")
	}
	for _, bad := range []int{-1, 0, len(s.conv.Messages)} {
		if aiRetryStepIn(s.conv.Messages, bad, s.profiles) != 0 {
			t.Errorf("index %d can be retried", bad)
		}
	}
	later := append(append([]ai.StoredMessage(nil), s.conv.Messages...), ai.StoredMessage{Role: ai.StoredUser, Text: "next"})
	if aiRetryStepIn(later, i, s.profiles) != 0 {
		t.Error("a run with a later message can be retried")
	}
	withErr := append(append([]ai.StoredMessage(nil), s.conv.Messages...), ai.StoredMessage{Role: ai.StoredError, Text: "x"})
	if aiRetryStepIn(withErr, i, s.profiles) != 2 {
		t.Error("an error line after the run blocks the retry")
	}
	prof := s.conv.Messages[i].Run.Profile
	s.conv.Messages[i].Run.Profile = "gone-profile"
	if m.aiRetryRun(i) == nil || !strings.Contains(covToastText(m), "no longer exists") {
		t.Errorf("profile gone: %q", covToastText(m))
	}
	s.conv.Messages[i].Run.Profile = prof
	s.provider = nil
	if m.aiRetryRun(i) == nil || !strings.Contains(covToastText(m), "AI is not configured") {
		t.Errorf("no provider: %q", covToastText(m))
	}
}

// A chat file that can not be written during a run: the run still ends
// and each failed save is in errors.log.
func TestCovAIRunChatFileNotWritable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes anywhere")
	}
	rev := &recordingAIProvider{replies: []ai.Chunk{{Delta: "found it"}}}
	m, _ := aiRunModel(t, rev, failingAIProvider{})
	dir := m.aiS().store.Dir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	driveAIChat(t, m, m.sendAIChat("go"), nil)
	if rm := aiRunMsg(t, m); rm.Run.State != ai.RunStateFailed {
		t.Errorf("run state %q", rm.Run.State)
	}
	cfg, _ := configDir()
	b, _ := os.ReadFile(filepath.Join(cfg, "errors.log"))
	if !strings.Contains(string(b), "[ai] save chat") {
		t.Errorf("errors.log does not name the failed save:\n%s", b)
	}
}
