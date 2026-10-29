package testkit

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Helpers for the wide-character scenarios (SCN-11): CJK, emoji (also
// ZWJ sequences and flags), combining marks and right-to-left text.

// Sample texts the SCN-11 tests put everywhere.
const (
	wideCJK   = "中文日本語"
	wideDev   = "👩‍💻" // woman + ZWJ + laptop: one glyph, two cells
	wideFlag  = "🇮🇷"  // two regional indicators: one flag
	wideMark  = "é"
	wideFa    = "سلام دنیا"
	wideMixed = "中文 " + wideDev + " " + wideFlag + " caf" + wideMark + " " + wideFa + " ok"
)

// wideFiles is a workspace whose names and texts are full of wide glyphs.
func wideFiles() map[string]string {
	return map[string]string{
		"中文目录/日本語のとても長いファイル名です.md":                         "# 日本語\n\n中文 text\n",
		"emoji-" + wideDev + "-" + wideFlag + "-notes.txt": wideMixed + "\n",
		"caf" + wideMark + "-naïve.txt":                    "accents\n",
		"فارسی-and-English-名前.txt":                         wideFa + " and English\n",
		"a.txt":                                            "alpha\n",
	}
}

// glyphProblems lists places in a frame where a glyph was cut: a
// replacement character, a combining mark or ZWJ with no letter before
// it, a ZWJ at the end of a glyph run, or half a flag. Width and UTF-8
// are already checked by FrameProblems.
func glyphProblems(frame string) []string {
	var out []string
	for y, row := range strings.Split(ansi.Strip(frame), "\n") {
		rs := []rune(row)
		ri := 0 // regional indicators in a row
		for i, r := range rs {
			prev := ' '
			if i > 0 {
				prev = rs[i-1]
			}
			next := ' '
			if i+1 < len(rs) {
				next = rs[i+1]
			}
			bad := ""
			switch {
			case r == unicode.ReplacementChar:
				bad = "replacement character"
			case unicode.In(r, unicode.Mn, unicode.Me) && (unicode.IsSpace(prev) || prev == '…' || isBoxRune(prev)):
				bad = "combining mark with no base"
			case r == 0x200D && (unicode.IsSpace(prev) || prev == '…' || unicode.IsSpace(next) || next == '…' || i+1 == len(rs)):
				bad = "ZWJ cut off"
			}
			if r >= 0x1F1E6 && r <= 0x1F1FF {
				ri++
			} else {
				if ri%2 == 1 {
					bad = "half a flag"
				}
				ri = 0
			}
			if bad != "" {
				out = append(out, fmt.Sprintf("row %d col %d: %s: %q", y, i, bad, row))
				break
			}
		}
		if ri%2 == 1 {
			out = append(out, fmt.Sprintf("row %d: half a flag at the end: %q", y, row))
		}
	}
	return out
}

func isBoxRune(r rune) bool { return r >= 0x2500 && r <= 0x257F }

// checkGlyphs fails the test for every cut glyph in the current frame.
func checkGlyphs(d *Driver, where string) {
	d.T.Helper()
	for _, p := range glyphProblems(d.Frame()) {
		d.T.Errorf("%s: %s", where, p)
	}
}

// ellipsisRows returns the rows of frame that hold an ellipsis, so a
// test can make sure a truncation really happened.
func ellipsisRows(frame string) []string {
	var out []string
	for _, row := range strings.Split(ansi.Strip(frame), "\n") {
		if strings.Contains(row, "…") {
			out = append(out, row)
		}
	}
	return out
}
