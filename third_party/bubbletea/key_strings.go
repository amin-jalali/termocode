package tea

// termocode patch (terminal-compat R3): escape strings in the input.
//
// A terminal answers some queries with a string: APC (ESC _ … ST, the
// kitty graphics answer), DCS (ESC P … ST: XTVERSION, DECRQSS), OSC
// (ESC ] … BEL or ST: colour reports), PM (ESC ^) and SOS (ESC X). An
// answer to the startup probe that comes after its deadline reaches
// Bubble Tea; upstream reads it as Alt+_ followed by typed text. A
// complete string in the buffer is one escapeStringMsg instead, which no
// model handles. Without its terminator the bytes stay keys (Alt+P …).

import "fmt"

// escapeStringMsg is a terminal string (APC, DCS, OSC, PM, SOS) read
// from the input. Models never see a key for it.
type escapeStringMsg []byte

func (e escapeStringMsg) String() string { return fmt.Sprintf("%q", string(e)) }

// detectEscapeString finds a complete escape string at the start of b.
func detectEscapeString(b []byte) (bool, int, Msg) {
	if len(b) < 3 || b[0] != '\x1b' {
		return false, 0, nil
	}
	switch b[1] {
	case '_', 'P', ']', '^', 'X':
	default:
		return false, 0, nil
	}
	osc := b[1] == ']'
	for i := 2; i < len(b); i++ {
		switch b[i] {
		case '\a':
			if osc {
				return true, i + 1, escapeStringMsg(b[:i+1])
			}
		case '\x1b':
			if i+1 < len(b) && b[i+1] == '\\' {
				return true, i + 2, escapeStringMsg(b[:i+2])
			}
			// Another escape before the terminator: not a string.
			return false, 0, nil
		}
	}
	return false, 0, nil
}
