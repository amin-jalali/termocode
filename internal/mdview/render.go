package mdview

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"

	"github.com/amin-jalali/termocode/internal/rtl"
	"github.com/amin-jalali/termocode/internal/theme"
)

// DefaultMaxColumn is the widest the reading column gets (cells). Wider
// windows get side margins instead of longer lines.
const DefaultMaxColumn = 100

// Options tune Render.
type Options struct {
	// Width is the full width of the view (cells). Every row Render
	// returns is exactly this wide.
	Width int
	// MaxColumn caps the reading column (0 = DefaultMaxColumn). Tables
	// may grow past it, up to the window minus a small margin.
	MaxColumn int
	// Image, if set, draws a local image for ![alt](dest) when the image
	// stands alone in a paragraph. It returns styled rows no wider than
	// width, or nil to show the "[image: alt]" placeholder instead.
	Image func(dest, alt string, width int) []string
	// RTL lays out right-to-left text (Persian, Arabic) here: each
	// wrapped row is shaped and put in reading order, and rows of an RTL
	// paragraph are right aligned. Code keeps its LTR order (only RTL runs
	// inside it turn). Off, the
	// text is drawn as written (for terminals that reorder by themselves).
	RTL bool
}

// Layout returns the left margin and the width of the reading column for
// a view that is width cells wide.
func Layout(width, maxCol int) (left, col int) {
	if maxCol <= 0 {
		maxCol = DefaultMaxColumn
	}
	if width < 16 {
		return 0, max(width, 1)
	}
	margin := 2
	if width >= 80 {
		margin = 4
	}
	col = min(width-2*margin, maxCol)
	return (width - col) / 2, col
}

var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

// Render renders src and returns the rows, each exactly opt.Width cells,
// with one blank row above and below the content.
func Render(src string, opt Options) []string {
	w := max(opt.Width, 1)
	left, col := Layout(w, opt.MaxColumn)
	source := []byte(src)
	doc := md.Parser().Parse(text.NewReader(source))
	r := newRenderer(source, opt)
	wide := max(col, w-4)
	body := r.blocks(doc, col, wide, false)

	blank := r.fill(w, r.p.bg)
	out := make([]string, 0, len(body)+2)
	out = append(out, blank)
	for _, l := range body {
		l.w = ansi.StringWidth(l.s) // measure: at tiny widths a block can overflow its claim
		off := left
		if l.w > col { // a wide table: keep it centred on the column
			off = max(left-(l.w-col+1)/2, 0)
			if off+l.w > w {
				off = max(w-l.w, 0)
			}
		}
		s := l.s
		if off+l.w > w {
			s = ansi.Truncate(s, w-off, "") + "\x1b[0m"
			l.w = w - off
		}
		out = append(out, r.fill(off, r.p.bg)+s+r.fill(w-off-l.w, r.p.bg))
	}
	return append(out, blank)
}

// line is one rendered row and its width in cells.
type line struct {
	s string
	w int
}

// attrs is the look of a run of text. It is comparable, so equal runs
// can be merged and their styles cached.
type attrs struct {
	fg, bg                    lipgloss.Color
	bold, ital, strike, under bool
}

// span is a run of inline text with one look. code spans never break.
type span struct {
	text string
	a    attrs
	link string
	code bool
}

type palette struct {
	bg, text, strong, muted, dim, link, codeFg, codeBg lipgloss.Color
	accent, h1Rule, rule, border, quoteBar, quoteText  lipgloss.Color
	check, h3, h4, card, cardLabel, headBg             lipgloss.Color
}

type glyphs struct {
	bullets            []string
	checked, unchecked string
	quote, cont        string
}

type renderer struct {
	src     []byte
	opt     Options
	p       palette
	g       glyphs
	base    attrs // the look of plain text in the current block
	depth   int   // list nesting
	rtlList bool  // inside a right-to-left list item: rows right aligned
	styles  map[attrs]lipgloss.Style
}

