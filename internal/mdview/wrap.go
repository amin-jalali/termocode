package mdview

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// piece is a measured run of text inside one row.
type piece struct {
	text string
	a    attrs
	link string
	w    int
}

// word is a run of pieces with no break opportunity inside. space says
// a space (styled sp) comes before it; brk is a hard line break.
type word struct {
	ps    []piece
	w     int
	space bool
	sp    attrs
	brk   bool
}

// words splits spans at spaces. Code spans stay whole, and text glued to
// them (a trailing comma) stays on the same row.
func words(spans []span) []word {
	var out []word
	cur := word{}
	pendSpace, pendAttrs := false, attrs{}
	flush := func() {
		if len(cur.ps) > 0 {
			cur.space, cur.sp = pendSpace, pendAttrs
			out = append(out, cur)
			pendSpace = false
		}
		cur = word{}
	}
	add := func(s span, t string) {
		if t == "" {
			return
		}
		w := ansi.StringWidth(t)
		cur.ps = append(cur.ps, piece{text: t, a: s.a, link: s.link, w: w})
		cur.w += w
	}
	for _, s := range spans {
		if s.text == "\n" {
			flush()
			out = append(out, word{brk: true})
			pendSpace = false
			continue
		}
		if s.code {
			add(s, s.text)
			continue
		}
		t := strings.ReplaceAll(s.text, "\t", " ")
		for i, part := range strings.Split(t, " ") {
			if i > 0 {
				flush()
				pendSpace, pendAttrs = true, s.a
				if s.link == "" {
					pendAttrs.under = false
				}
			}
			add(s, part)
		}
	}
	flush()
	return out
}

// wrap lays words out in rows of at most width cells. A word wider than
// a row is cut by cells.
func wrap(ws []word, width int) [][]piece {
	width = max(width, 1)
	var rows [][]piece
	var cur []piece
	curW := 0
	push := func() {
		rows = append(rows, cur)
		cur, curW = nil, 0
	}
	for _, wd := range ws {
		if wd.brk {
			push()
			continue
		}
		sp := 0
		if curW > 0 && wd.space {
			sp = 1
		}
		switch {
		case curW+sp+wd.w <= width:
			if sp == 1 {
				cur = append(cur, piece{text: " ", a: wd.sp, w: 1})
			}
			cur = append(cur, wd.ps...)
			curW += sp + wd.w
		case wd.w <= width:
			push()
			cur, curW = append(cur, wd.ps...), wd.w
		default: // longer than a row: cut it
			if curW > 0 {
				push()
			}
			for _, p := range wd.ps {
				for p.w > 0 {
					room := width - curW
					if room <= 0 {
						push()
						room = width
					}
					if p.w <= room {
						cur = append(cur, p)
						curW += p.w
						break
					}
					head := ansi.Truncate(p.text, room, "")
					hw := ansi.StringWidth(head)
					if hw == 0 { // a wide rune that does not fit
						push()
						continue
					}
					cur = append(cur, piece{text: head, a: p.a, link: p.link, w: hw})
					curW += hw
					p.text = ansi.TruncateLeft(p.text, hw, "")
					p.w -= hw
				}
			}
		}
	}
	if len(cur) > 0 || len(rows) == 0 {
		push()
	}
	return rows
}

// wrapLines wraps spans into rows exactly width cells wide.
func (r *renderer) wrapLines(spans []span, width int, bg lipgloss.Color) []line {
	rows := wrap(words(spans), width)
	out := make([]line, 0, len(rows))
	for _, ps := range rows {
		out = append(out, r.row(ps, width, bg, 'l'))
	}
	return out
}

// row draws pieces padded to width with bg, aligned left, right or
// centre ('l', 'r', 'c').
func (r *renderer) row(ps []piece, width int, bg lipgloss.Color, align byte) line {
	var b strings.Builder
	w := 0
	for i := 0; i < len(ps); {
		j, t := i, strings.Builder{}
		for j < len(ps) && ps[j].a == ps[i].a && ps[j].link == ps[i].link {
			t.WriteString(ps[j].text)
			w += ps[j].w
			j++
		}
		s := r.style(ps[i].a).Render(t.String())
		if ps[i].link != "" {
			s = ansi.SetHyperlink(ps[i].link) + s + ansi.ResetHyperlink()
		}
		b.WriteString(s)
		i = j
	}
	gap := max(width-w, 0)
	switch align {
	case 'r':
		return line{r.fill(gap, bg) + b.String(), max(w, width)}
	case 'c':
		return line{r.fill(gap/2, bg) + b.String() + r.fill(gap-gap/2, bg), max(w, width)}
	}
	return line{b.String() + r.fill(gap, bg), max(w, width)}
}
