package tea

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// screenCell is one cell of the test terminal: its glyph and the SGR
// sequences in effect when it was written ("" for the right half of a
// wide glyph: cont).
type screenCell struct {
	glyph, state string
	cont         bool
}

// testScreen is a small terminal: enough of one to replay the renderer's
// output (cursor moves, SGR, erase, text) and read the cells back.
type testScreen struct {
	w, h     int
	row, col int
	state    string
	cells    [][]screenCell
}

func newTestScreen(w, h int) *testScreen {
	s := &testScreen{w: w, h: h}
	s.cells = make([][]screenCell, h)
	for y := range s.cells {
		s.cells[y] = make([]screenCell, w)
		for x := range s.cells[y] {
			s.cells[y][x] = screenCell{glyph: " "}
		}
	}
	return s
}

func (s *testScreen) erase(y, from int) {
	for x := from; x < s.w; x++ {
		s.cells[y][x] = screenCell{glyph: " ", state: s.state}
	}
}

func (s *testScreen) write(out string) {
	for i := 0; i < len(out); {
		c := out[i]
		switch {
		case c == 0x1b:
			j := i + 2
			for j < len(out) && (out[j] < 0x40 || out[j] > 0x7e) {
				j++
			}
			params, final := out[i+2:j], out[j]
			n := 1
			if v, err := strconv.Atoi(params); err == nil {
				n = v
			}
			switch final {
			case 'm':
				if params == "0" || params == "" {
					s.state = ""
				} else {
					s.state += out[i : j+1]
				}
			case 'H':
				s.row, s.col = 0, 0
				if p := strings.Split(params, ";"); len(p) == 2 {
					r, _ := strconv.Atoi(p[0])
					c, _ := strconv.Atoi(p[1])
					s.row, s.col = max(r-1, 0), max(c-1, 0)
				}
			case 'C':
				s.col = min(s.col+n, s.w-1)
			case 'A':
				s.row = max(s.row-n, 0)
			case 'K':
				s.erase(s.row, s.col)
			case 'J':
				s.erase(s.row, s.col)
				for y := s.row + 1; y < s.h; y++ {
					s.erase(y, 0)
				}
			}
			i = j + 1
		case c == '\r':
			s.col = 0
			i++
		case c == '\n':
			s.row = min(s.row+1, s.h-1)
			i++
		default:
			r, n := rune(c), 1
			if c >= 0x80 {
				r, n = decodeRune(out[i:])
			}
			_ = r
			g := out[i : i+n]
			w := ansi.StringWidth(g)
			if s.col+w <= s.w {
				s.cells[s.row][s.col] = screenCell{glyph: g, state: s.state}
				if w == 2 {
					s.cells[s.row][s.col+1] = screenCell{state: s.state, cont: true}
				}
				s.col += w
			}
			i += n
		}
	}
}

func decodeRune(s string) (rune, int) {
	for n := 1; n <= 4 && n <= len(s); n++ {
		if r := []rune(s[:n]); len(r) == 1 && r[0] != 0xfffd {
			return r[0], n
		}
	}
	return 0xfffd, 1
}

func (s *testScreen) String() string {
	var b strings.Builder
	for _, row := range s.cells {
		for _, c := range row {
			b.WriteString(c.state + "|" + c.glyph + "·")
		}
		b.WriteByte('\n')
	}
	return b.String()
}

var testStyles = []string{"", "\x1b[31m", "\x1b[1;38;2;1;2;3;48;2;9;9;9m", "\x1b[44m", "\x1b[4m"}

// randomLine is a line of exactly w cells in styled runs.
func randomLine(rng *rand.Rand, w int) string {
	glyphs := []string{"a", "b", "x", " ", "─", "▎", "界", "é", ""}
	var b strings.Builder
	col := 0
	for col < w {
		st := testStyles[rng.Intn(len(testStyles))]
		b.WriteString(st)
		for k := 1 + rng.Intn(6); k > 0 && col < w; k-- {
			g := glyphs[rng.Intn(len(glyphs))]
			if gw := ansi.StringWidth(g); col+gw > w {
				g = " "
			}
			b.WriteString(g)
			col += ansi.StringWidth(g)
		}
		if st != "" || rng.Intn(3) == 0 {
			b.WriteString("\x1b[0m")
		}
	}
	return b.String()
}