func newRenderer(src []byte, opt Options) *renderer {
	bg := theme.BgEditor
	c := theme.LG
	card := theme.Mix(bg, theme.TextPrimary, 0.05)
	if !theme.IsLight() {
		card = theme.Lerp(theme.ToRGB(bg), theme.RGB{}, 0.25).LG()
	}
	p := palette{
		bg:        c(bg),
		text:      c(theme.TextPrimary),
		strong:    c(theme.TextWhite),
		muted:     c(theme.TextMuted),
		dim:       c(theme.TextDim),
		link:      c(theme.AccentBlue),
		codeFg:    c(theme.AccentAmber),
		codeBg:    theme.Mix(bg, theme.TextPrimary, 0.10),
		accent:    c(theme.AccentBlue),
		h1Rule:    theme.Mix(theme.AccentBlue, bg, 0.35),
		rule:      c(theme.BorderSubtle),
		border:    c(theme.BorderDefault),
		quoteBar:  c(theme.AccentLavender),
		quoteText: c(theme.TextSecondary),
		check:     c(theme.GitAdded),
		h3:        c(theme.TextWhite),
		h4:        c(theme.TextSecondary),
		card:      card,
		cardLabel: c(theme.TextMuted),
		headBg:    theme.Mix(bg, theme.AccentBlue, 0.14),
	}
	g := glyphs{bullets: []string{"•", "◦", "▪"}, checked: "☑", unchecked: "☐", quote: "▎", cont: "↪"}
	if theme.CurrentIconMode == theme.IconModeASCII {
		g = glyphs{bullets: []string{"-", "*", "+"}, checked: "[x]", unchecked: "[ ]", quote: "|", cont: ">"}
	}
	r := &renderer{src: src, opt: opt, p: p, g: g, styles: map[attrs]lipgloss.Style{}}
	r.base = attrs{fg: p.text, bg: p.bg}
	return r
}

func (r *renderer) style(a attrs) lipgloss.Style {
	if st, ok := r.styles[a]; ok {
		return st
	}
	st := lipgloss.NewStyle().Foreground(a.fg).Background(a.bg).Bold(a.bold).
		Italic(a.ital).Strikethrough(a.strike).Underline(a.under)
	r.styles[a] = st
	return st
}

// fill is n cells of background bg.
func (r *renderer) fill(n int, bg lipgloss.Color) string {
	if n <= 0 {
		return ""
	}
	return r.style(attrs{bg: bg, fg: r.p.text}).Render(strings.Repeat(" ", n))
}

func (r *renderer) text(s string, a attrs) string { return r.style(a).Render(s) }

func (r *renderer) blank(width int) line { return line{r.fill(width, r.p.bg), width} }

// ── blocks ──────────────────────────────────────────────────────────────

// blocks renders the children of parent, one blank row between blocks
// (none when tight, as in a tight list item).
func (r *renderer) blocks(parent ast.Node, width, wide int, tight bool) []line {
	var out []line
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		ls := r.block(n, width, wide)
		if len(ls) == 0 {
			continue
		}
		if len(out) > 0 && !tight {
			out = append(out, r.blank(width))
		}
		out = append(out, ls...)
	}
	return out
}

func (r *renderer) block(n ast.Node, width, wide int) []line {
	switch v := n.(type) {
	case *ast.Heading:
		return r.heading(v, width)
	case *ast.Paragraph, *ast.TextBlock:
		if ls, ok := r.imageBlock(n, width); ok {
			return ls
		}
		return r.wrapLines(r.inline(n, r.base), width, r.base.bg)
	case *ast.List:
		return r.list(v, width)
	case *ast.FencedCodeBlock:
		return r.code(r.rawLines(v.Lines()), string(v.Language(r.src)), width)
	case *ast.CodeBlock:
		return r.code(r.rawLines(v.Lines()), "", width)
	case *ast.Blockquote:
		return r.quote(v, width)
	case *ast.ThematicBreak:
		return []line{{r.text(strings.Repeat("─", width), attrs{fg: r.p.rule, bg: r.p.bg}), width}}
	case *ast.HTMLBlock:
		ls := r.rawLines(v.Lines())
		if v.HasClosure() {
			ls = append(ls, string(v.ClosureLine.Value(r.src)))
		}
		var out []line
		a := attrs{fg: r.p.muted, bg: r.p.bg}
		for _, s := range ls {
			out = append(out, r.wrapLines([]span{{text: strings.TrimRight(s, "\n"), a: a}}, width, r.p.bg)...)
		}
		return out
	case *east.Table:
		return r.table(v, wide)
	}
	if n.HasChildren() {
		return r.blocks(n, width, wide, false)
	}
	return nil
}

func (r *renderer) rawLines(segs *text.Segments) []string {
	out := make([]string, 0, segs.Len())
	for i := 0; i < segs.Len(); i++ {
		s := segs.At(i)
		out = append(out, strings.TrimRight(string(s.Value(r.src)), "\r\n"))
	}
	return out
}

