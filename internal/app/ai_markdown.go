package app

import (
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Chat Markdown: a small renderer for AI answers in the chat panel.
// glamour targets whole documents with its own margins and colours; the
// chat needs exact cell widths, theme tokens, clickable code-block
// buttons and cheap re-renders while a reply streams, so this renders
// the common subset itself:
//
//	# headings, paragraphs with **bold** *italic* `code` ~~strike~~ [links](…)
//	- / * / + / 1. lists (nested, hanging indent), > quotes, --- rules,
//	```lang fenced code: syntax colours (chroma) on a darker card with a
//	language label and Copy / Apply buttons, | tables | kept verbatim.
//
// Every returned line is exactly width cells wide (styled), with buttons
// recorded in panel-local columns.

// mdSpan is a run of text with one style.
type mdSpan struct {
	text string
	st   lipgloss.Style
	code bool // inline code: never broken across lines
}

// mdStyles are the derived styles for one render (one theme lookup).
type mdStyles struct {
	bg                     theme.Color256
	text, bold, ital, dim  lipgloss.Style
	link, strike, code     lipgloss.Style
	h1, h2, h3             lipgloss.Style
	bullet, quoteBar, rule lipgloss.Style
	cardBg, cardHead       lipgloss.Color
	cardLabel, cardButton  lipgloss.Style
	syntax                 func(tt chroma.TokenType) lipgloss.Style
}

func newMDStyles(bg theme.Color256) mdStyles {
	b := theme.LG(bg)
	st := func(c theme.Color256) lipgloss.Style {
		return lipgloss.NewStyle().Background(b).Foreground(theme.LG(c))
	}
	card := theme.Mix(bg, theme.BgEditor, 0.6)
	if !theme.IsLight() {
		card = theme.Lerp(theme.ToRGB(bg), theme.RGB{}, 0.28).LG()
	}
	head := theme.Mix(bg, theme.TextPrimary, 0.08)
	s := mdStyles{
		bg:         bg,
		text:       st(theme.TextPrimary),
		bold:       st(theme.TextWhite).Bold(true),
		ital:       st(theme.TextPrimary).Italic(true),
		dim:        st(theme.TextDim),
		link:       st(theme.AccentBlue).Underline(true),
		strike:     st(theme.TextMuted).Strikethrough(true),
		code:       lipgloss.NewStyle().Background(theme.Mix(bg, theme.TextPrimary, 0.12)).Foreground(theme.LG(theme.AccentAmber)),
		h1:         st(theme.BorderFocus).Bold(true),
		h2:         st(theme.TextWhite).Bold(true),
		h3:         st(theme.TextSecondary).Bold(true),
		bullet:     st(theme.AIAccent),
		quoteBar:   st(theme.AIAccent),
		rule:       st(theme.BorderSubtle),
		cardBg:     card,
		cardHead:   head,
		cardLabel:  lipgloss.NewStyle().Background(head).Foreground(theme.LG(theme.TextMuted)),
		cardButton: lipgloss.NewStyle().Background(head).Foreground(theme.LG(theme.AIAccent)).Bold(true),
	}
	s.syntax = func(tt chroma.TokenType) lipgloss.Style {
		c := theme.TextPrimary
		switch {
		case tt.InCategory(chroma.Comment):
			c = theme.SyntaxComment
		case tt == chroma.KeywordType:
			c = theme.SyntaxType
		case tt == chroma.KeywordNamespace || tt == chroma.KeywordDeclaration:
			c = theme.SyntaxControl
		case tt.InCategory(chroma.Keyword):
			c = theme.SyntaxKeyword
		case tt.InSubCategory(chroma.LiteralString):
			c = theme.SyntaxString
		case tt.InSubCategory(chroma.LiteralNumber):
			c = theme.SyntaxNumber
		case tt == chroma.NameFunction || tt == chroma.NameFunctionMagic:
			c = theme.SyntaxFunction
		case tt == chroma.NameBuiltin || tt == chroma.NameClass || tt == chroma.NameNamespace:
			c = theme.SyntaxType
		case tt == chroma.NameConstant || tt == chroma.NameBuiltinPseudo:
			c = theme.SyntaxConstant
		case tt.InCategory(chroma.Operator) || tt == chroma.Punctuation:
			c = theme.SyntaxOperator
		case tt.InCategory(chroma.Name):
			c = theme.SyntaxVariable
		}
		return lipgloss.NewStyle().Background(card).Foreground(theme.LG(c))
	}
	return s
}

// renderChatMarkdown renders one assistant message. indent is the left
// margin (cells), width the full panel width; msgIdx tags the code-block
// buttons ("copy:<msg>:<block>", "apply:<msg>:<block>"); streaming hides
// the buttons of an unfinished block.
func renderChatMarkdown(text string, width, indent, msgIdx int, streaming bool) []aiLine {
	s := newMDStyles(theme.BgPanel)
	inner := width - indent - 1
	if inner < 8 {
		inner = 8
	}
	var out []aiLine
	pad := func(spans []mdSpan) aiLine {
		var b strings.Builder
		w := 0
		b.WriteString(theme.Bg(s.bg).Render(strings.Repeat(" ", indent)))
		w += indent
		for _, sp := range spans {
			b.WriteString(sp.st.Render(sp.text))
			w += runewidth.StringWidth(sp.text)
		}
		if w < width {
			b.WriteString(theme.Bg(s.bg).Render(strings.Repeat(" ", width-w)))
		}
		return aiLine{styled: b.String()}
	}
	blank := func() { out = append(out, pad(nil)) }

	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	block := 0
	var para []string
	flushPara := func() {
		if len(para) == 0 {
			return
		}
		for _, l := range wrapSpans(parseInline(strings.Join(para, " "), s, s.text), inner) {
			out = append(out, pad(l))
		}
		para = nil
	}
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		trim := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(trim, "```"):
			flushPara()
			lang := strings.TrimSpace(strings.TrimPrefix(trim, "```"))
			var code []string
			closed := false
			for i++; i < len(lines); i++ {
				if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
					closed = true
					break
				}
				code = append(code, lines[i])
			}
			out = append(out, renderCodeCard(code, lang, s, width, indent, msgIdx, block, closed || !streaming)...)
			block++
		case trim == "":
			flushPara()
			if len(out) > 0 && out[len(out)-1].styled != "" && !isBlankAILine(out[len(out)-1]) {
				blank()
			}
		case strings.HasPrefix(trim, "#"):
			flushPara()
			level := len(trim) - len(strings.TrimLeft(trim, "#"))
			title := strings.TrimSpace(trim[level:])
			hs := s.h3
			switch level {
			case 1:
				hs = s.h1
			case 2:
				hs = s.h2
			}
			for _, l := range wrapSpans(parseInline(title, s, hs), inner) {
				out = append(out, pad(l))
			}
		case trim == "---" || trim == "***" || trim == "___":
			flushPara()
			out = append(out, pad([]mdSpan{{text: strings.Repeat("─", inner), st: s.rule}}))
		case strings.HasPrefix(trim, ">"):
			flushPara()
			body := strings.TrimSpace(strings.TrimPrefix(trim, ">"))
			for _, l := range wrapSpans(parseInline(body, s, s.ital.Foreground(theme.LG(theme.TextSecondary))), inner-2) {
				out = append(out, pad(append([]mdSpan{{text: "▎ ", st: s.quoteBar}}, l...)))
			}
		case strings.HasPrefix(trim, "|"):
			flushPara()
			out = append(out, pad([]mdSpan{{text: runewidth.Truncate(trim, inner, "…"), st: s.dim}}))
		default:
			if marker, body, ok := listItem(raw); ok {
				flushPara()
				lead := strings.Repeat("  ", listDepth(raw))
				mw := runewidth.StringWidth(lead + marker + " ")
				wrapped := wrapSpans(parseInline(body, s, s.text), inner-mw)
				for k, l := range wrapped {
					head := mdSpan{text: strings.Repeat(" ", mw), st: s.text}
					if k == 0 {
						ms := s.bullet
						if strings.HasSuffix(marker, ".") {
							ms = s.dim
						}
						out = append(out, pad(append([]mdSpan{{text: lead, st: s.text}, {text: marker + " ", st: ms}}, l...)))
						continue
					}
					out = append(out, pad(append([]mdSpan{head}, l...)))
				}
				continue
			}
			para = append(para, trim)
		}
	}
	flushPara()
	for len(out) > 0 && isBlankAILine(out[len(out)-1]) {
		out = out[:len(out)-1]
	}
	return out
}

