package explorer

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/clock"
)

func TestExplorerPreviewToggleAndSelection(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir+"/a.txt", "alpha")
	mustWrite(t, dir+"/b.txt", "bravo")
	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := Model{root: root}
	m.SetSize(76, 30)
	m.SetTerminalWidth(150)
	var timers []chan time.Time
	t.Cleanup(clock.SetAfter(func(d time.Duration) <-chan time.Time {
		if d != 100*time.Millisecond {
			t.Fatal(d)
		}
		ch := make(chan time.Time, 1)
		timers = append(timers, ch)
		return ch
	}))
	cmd := m.TogglePreview()
	if cmd == nil || !m.PreviewVisible() {
		t.Fatal("toggle")
	}
	m.RevealPath(dir + "/a.txt")
	a := m.SchedulePreview()
	m.RevealPath(dir + "/b.txt")
	b := m.SchedulePreview()
	for _, ch := range timers {
		ch <- clock.Now()
	}
	m, _ = m.Update(cmd())
	m, _ = m.Update(a())
	if strings.Contains(m.previewText, "alpha") {
		t.Fatal("stale result")
	}
	m, _ = m.Update(b())
	if m.previewText != "bravo" {
		t.Fatal(m.previewText)
	}
	view := ansi.Strip(m.View(true, "", false, nil))
	if !strings.Contains(view, "bravo") || !strings.Contains(view, "Preview") {
		t.Fatal(view)
	}
	if m.SchedulePreview() != nil {
		t.Fatal("same selection scheduled")
	}
	// A preview click changes the selection without opening an editor.
	next, click := m.HandleMouse(3, m.HeaderChromeRows()+1, tea.MouseLeft, false, false)
	if next.CursorPath() != dir+"/a.txt" {
		t.Fatal(next.CursorPath())
	}
	if click != nil {
		for _, ch := range timers {
			select {
			case ch <- clock.Now():
			default:
			}
		}
		var check func(tea.Msg)
		check = func(msg tea.Msg) {
			if _, ok := msg.(OpenFileMsg); ok {
				t.Fatal("opened editor")
			}
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, cmd := range batch {
					check(cmd())
				}
			}
		}
		check(click())
	}

	m.SetTerminalWidth(99)
	if m.PreviewVisible() {
		t.Fatal("narrow preview")
	}
	if !strings.Contains(ansi.Strip(m.View(true, "", false, nil)), "100 cols") {
		t.Fatal("missing hint")
	}
	m.SetTerminalWidth(150)
	if m.SchedulePreview() == nil {
		t.Fatal("resize must refresh")
	}
	m.TogglePreview()
	if m.PreviewEnabled() {
		t.Fatal("toggle off")
	}
}

func TestExplorerPreviewScrollAndReload(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	for i := 0; i < 80; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	mustWrite(t, dir+"/a.txt", strings.Join(lines, "\n"))
	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := Model{root: root}
	m.SetSize(76, 20)
	m.SetTerminalWidth(150)
	m.RevealPath(dir + "/a.txt")
	t.Cleanup(clock.SetAfter(func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); ch <- clock.Now(); return ch }))
	cmd := m.TogglePreview()
	m, _ = m.Update(cmd())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.previewTop != 17 {
		t.Fatal(m.previewTop)
	}
	if !strings.Contains(ansi.Strip(m.View(true, "", false, nil)), "line 17") {
		t.Fatal("page did not scroll")
	}
	m, _ = m.HandleMouse(60, 8, tea.MouseWheelUp, false, false)
	if m.previewTop != 14 {
		t.Fatal(m.previewTop)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.previewTop != 0 {
		t.Fatal("negative scroll")
	}
	// Reloading the same path must not leave stale text, including after swaps.
	t.Chdir(dir)
	mustWrite(t, dir+"/a.txt", "new content")
	m.Reload()
	cmd = m.SchedulePreview()
	if cmd == nil {
		t.Fatal("reload did not refresh")
	}
	m, _ = m.Update(cmd())
	if m.previewText != "new content" {
		t.Fatal(m.previewText)
	}
}
