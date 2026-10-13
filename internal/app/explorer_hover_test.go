package app

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
)

// A hover over the file tree must not touch the disk: it used to load
// and save the session file on every mouse move.
func TestExplorerHoverDoesNotSaveSession(t *testing.T) {
	withConfigDir(t)
	m0, _ := New().Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	m := m0.(Model)
	p, err := sessionPath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(p)
	for y := 2; y < 10; y++ {
		m = hoverAt(m, activity.Width+3, y)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("hover over the explorer wrote session.json")
	}
}
