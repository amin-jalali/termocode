package bench

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// screen is a small VT emulator: the cells a user sees after the program's
// output, so a bench matches what is drawn and not the byte stream (a
// program redraws only the cells that changed). It knows cursor moves,
// erase, insert and delete, scroll regions and the alternate screen; colors
// and modes are ignored. It answers the terminal queries programs wait on
// (DA1, DSR) through reply.
type screen struct {
	rows, cols int
	main, alt  [][]string // a cell is "" (blank), a grapheme, or "\x00" (right half of a wide one)
	cur        *[][]string
	x, y       int
	wrap       bool // the cursor is past the last column: the next print wraps
	top, bot   int  // the scroll region, both rows included
	sx, sy     int  // saved cursor
	p          *ansi.Parser
	pending    []byte
	reply      func([]byte)
}

func newScreen(rows, cols int, reply func([]byte)) *screen {
	s := &screen{rows: rows, cols: cols, p: ansi.NewParser(), reply: reply}
	s.main, s.alt = blankGrid(rows, cols), blankGrid(rows, cols)
	s.cur = &s.main
	s.bot = rows - 1
	return s
}

func blankGrid(rows, cols int) [][]string {
	g := make([][]string, rows)
	for i := range g {
		g[i] = make([]string, cols)
	}
	return g
}

