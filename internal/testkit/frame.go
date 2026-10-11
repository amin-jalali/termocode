package testkit

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// CheckFrame fails t (t.Errorf) for every frame invariant that frame
// breaks. It returns true when the frame is fine.
func CheckFrame(t testing.TB, frame string, w, h int) bool {
	t.Helper()
	probs := FrameProblems(frame, w, h)
	for _, p := range probs {
		t.Errorf("frame %dx%d: %s", w, h, p)
	}
	return len(probs) == 0
}

// maxProblems caps how many problems FrameProblems lists.
const maxProblems = 8

// FrameProblems returns what is wrong with a w×h frame, or nil.
func FrameProblems(frame string, w, h int) []string {
	var out []string
	add := func(format string, args ...any) bool {
		out = append(out, fmt.Sprintf(format, args...))
		return len(out) < maxProblems
	}
	if !utf8.ValidString(frame) {
		i := firstInvalidUTF8(frame)
		add("invalid UTF-8 at byte %d: %q", i, around(frame, i))
	}
	rows := strings.Split(frame, "\n")
	if len(rows) != h {
		add("%d rows, want %d", len(rows), h)
	}
	for i, r := range rows {
		if got := ansi.StringWidth(r); got != w {
			if !add("row %d is %d cells, want %d: %q", i, got, w, ansi.Strip(r)) {
				return out
			}
		}
		if p := badControl(r); p != "" {
			if !add("row %d: %s", i, p) {
				return out
			}
		}
	}
	return out
}

func firstInvalidUTF8(s string) int {
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && n <= 1 {
			return i
		}
		i += n
	}
	return -1
}

func around(s string, i int) string {
	lo, hi := max(0, i-24), min(len(s), i+24)
	return s[lo:hi]
}

// badControl returns a description of the first control sequence in row
// that is not SGR or an OSC 8 hyperlink, or "".
func badControl(row string) string {
	for i := 0; i < len(row); i++ {
		c := row[i]
		switch {
		case c == 0x1b:
			if i+1 >= len(row) {
				return "lone ESC at end of row"
			}
			switch row[i+1] {
			case '[': // CSI: parameters, intermediates, one final byte
				j := i + 2
				for j < len(row) && row[j] >= 0x20 && row[j] <= 0x3f {
					j++
				}
				if j >= len(row) || row[j] < 0x40 || row[j] > 0x7e {
					return fmt.Sprintf("unterminated CSI at byte %d: %q", i, around(row, i))
				}
				if row[j] != 'm' {
					return fmt.Sprintf("non-SGR CSI %q at byte %d", row[i:j+1], i)
				}
				i = j
			case ']': // OSC … BEL or ST
				j := i + 2
				for j < len(row) && row[j] != 0x07 && !(row[j] == 0x1b && j+1 < len(row) && row[j+1] == '\\') {
					j++
				}
				if j >= len(row) {
					return fmt.Sprintf("unterminated OSC at byte %d: %q", i, around(row, i))
				}
				body := row[i+2 : j]
				if !strings.HasPrefix(body, "8;") {
					return fmt.Sprintf("unexpected OSC %q at byte %d", body, i)
				}
				if row[j] == 0x1b {
					j++
				}
				i = j
			default:
				return fmt.Sprintf("unexpected escape %q at byte %d", row[i:min(len(row), i+2)], i)
			}
		case c < 0x20 || c == 0x7f:
			return fmt.Sprintf("control byte 0x%02x at byte %d: %q", c, i, around(row, i))
		case c >= 0x80:
			r, n := utf8.DecodeRuneInString(row[i:])
			if r >= 0x80 && r <= 0x9f {
				return fmt.Sprintf("C1 control U+%04X at byte %d", r, i)
			}
			i += n - 1
		}
	}
	return ""
}
