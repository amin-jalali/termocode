package toast

import (
	"testing"
	"time"
)

type privateClick struct{}

// Spec (web-easy): a private toast (a one-time link) is shown and
// clickable, stays long enough to read, and never reaches OnPush (the
// error log).
func TestPushPrivateSkipsHookAndStays(t *testing.T) {
	old := OnPush
	t.Cleanup(func() { OnPush = old })
	hooked := 0
	OnPush = func(Severity, string) { hooked++ }
	m, _ := New().PushPrivate(Warn, "Could not open a browser", "http://127.0.0.1:1/#s=x", privateClick{}, 30*time.Second)
	live := m.Live()
	if len(live) != 1 || !live[0].Private || live[0].Click != (privateClick{}) || live[0].Detail != "http://127.0.0.1:1/#s=x" {
		t.Fatalf("toast %+v", live)
	}
	if left := time.Until(live[0].ExpiresAt); left < 25*time.Second {
		t.Fatalf("expires in %v", left)
	}
	if hooked != 0 {
		t.Fatal("OnPush saw a private toast")
	}
}
