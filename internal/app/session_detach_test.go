package app

import "testing"

// TestDetachSessionOutsideSession: without a session the command only
// shows a toast.
func TestDetachSessionOutsideSession(t *testing.T) {
	t.Setenv("TERMOCODE_SESSION", "")
	m := Model{w: 120, h: 40}
	if cmd := m.dispatchPaletteAction("detach-session"); cmd == nil || m.toast.Empty() {
		t.Fatalf("want a toast, got cmd=%v empty=%v", cmd != nil, m.toast.Empty())
	}
}
