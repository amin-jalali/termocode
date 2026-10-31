// Package rtl lays out right-to-left text (Persian, Arabic, Hebrew) for
// a terminal that does not do it.
//
// Most terminals (kitty, Alacritty, xterm, Ghostty, Windows Terminal;
// WezTerm unless bidi_enabled is set) draw each code point in the cell
// it was written to: Arabic-script letters show in memory (logical)
// order, so a word reads backwards, and unjoined. termocode draws its own
// UI (the AI chat, the Markdown preview), so it lays out RTL text itself:
//
//   - the paragraph direction is the first strong letter (UAX #9 P2/P3);
//   - text is wrapped in logical order, then each visual line is shaped
//     (Shape: presentation forms, lam-alef ligature) and reordered with a
//     subset of the Unicode Bidirectional Algorithm (W1-W7, N1-N2, I1-I2,
//     L1-L2, mirrored brackets; no explicit embeddings or bracket pairs);
//   - every Glyph remembers the logical runes it came from, so a copy
//     gives the original text.
//
// Terminals that reorder by themselves would undo the work, see Mode.
package rtl

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/bidi"
)

// Dir is a paragraph direction.
type Dir int8

const (
	LTR Dir = iota
	RTL
)

func (d Dir) String() string {
	if d == RTL {
		return "rtl"
	}
	return "ltr"
}

func class(r rune) bidi.Class {
	p, _ := bidi.LookupRune(r)
	return p.Class()
}

// IsRTL reports a strong right-to-left rune (Hebrew, Arabic script).
func IsRTL(r rune) bool {
	if r < 0x0590 {
		return false
	}
	c := class(r)
	return c == bidi.R || c == bidi.AL
}

// HasRTL reports whether s has any right-to-left letter.
func HasRTL(s string) bool {
	for _, r := range s {
		if IsRTL(r) {
			return true
		}
	}
	return false
}

// BaseDir is the direction of the first strong letter of s (LTR when
// there is none).
func BaseDir(s string) Dir {
	for _, r := range s {
		if r < 0x80 {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				return LTR
			}
			continue
		}
		switch class(r) {
		case bidi.L:
			return LTR
		case bidi.R, bidi.AL:
			return RTL
		}
	}
	return LTR
}

// Glyph is one drawn cluster of a visual line: the text to draw (a
// letter in its shaped form plus its marks) and the logical runes
// [Lo, Hi) it stands for. A lam-alef ligature stands for two letters; a
// dropped ZWNJ belongs to the glyph before it.
type Glyph struct {
	Text   string
	Lo, Hi int
}

// Text joins the glyphs' text.
func Text(gs []Glyph) string {
	n := 0
	for _, g := range gs {
		n += len(g.Text)
	}
	b := make([]byte, 0, n)
	for _, g := range gs {
		b = append(b, g.Text...)
	}
	return string(b)
}

// Pick returns the logical runes of rs that the glyphs stand for, in
// logical order: the original text of a (visual) selection.
func Pick(rs []rune, gs []Glyph) string {
	in := make([]bool, len(rs))
	for _, g := range gs {
		for i := max(g.Lo, 0); i < g.Hi && i < len(rs); i++ {
			in[i] = true
		}
	}
	var out []rune
	for i, ok := range in {
		if ok {
			out = append(out, rs[i])
		}
	}
	return string(out)
}

// Visual is s laid out as one line with paragraph direction dir.
func Visual(s string, dir Dir) string {
	return Text(Line([]rune(s), dir, nil))
}

// cluster is a base rune with the marks and joiner controls after it.
type cluster struct {
	base   rune
	text   string // drawn text: shaped base + marks
	lo, hi int
	atom   int
	zwnj   bool // ends with a ZWNJ: does not join the next letter
	marks  bool
}

