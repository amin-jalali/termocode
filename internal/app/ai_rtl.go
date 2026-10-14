package app

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/rtl"
)

// Right-to-left text (Persian, Arabic) in the AI chat (N5).
//
// Text is wrapped in logical order; each row is then shaped and put in
// reading order (internal/rtl), and rows of an RTL paragraph are right
// aligned. A row's selectable text (aiSel.text) is what is drawn; its
// aiBidi maps every drawn glyph back to the logical runes, so a copy
// gives the text as written. Code blocks stay LTR.

// rtlMode is the resolved rtl setting (ApplyDisplaySettings).
var rtlMode = rtl.Termocode

// rtlOn reports whether termocode lays out RTL text itself.
func rtlOn() bool { return rtlMode == rtl.Termocode }

// RTLTerminalModes are the escape sequences cmd/termocode writes when the
// UI (or a session client) starts and ends: ask the terminal not to
// reorder text (BDSM explicit mode) while termocode lays it out. Empty
// unless the rtl setting resolves to termocode for this terminal.
func RTLTerminalModes() (enter, leave string) {
	if rtl.Resolve(loadSettings().RTL, os.Getenv) != rtl.Termocode {
		return "", ""
	}
	return rtl.ExplicitMode, rtl.ImplicitMode
}

// rtlPara returns whether a paragraph needs the RTL layout, and its
// direction.
func rtlPara(src string) (bool, rtl.Dir) {
	if !rtlOn() || !rtl.HasRTL(src) {
		return false, rtl.LTR
	}
	return true, rtl.BaseDir(src)
}

// aiBidi maps a drawn row back to its logical text.
type aiBidi struct {
	rs []rune      // logical text of the row
	gs []rtl.Glyph // drawn glyphs, left to right
}

// prefixed adds text drawn (and written) before the row, such as a list
// marker.
func (b *aiBidi) prefixed(p string) *aiBidi {
	if b == nil || p == "" {
		return b
	}
	pr := []rune(p)
	n := len(pr)
	gs := make([]rtl.Glyph, 0, n+len(b.gs))
	for i, r := range pr {
		gs = append(gs, rtl.Glyph{Text: string(r), Lo: i, Hi: i + 1})
	}
	for _, g := range b.gs {
		gs = append(gs, rtl.Glyph{Text: g.Text, Lo: g.Lo + n, Hi: g.Hi + n})
	}
	return &aiBidi{rs: append(pr, b.rs...), gs: gs}
}

// mirroredLead adds a lead (list marker) that is written before the row
// but drawn after it, mirrored.
func (b *aiBidi) mirroredLead(p string) *aiBidi {
	if b == nil || p == "" {
		return b
	}
	pr := []rune(p)
	n := len(pr)
	gs := make([]rtl.Glyph, 0, n+len(b.gs))
	for _, g := range b.gs {
		gs = append(gs, rtl.Glyph{Text: g.Text, Lo: g.Lo + n, Hi: g.Hi + n})
	}
	for i := n - 1; i >= 0; i-- {
		gs = append(gs, rtl.Glyph{Text: string(pr[i]), Lo: i, Hi: i + 1})
	}
	return &aiBidi{rs: append(pr, b.rs...), gs: gs}
}

// mirrorSpans reverses spans and their text: a lead drawn on the right.
func mirrorSpans(sp []mdSpan) []mdSpan {
	out := make([]mdSpan, 0, len(sp))
	for i := len(sp) - 1; i >= 0; i-- {
		rs := []rune(sp[i].text)
		for a, z := 0, len(rs)-1; a < z; a, z = a+1, z-1 {
			rs[a], rs[z] = rs[z], rs[a]
		}
		out = append(out, mdSpan{text: string(rs), st: sp[i].st, code: sp[i].code})
	}
	return out
}

