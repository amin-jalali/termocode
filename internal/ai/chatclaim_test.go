//go:build unix

package ai

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// SCN-10: a chat another process holds cannot be claimed; this process
// gets its own claim back; after Release another claim works.
func TestChatClaimOnePerProcess(t *testing.T) {
	s := ChatStore{Dir: t.TempDir()}
	a, err := s.Claim("1700000000000-1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if again, err := s.Claim("1700000000000-1"); err != nil || again != a {
		t.Fatalf("second claim in the same process: %v %v", again, err)
	}
	if a.ID() != "1700000000000-1" {
		t.Errorf("ID = %q", a.ID())
	}
	a.Release()

	// Another process: an flock on the same file from another open file.
	lock := filepath.Join(s.Dir, ".locks", "1700000000000-1.json.lock")
	f, err := os.OpenFile(lock, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim("1700000000000-1"); !errors.Is(err, ErrChatInUse) {
		t.Fatalf("claim held elsewhere: %v, want ErrChatInUse", err)
	}
	_ = f.Close() // that process ends
	b, err := s.Claim("1700000000000-1")
	if err != nil {
		t.Fatalf("claim after the other let go: %v", err)
	}
	b.Release()
	var nilClaim *ChatClaim
	nilClaim.Release()
	if nilClaim.ID() != "" {
		t.Error("nil claim has an id")
	}
	// The lock lives beside the chats, never listed as one.
	if got := s.List(); len(got) != 0 {
		t.Errorf("List = %v", got)
	}
}
