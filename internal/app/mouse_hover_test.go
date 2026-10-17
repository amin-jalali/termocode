package app

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sameMsgType(cmd tea.Cmd, want tea.Msg) bool {
	if cmd == nil {
		return false
	}
	got := cmd()
	if b, ok := got.(tea.BatchMsg); ok {
		for _, c := range b {
			if sameMsgType(c, want) {
				return true
			}
		}
		return false
	}
	return reflect.TypeOf(got) == reflect.TypeOf(want)
}

// Terminal R6: all-motion tracking (1003) only while something can use
// hover: the window has focus, no text input is open, and the setting
// mouse_hover is not off.
func TestMouseAllMotionOnlyWhenHoverNeeded(t *testing.T) {
	defer func(v bool) { mouseHover = v }(mouseHover)
	mouseHover = true
	cell, all := tea.EnableMouseCellMotion(), tea.EnableMouseAllMotion()

	m := Model{mouseAllMotion: true}
	nm, cmd := m.update(tea.BlurMsg{})
	if nm.mouseAllMotion || !sameMsgType(cmd, cell) {
		t.Fatalf("blurred: all motion %v, cmd to cell motion %v", nm.mouseAllMotion, sameMsgType(cmd, cell))
	}
	nm, cmd = nm.update(tea.FocusMsg{})
	if !nm.mouseAllMotion || !sameMsgType(cmd, all) {
		t.Fatalf("focused: all motion %v, cmd to all motion %v", nm.mouseAllMotion, sameMsgType(cmd, all))
	}

	m = Model{mouseAllMotion: true, promptOpen: true}
	if cmd := m.syncMouseMode(); !sameMsgType(cmd, cell) || m.mouseAllMotion {
		t.Error("text input open: still all motion")
	}

	mouseHover = false
	m = Model{mouseAllMotion: true}
	if cmd := m.syncMouseMode(); !sameMsgType(cmd, cell) || m.mouseAllMotion {
		t.Error("mouse_hover off: still all motion")
	}
	if cmd := m.syncMouseMode(); cmd != nil {
		t.Error("no change: a mode command anyway")
	}
}

func TestResolveMouseHover(t *testing.T) {
	for v, want := range map[string]bool{"": true, "auto": true, "on": true, "off": false, "OFF": false} {
		if got := ResolveMouseHover(v); got != want {
			t.Errorf("%q: %v, want %v", v, got, want)
		}
	}
}
