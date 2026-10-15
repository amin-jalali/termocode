package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// syncTerminalState asked nvim about terminal buffers (a Lua call over
// RPC) up to 4 times a second while scrolling, also with no terminal
// open, where the answer changes nothing. It must not ask then.
func TestSyncTerminalStateSkipsNvimWithoutTerminal(t *testing.T) {
	(&Model{}).invalidateTerminalProbeCache()
	m := Model{nvim: &nvim.Client{}} // any call on it would panic
	if m.syncTerminalState() {
		t.Error("changed = true with no terminal open")
	}
	if !terminalProbeAt.IsZero() {
		t.Error("nvim was asked about terminals with no terminal open")
	}
}