// heading draws one step of the heading ladder (UI review #11): H1 in the
// brightest text over an accent rule, H2 in the accent over a thin rule,
// H3 bold bright text, H4 and below bold secondary text. No markers: a
// chevron reads as "fold here".
func (r *renderer) heading(h *ast.Heading, width int) []line {
	a := attrs{bg: r.p.bg, bold: true}
	switch h.Level {
	case 1:
		a.fg = r.p.strong
	case 2:
		a.fg = r.p.accent
	case 3:
		a.fg = r.p.h3
	default:
		a.fg = r.p.h4
	}
	body := r.wrapLines(r.inline(h, a), width, r.p.bg)
	out := make([]line, 0, len(body)+1)
	for _, l := range body {
		out = append(out, line{l.s, width})
	}
	switch h.Level {
	case 1:
		out = append(out, line{r.text(strings.Repeat("━", width), attrs{fg: r.p.h1Rule, bg: r.p.bg}), width})
	case 2:
		out = append(out, line{r.text(strings.Repeat("─", width), attrs{fg: r.p.rule, bg: r.p.bg}), width})
	}
	return out
}

// imageBlock draws a paragraph that holds only images.
func (r *renderer) imageBlock(n ast.Node, width int) ([]line, bool) {
	var imgs []*ast.Image
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch v := c.(type) {
		case *ast.Image:
			imgs = append(imgs, v)
		case *ast.Text:
			if strings.TrimSpace(string(v.Value(r.src))) != "" {
				return nil, false
			}
		default:
			return nil, false
		}
	}
	if len(imgs) == 0 {
		return nil, false
	}
	var out []line
	for _, im := range imgs {
		alt := r.plain(im)
		var rows []string
		if r.opt.Image != nil {
			rows = r.opt.Image(string(im.Destination), alt, width)
		}
		if rows == nil {
			out = append(out, r.wrapLines([]span{r.imageSpan(alt, string(im.Destination))}, width, r.p.bg)...)
			continue
		}
		for _, s := range rows {
			w := ansi.StringWidth(s)
			if w > width {
				s, w = ansi.Truncate(s, width, "")+"\x1b[0m", width
			}
			out = append(out, line{s + "\x1b[0m" + r.fill(width-w, r.p.bg), width})
		}
	}
	return out, true
}

func (r *renderer) imageSpan(alt, dest string) span {
	if alt == "" {
		alt = dest
	}
	return span{text: "[image: " + alt + "]", a: attrs{fg: r.p.muted, bg: r.p.bg, ital: true}, code: true}
}

// list renders bullets (•, ◦, ▪ by depth), numbers (right-aligned) and
// task boxes, with wrapped lines hanging under the item text.
func (r *renderer) list(l *ast.List, width int) []line {
	depth := r.depth
	r.depth++
	defer func() { r.depth-- }()
	lead := 0
	if depth == 0 {
		lead = 2
	}
	numW := 0
	if l.IsOrdered() {
		last := l.Start + l.ChildCount() - 1
		numW = len(strconv.Itoa(max(last, l.Start))) + 1
	}
	var out []line
	i := 0
	for it := l.FirstChild(); it != nil; it = it.NextSibling() {
		marker, ma := "", attrs{fg: r.p.accent, bg: r.p.bg}
		switch {
		case l.IsOrdered():
			marker = strconv.Itoa(l.Start+i) + "."
			marker = strings.Repeat(" ", numW-len(marker)) + marker
			ma.fg = r.p.muted
		default:
			marker = r.g.bullets[depth%len(r.g.bullets)]
		}
		saved := r.base
		if box := taskBox(it); box != nil {
			if box.IsChecked {
				marker, ma.fg = r.g.checked, r.p.check
				r.base = attrs{fg: r.p.muted, bg: r.p.bg}
			} else {
				marker, ma.fg = r.g.unchecked, r.p.muted
			}
		}
		ind := lead + ansi.StringWidth(marker) + 1
		// A right-to-left item: text right aligned, marker on the right.
		rtlItem := false
		if r.opt.RTL {
			src := r.plain(it)
			rtlItem = rtl.HasRTL(src) && rtl.BaseDir(src) == rtl.RTL
		}
		savedRTL := r.rtlList
		r.rtlList = rtlItem
		inner := r.blocks(it, max(width-ind, 4), max(width-ind, 4), l.IsTight)
		r.rtlList = savedRTL
		r.base = saved
		if len(inner) == 0 {
			inner = []line{r.blank(max(width-ind, 4))}
		}
		if i > 0 && !l.IsTight {
			out = append(out, r.blank(width))
		}
		for k, ln := range inner {
			if rtlItem {
				post := r.fill(ind, r.p.bg)
				if k == 0 {
					mk := strings.TrimLeft(marker, " ")
					post = r.fill(1, r.p.bg) + r.text(mk, ma) + r.fill(ind-1-ansi.StringWidth(mk), r.p.bg)
				}
				out = append(out, line{ln.s + post, ind + ln.w})
				continue
			}
			pre := r.fill(ind, r.p.bg)
			if k == 0 {
				pre = r.fill(lead, r.p.bg) + r.text(marker, ma) + r.fill(1, r.p.bg)
			}
			out = append(out, line{pre + ln.s, ind + ln.w})
		}
		i++
	}
	return out
}

