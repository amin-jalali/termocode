// Package keytest turns key names ("ctrl+shift+o", "alt+up", "f5") into
// the bytes a terminal sends for them, in the legacy (xterm) encoding and
// in the kitty keyboard protocol, so tests can drive the real input parser
// with every key combination.
package keytest

import (
	"fmt"
	"strconv"
	"strings"
)

// Encode returns the bytes for key in the legacy and kitty encodings; ""
// means that encoding cannot send the key.
func Encode(key string) (legacy, kitty string) {
	k := parseKeySpec(key)
	return legacyBytes(k), kittyBytes(k)
}

func modParam(shift, alt, ctrl bool) int {
	m := 1
	if shift {
		m++
	}
	if alt {
		m += 2
	}
	if ctrl {
		m += 4
	}
	return m
}

type keySpec struct {
	shift, alt, ctrl bool
	name             string // after the modifiers: "a", "A", "up", "f5", ".", "enter"
}

func parseKeySpec(s string) keySpec {
	var k keySpec
	parts := strings.Split(s, "+")
	if len(parts) > 1 && parts[len(parts)-1] == "" { // "ctrl++"
		parts = append(parts[:len(parts)-2], "+")
	}
	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "shift":
			k.shift = true
		case "alt":
			k.alt = true
		case "ctrl":
			k.ctrl = true
		}
	}
	k.name = parts[len(parts)-1]
	return k
}

// csiLetter: specials sent as CSI [1;m] <letter>.
var csiLetter = map[string]string{"up": "A", "down": "B", "right": "C", "left": "D", "home": "H", "end": "F"}

// csiTilde: specials sent as CSI n [;m] ~.
var csiTilde = map[string]int{"insert": 2, "delete": 3, "pgup": 5, "pgdown": 6,
	"f5": 15, "f6": 17, "f7": 18, "f8": 19, "f9": 20, "f10": 21, "f11": 23, "f12": 24}

// ss3: F1–F4 (SS3 P..S plain, CSI 1;m P..S with modifiers).
var ss3 = map[string]string{"f1": "P", "f2": "Q", "f3": "R", "f4": "S"}

// xtermShiftedF: Bubble Tea names Shift+F1..F12 as f13..f24 (xterm).
func xtermShiftedF(name string) (string, bool) {
	if !strings.HasPrefix(name, "f") {
		return "", false
	}
	n, err := strconv.Atoi(name[1:])
	if err != nil || n < 13 || n > 24 {
		return "", false
	}
	return "f" + strconv.Itoa(n-12), true
}

func legacyBytes(k keySpec) string {
	if base, ok := xtermShiftedF(k.name); ok {
		k.name, k.shift = base, true
	}
	m := modParam(k.shift, k.alt, k.ctrl)
	if l, ok := csiLetter[k.name]; ok {
		if m == 1 {
			return "\x1b[" + l
		}
		return fmt.Sprintf("\x1b[1;%d%s", m, l)
	}
	if n, ok := csiTilde[k.name]; ok {
		if m == 1 {
			return fmt.Sprintf("\x1b[%d~", n)
		}
		return fmt.Sprintf("\x1b[%d;%d~", n, m)
	}
	if l, ok := ss3[k.name]; ok {
		if m == 1 {
			return "\x1bO" + l
		}
		return fmt.Sprintf("\x1b[1;%d%s", m, l)
	}
	alt := ""
	if k.alt {
		alt = "\x1b"
	}
	switch k.name {
	case "enter":
		if k.ctrl || k.shift {
			return ""
		}
		return alt + "\r"
	case "tab":
		switch {
		case k.ctrl:
			return ""
		case k.shift:
			return alt + "\x1b[Z"
		}
		return alt + "\t"
	case "backspace":
		if k.ctrl || k.shift {
			return ""
		}
		return alt + "\x7f"
	case "esc":
		if k.ctrl || k.shift {
			return ""
		}
		return alt + "\x1b"
	case "space":
		if k.shift {
			return ""
		}
		if k.ctrl {
			return alt + "\x00"
		}
		return alt + " "
	}
	r := []rune(k.name)
	if len(r) != 1 {
		return ""
	}
	c := r[0]
	if k.ctrl {
		if k.shift {
			return "" // legacy cannot tell Ctrl+Shift+X from Ctrl+X
		}
		switch {
		case c >= 'a' && c <= 'z':
			return alt + string(rune(c-'a'+1))
		case c == '@':
			return alt + "\x00"
		case c == '[':
			return alt + "\x1b"
		case c == '\\':
			return alt + "\x1c"
		case c == ']':
			return alt + "\x1d"
		case c == '^':
			return alt + "\x1e"
		case c == '_':
			return alt + "\x1f"
		}
		return ""
	}
	if k.shift && c >= 'a' && c <= 'z' {
		c = c - 'a' + 'A'
	}
	return alt + string(c)
}

func kittyBytes(k keySpec) string {
	if _, ok := xtermShiftedF(k.name); ok {
		return "" // a Bubble Tea name for a legacy sequence
	}
	m := modParam(k.shift, k.alt, k.ctrl)
	// Specials keep their legacy form under the kitty protocol.
	if _, ok := csiLetter[k.name]; ok {
		return legacyBytes(k)
	}
	if _, ok := csiTilde[k.name]; ok {
		return legacyBytes(k)
	}
	if _, ok := ss3[k.name]; ok {
		return legacyBytes(k)
	}
	code := 0
	switch k.name {
	case "enter":
		code = 13
	case "tab":
		code = 9
	case "backspace":
		code = 127
	case "esc":
		code = 27
	case "space":
		code = 32
	default:
		r := []rune(k.name)
		if len(r) != 1 {
			return ""
		}
		code = int(r[0])
		if code >= 'A' && code <= 'Z' { // "alt+T": the key is t with Shift
			code += 'a' - 'A'
			m = modParam(true, k.alt, k.ctrl)
		}
	}
	if m == 1 {
		return string(rune(code))
	}
	return fmt.Sprintf("\x1b[%d;%du", code, m)
}
