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
	case "f1":
		return tea.KeyMsg{Type: tea.KeyF1}
	case "alt+x":
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true}
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
		// Terminal R12: VS Code's and JetBrains' terminals take F1 for
		// their own palette; Alt+X opens termocode's there.
		{"f1", ActionCommandPalette},
		{"alt+x", ActionCommandPalette},
		{"a", ActionNone},
	}
	for _, tc := range cases {
		got := km.Match(key(tc.in))
		if got != tc.want {
			t.Errorf("%s: got %v want %v", tc.in, got, tc.want)
		}
	}
}

// F4 / Shift+F4 belong to Find in Files result navigation (VSCode); the
// app actions that used to sit there moved to Alt+L and Alt+Shift+T.
func TestSearchResultKeys(t *testing.T) {
	b := Default().bindings
	cases := map[string]Action{
		"f4":           ActionNextSearchResult,
		"shift+f4":     ActionPrevSearchResult,
		"f16":          ActionPrevSearchResult,
		"alt+l":        ActionToggleInlayHints,
		"alt+shift+t":  ActionReopenClosed,
		"alt+T":        ActionReopenClosed,
		"ctrl+shift+t": ActionReopenClosed,
	}
	for k, want := range cases {
		if got := b[k]; got != want {
			t.Errorf("%s: got %s, want %s", k, ActionName(got), ActionName(want))
		}
	}
	if got := Default().Match(tea.KeyMsg{Type: tea.KeyF4}); got != ActionNextSearchResult {
		t.Errorf("F4 KeyMsg: got %s", ActionName(got))
	}
}
