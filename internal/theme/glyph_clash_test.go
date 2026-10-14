package theme

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// clashingGlyphs are private-use codepoints that Nerd icons must not use.
// When the terminal font is not a Nerd Font, fontconfig draws icons from
// a fallback font; for these codepoints a common text font wins instead
// (seen in GNOME Terminal with Ubuntu Mono):
//   - U+F000–F002: legacy Mac "fi"/"fl" slots, in Ubuntu Mono itself.
//   - U+E0A0–E0D7: the Powerline block, also in Linux Libertine.
//   - U+E843: gone from Nerd Fonts 3 (the music font Bravura draws it).
var clashingGlyphs = [][2]rune{{0xf000, 0xf002}, {0xe0a0, 0xe0d7}, {0xe843, 0xe843}}

func clashes(r rune) bool {
	for _, g := range clashingGlyphs {
		if r >= g[0] && r <= g[1] {
			return true
		}
	}
	return false
}

var escapedGlyph = regexp.MustCompile(`\\u([0-9a-fA-F]{4})|\\U([0-9a-fA-F]{8})`)

// TestNoClashingNerdGlyphs: no source file draws a clashing codepoint,
// written either as the rune itself or as a \u escape.
func TestNoClashingNerdGlyphs(t *testing.T) {
	var bad []string
	for _, root := range []string{"../../internal", "../../cmd"} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && (d.Name() == "testdata" || d.Name() == "devtools") {
				return filepath.SkipDir
			}
			if d.IsDir() || strings.HasSuffix(p, "_test.go") ||
				!(strings.HasSuffix(p, ".go") || strings.HasSuffix(p, ".lua")) {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(b), "\n") {
				for _, r := range line {
					if clashes(r) {
						bad = append(bad, fmt.Sprintf("%s:%d U+%04X", p, i+1, r))
					}
				}
				for _, m := range escapedGlyph.FindAllStringSubmatch(line, -1) {
					v, _ := strconv.ParseUint(m[1]+m[2], 16, 32)
					if clashes(rune(v)) {
						bad = append(bad, fmt.Sprintf("%s:%d U+%04X", p, i+1, v))
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, b := range bad {
		t.Errorf("clashing Nerd glyph: %s", b)
	}
}