// slice is the logical text of the glyphs drawn in cells [from, to) of
// a row whose text starts at column x.
func (b *aiBidi) slice(x, from, to int) string {
	col := x
	var pick []rtl.Glyph
	prevIn := false
	for _, g := range b.gs {
		w := runewidth.StringWidth(g.Text)
		in := prevIn
		if w > 0 {
			in = col < to && col+w > from
		}
		if in {
			pick = append(pick, g)
		}
		prevIn = in
		col += w
	}
	return rtl.Pick(b.rs, pick)
}

// bidiRow lays out one logical row of plain text.
func bidiRow(row string, dir rtl.Dir) (string, *aiBidi) {
	rs := []rune(row)
	gs := rtl.Line(rs, dir, nil)
	return rtl.Text(gs), &aiBidi{rs: rs, gs: gs}
}

// bidiSpans lays out one wrapped row of styled spans: the spans in
// drawing order and the glyph map. Inline code moves as one unit.
func bidiSpans(row []mdSpan, dir rtl.Dir) ([]mdSpan, *aiBidi) {
	var rs []rune
	var idx, atoms []int
	for k, sp := range row {
		for _, r := range sp.text {
			rs = append(rs, r)
			idx = append(idx, k)
			a := 0
			if sp.code {
				a = k + 1
			}
			atoms = append(atoms, a)
		}
	}
	gs := rtl.Line(rs, dir, atoms)
	var out []mdSpan
	last := -1
	for _, g := range gs {
		k := 0
		if g.Lo < len(idx) {
			k = idx[g.Lo]
		}
		if k == last && len(out) > 0 {
			out[len(out)-1].text += g.Text
			continue
		}
		out = append(out, mdSpan{text: g.Text, st: row[k].st, code: row[k].code})
		last = k
	}
	return out, &aiBidi{rs: rs, gs: gs}
}

// aiBidiWrap word-wraps plain text (newlines kept) like wrapPlain, with
// the selection joins of each row, and lays out the rows of
// RTL paragraphs: the drawn rows, their joins, glyph maps (nil = drawn
// as written) and whether a row is right aligned.
func aiBidiWrap(text string, w int) (rows, joins []string, maps []*aiBidi, right []bool) {
	for _, para := range strings.Split(text, "\n") {
		r := wrapPlain(para, w)
		js := aiJoins(para, r)
		on, dir := rtlPara(para)
		for k := range r {
			var bm *aiBidi
			if on {
				r[k], bm = bidiRow(r[k], dir)
			}
			maps = append(maps, bm)
			right = append(right, on && dir == rtl.RTL)
		}
		rows = append(rows, r...)
		joins = append(joins, js...)
	}
	return rows, joins, maps, right
}

// aiInputPara is the input line (between newlines) that holds index i.
func aiInputPara(input []rune, i int) string {
	a, b := min(i, len(input)), min(i, len(input))
	for a > 0 && input[a-1] != '\n' {
		a--
	}
	for b < len(input) && input[b] != '\n' {
		b++
	}
	return string(input[a:b])
}

// aiInputBidiRow draws one input row of an RTL line in reading order.
// co is the caret's offset in the row (-1 = no caret here): the glyph of
// the rune at the caret is drawn as the cursor; at the end of the row an
// extra cursor cell goes where the text ends (left for RTL). Returns the
// styled row and its width.
func aiInputBidiRow(rs []rune, dir rtl.Dir, co int, txt, cur lipgloss.Style) (string, int) {
	gs := rtl.Line(rs, dir, nil)
	var b strings.Builder
	w := 0
	end := co >= len(rs)
	if end && dir == rtl.RTL {
		b.WriteString(cur.Render(" "))
		w++
	}
	var run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			b.WriteString(txt.Render(run.String()))
			run.Reset()
		}
	}
	for _, g := range gs {
		w += runewidth.StringWidth(g.Text)
		if co >= 0 && !end && co >= g.Lo && co < g.Hi && g.Text != "" {
			flush()
			b.WriteString(cur.Render(g.Text))
			continue
		}
		run.WriteString(g.Text)
	}
	flush()
	if end && dir == rtl.LTR {
		b.WriteString(cur.Render(" "))
		w++
	}
	return b.String(), w
}