func isBlankAILine(l aiLine) bool {
	return l.styled != "" && strings.TrimSpace(stripANSI(l.styled)) == ""
}

// listItem recognises "- x", "* x", "+ x" and "12. x" (after indentation).
func listItem(raw string) (marker, body string, ok bool) {
	t := strings.TrimLeft(raw, " \t")
	if len(t) >= 2 && (t[0] == '-' || t[0] == '*' || t[0] == '+') && t[1] == ' ' {
		bullet := "•"
		if theme.CurrentIconMode == theme.IconModeASCII {
			bullet = "-"
		}
		return bullet, strings.TrimSpace(t[2:]), true
	}
	n := 0
	for n < len(t) && t[n] >= '0' && t[n] <= '9' {
		n++
	}
	if n > 0 && n+1 < len(t) && (t[n] == '.' || t[n] == ')') && t[n+1] == ' ' {
		return t[:n] + ".", strings.TrimSpace(t[n+2:]), true
	}
	return "", "", false
}

func listDepth(raw string) int {
	sp := len(raw) - len(strings.TrimLeft(raw, " \t"))
	return sp / 2
}

// parseInline splits text into styled spans.
func parseInline(text string, s mdStyles, base lipgloss.Style) []mdSpan {
	var out []mdSpan
	var cur strings.Builder
	bold, ital, strike := false, false, false
	style := func() lipgloss.Style {
		st := base
		if bold {
			st = st.Bold(true).Foreground(theme.LG(theme.TextWhite))
		}
		if ital {
			st = st.Italic(true)
		}
		if strike {
			st = s.strike
		}
		return st
	}
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, mdSpan{text: cur.String(), st: style()})
			cur.Reset()
		}
	}
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		next := func(k int) rune {
			if i+k < len(rs) {
				return rs[i+k]
			}
			return 0
		}
		switch {
		case r == '`':
			j := i + 1
			for j < len(rs) && rs[j] != '`' {
				j++
			}
			if j < len(rs) {
				flush()
				out = append(out, mdSpan{text: " " + string(rs[i+1:j]) + " ", st: s.code, code: true})
				i = j
				continue
			}
		case (r == '*' && next(1) == '*') || (r == '_' && next(1) == '_'):
			flush()
			bold = !bold
			i++
			continue
		case r == '~' && next(1) == '~':
			flush()
			strike = !strike
			i++
			continue
		case r == '*' || (r == '_' && (i == 0 || !isWordRune(rs[i-1]) || !isWordRune(next(1)))):
			// A lone * / _ toggles italics unless it sits inside a word
			// (snake_case) or is a spaced operator ("a * b").
			if r == '*' && (next(1) == ' ' || next(1) == 0) && ital == false {
				break
			}
			flush()
			ital = !ital
			continue
		case r == '[':
			if j := indexRune(rs, ']', i+1); j > 0 && j+1 < len(rs) && rs[j+1] == '(' {
				if k := indexRune(rs, ')', j+2); k > 0 {
					flush()
					out = append(out, mdSpan{text: string(rs[i+1 : j]), st: s.link})
					i = k
					continue
				}
			}
		}
		cur.WriteRune(r)
	}
	flush()
	return out
}

