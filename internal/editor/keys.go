package editor

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// translateKey converts a Bubble Tea key event into the keycode string nvim
// expects via nvim_input. Typed text goes as text; Ctrl+Z / Ctrl+Y are
// undo / redo in any mode; every other key goes by its name: the
// modifiers (Ctrl, Alt, Shift) and the key become nvim notation
// ("ctrl+alt+up" → "<C-M-Up>", "shift+pgup" → "<S-PageUp>"), so no
// combination is dropped or garbled (keys_all_test.go covers them all).
func translateKey(k tea.KeyMsg) string {
	switch k.Type {
	case tea.KeyCtrlZ:
		if !k.Alt {
			return undoKeys("undo")
		}
	case tea.KeyCtrlY:
		if !k.Alt {
			return undoKeys("redo")
		}
	case tea.KeyRunes:
		if len(k.Runes) == 0 {
			return ""
		}
		if k.Alt {
			return fmt.Sprintf("<M-%s>", escapeRunes(string(k.Runes)))
		}
		// Escape special characters that nvim_input interprets as keycodes.
		return escapeRunes(string(k.Runes))
	case tea.KeySpace:
		if !k.Alt {
			return " "
		}
	}
	return keyNameToNvim(k.String())
}

// nvimKeyNames are Bubble Tea's names for keys that nvim spells another
// way.
var nvimKeyNames = map[string]string{
	"up": "Up", "down": "Down", "left": "Left", "right": "Right",
	"home": "Home", "end": "End", "pgup": "PageUp", "pgdown": "PageDown",
	"delete": "Del", "insert": "Insert", "enter": "CR", "tab": "Tab",
	"backspace": "BS", "esc": "Esc", " ": "Space", "space": "Space",
	"<": "lt", "\\": "Bslash", "|": "Bar",
}

// keyNameToNvim turns a key name ("ctrl+shift+o", "alt+enter", "f17",
// "ctrl+alt+pgup") into nvim notation ("<C-S-o>", "<M-CR>", "<S-F5>",
// "<C-M-PageUp>"). xterm's F13–F24 are Shift+F1–F12. "" only for an empty
// name.
func keyNameToNvim(name string) string {
	parts := strings.Split(name, "+")
	if len(parts) > 1 && parts[len(parts)-1] == "" { // "ctrl++"
		parts = append(parts[:len(parts)-2], "+")
	}
	key := parts[len(parts)-1]
	var ctrl, alt, shift bool
	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "ctrl":
			ctrl = true
		case "alt":
			alt = true
		case "shift":
			shift = true
		}
	}
	if key == "" {
		return ""
	}
	if len(key) > 1 && key[0] == 'f' {
		if n, err := strconv.Atoi(key[1:]); err == nil {
			if n >= 13 && n <= 24 {
				n, shift = n-12, true
			}
			key = "F" + strconv.Itoa(n)
		}
	} else if v, ok := nvimKeyNames[key]; ok {
		key = v
	}
	var b strings.Builder
	if ctrl {
		b.WriteString("C-")
	}
	if alt {
		b.WriteString("M-")
	}
	if shift {
		b.WriteString("S-")
	}
	if b.Len() == 0 && len([]rune(key)) == 1 {
		return key
	}
	return "<" + b.String() + key + ">"
}

func escapeRunes(s string) string {
	if !strings.ContainsAny(s, "<\\") {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '<':
			b.WriteString("<lt>")
		case '\\':
			b.WriteString("<Bslash>")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// undoKeys is Ctrl+Z / Ctrl+Y for nvim: run :undo / :redo in whatever mode
// nvim is in, leaving a selection first. It was "<C-o>u", which only works
// in insert mode: in normal mode <C-o> jumped back through the jump list
// (to another file) and typed "u" there, and with a selection "u"
// lowercased it.
func undoKeys(cmd string) string {
	return "<Cmd>lua if vim.fn.mode():match('^[vVsS\\22\\19]') then vim.cmd('normal! \\27') end vim.cmd('silent! " + cmd + "')<CR>"
}