// Text is the screen, one line per row, without trailing blanks.
func (s *screen) Text() string {
	lines := make([]string, 0, s.rows)
	for _, row := range *s.cur {
		var b strings.Builder
		for _, c := range row {
			switch c {
			case "":
				b.WriteByte(' ')
			case "\x00":
			default:
				b.WriteString(c)
			}
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// Write feeds program output; a sequence cut between two writes waits for
// the rest.
func (s *screen) Write(b []byte) {
	data := append(s.pending, b...)
	s.pending = nil
	for len(data) > 0 {
		if data[0] >= utf8.RuneSelf && !utf8.FullRune(data) {
			s.pending = append([]byte(nil), data...)
			return
		}
		seq, width, n, state := ansi.DecodeSequence(data, ansi.NormalState, s.p)
		if n <= 0 {
			return
		}
		if n == len(data) && state != ansi.NormalState {
			s.pending = append([]byte(nil), data...)
			return
		}
		data = data[n:]
		switch c := seq[0]; {
		case c == ansi.ESC && len(seq) > 1:
			switch seq[1] {
			case '[':
				s.csi()
			case ']', 'P', '_', '^', 'X': // OSC, DCS, APC, PM, SOS: nothing on screen
			default:
				s.esc()
			}
		case c < 0x20 || c == 0x7f:
			s.control(c)
		case c >= 0x80 && c < 0xa0: // C1 controls
		default:
			s.print(string(seq), width)
		}
	}
}

func (s *screen) grid() [][]string { return *s.cur }

func (s *screen) print(g string, w int) {
	if w <= 0 {
		// a combining mark joins the cell before the cursor
		if x := s.x - 1; x >= 0 && !s.wrap {
			s.grid()[s.y][x] += g
		} else if s.wrap {
			s.grid()[s.y][s.cols-1] += g
		}
		return
	}
	if s.wrap {
		s.wrap = false
		s.x = 0
		s.lineFeed()
	}
	if s.x+w > s.cols {
		s.x = 0
		s.lineFeed()
	}
	row := s.grid()[s.y]
	row[s.x] = g
	for i := 1; i < w && s.x+i < s.cols; i++ {
		row[s.x+i] = "\x00"
	}
	if s.x+w >= s.cols {
		s.x = s.cols - 1
		s.wrap = true
	} else {
		s.x += w
	}
}

func (s *screen) control(c byte) {
	switch c {
	case '\r':
		s.x, s.wrap = 0, false
	case '\n', '\v', '\f':
		s.wrap = false
		s.lineFeed()
	case '\b':
		if s.x > 0 {
			s.x--
		}
		s.wrap = false
	case '\t':
		s.x = min((s.x/8+1)*8, s.cols-1)
	}
}

func (s *screen) lineFeed() {
	if s.y == s.bot {
		s.scrollUp(1)
	} else if s.y < s.rows-1 {
		s.y++
	}
}

func (s *screen) reverseIndex() {
	if s.y == s.top {
		s.scrollDown(1)
	} else if s.y > 0 {
		s.y--
	}
}

// scrollUp moves the region's rows up by n; blank rows come in at the bottom.
func (s *screen) scrollUp(n int) {
	g := s.grid()
	n = min(n, s.bot-s.top+1)
	copy(g[s.top:s.bot+1], g[s.top+n:s.bot+1])
	for i := s.bot - n + 1; i <= s.bot; i++ {
		g[i] = make([]string, s.cols)
	}
}

func (s *screen) scrollDown(n int) {
	g := s.grid()
	n = min(n, s.bot-s.top+1)
	copy(g[s.top+n:s.bot+1], g[s.top:s.bot+1-n])
	for i := s.top; i < s.top+n; i++ {
		g[i] = make([]string, s.cols)
	}
}

func (s *screen) esc() {
	switch ansi.Cmd(s.p.Command()).Final() {
	case '7':
		s.sx, s.sy = s.x, s.y
	case '8':
		s.x, s.y, s.wrap = s.sx, s.sy, false
	case 'D':
		s.lineFeed()
	case 'E':
		s.x = 0
		s.lineFeed()
	case 'M':
		s.reverseIndex()
	case 'c':
		*s = *newScreen(s.rows, s.cols, s.reply)
	}
}

func (s *screen) param(i, def int) int {
	v, _ := s.p.Param(i, def)
	if v == 0 && def > 0 {
		return def
	}
	return v
}

func (s *screen) moveTo(x, y int) {
	s.x = max(0, min(x, s.cols-1))
	s.y = max(0, min(y, s.rows-1))
	s.wrap = false
}

func (s *screen) clear(row, from, to int) {
	r := s.grid()[row]
	for i := max(from, 0); i < min(to, s.cols); i++ {
		r[i] = ""
	}
}

func (s *screen) csi() {
	cmd := ansi.Cmd(s.p.Command())
	if cmd.Intermediate() != 0 {
		return // DECSCUSR, DECRQM and the like: no cells change
	}
	if cmd.Prefix() == '?' {
		s.privateMode(cmd.Final())
		return
	}
	if cmd.Prefix() != 0 {
		return // CSI > c, CSI > q, CSI = c ...: queries a bench need not answer
	}
	n := s.param(0, 1)
	switch cmd.Final() {
	case 'A':
		s.moveTo(s.x, s.y-n)
	case 'B', 'e':
		s.moveTo(s.x, s.y+n)
	case 'C', 'a':
		s.moveTo(s.x+n, s.y)
	case 'D':
		s.moveTo(s.x-n, s.y)
	case 'E':
		s.moveTo(0, s.y+n)
	case 'F':
		s.moveTo(0, s.y-n)
	case 'G', '`':
		s.moveTo(n-1, s.y)
	case 'd':
		s.moveTo(s.x, n-1)
	case 'H', 'f':
		s.moveTo(s.param(1, 1)-1, n-1)
	case 'J':
		switch s.param(0, 0) {
		case 0:
			s.clear(s.y, s.x, s.cols)
			for y := s.y + 1; y < s.rows; y++ {
				s.clear(y, 0, s.cols)
			}
		case 1:
			s.clear(s.y, 0, s.x+1)
			for y := 0; y < s.y; y++ {
				s.clear(y, 0, s.cols)
			}
		case 2, 3:
			for y := 0; y < s.rows; y++ {
				s.clear(y, 0, s.cols)
			}
		}
	case 'K':
		switch s.param(0, 0) {
		case 0:
			s.clear(s.y, s.x, s.cols)
		case 1:
			s.clear(s.y, 0, s.x+1)
		case 2:
			s.clear(s.y, 0, s.cols)
		}
	case 'X':
		s.clear(s.y, s.x, s.x+n)
	case '@':
		r := s.grid()[s.y]
		n = min(n, s.cols-s.x)
		copy(r[s.x+n:], r[s.x:s.cols-n])
		s.clear(s.y, s.x, s.x+n)
	case 'P':
		r := s.grid()[s.y]
		n = min(n, s.cols-s.x)
		copy(r[s.x:], r[s.x+n:])
		s.clear(s.y, s.cols-n, s.cols)
	case 'L', 'M':
		if s.y < s.top || s.y > s.bot {
			return
		}
		top := s.top
		s.top = s.y
		if cmd.Final() == 'L' {
			s.scrollDown(n)
		} else {
			s.scrollUp(n)
		}
		s.top = top
		s.x = 0
	case 'S':
		s.scrollUp(n)
	case 'T':
		s.scrollDown(n)
	case 'b': // REP: repeat the last printed cell
		if x := s.x - 1; x >= 0 && !s.wrap {
			last := s.grid()[s.y][x]
			for i := 0; i < n && last != "" && last != "\x00"; i++ {
				s.print(last, 1)
			}
		}
	case 'r':
		top, bot := s.param(0, 1)-1, s.param(1, s.rows)-1
		if top < bot && bot < s.rows {
			s.top, s.bot = top, bot
			s.moveTo(0, 0)
		}
	case 's':
		s.sx, s.sy = s.x, s.y
	case 'u':
		s.x, s.y, s.wrap = s.sx, s.sy, false
	case 'c': // DA1: who are you? A VT220 with ANSI color.
		s.reply([]byte("\x1b[?62;22c"))
	case 'n':
		switch s.param(0, 0) {
		case 5:
			s.reply([]byte("\x1b[0n"))
		case 6:
			s.reply([]byte("\x1b[" + strconv.Itoa(s.y+1) + ";" + strconv.Itoa(s.x+1) + "R"))
		}
	}
}

func (s *screen) privateMode(final byte) {
	if final != 'h' && final != 'l' {
		return
	}
	on := final == 'h'
	for i := range s.p.Params() {
		switch s.param(i, 0) {
		case 1049, 1047, 47:
			if on && s.cur != &s.alt {
				s.sx, s.sy = s.x, s.y
				s.alt = blankGrid(s.rows, s.cols)
				s.cur = &s.alt
			} else if !on && s.cur == &s.alt {
				s.cur = &s.main
				s.x, s.y, s.wrap = s.sx, s.sy, false
			}
		}
	}
}