func isWordRune(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func indexRune(rs []rune, r rune, from int) int {
	for i := from; i < len(rs); i++ {
		if rs[i] == r {
			return i
		}
	}
	return -1
}

// wrapSpans word-wraps spans to width cells. Inline code chunks are kept
// whole unless they alone are wider than the line.
func wrapSpans(spans []mdSpan, width int) [][]mdSpan {
	if width < 4 {
		width = 4
	}
	type word struct {
		parts []mdSpan
		w     int
	}
	// Split into words (space-separated), keeping styles; a code span is
	// one word.
	var words []word
	var cur word
	var spaceStyle lipgloss.Style
	pendingSpace := false
	push := func() {
		if len(cur.parts) > 0 {
			words = append(words, cur)
		}
		cur = word{}
	}
	for _, sp := range spans {
		if sp.code {
			// Inline code sticks to the word around it ("`x`," stays
			// together) and is never broken at its inner spaces.
			cur.parts = append(cur.parts, sp)
			cur.w += runewidth.StringWidth(sp.text)
			continue
		}
		fields := strings.Split(sp.text, " ")
		for k, f := range fields {
			if k > 0 {
				push()
				pendingSpace = true
				spaceStyle = sp.st
			}
			if f != "" {
				cur.parts = append(cur.parts, mdSpan{text: f, st: sp.st})
				cur.w += runewidth.StringWidth(f)
			}
		}
	}
	push()
	_ = pendingSpace
	var lines [][]mdSpan
	var line []mdSpan
	lw := 0
	for _, w := range words {
		for w.w > width { // hard-break an over-long word
			head := runewidth.Truncate(joinParts(w.parts), width-lw, "")
			if head == "" || lw > 0 {
				if len(line) > 0 {
					lines = append(lines, line)
				}
				line, lw = nil, 0
				head = runewidth.Truncate(joinParts(w.parts), width, "")
			}
			line = append(line, mdSpan{text: head, st: w.parts[0].st})
			lines = append(lines, line)
			line, lw = nil, 0
			rest := strings.TrimPrefix(joinParts(w.parts), head)
			w = word{parts: []mdSpan{{text: rest, st: w.parts[len(w.parts)-1].st, code: w.parts[0].code}}, w: runewidth.StringWidth(rest)}
		}
		if w.w == 0 {
			continue
		}
		need := w.w
		if lw > 0 {
			need++
		}
		if lw+need > width && lw > 0 {
			lines = append(lines, line)
			line, lw = nil, 0
			need = w.w
		}
		if lw > 0 {
			line = append(line, mdSpan{text: " ", st: spaceStyle})
		}
		line = append(line, w.parts...)
		lw += need
	}
	if len(line) > 0 {
		lines = append(lines, line)
	}
	return lines
}

func joinParts(ps []mdSpan) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(p.text)
	}
	return b.String()
}

