package keymap

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+q":
		return tea.KeyMsg{Type: tea.KeyCtrlQ}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+b":
		return tea.KeyMsg{Type: tea.KeyCtrlB}
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "ctrl+f":
		return tea.KeyMsg{Type: tea.KeyCtrlF}
	case "ctrl+w":
		return tea.KeyMsg{Type: tea.KeyCtrlW}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "f6":
		return tea.KeyMsg{Type: tea.KeyF6}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "a":
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	}
	return tea.KeyMsg{}
}

func TestDefaultBindings(t *testing.T) {
	km := Default()
	cases := []struct {
		in   string
		want Action
	}{
		{"ctrl+c", ActionCopy},
		{"ctrl+q", ActionQuit},
		{"ctrl+s", ActionSave},
		{"ctrl+b", ActionToggleExplorer},
		{"f6", ActionFocusSwap},
		{"ctrl+p", ActionQuickOpen},
		{"ctrl+f", ActionFind},
		{"ctrl+w", ActionCloseBuffer},
		// Ctrl+D is reserved for vim-visual-multi (multi-cursor "find next").
		// At the keymap layer it must NOT match any action so the editor
		// forwards it to nvim untouched.
		{"ctrl+d", ActionNone},
		{"tab", ActionNone}, // Tab is no longer a focus swap; passes through to focused pane
		{"a", ActionNone},
	}
	for _, tc := range cases {
		got := km.Match(key(tc.in))
		if got != tc.want {
			t.Errorf("%s: got %v want %v", tc.in, got, tc.want)
		}
	}
}
