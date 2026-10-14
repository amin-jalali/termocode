package mdview

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/x/ansi"
	east "github.com/yuin/goldmark/extension/ast"

	"github.com/amin-jalali/termocode/internal/rtl"
	"github.com/amin-jalali/termocode/internal/theme"
)

// code draws a fenced block as a tinted card: the language label on the
// top row (right-aligned), syntax colours when chroma knows the language,
// two cells of padding, and long lines soft-wrapped with a ↪ in the gutter.
func (r *renderer) code(src []string, lang string, width int) []line {
	lang = strings.TrimSpace(strings.SplitN(lang, " ", 2)[0])
	card := r.p.card
	inner := max(width-4, 1)
	out := make([]line, 0, len(src)+2)

	top := r.fill(width, card)
	if lang != "" {
		label := ansi.Truncate(lang, max(width-4, 1), "…")
		lw := ansi.StringWidth(label)
		top = r.fill(width-lw-2, card) + r.text(label, attrs{fg: r.p.cardLabel, bg: card, ital: true}) + r.fill(2, card)
	}
	out = append(out, line{top, width})

	cont := r.text(r.g.cont, attrs{fg: r.p.dim, bg: card}) + r.fill(1, card)
	pad := r.fill(2, card)
	for _, ps := range r.highlight(src, lang) {
		ws := []word{{ps: ps, w: sumW(ps)}}
		var text strings.Builder
		for _, p := range ps {
			text.WriteString(p.text)
		}
		turn := r.opt.RTL && rtl.HasRTL(text.String())
		for k, row := range wrap(ws, inner) {
			if turn { // code stays LTR; a Persian comment reads right to left
				row = bidiPieces(row, rtl.LTR)
			}
			l := r.row(row, inner, card, 'l')
			pre := pad
			if k > 0 {
				pre = cont
			}
			out = append(out, line{pre + l.s + pad, width})
		}
	}
	return append(out, line{r.fill(width, card), width})
}

func sumW(ps []piece) int {
	w := 0
	for _, p := range ps {
		w += p.w
	}
	return w
}

// highlight splits code into rows of coloured pieces (tabs expanded).
func (r *renderer) highlight(src []string, lang string) [][]piece {
	for i, l := range src {
		src[i] = expandTabs(l)
	}
	for len(src) > 0 && strings.TrimSpace(src[len(src)-1]) == "" {
		src = src[:len(src)-1]
	}
	plain := attrs{fg: r.p.text, bg: r.p.card}
	rows := make([][]piece, len(src))
	var lx chroma.Lexer
	if lang != "" {
		lx = lexers.Get(lang)
	}
	var it chroma.Iterator
	if lx != nil {
		it, _ = chroma.Coalesce(lx).Tokenise(nil, strings.Join(src, "\n"))
	}
	if it == nil {
		for i, l := range src {
			if l != "" {
				rows[i] = []piece{{text: l, a: plain, w: ansi.StringWidth(l)}}
			}
		}
		return rows
	}
	row := 0
	for tok := it(); tok != chroma.EOF; tok = it() {
		a := attrs{fg: theme.LG(syntaxColor(tok.Type)), bg: r.p.card, ital: tok.Type.InCategory(chroma.Comment)}
		for i, part := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				row++
			}
			if part != "" && row < len(rows) {
				rows[row] = append(rows[row], piece{text: part, a: a, w: ansi.StringWidth(part)})
			}
		}
	}
	return rows
}

func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	col := 0
	for _, c := range s {
		if c == '\t' {
			n := 4 - col%4
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(c)
		col++
	}
	return b.String()
}

func syntaxColor(tt chroma.TokenType) theme.Color256 {
	switch {
	case tt.InCategory(chroma.Comment):
		return theme.SyntaxComment
	case tt == chroma.KeywordType:
		return theme.SyntaxType
	case tt == chroma.KeywordNamespace || tt == chroma.KeywordDeclaration:
		return theme.SyntaxControl
	case tt.InCategory(chroma.Keyword):
		return theme.SyntaxKeyword
	case tt.InSubCategory(chroma.LiteralString):
		return theme.SyntaxString
	case tt.InSubCategory(chroma.LiteralNumber):
		return theme.SyntaxNumber
	case tt == chroma.NameFunction || tt == chroma.NameFunctionMagic:
		return theme.SyntaxFunction
	case tt == chroma.NameBuiltin || tt == chroma.NameClass || tt == chroma.NameNamespace:
		return theme.SyntaxType
	case tt == chroma.NameConstant || tt == chroma.NameBuiltinPseudo:
		return theme.SyntaxConstant
	case tt.InCategory(chroma.Operator) || tt == chroma.Punctuation:
		return theme.SyntaxOperator
	case tt == chroma.NameTag:
		return theme.SyntaxKeyword
	case tt.InCategory(chroma.Name):
		return theme.SyntaxVariable
	}
	return theme.TextPrimary
}

