package keymap

import (
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestExplorerLocalKeysAreFree(t *testing.T) {
	km := Default()
	for _, k := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune(ExplorerPreviewKey)}, {Type: tea.KeyRunes, Runes: []rune(ExplorerRenameKey)}, {Type: tea.KeyF2}} {
		if a := km.Match(k); a != ActionNone {
			t.Fatalf("%s is bound globally: %v", k.String(), a)
		}
	}
}
