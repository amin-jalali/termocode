package app

import (
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// ── The inline "/" filter ────────────────────────────────────────────────
//
// Containers and Databases filter the same way: "/" starts typing, every key
// narrows the rows, Enter keeps the filter and gives the keys back, Esc
// clears it. This is the key handling both share; each view keeps its own
// text, applies it to its own rows and draws it in its title.

// maxFilterRunes is the longest filter text.
const maxFilterRunes = 64

// inlineFilterStep applies one key to a filter. text and typing are the
// filter now; handled=false means the key is not for the filter and the view
// handles it as it would without one (also for the keys that move the
// cursor while typing: arrows, Tab, PgUp, PgDn, Home, End).
func inlineFilterStep(text string, typing bool, k tea.KeyMsg) (newText string, newTyping, handled bool) {
	if !typing {
		// "/" starts typing. A fast typist (or a paste) can deliver "/"
		// and the next letters in one key message: the rest is text.
		if k.Type == tea.KeyRunes && len(k.Runes) > 1 && k.Runes[0] == '/' && !k.Alt {
			return inlineFilterStep(text, true, tea.KeyMsg{Type: tea.KeyRunes, Runes: k.Runes[1:]})
		}
		switch k.String() {
		case "/":
			return text, true, true
		case "esc":
			if text != "" {
				return "", false, true
			}
		}
		return text, false, false
	}
	switch k.String() {
	case "esc":
		return "", false, true
	case "enter":
		return text, false, true
	case "backspace", "ctrl+h":
		if r := []rune(text); len(r) > 0 {
			text = string(r[:len(r)-1])
		}
		return text, true, true
	case "ctrl+u":
		return "", true, true
	case "up", "down", "pgup", "pgdown", "tab", "shift+tab", "home", "end":
		return text, true, false // move while typing
	}
	if k.Type == tea.KeyRunes || k.Type == tea.KeySpace {
		for _, r := range k.Runes {
			if unicode.IsPrint(r) && len([]rune(text)) < maxFilterRunes {
				text += string(r)
			}
		}
		if k.Type == tea.KeySpace && len(k.Runes) == 0 && len([]rune(text)) < maxFilterRunes {
			text += " "
		}
		return text, true, true
	}
	return text, true, true // other keys do nothing while typing
}

// inlineFilterTitle is the filter as the title row shows it: "/text" and a
// caret while typing ("" when there is no filter and no typing).
func inlineFilterTitle(text string, typing bool, ascii bool) string {
	if text == "" && !typing {
		return ""
	}
	t := "/" + sanitizeOutputLine(text)
	if typing {
		if ascii {
			t += "_"
		} else {
			t += "▏"
		}
	}
	return t
}
