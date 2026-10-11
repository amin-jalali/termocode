package session

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Modes follows the terminal state the UI sets with escape sequences, so
// a new client terminal can be put into the same state on attach (alt
// screen, mouse tracking, bracketed paste, focus events, hidden cursor,
// cursor shape, colours, kitty keyboard flags, font size) and an old one
// can be put back to normal on detach.
//
// It watches the byte stream instead of asking the UI because the UI
// changes these at run time (the mouse mode drops to button-only while a
// text field has focus), and it works for any program in the PTY.
type Modes struct {
	st       parseState
	buf      []byte
	overflow bool

	dec   map[int]bool // DEC private modes: CSI ? n h / l
	order []int        // first-seen order, for a stable replay
	// cursor is the last DECSCUSR (CSI n SP q); "" = terminal default.
	cursor string
	osc    map[string]string // OSC 10/11/12 colour → full sequence
	kitty  []string          // pushed kitty keyboard flags (CSI > n u)
	font   int               // net OSC 1337 ChangeFontSize delta
}

type parseState uint8

const (
	stGround parseState = iota
	stEsc
	stCSI
	stOSC
	stOSCEsc
	stStr // DCS / APC / PM / SOS: skipped up to ST
	stStrEsc
)

// Long CSI / OSC sequences are skipped, not recorded.
const (
	maxCSI = 64
	maxOSC = 512
)

// NewModes returns an empty tracker (a terminal in its default state).
func NewModes() *Modes {
	return &Modes{dec: map[int]bool{}, osc: map[string]string{}}
}

// Feed scans output bytes. Sequences may be split across calls.
func (m *Modes) Feed(p []byte) {
	for _, c := range p {
		m.step(c)
	}
}

func (m *Modes) step(c byte) {
	switch m.st {
	case stGround:
		if c == 0x1b {
			m.st = stEsc
		}
	case stEsc:
		m.buf, m.overflow = m.buf[:0], false
		switch c {
		case '[':
			m.st = stCSI
		case ']':
			m.st = stOSC
		case 'P', '_', '^', 'X':
			m.st = stStr
		case 0x1b:
		default:
			m.st = stGround
		}
	case stCSI:
		switch {
		case c >= 0x40 && c <= 0x7e:
			if !m.overflow {
				m.csi(string(m.buf), c)
			}
			m.st = stGround
		case c == 0x1b:
			m.st = stEsc
		case c == 0x18 || c == 0x1a: // CAN / SUB abort
			m.st = stGround
		default:
			m.add(c, maxCSI)
		}
	case stOSC:
		switch c {
		case 0x07:
			m.endOSC()
		case 0x1b:
			m.st = stOSCEsc
		default:
			m.add(c, maxOSC)
		}
	case stOSCEsc:
		if c == '\\' {
			m.endOSC()
			return
		}
		m.st = stEsc
		m.step(c)
	case stStr:
		if c == 0x1b {
			m.st = stStrEsc
		}
	case stStrEsc:
		if c == '\\' {
			m.st = stGround
			return
		}
		m.st = stEsc
		m.step(c)
	}
}

func (m *Modes) add(c byte, limit int) {
	if len(m.buf) >= limit {
		m.overflow = true
		return
	}
	m.buf = append(m.buf, c)
}

func (m *Modes) endOSC() {
	if !m.overflow {
		m.oscDone(string(m.buf))
	}
	m.st = stGround
}

func (m *Modes) csi(params string, final byte) {
	switch {
	case strings.HasPrefix(params, "?") && (final == 'h' || final == 'l'):
		for _, f := range strings.Split(params[1:], ";") {
			n, err := strconv.Atoi(f)
			if err != nil {
				continue
			}
			if _, seen := m.dec[n]; !seen {
				m.order = append(m.order, n)
			}
			m.dec[n] = final == 'h'
		}
	case final == 'q' && strings.HasSuffix(params, " "):
		n := strings.TrimSuffix(params, " ")
		if n == "" || n == "0" {
			m.cursor = ""
		} else {
			m.cursor = "\x1b[" + params + "q"
		}
	case final == 'u' && strings.HasPrefix(params, ">"):
		if len(m.kitty) < 16 {
			m.kitty = append(m.kitty, "\x1b["+params+"u")
		}
	case final == 'u' && strings.HasPrefix(params, "<"):
		n := 1
		if v, err := strconv.Atoi(params[1:]); err == nil && v > 0 {
			n = v
		}
		m.kitty = m.kitty[:max(0, len(m.kitty)-n)]
	}
}

func (m *Modes) oscDone(data string) {
	num, rest, _ := strings.Cut(data, ";")
	switch num {
	case "10", "11", "12":
		m.osc[num] = "\x1b]" + data + "\x07"
	case "110", "111", "112":
		delete(m.osc, num[1:])
	case "1337":
		if v, ok := strings.CutPrefix(rest, "ChangeFontSize="); ok {
			if n, err := strconv.Atoi(strings.TrimPrefix(v, "+")); err == nil {
				m.font += n
			}
		}
	}
}

// altModes switch the screen buffer; they go first on attach (other modes
// and the repaint belong to the alt screen) and last on detach.
var altModes = map[int]bool{47: true, 1047: true, 1049: true}

// Replay returns the sequences that put a fresh terminal into the state
// the UI has set. The UI repaints right after it.
func (m *Modes) Replay() []byte {
	var b strings.Builder
	for _, n := range m.order {
		if altModes[n] && m.dec[n] {
			fmt.Fprintf(&b, "\x1b[?%dh", n)
		}
	}
	for _, n := range m.order {
		if altModes[n] {
			continue
		}
		if m.dec[n] {
			fmt.Fprintf(&b, "\x1b[?%dh", n)
		} else {
			fmt.Fprintf(&b, "\x1b[?%dl", n)
		}
	}
	b.WriteString(m.cursor)
	for _, k := range sortedKeys(m.osc) {
		b.WriteString(m.osc[k])
	}
	for _, k := range m.kitty {
		b.WriteString(k)
	}
	if m.font != 0 {
		fmt.Fprintf(&b, "\x1b]1337;ChangeFontSize=%d\x07", m.font)
	}
	return []byte(b.String())
}

// Reset returns the sequences that undo Replay: a terminal the session
// leaves (another client took over) goes back to its normal state.
func (m *Modes) Reset() []byte {
	var b strings.Builder
	for _, n := range m.order {
		if altModes[n] || !m.dec[n] || n == 25 {
			continue
		}
		fmt.Fprintf(&b, "\x1b[?%dl", n)
	}
	if len(m.kitty) > 0 {
		fmt.Fprintf(&b, "\x1b[<%du", len(m.kitty))
	}
	if m.cursor != "" {
		b.WriteString("\x1b[0 q")
	}
	for _, k := range sortedKeys(m.osc) {
		b.WriteString("\x1b]1" + k + "\x07")
	}
	if m.font != 0 {
		fmt.Fprintf(&b, "\x1b]1337;ChangeFontSize=%d\x07", -m.font)
	}
	b.WriteString("\x1b[?25h")
	for _, n := range m.order {
		if altModes[n] && m.dec[n] {
			fmt.Fprintf(&b, "\x1b[?%dl", n)
		}
	}
	return []byte(b.String())
}

// Mode reports whether DEC private mode n is set.
func (m *Modes) Mode(n int) bool { return m.dec[n] }

func sortedKeys(mp map[string]string) []string {
	ks := make([]string, 0, len(mp))
	for k := range mp {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
