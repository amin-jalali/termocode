package app

import (
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The first frame drawn at the window's size ends startup: the GC held
// off by bootgc is turned on then (once), not before (the unsized
// "loading" frame) and not again.
func TestFirstSizedFrameEndsStartup(t *testing.T) {
	withConfigDir(t)
	calls := 0
	oldOnce, oldHook := firstFrameOnce, onFirstFrame
	firstFrameOnce, onFirstFrame = &sync.Once{}, func() { calls++ }
	t.Cleanup(func() { firstFrameOnce, onFirstFrame = oldOnce, oldHook })

	m := newTestModel(t)
	_ = m.View()
	if calls != 0 {
		t.Fatalf("startup ended on the unsized frame (calls=%d)", calls)
	}
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)
	_ = m.View()
	_ = m.View()
	if calls != 1 {
		t.Fatalf("onFirstFrame calls = %d after two sized frames, want 1", calls)
	}
}
