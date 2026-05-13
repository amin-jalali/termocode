package editor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// translateKey converts a Bubble Tea key event into the keycode string nvim
// expects via nvim_input. Returns "" for keys we don't forward.
//
// Where possible, common VSCode-style shortcuts are mapped to their nvim
// equivalents (e.g. Ctrl+Z → undo from insert mode).
func translateKey(k tea.KeyMsg) string {
	if k.Alt {
		switch k.Type {
		case tea.KeyUp:
			return "<M-Up>"
		case tea.KeyDown:
			return "<M-Down>"
		case tea.KeyLeft:
			return "<M-Left>"
		case tea.KeyRight:
			return "<M-Right>"
		case tea.KeyShiftUp:
			return "<M-S-Up>"
		case tea.KeyShiftDown:
			return "<M-S-Down>"
		case tea.KeyShiftLeft:
			return "<M-S-Left>"
		case tea.KeyShiftRight:
			return "<M-S-Right>"
		case tea.KeyHome:
			return "<M-Home>"
		case tea.KeyEnd:
			return "<M-End>"
		case tea.KeyPgUp:
			return "<M-PageUp>"
		case tea.KeyPgDown:
			return "<M-PageDown>"
		case tea.KeyRunes:
			if len(k.Runes) > 0 {
				return fmt.Sprintf("<M-%s>", escapeRunes(string(k.Runes)))
			}
		}
	}
	switch k.Type {
	case tea.KeyCtrlZ:
		return "<C-o>u"
	case tea.KeyCtrlY:
		return "<C-o><C-r>"
	case tea.KeyEnter:
		return "<CR>"
	case tea.KeyEsc:
		return "<Esc>"
	case tea.KeyTab:
		return "<Tab>"
	case tea.KeyShiftTab:
		return "<S-Tab>"
	case tea.KeyBackspace:
		return "<BS>"
	case tea.KeySpace:
		return " "
	case tea.KeyLeft:
		return "<Left>"
	case tea.KeyRight:
		return "<Right>"
	case tea.KeyUp:
		return "<Up>"
	case tea.KeyDown:
		return "<Down>"
	case tea.KeyHome:
		return "<Home>"
	case tea.KeyEnd:
		return "<End>"
	case tea.KeyPgUp:
		return "<PageUp>"
	case tea.KeyPgDown:
		return "<PageDown>"
	case tea.KeyDelete:
		return "<Del>"
	case tea.KeyShiftUp:
		return "<S-Up>"
	case tea.KeyShiftDown:
		return "<S-Down>"
	case tea.KeyShiftLeft:
		return "<S-Left>"
	case tea.KeyShiftRight:
		return "<S-Right>"
	case tea.KeyF1:
		return "<F1>"
	case tea.KeyF2:
		return "<F2>"
	case tea.KeyF3:
		return "<F3>"
	case tea.KeyF4:
		return "<F4>"
	case tea.KeyF5:
		return "<F5>"
	case tea.KeyF7:
		return "<F7>"
	case tea.KeyF8:
		return "<F8>"
	case tea.KeyF9:
		return "<F9>"
	case tea.KeyF10:
		return "<F10>"
	case tea.KeyF12:
		return "<F12>"
	case tea.KeyRunes:
		// Escape special characters that nvim_input interprets as keycodes.
		return escapeRunes(string(k.Runes))
	}

	// Modifier + arrow / nav combos. Bubble Tea reports these via String()
	// as "ctrl+right", "ctrl+shift+right", etc., but tea has no dedicated
	// KeyCtrlRight constants — we have to dispatch on the canonical name.
	// nvim_input expects them as <C-Right>, <C-S-Right>, etc.
	s := k.String()
	if mapped := modArrowToNvim(s); mapped != "" {
		return mapped
	}

	// Ctrl+letter handling via String().
	if strings.HasPrefix(s, "ctrl+") && len(s) > len("ctrl+") {
		ch := s[len("ctrl+"):]
		if len(ch) == 1 {
			return fmt.Sprintf("<C-%s>", ch)
		}
	}
	if strings.HasPrefix(s, "alt+") && len(s) > len("alt+") {
		ch := s[len("alt+"):]
		return fmt.Sprintf("<M-%s>", ch)
	}
	return ""
}

// modArrowToNvim translates Bubble Tea's stringified modifier+motion keys
// (ctrl+right, ctrl+shift+home, alt+pgdown, …) into the nvim_input keycode
// form (<C-Right>, <C-S-Home>, <M-PageDown>, …). Returns "" for unrecognised
// inputs so callers can fall through to other branches.
//
// Coverage: every motion key (Left/Right/Up/Down/Home/End/PageUp/PageDown)
// crossed with every common modifier set (none, Shift, Ctrl, Ctrl+Shift,
// Alt, Alt+Shift). Combined with `set keymodel=startsel,stopsel` in nvim,
// any *-Shift-* combo extends a Visual selection automatically.
func modArrowToNvim(s string) string {
	table := map[string]string{
		// Ctrl + motion → word/file jump
		"ctrl+right":   "<C-Right>",
		"ctrl+left":    "<C-Left>",
		"ctrl+up":      "<C-Up>",
		"ctrl+down":    "<C-Down>",
		"ctrl+home":    "<C-Home>",
		"ctrl+end":     "<C-End>",
		"ctrl+pgup":    "<C-PageUp>",
		"ctrl+pgdown":  "<C-PageDown>",

		// Ctrl + Shift + motion → word/file jump + extend selection
		"ctrl+shift+right":  "<C-S-Right>",
		"ctrl+shift+left":   "<C-S-Left>",
		"ctrl+shift+up":     "<C-S-Up>",
		"ctrl+shift+down":   "<C-S-Down>",
		"ctrl+shift+home":   "<C-S-Home>",
		"ctrl+shift+end":    "<C-S-End>",
		"ctrl+shift+pgup":   "<C-S-PageUp>",
		"ctrl+shift+pgdown": "<C-S-PageDown>",

		// Shift + non-arrow motion → extend selection (arrows are already
		// handled by tea.KeyShift{Up,Down,Left,Right} above).
		"shift+home":   "<S-Home>",
		"shift+end":    "<S-End>",
		"shift+pgup":   "<S-PageUp>",
		"shift+pgdown": "<S-PageDown>",

		// Alt + motion → word jump (alternative to Ctrl on some keyboards)
		"alt+right":   "<M-Right>",
		"alt+left":    "<M-Left>",
		"alt+up":      "<M-Up>",
		"alt+down":    "<M-Down>",
		"alt+home":    "<M-Home>",
		"alt+end":     "<M-End>",
		"alt+pgup":    "<M-PageUp>",
		"alt+pgdown":  "<M-PageDown>",

		// Alt + Shift + motion → mostly used for line manipulation
		// (Alt+Shift+Down = duplicate line via lsp_lua mapping). Forward
		// them all so future bindings can use them.
		"alt+shift+up":     "<M-S-Up>",
		"alt+shift+down":   "<M-S-Down>",
		"alt+shift+left":   "<M-S-Left>",
		"alt+shift+right":  "<M-S-Right>",
		"alt+shift+home":   "<M-S-Home>",
		"alt+shift+end":    "<M-S-End>",
		"alt+shift+pgup":   "<M-S-PageUp>",
		"alt+shift+pgdown": "<M-S-PageDown>",
	}
	return table[s]
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
