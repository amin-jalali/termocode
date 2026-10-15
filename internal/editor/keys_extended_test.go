package editor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Spec (terminal R1): keys only the kitty keyboard protocol reports reach
// nvim in its own notation when termocode does not bind them.
func TestTranslateExtendedKeys(t *testing.T) {
	cases := map[string]string{
		"ctrl+shift+o":     "<C-S-o>",
		"ctrl+.":           "<C-.>",
		"ctrl+/":           "<C-/>",
		"ctrl+tab":         "<C-Tab>",
		"ctrl+shift+tab":   "<C-S-Tab>",
		"shift+enter":      "<S-CR>",
		"ctrl+enter":       "<C-CR>",
		"ctrl+alt+i":       "<C-M-i>",
		"ctrl+alt+shift+t": "<C-M-S-t>",
		"shift+space":      "<S-Space>",
		"ctrl+backspace":   "<C-BS>",
		"ctrl+shift+esc":   "<C-S-Esc>",
		"ctrl+<":           "<C-lt>",
		"ctrl+\\":          "<C-Bslash>",
		"ctrl+|":           "<C-Bar>",
	}
	for name, want := range cases {
		got := translateKey(tea.KeyMsg{Type: tea.KeyExtended, Name: name})
		if got != want {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
}