// renderCodeCard draws a fenced code block as a darker card: a header row
// with the language and (when finished) Copy / Apply buttons, then the
// highlighted code, hard-wrapped to the card width.
func renderCodeCard(code []string, lang string, s mdStyles, width, indent, msgIdx, block int, done bool) []aiLine {
	cardW := width - indent - 1
	if cardW < 10 {
		cardW = 10
	}
	edge := theme.Bg(s.bg)
	left := edge.Render(strings.Repeat(" ", indent))
	right := func(used int) string {
		if used < width {
			return edge.Render(strings.Repeat(" ", width-used))
		}
		return ""
	}
	var out []aiLine

	// Header row.
	head := lipgloss.NewStyle().Background(s.cardHead)
	label := lang
	if label == "" {
		label = "code"
	}
	copyL, applyL := "Copy", "Apply"
	switch theme.CurrentIconMode {
	case theme.IconModeNerdFont:
		copyL, applyL = " Copy", " Apply"
	case theme.IconModeUnicode:
		copyL, applyL = "⧉ Copy", "✓ Apply"
	}
	btns := ""
	var buttons []aiLineButton
	if done {
		btns = copyL + "  " + applyL
	}
	lw := runewidth.StringWidth(" " + label)
	bw := runewidth.StringWidth(btns)
	gap := cardW - lw - bw - 1
	if gap < 1 {
		btns, bw, gap = "", 0, cardW-lw
	}
	if gap < 0 {
		gap = 0
	}
	row := left + s.cardLabel.Render(" "+label) + head.Render(strings.Repeat(" ", gap))
	if btns != "" {
		x := indent + lw + gap
		buttons = append(buttons,
			aiLineButton{x, x + runewidth.StringWidth(copyL), "copy:" + strconv.Itoa(msgIdx) + ":" + strconv.Itoa(block)})
		x += runewidth.StringWidth(copyL) + 2
		buttons = append(buttons, aiLineButton{x, x + runewidth.StringWidth(applyL), "apply:" + strconv.Itoa(msgIdx) + ":" + strconv.Itoa(block)})
		row += s.cardButton.Render(copyL) + head.Render("  ") + s.cardButton.Render(applyL) + head.Render(" ")
	}
	out = append(out, aiLine{styled: row + right(indent+cardW), buttons: buttons})

	// Highlighted body.
	bodyBg := lipgloss.NewStyle().Background(s.cardBg)
	for _, line := range highlightCode(strings.Join(code, "\n"), lang, s) {
		for _, seg := range wrapStyledLine(line, cardW-2) {
			w := 0
			var b strings.Builder
			for _, sp := range seg {
				b.WriteString(sp.st.Render(sp.text))
				w += runewidth.StringWidth(sp.text)
			}
			r := left + bodyBg.Render(" ") + b.String()
			if fill := cardW - 1 - w; fill > 0 {
				r += bodyBg.Render(strings.Repeat(" ", fill))
			}
			out = append(out, aiLine{styled: r + right(indent+cardW)})
		}
	}
	out = append(out, aiLine{styled: left + bodyBg.Render(strings.Repeat(" ", cardW)) + right(indent+cardW)})
	return out
}

