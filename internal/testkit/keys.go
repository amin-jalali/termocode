package testkit

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// KeyMsg builds the tea.KeyMsg whose String() is s: "ctrl+p", "alt+x",
// "alt+ctrl+x", "shift+tab", "f5", "enter", "a", "é".
func KeyMsg(s string) tea.KeyMsg {
	if k, ok := namedKeys()[s]; ok {
		return tea.KeyMsg{Type: k}
	}
	if rest, ok := strings.CutPrefix(s, "alt+"); ok && rest != "" {
		k := KeyMsg(rest)
		k.Alt = true
		return k
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

var keyNames map[string]tea.KeyType

func namedKeys() map[string]tea.KeyType {
	if keyNames == nil {
		keyNames = map[string]tea.KeyType{}
		for k := tea.KeyType(-200); k < 200; k++ {
			if k == tea.KeyRunes {
				continue
			}
			if s := (tea.KeyMsg{Type: k}).String(); s != "" && !strings.HasPrefix(s, "KeyType(") {
				if _, dup := keyNames[s]; !dup {
					keyNames[s] = k
				}
			}
		}
	}
	return keyNames
}

// KeyNames returns every named (non-rune) key string, for random tests.
func KeyNames() []string {
	var out []string
	for s := range namedKeys() {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
