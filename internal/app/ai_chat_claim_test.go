//go:build unix

package app

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/toast"
)

// SCN-10: a second window on the same folder must not take the chat the
// first window shows. Both saved the same chat file, and the last save
// dropped the other window's turns. The second window starts its own
// chat, and the history list does not open the chat while the first
// window holds it.
func TestChatOpenInAnotherWindowIsNotShared(t *testing.T) {
	m := aiTestModel(t)
	m.toast = toast.New()
	s := m.ai
	s.conv.Messages = []ai.StoredMessage{{Role: ai.StoredUser, Text: "from window a"}, {Role: ai.StoredAssistant, Text: "ok"}}
	if err := s.store.Save(&s.conv); err != nil {
		t.Fatal(err)
	}
	m.persistAIState()
	id := s.conv.ID

	// Window A is another process: its flock on the chat.
	s.claim.Release()
	s.claim = nil
	lock := filepath.Join(s.store.Dir, ".locks", id+".json.lock")
	f, err := os.OpenFile(lock, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("no lock file for the shown chat: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}

	b := newAIState() // window B starts
	if b.conv.ID == id || len(b.conv.Messages) != 0 {
		t.Fatalf("window B took window A's chat %q (%d messages)", b.conv.ID, len(b.conv.Messages))
	}
	mb := Model{w: 120, h: 40, ai: b, toast: toast.New()}
	mb.onAIHistoryPicked(id)
	if b.conv.ID == id {
		t.Fatal("the history list opened a chat another window holds")
	}

	// Window A ends: B may open the chat now.
	_ = f.Close()
	mb.onAIHistoryPicked(id)
	if b.conv.ID != id || len(b.conv.Messages) != 2 {
		t.Fatalf("after window A ended: chat %q, %d messages", b.conv.ID, len(b.conv.Messages))
	}
}