// table draws a GFM table with box-drawing borders, a tinted bold header
// row and the column alignment from the delimiter row. Columns shrink
// (cells wrap) when the table is wider than wide.
func (r *renderer) table(t *east.Table, wide int) []line {
	type cell struct {
		spans []span
		align byte
	}
	var rows [][]cell
	ncol := 0
	for rn := t.FirstChild(); rn != nil; rn = rn.NextSibling() {
		_, head := rn.(*east.TableHeader)
		a := attrs{fg: r.p.text, bg: r.p.bg}
		if head {
			a = attrs{fg: r.p.strong, bg: r.p.headBg, bold: true}
		}
		var row []cell
		for cn := rn.FirstChild(); cn != nil; cn = cn.NextSibling() {
			al := byte('l')
			if tc, ok := cn.(*east.TableCell); ok {
				switch tc.Alignment {
				case east.AlignRight:
					al = 'r'
				case east.AlignCenter:
					al = 'c'
				}
			}
			row = append(row, cell{spans: r.inline(cn, a), align: al})
		}
		ncol = max(ncol, len(row))
		rows = append(rows, row)
	}
	if ncol == 0 {
		return nil
	}
	colW := make([]int, ncol)
	for _, row := range rows {
		for c, cl := range row {
			w := 0
			for _, wd := range words(cl.spans) {
				if wd.space && w > 0 {
					w++
				}
				w += wd.w
			}
			colW[c] = max(colW[c], w)
		}
	}
	total := ncol + 1
	for c := range colW {
		colW[c] = max(colW[c], 1)
		total += colW[c] + 2
	}
	for total > wide {
		j := 0
		for c := range colW {
			if colW[c] > colW[j] {
				j = c
			}
		}
		if colW[j] <= 3 {
			break
		}
		colW[j]--
		total--
	}

	bd := attrs{fg: r.p.border, bg: r.p.bg}
	rule := func(l, m, rr string) line {
		var b strings.Builder
		b.WriteString(l)
		for c, w := range colW {
			if c > 0 {
				b.WriteString(m)
			}
			b.WriteString(strings.Repeat("─", w+2))
		}
		b.WriteString(rr)
		return line{r.text(b.String(), bd), total}
	}
	out := []line{rule("┌", "┬", "┐")}
	for ri, row := range rows {
		bg := r.p.bg
		if ri == 0 {
			bg = r.p.headBg
		}
		bar := r.text("│", attrs{fg: r.p.border, bg: bg})
		cells := make([][][]piece, ncol)
		h := 1
		for c := range colW {
			if c < len(row) {
				cells[c] = wrap(words(row[c].spans), colW[c])
			}
			h = max(h, len(cells[c]))
		}
		for k := 0; k < h; k++ {
			var b strings.Builder
			b.WriteString(r.text("│", bd))
			for c, w := range colW {
				if c > 0 {
					b.WriteString(bar)
				}
				var ps []piece
				al := byte('l')
				if c < len(row) {
					al = row[c].align
				}
				if k < len(cells[c]) {
					ps = cells[c][k]
				}
				if c < len(row) && r.opt.RTL { // a cell of RTL text: its own reading order
					if src := spansText(row[c].spans); rtl.HasRTL(src) {
						dir := rtl.BaseDir(src)
						ps = bidiPieces(ps, dir)
						if dir == rtl.RTL && al == 'l' {
							al = 'r'
						}
					}
				}
				b.WriteString(r.fill(1, bg) + r.row(ps, w, bg, al).s + r.fill(1, bg))
			}
			b.WriteString(r.text("│", bd))
			out = append(out, line{b.String(), total})
		}
		if ri == 0 && len(rows) > 1 {
			out = append(out, rule("├", "┼", "┤"))
		}
	}
	return append(out, rule("└", "┴", "┘"))
}
