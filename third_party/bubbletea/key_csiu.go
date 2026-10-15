package tea

// termocode patch: the kitty keyboard protocol, "disambiguate escape
// codes" flag (CSI > 1 u). The terminal then reports Esc and the ctrl /
// alt combos as CSI code[:alternates] ; modifiers[:event] u. Combos a
// legacy terminal can also send come out exactly as the legacy bytes
// would (same Type and String); the others become KeyExtended with the
// names termocode's keymap uses ("ctrl+shift+o", "ctrl+.", "ctrl+tab",
// "shift+enter").

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// csiuRe matches CSI code[:alt…][;mods[:event]][;text] u. A leading "?"
// (the flags reply) does not match.
var csiuRe = regexp.MustCompile(`^\x1b\[(\d+)(?::\d*)*(?:;(\d*)(?::(\d+))?)?(?:;[\d:]*)?u`)

const (
	csiuShift = 1
	csiuAlt   = 2
	csiuCtrl  = 4
	csiuOther = 8 | 16 | 32 // super, hyper, meta
	csiuLocks = 64 | 128    // caps lock, num lock: ignored
)

func detectCSIu(b []byte) (bool, int, Msg) {
	if len(b) < 4 || b[0] != '\x1b' || b[1] != '[' || b[2] < '0' || b[2] > '9' {
		return false, 0, nil
	}
	m := csiuRe.FindSubmatch(b)
	if m == nil {
		return false, 0, nil
	}
	w := len(m[0])
	code, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return true, w, unknownCSISequenceMsg(b[:w])
	}
	mods := 0
	if len(m[2]) > 0 {
		if v, err := strconv.Atoi(string(m[2])); err == nil && v > 0 {
			mods = (v - 1) &^ csiuLocks
		}
	}
	if len(m[3]) > 0 && string(m[3]) == "3" { // key release
		return true, w, unknownCSISequenceMsg(b[:w])
	}
	k, ok := csiuKey(code, mods)
	if !ok {
		return true, w, unknownCSISequenceMsg(b[:w])
	}
	return true, w, KeyMsg(k)
}

// csiuKey maps a kitty key code and modifier bits to a Key.
func csiuKey(code, mods int) (Key, bool) {
	if mods&csiuOther != 0 || code <= 0 || code > unicode.MaxRune {
		return Key{}, false
	}
	shift, alt, ctrl := mods&csiuShift != 0, mods&csiuAlt != 0, mods&csiuCtrl != 0
	ext := func(name string) (Key, bool) {
		var b strings.Builder
		if ctrl {
			b.WriteString("ctrl+")
		}
		if alt {
			b.WriteString("alt+")
		}
		if shift {
			b.WriteString("shift+")
		}
		b.WriteString(name)
		return Key{Type: KeyExtended, Name: b.String()}, true
	}
	legacy := func(t KeyType) (Key, bool) { return Key{Type: t, Alt: alt}, true }
	switch code {
	case 27:
		if ctrl || shift {
			return ext("esc")
		}
		return legacy(KeyEscape)
	case 13:
		if ctrl || shift {
			return ext("enter")
		}
		return legacy(KeyEnter)
	case 9:
		switch {
		case ctrl:
			return ext("tab")
		case shift && !alt:
			return Key{Type: KeyShiftTab}, true
		case shift:
			return ext("tab")
		}
		return legacy(KeyTab)
	case 127:
		if ctrl || shift {
			return ext("backspace")
		}
		return legacy(KeyBackspace)
	case 32:
		switch {
		case shift:
			return ext("space")
		case ctrl:
			return legacy(KeyCtrlAt)
		}
		return Key{Type: KeySpace, Runes: spaceRunes, Alt: alt}, true
	}
	if code >= 57344 && code <= 63743 { // kitty's functional keys (keypad, media …)
		return Key{}, false
	}
	r := rune(code)
	if !unicode.IsPrint(r) {
		return Key{}, false
	}
	if ctrl && !shift {
		// The ctrl combos a legacy terminal sends as C0 bytes. ctrl+i,
		// ctrl+m and ctrl+[ are tab, enter and esc there, so they stay
		// those keys, as before; ctrl+alt+i is new (legacy: alt+tab).
		if r >= 'a' && r <= 'z' {
			if r == 'i' && alt {
				return ext("i")
			}
			return legacy(KeyCtrlA + KeyType(r-'a'))
		}
		switch r {
		case '[': // Esc, as on every legacy terminal (vim users rely on it)
			return legacy(KeyEscape)
		case '@':
			return legacy(KeyCtrlAt)
		case '\\':
			return legacy(KeyCtrlBackslash)
		case ']':
			return legacy(KeyCtrlCloseBracket)
		case '^':
			return legacy(KeyCtrlCaret)
		case '_':
			return legacy(KeyCtrlUnderscore)
		}
		return ext(string(r))
	}
	if ctrl {
		return ext(string(r))
	}
	if shift {
		u := unicode.ToUpper(r)
		if u == r { // shifted punctuation: the shifted symbol depends on the layout
			return ext(string(r))
		}
		r = u
	}
	return Key{Type: KeyRunes, Runes: []rune{r}, Alt: alt}, true
}
