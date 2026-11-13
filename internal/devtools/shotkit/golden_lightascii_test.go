package shotkit

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
)

// lightASCIICases are SCM, Databases, Containers and the AI panel in the
// light theme with ASCII icons together (SCN-34), at 150x42 and 80x24.
func lightASCIICases() []goldenCase {
	var out []goldenCase
	base := []Scene{
		withName(SCM(), "scm-light-ascii"),
		withName(Databases(), "db-light-ascii"),
		withName(Containers(), "containers-light-ascii"),
		{Name: "ai-chat-light-ascii", Env: aiEnv, Steps: WithEditor(Act("palette:ai-open-chat"), Act("aidemo"), Pause(600*time.Millisecond))},
	}
	for _, sc := range base {
		sc.Theme = "catppuccin-latte"
		sc.Icons = "ascii"
		for _, sz := range [][2]int{{150, 42}, {80, 24}} {
			out = append(out, goldenCase{fmt.Sprintf("%s-%dx%d", sc.Name, sz[0], sz[1]), sc, sz[0], sz[1]})
		}
	}
	return out
}

var sgrRe = regexp.MustCompile("\x1b\\[([0-9;]*)m")

type sgrCell struct {
	fg, bg [3]float64
	hasFG  bool
	hasBG  bool
	r      rune
}

// parseCells reads a frame with 24-bit colours into cells that have both a
// foreground and a background colour.
func parseCells(frame string) []sgrCell {
	var out []sgrCell
	var fg, bg [3]float64
	var hasFG, hasBG bool
	pos := 0
	emit := func(text string) {
		for _, r := range text {
			if r == '\n' || r == '\r' {
				continue
			}
			out = append(out, sgrCell{fg, bg, hasFG, hasBG, r})
		}
	}
	for _, m := range sgrRe.FindAllStringSubmatchIndex(frame, -1) {
		emit(frame[pos:m[0]])
		pos = m[1]
		p := strings.Split(frame[m[2]:m[3]], ";")
		for i := 0; i < len(p); i++ {
			n, _ := strconv.Atoi(p[i])
			switch {
			case n == 0 || p[i] == "":
				hasFG, hasBG = false, false
			case n == 39:
				hasFG = false
			case n == 49:
				hasBG = false
			case (n == 38 || n == 48) && i+4 < len(p) && p[i+1] == "2":
				var c [3]float64
				for k := 0; k < 3; k++ {
					v, _ := strconv.Atoi(p[i+2+k])
					c[k] = float64(v)
				}
				if n == 38 {
					fg, hasFG = c, true
				} else {
					bg, hasBG = c, true
				}
				i += 4
			}
		}
	}
	emit(frame[pos:])
	return out
}

func luminance(c [3]float64) float64 {
	f := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c[0]) + 0.7152*f(c[1]) + 0.0722*f(c[2])
}

func contrast(a, b [3]float64) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// latteSyntax are the Catppuccin Latte token colours of the editor's code
// (Type/String/Number): upstream colours, 2.3 to 3:1 on the light editor.
// Code text in them is not judged here; the interface text is.
var latteSyntax = map[[3]float64]bool{{223, 142, 29}: true, {64, 160, 43}: true, {254, 100, 11}: true}

// TestLightASCIIFramesAreReadable checks the committed light+ASCII frames:
// no Nerd Font private-use glyph is left (ASCII icons are used), and every
// run of three or more letters or digits in the interface has a contrast of
// at least 2.8:1 with its background. Muted placeholders sit just above it
// (2.84 "Ask AI anything…"); a single dim glyph such as a history dot is a
// mark, not text, and a code token in a Latte syntax colour is not judged.
func TestLightASCIIFramesAreReadable(t *testing.T) {
	for _, c := range lightASCIICases() {
		t.Run(c.name, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(goldenDir, c.name+".ans"))
			if err != nil {
				t.Fatalf("no golden yet (go test ./internal/devtools/shotkit -update): %v", err)
			}
			cells := parseCells(string(b))
			for _, cell := range cells {
				if (cell.r >= 0xE000 && cell.r <= 0xF8FF) || cell.r >= 0xF0000 {
					t.Fatalf("private-use icon glyph %U in an ASCII-icon frame", cell.r)
				}
			}
			for i := 0; i < len(cells); {
				j := i
				for j < len(cells) && cells[j].hasFG && cells[j].hasBG && cells[j].fg == cells[i].fg && cells[j].bg == cells[i].bg {
					j++
				}
				if j == i {
					i++
					continue
				}
				run, alnum := "", 0
				for _, x := range cells[i:j] {
					run += string(x.r)
					if unicode.IsLetter(x.r) || unicode.IsDigit(x.r) {
						alnum++
					}
				}
				if r := contrast(cells[i].fg, cells[i].bg); alnum >= 3 && r < 2.8 && !latteSyntax[cells[i].fg] {
					t.Errorf("low contrast %.2f:1 on %q", r, run)
				}
				i = j
			}
		})
	}
}