// mutate changes a few cells of line (keeping its width) or, sometimes,
// makes it something only a whole-line draw can handle.
func mutate(rng *rand.Rand, line string, w int) string {
	switch rng.Intn(8) {
	case 0:
		return randomLine(rng, w)
	case 1:
		return "short" // drawn whole, erased to the right
	case 2:
		return "\x1b[?25h" + randomLine(rng, w) // not SGR: drawn whole
	case 3:
		return line
	}
	cells, _, ok := parseLine(line)
	if !ok || len(cells) == 0 {
		return randomLine(rng, w)
	}
	k := rng.Intn(len(cells))
	c := cells[k]
	repl := "Q"
	if c.w == 2 {
		repl = "ZZ"
	}
	return line[:c.start] + testStyles[rng.Intn(len(testStyles))] + repl + "\x1b[0m" + c.state + line[c.end:]
}

func renderFrames(t *testing.T, w int, frames ...string) (*testScreen, []int) {
	t.Helper()
	out := &syncBuf{}
	r := newRenderer(out, false, 60).(*standardRenderer)
	r.altScreenActive = true
	r.width, r.height = w, 50
	var sizes []int
	for _, f := range frames {
		n := len(out.String())
		r.buf.Reset()
		r.buf.WriteString(f)
		r.flushFrame(false)
		sizes = append(sizes, len(out.String())-n)
	}
	h := strings.Count(frames[0], "\n") + 1
	s := newTestScreen(w, h)
	s.write(out.String())
	return s, sizes
}

// Drawing frame A then frame B with line updates leaves the screen as
// drawing B on its own does, over many random frames.
func TestLineUpdatesGiveTheSameScreen(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for round := 0; round < 400; round++ {
		w, h := 4+rng.Intn(30), 1+rng.Intn(6)
		a := make([]string, h)
		for y := range a {
			a[y] = randomLine(rng, w)
		}
		b := make([]string, h)
		for y := range b {
			b[y] = mutate(rng, a[y], w)
		}
		c := make([]string, h)
		for y := range c {
			c[y] = mutate(rng, b[y], w)
		}
		fa, fb, fc := strings.Join(a, "\n"), strings.Join(b, "\n"), strings.Join(c, "\n")
		got, _ := renderFrames(t, w, fa, fb, fc)
		want, _ := renderFrames(t, w, fc)
		if got.String() != want.String() {
			t.Fatalf("round %d: screens differ\nframes:\n%q\n%q\n%q\ngot:\n%s\nwant:\n%s", round, a, b, c, got, want)
		}
	}
}

// One changed cell in a long styled line sends a few bytes, not the line.
func TestLineUpdateSendsOnlyTheChange(t *testing.T) {
	const w = 120
	row := func(ch string) string {
		var b strings.Builder
		for i := 0; i < w/4; i++ {
			if i == 10 {
				b.WriteString("\x1b[38;2;200;200;200;48;2;30;30;30m" + ch + "bc \x1b[0m")
				continue
			}
			b.WriteString("\x1b[38;2;200;200;200;48;2;30;30;30mabc \x1b[0m")
		}
		return b.String()
	}
	_, sizes := renderFrames(t, w, row("a")+"\n"+row("a"), row("a")+"\n"+row("Z"))
	if sizes[1] > 80 {
		t.Fatalf("one changed cell sent %d bytes (first frame %d), want <= 80", sizes[1], sizes[0])
	}
}

// BenchmarkLineUpdate is the diff of a typical changed editor row.
func BenchmarkLineUpdate(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	old := randomLine(rng, 200)
	cells, _, _ := parseLine(old)
	c := cells[50]
	new := old[:c.start] + "Q" + old[c.end:]
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = lineUpdate(old, new, 200)
	}
}
