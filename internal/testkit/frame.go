package testkit

import (
	"fmt"
	"strings"
	"sync/atomic"
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
	return frameProblems(frame, w, h, nil)
}

// rowMemo keeps the rows of the last frame that passed the row checks, by
// position, for one width: a driver draws a frame after every message
// and most rows do not change, so they are not measured again.
type rowMemo struct {
	w    int
	good []string
}

// rowsChecked counts the rows frameProblems measured (tests).
var rowsChecked atomic.Int64

// frameProblems is FrameProblems; rows that memo saw pass at the same
// position are not checked again (memo may be nil).
func frameProblems(frame string, w, h int, memo *rowMemo) []string {
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
	if memo != nil && memo.w != w {
		memo.w, memo.good = w, nil
	}
	for i, r := range rows {
		if memo != nil && i < len(memo.good) && memo.good[i] == r {
			continue
		}
		rowsChecked.Add(1)
		bad := len(out)
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
		if memo != nil && len(out) == bad {
			for len(memo.good) <= i {
				memo.good = append(memo.good, "\x00") // no real row
			}
			memo.good[i] = r
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
			case '_', 'P': // APC (kitty graphics "G…") or DCS (sixel) … ST
				j := strings.Index(row[i+2:], "\x1b\\")
				if j < 0 {
					return fmt.Sprintf("unterminated APC/DCS at byte %d", i)
				}
				body := row[i+2 : i+2+j]
				if row[i+1] == '_' && !strings.HasPrefix(body, "G") {
					return fmt.Sprintf("unexpected APC %q at byte %d", body[:min(len(body), 16)], i)
				}
				if row[i+1] == 'P' && !strings.Contains(body[:min(len(body), 16)], "q") {
					return fmt.Sprintf("unexpected DCS %q at byte %d", body[:min(len(body), 16)], i)
				}
				i += 2 + j + 1
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