// taskBox returns the [ ] / [x] box of a task list item, or nil.
func taskBox(item ast.Node) *east.TaskCheckBox {
	if p := item.FirstChild(); p != nil {
		if b, ok := p.FirstChild().(*east.TaskCheckBox); ok {
			return b
		}
	}
	return nil
}

func (r *renderer) quote(q *ast.Blockquote, width int) []line {
	saved := r.base
	r.base = attrs{fg: r.p.quoteText, bg: r.p.bg, ital: true}
	inner := r.blocks(q, max(width-2, 4), max(width-2, 4), false)
	r.base = saved
	bar := r.text(r.g.quote, attrs{fg: r.p.quoteBar, bg: r.p.bg}) + r.fill(1, r.p.bg)
	out := make([]line, 0, len(inner))
	for _, l := range inner {
		out = append(out, line{bar + l.s, l.w + 2})
	}
	return out
}

// ── inline ──────────────────────────────────────────────────────────────

// inline collects the inline children of n as spans, starting from look a.
func (r *renderer) inline(n ast.Node, a attrs) []span {
	var out []span
	r.walkInline(n, a, "", &out)
	return out
}

func (r *renderer) walkInline(n ast.Node, a attrs, link string, out *[]span) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch v := c.(type) {
		case *ast.Text:
			*out = append(*out, span{text: string(v.Value(r.src)), a: a, link: link})
			switch {
			case v.HardLineBreak():
				*out = append(*out, span{text: "\n", a: a})
			case v.SoftLineBreak():
				*out = append(*out, span{text: " ", a: a, link: link})
			}
		case *ast.String:
			*out = append(*out, span{text: string(v.Value), a: a, link: link})
		case *ast.CodeSpan:
			ca := attrs{fg: r.p.codeFg, bg: r.p.codeBg, under: a.under}
			*out = append(*out, span{text: " " + r.plain(v) + " ", a: ca, link: link, code: true})
		case *ast.Emphasis:
			b := a
			if v.Level >= 2 {
				b.bold = true
				if link == "" && a.fg == r.base.fg {
					b.fg = r.p.strong
				}
			} else {
				b.ital = true
			}
			r.walkInline(v, b, link, out)
		case *east.Strikethrough:
			b := a
			b.strike, b.fg = true, r.p.muted
			r.walkInline(v, b, link, out)
		case *ast.Link:
			b := a
			b.fg, b.under = r.p.link, true
			dest := string(v.Destination)
			r.walkInline(v, b, clickable(dest), out)
			if u := shortURL(dest); u != "" && u != strings.TrimSpace(r.plain(v)) {
				*out = append(*out, span{text: " ", a: a}, span{text: u, a: attrs{fg: r.p.dim, bg: a.bg}, link: clickable(dest), code: true})
			}
		case *ast.AutoLink:
			b := a
			b.fg, b.under = r.p.link, true
			url := string(v.URL(r.src))
			*out = append(*out, span{text: string(v.Label(r.src)), a: b, link: clickable(url)})
		case *ast.Image:
			s := r.imageSpan(r.plain(v), string(v.Destination))
			s.a.bg = a.bg
			*out = append(*out, s)
		case *ast.RawHTML:
			var b strings.Builder
			for i := 0; i < v.Segments.Len(); i++ {
				seg := v.Segments.At(i)
				b.Write(seg.Value(r.src))
			}
			*out = append(*out, span{text: b.String(), a: attrs{fg: r.p.muted, bg: a.bg}})
		case *east.TaskCheckBox:
			// drawn as the list marker
		default:
			r.walkInline(c, a, link, out)
		}
	}
}

// plain is the text of n's inline children without any styling.
func (r *renderer) plain(n ast.Node) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch v := c.(type) {
			case *ast.Text:
				b.Write(v.Value(r.src))
				if v.SoftLineBreak() || v.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				b.Write(v.Value)
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return b.String()
}

// clickable returns dest when a terminal can open it from an OSC 8 link
// (web and mail links only), with control bytes removed.
func clickable(dest string) string {
	l := strings.ToLower(dest)
	if !strings.HasPrefix(l, "http://") && !strings.HasPrefix(l, "https://") && !strings.HasPrefix(l, "mailto:") {
		return ""
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, dest)
}

// shortURL is the dimmed hint after a web link's text: the URL without
// its scheme, cut to 40 cells. Relative links get none.
func shortURL(dest string) string {
	u := clickable(dest)
	if u == "" || strings.HasPrefix(strings.ToLower(u), "mailto:") {
		return ""
	}
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	u = strings.TrimPrefix(strings.TrimSuffix(u, "/"), "www.")
	return ansi.Truncate(u, 40, "…")
}