// invisible are format controls that are not drawn (they only steer
// joining or direction, which is done here).
func invisible(r rune) bool {
	switch {
	case r == zwnj, r == zwj, r == 0x200E, r == 0x200F, r == 0x061C,
		r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}

// follower: a rune that belongs to the cluster before it.
func follower(r rune) bool {
	return invisible(r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r)
}

// shapeClusters splits rs into clusters (atoms never share one) and
// shapes the Arabic letters.
func shapeClusters(rs []rune, atoms []int) []cluster {
	atomAt := func(i int) int {
		if i < len(atoms) {
			return atoms[i]
		}
		return 0
	}
	var cs []cluster
	for i, r := range rs {
		a := atomAt(i)
		if n := len(cs); n > 0 && follower(r) && cs[n-1].atom == a {
			c := &cs[n-1]
			c.hi = i + 1
			if r == zwnj {
				c.zwnj = true
			}
			if !invisible(r) {
				c.text += string(r)
				c.marks = true
			}
			continue
		}
		c := cluster{base: r, lo: i, hi: i + 1, atom: a, zwnj: r == zwnj}
		if !invisible(r) {
			c.text = string(r)
		}
		cs = append(cs, c)
	}
	out := cs[:0:0]
	for k := 0; k < len(cs); k++ {
		c := cs[k]
		f, ok := forms[c.base]
		if !ok {
			out = append(out, c)
			continue
		}
		var prev, next *cluster
		if k > 0 && cs[k-1].atom == c.atom {
			prev = &cs[k-1]
		}
		if k+1 < len(cs) && cs[k+1].atom == c.atom {
			next = &cs[k+1]
		}
		p := prev != nil && !prev.zwnj && joinsAfter(prev.base)
		marks := c.text[len(string(c.base)):]
		if c.base == lam && next != nil && !c.zwnj && !c.marks {
			if lig, ok := lamAlef[next.base]; ok {
				if p {
					lig++
				}
				out = append(out, cluster{base: next.base, text: string(lig) + next.text[len(string(next.base)):],
					lo: c.lo, hi: next.hi, atom: c.atom, zwnj: next.zwnj})
				k++
				continue
			}
		}
		n := next != nil && !c.zwnj && joinsAfter(c.base) && joinsBefore(next.base)
		idx := 0
		switch {
		case p && n:
			idx = 3
		case p:
			idx = 1
		case n:
			idx = 2
		}
		if f[idx] == 0 {
			idx = map[int]int{3: 1, 2: 0, 1: 0}[idx]
		}
		if f[idx] != 0 {
			c.text = string(f[idx]) + marks
		}
		out = append(out, c)
	}
	return out
}

// Line shapes and reorders one visual line of logical runes rs with
// paragraph direction dir. atoms (may be nil) groups runs of runes with
// the same id > 0 into units that move as a whole and keep their inner
// left-to-right order (inline code). The glyphs come in drawing order,
// left to right.
func Line(rs []rune, dir Dir, atoms []int) []Glyph {
	cs := shapeClusters(rs, atoms)
	// Units: one cluster, or a whole atom.
	type unit struct {
		from, to int
		prepend  bool
	} // clusters [from, to)
	var us []unit
	var cls []bidi.Class
	for k := 0; k < len(cs); {
		j := k + 1
		if cs[k].atom > 0 {
			for j < len(cs) && cs[j].atom == cs[k].atom {
				j++
			}
			us = append(us, unit{from: k, to: j})
			cls = append(cls, bidi.ON)
			k = j
			continue
		}
		// Prepend marks belong to the following grapheme, not the
		// preceding one. Keep them ahead of its already shaped letter.
		for j < len(cs) && cs[j].atom == cs[k].atom && unicode.Is(unicode.Prepended_Concatenation_Mark, cs[j-1].base) {
			j++
		}
		us = append(us, unit{from: k, to: j, prepend: j > k+1})
		cls = append(cls, class(cs[j-1].base))
		k = j
	}
	ord, lv := reorder(cls, dir)
	out := make([]Glyph, 0, len(cs))
	for _, ui := range ord {
		u := us[ui]
		if u.prepend {
			var text strings.Builder
			for _, c := range cs[u.from:u.to] {
				text.WriteString(c.text)
			}
			out = append(out, Glyph{text.String(), cs[u.from].lo, cs[u.to-1].hi})
			continue
		}
		if u.to-u.from == 1 && cs[u.from].atom == 0 {
			c := cs[u.from]
			t := c.text
			if lv[ui]%2 == 1 {
				if m, ok := mirror[c.base]; ok {
					t = string(m) + t[len(string(c.base)):]
				}
			}
			out = append(out, Glyph{t, c.lo, c.hi})
			continue
		}
		// An atom: its own clusters in LTR order.
		inner := make([]bidi.Class, 0, u.to-u.from)
		for k := u.from; k < u.to; k++ {
			inner = append(inner, class(cs[k].base))
		}
		io, il := reorder(inner, LTR)
		for _, k := range io {
			c := cs[u.from+k]
			t := c.text
			if il[k]%2 == 1 {
				if m, ok := mirror[c.base]; ok {
					t = string(m) + t[len(string(c.base)):]
				}
			}
			out = append(out, Glyph{t, c.lo, c.hi})
		}
	}
	return out
}

func isNeutral(c bidi.Class) bool {
	switch c {
	case bidi.B, bidi.S, bidi.WS, bidi.ON, bidi.BN:
		return true
	}
	return false
}

// reorder resolves the levels of a line of classes (base direction dir)
// and returns the visual order of the indices and the levels.
func reorder(orig []bidi.Class, dir Dir) (order []int, levels []int) {
	n := len(orig)
	base := 0
	sos := bidi.L
	if dir == RTL {
		base, sos = 1, bidi.R
	}
	c := make([]bidi.Class, n)
	for i, x := range orig {
		switch x {
		case bidi.Control, bidi.LRO, bidi.RLO, bidi.LRE, bidi.RLE, bidi.PDF,
			bidi.LRI, bidi.RLI, bidi.FSI, bidi.PDI:
			x = bidi.BN
		}
		c[i] = x
	}
	// W1: a mark takes the class before it.
	for i := range c {
		if c[i] == bidi.NSM {
			if i == 0 {
				c[i] = sos
			} else {
				c[i] = c[i-1]
			}
		}
	}
	// W2, W3.
	last := sos
	for i := range c {
		switch c[i] {
		case bidi.L, bidi.R, bidi.AL:
			last = c[i]
		case bidi.EN:
			if last == bidi.AL {
				c[i] = bidi.AN
			}
		}
	}
	for i := range c {
		if c[i] == bidi.AL {
			c[i] = bidi.R
		}
	}
	// W4.
	for i := 1; i+1 < n; i++ {
		switch {
		case c[i] == bidi.ES && c[i-1] == bidi.EN && c[i+1] == bidi.EN:
			c[i] = bidi.EN
		case c[i] == bidi.CS && c[i-1] == c[i+1] && (c[i-1] == bidi.EN || c[i-1] == bidi.AN):
			c[i] = c[i-1]
		}
	}
	// W5: terminators next to a European number.
	for i := 0; i < n; {
		if c[i] != bidi.ET {
			i++
			continue
		}
		j := i
		for j < n && c[j] == bidi.ET {
			j++
		}
		if (i > 0 && c[i-1] == bidi.EN) || (j < n && c[j] == bidi.EN) {
			for k := i; k < j; k++ {
				c[k] = bidi.EN
			}
		}
		i = j
	}
	// W6.
	for i := range c {
		switch c[i] {
		case bidi.ES, bidi.ET, bidi.CS:
			c[i] = bidi.ON
		}
	}
	// W7.
	last = sos
	for i := range c {
		switch c[i] {
		case bidi.L, bidi.R:
			last = c[i]
		case bidi.EN:
			if last == bidi.L {
				c[i] = bidi.L
			}
		}
	}
	// N1, N2.
	strong := func(x bidi.Class) bidi.Class {
		if x == bidi.L {
			return bidi.L
		}
		return bidi.R // R, EN, AN
	}
	for i := 0; i < n; {
		if !isNeutral(c[i]) {
			i++
			continue
		}
		j := i
		for j < n && isNeutral(c[j]) {
			j++
		}
		before, after := sos, sos
		if i > 0 {
			before = strong(c[i-1])
		}
		if j < n {
			after = strong(c[j])
		}
		e := sos
		if before == after {
			e = before
		}
		for k := i; k < j; k++ {
			c[k] = e
		}
		i = j
	}
	// I1, I2.
	levels = make([]int, n)
	for i := range c {
		l := base
		switch {
		case base == 0 && c[i] == bidi.R:
			l = 1
		case base == 0 && (c[i] == bidi.EN || c[i] == bidi.AN):
			l = 2
		case base == 1 && (c[i] == bidi.L || c[i] == bidi.EN || c[i] == bidi.AN):
			l = 2
		}
		levels[i] = l
	}
	// L1: separators and trailing white space go to the base level.
	trailing := true
	for i := n - 1; i >= 0; i-- {
		switch orig[i] {
		case bidi.S, bidi.B:
			levels[i] = base
			trailing = true
		case bidi.WS, bidi.BN:
			if trailing {
				levels[i] = base
			}
		default:
			trailing = false
		}
	}
	// L2.
	order = make([]int, n)
	hi, lo := 0, 1<<30
	for i, l := range levels {
		order[i] = i
		hi = max(hi, l)
		if l%2 == 1 {
			lo = min(lo, l)
		}
	}
	for l := hi; l >= lo && l > 0; l-- {
		for i := 0; i < n; {
			if levels[order[i]] < l {
				i++
				continue
			}
			j := i
			for j < n && levels[order[j]] >= l {
				j++
			}
			for a, b := i, j-1; a < b; a, b = a+1, b-1 {
				order[a], order[b] = order[b], order[a]
			}
			i = j
		}
	}
	return order, levels
}

// mirror holds the paired glyphs drawn in right-to-left runs (L4).
var mirror = map[rune]rune{
	'(': ')', ')': '(', '[': ']', ']': '[', '{': '}', '}': '{', '<': '>', '>': '<',
	'«': '»', '»': '«', '‹': '›', '›': '‹', '⁅': '⁆', '⁆': '⁅', '≤': '≥', '≥': '≤',
	'⟨': '⟩', '⟩': '⟨', '「': '」', '」': '「',
}