// highlightCode tokenises code with chroma and returns styled spans per
// source line (plain text on the card when the language is unknown).
func highlightCode(code, lang string, s mdStyles) [][]mdSpan {
	plain := lipgloss.NewStyle().Background(s.cardBg).Foreground(theme.LG(theme.TextPrimary))
	var lexer chroma.Lexer
	if lang != "" {
		lexer = lexers.Get(lang)
	}
	if lexer == nil {
		lexer = lexers.Analyse(code)
	}
	var lines [][]mdSpan
	if lexer == nil {
		for _, l := range strings.Split(code, "\n") {
			lines = append(lines, []mdSpan{{text: aiCleanText(l), st: plain}})
		}
		return lines
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return highlightCode(code, "\x00none", s)
	}
	cur := []mdSpan{}
	for tok := it(); tok != chroma.EOF; tok = it() {
		parts := strings.Split(aiCleanTextKeepNL(tok.Value), "\n")
		for k, p := range parts {
			if k > 0 {
				lines = append(lines, cur)
				cur = []mdSpan{}
			}
			if p != "" {
				cur = append(cur, mdSpan{text: p, st: s.syntax(tok.Type)})
			}
		}
	}
	lines = append(lines, cur)
	// Chroma adds a trailing newline token; drop the empty last line.
	if n := len(lines); n > 1 && len(lines[n-1]) == 0 {
		lines = lines[:n-1]
	}
	return lines
}

func aiCleanTextKeepNL(s string) string { return aiCleanText(s) }

// wrapStyledLine wraps a styled code line to w cells. It breaks after
// whitespace or punctuation (never inside an identifier when it can be
// avoided); continuation rows start with a dim "↪" and the line's own
// indent, so wrapped code still reads as one statement.
func wrapStyledLine(spans []mdSpan, w int) [][]mdSpan {
	if w < 4 {
		w = 4
	}
	type cell struct {
		r  rune
		st lipgloss.Style
		cw int
	}
	var cells []cell
	total := 0
	for _, sp := range spans {
		for _, r := range sp.text {
			cw := runewidth.RuneWidth(r)
			cells = append(cells, cell{r, sp.st, cw})
			total += cw
		}
	}
	emit := func(cs []cell) []mdSpan {
		var out []mdSpan
		for _, c := range cs {
			if n := len(out); n > 0 && out[n-1].st.GetForeground() == c.st.GetForeground() {
				out[n-1].text += string(c.r)
				continue
			}
			out = append(out, mdSpan{text: string(c.r), st: c.st})
		}
		return out
	}
	if total <= w {
		return [][]mdSpan{emit(cells)}
	}
	indent := 0
	for indent < len(cells) && cells[indent].r == ' ' {
		indent++
	}
	if indent > w/3 {
		indent = w / 3
	}
	cont := "↪ "
	if theme.CurrentIconMode == theme.IconModeASCII {
		cont = "> "
	}
	var contSt lipgloss.Style
	if len(cells) > 0 {
		contSt = cells[0].st.Foreground(theme.LG(theme.TextDim))
	}
	var out [][]mdSpan
	first := true
	for len(cells) > 0 {
		room := w
		var prefix []mdSpan
		if !first {
			prefix = []mdSpan{{text: strings.Repeat(" ", indent), st: contSt}, {text: cont, st: contSt}}
			room = w - indent - runewidth.StringWidth(cont)
			if room < 4 {
				room = 4
			}
		}
		// Fits?
		used, n := 0, 0
		for n < len(cells) && used+cells[n].cw <= room {
			used += cells[n].cw
			n++
		}
		if n == len(cells) {
			out = append(out, append(prefix, emit(cells)...))
			break
		}
		// Prefer a break after a space / punctuation in the last 2/3.
		cut := n
		for k := n; k > room/3; k-- {
			if isCodeBreak(cells[k-1].r) {
				cut = k
				break
			}
		}
		if cut == 0 {
			cut = 1
		}
		out = append(out, append(prefix, emit(cells[:cut])...))
		cells = cells[cut:]
		// Continuations don't start with the spaces we broke at.
		for len(cells) > 0 && cells[0].r == ' ' {
			cells = cells[1:]
		}
		first = false
	}
	return out
}

func isCodeBreak(r rune) bool {
	return strings.ContainsRune(" ,;(){}[].:=+-*/&|<>!?", r)
}
