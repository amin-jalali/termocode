package app

import (
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Text selection and copy in the AI chat transcript.
//
// Every transcript row (aiLine) may carry the plain text it shows (aiSel):
// where it starts, the text, and how it joins the row above when copied
// ("\n" for a new source line, " " for a word wrap, "" for a hard break).
// So a copy is the text as shown, with soft wraps undone, and no
// borders, markers or padding. A fully selected code line copies as its
// source line (tabs kept).
//
// The selection is drawn on top of the cached rows (renderAIPanelBody):
// only the selected rows are drawn again, the transcript cache is not
// touched.

// aiSel is the selectable text of one transcript row.
type aiSel struct {
	ok   bool
	x    int    // panel-local column where text starts
	text string // the text as shown (no styling)
	join string // what joins this row to the selectable row above
	code bool   // a code-block row
	raw  string // code rows starting a source line: the source line
	// bidi maps a right-to-left row (text is the drawn, visual order)
	// back to the logical text a copy gives (ai_rtl.go); nil otherwise.
	bidi *aiBidi
}

// aiSelPos is a transcript row index and a panel-local column.
type aiSelPos struct{ row, col int }

func (a aiSelPos) less(b aiSelPos) bool {
	return a.row < b.row || (a.row == b.row && a.col < b.col)
}

// Selection modes.
const (
	aiSelNone = iota
	aiSelDrag // anchor … head, both cells included
	aiSelFixed
)

// aiSelState is the chat selection (in aiState).
type aiSelState struct {
	mode         int
	anchor, head aiSelPos // aiSelDrag
	start, end   aiSelPos // aiSelFixed: [start, end)
	dragging     bool     // the left button is down
	width        int      // panel width the positions belong to

	// Multi-click detection.
	clicks  int
	lastAt  time.Time
	lastPos aiSelPos

	// Right-click menu target: message and code block, index+1 (0 = none).
	ctxMsg, ctxBlock int
}

// aiMultiClick is the most time between clicks of a double / triple click.
const aiMultiClick = 450 * time.Millisecond

func (s *aiSelState) clear() { s.mode, s.dragging = aiSelNone, false }

// rng is the selected range [start, end) (end.col exclusive on end.row).
func (s aiSelState) rng() (start, end aiSelPos, ok bool) {
	switch s.mode {
	case aiSelDrag:
		if s.anchor == s.head {
			return start, end, false
		}
		lo, hi := s.anchor, s.head
		if hi.less(lo) {
			lo, hi = hi, lo
		}
		return lo, aiSelPos{hi.row, hi.col + 1}, true
	case aiSelFixed:
		return s.start, s.end, s.start.less(s.end)
	}
	return start, end, false
}

// aiCellSlice returns the part of text (drawn from column x) that meets
// the cells [from, to), and the cells it covers. A wide character is
// taken whole when any of its cells is in the range.
func aiCellSlice(text string, x, from, to int) (sub string, c0, c1 int) {
	col := x
	start, stop := -1, -1
	prevIn := false
	for i, r := range text {
		w := runewidth.RuneWidth(r)
		in := prevIn // zero-width marks follow their base
		if w > 0 {
			in = col < to && col+w > from
		}
		if in {
			if start < 0 {
				start, c0 = i, col
			}
			stop, c1 = i+utf8.RuneLen(r), col+w
		}
		prevIn = in
		col += w
	}
	if start < 0 {
		return "", 0, 0
	}
	return text[start:stop], c0, c1
}

// aiRowBounds is the column range of row r inside [start, end).
func aiRowBounds(r int, start, end aiSelPos) (from, to int) {
	from, to = -1<<30, 1<<30
	if r == start.row {
		from = start.col
	}
	if r == end.row {
		to = end.col
	}
	return from, to
}

// aiLineEnd is the last row of the source line that row r starts or
// continues (rows joined by a wrap, not a newline).
func aiLineEnd(lines []aiLine, r int) int {
	for r+1 < len(lines) && lines[r+1].sel.ok && lines[r+1].sel.join != "\n" {
		r++
	}
	return r
}

// aiLineStart is the first row of the source line row r belongs to.
func aiLineStart(lines []aiLine, r int) int {
	for r > 0 && lines[r].sel.join != "\n" && lines[r-1].sel.ok {
		r--
	}
	return r
}

func aiSelEnd(s aiSel) int { return s.x + runewidth.StringWidth(s.text) }

// aiSelectionText is the plain text of the range [start, end): rows
// joined back the way the source was, no styling, borders or padding.
func aiSelectionText(lines []aiLine, start, end aiSelPos) string {
	var b strings.Builder
	first := true
	for r := max(start.row, 0); r <= end.row && r < len(lines); r++ {
		l := lines[r].sel
		if !l.ok {
			continue
		}
		from, to := aiRowBounds(r, start, end)
		// A whole code line copies as its source line.
		if l.code && l.join == "\n" {
			last := aiLineEnd(lines, r)
			lf, _ := aiRowBounds(r, start, end)
			_, lt := aiRowBounds(last, start, end)
			if last <= end.row && lf <= l.x && lt >= aiSelEnd(lines[last].sel) {
				if !first {
					b.WriteString(l.join)
				}
				b.WriteString(l.raw)
				first = false
				r = last
				continue
			}
		}
		sub, _, _ := aiCellSlice(l.text, l.x, from, to)
		if l.bidi != nil {
			sub = l.bidi.slice(l.x, from, to)
		}
		if first && sub == "" {
			continue
		}
		if !first {
			b.WriteString(l.join)
		}
		b.WriteString(sub)
		first = false
	}
	return strings.Trim(b.String(), "\n")
}

// aiSelRowSpan is the highlighted cells of row r for the range.
func aiSelRowSpan(l aiLine, r int, start, end aiSelPos) (c0, c1 int, ok bool) {
	if !l.sel.ok || r < start.row || r > end.row {
		return 0, 0, false
	}
	from, to := aiRowBounds(r, start, end)
	_, c0, c1 = aiCellSlice(l.sel.text, l.sel.x, from, to)
	return c0, c1, c1 > c0
}

// aiWordAt is the word under p (letters, digits, _), or the single
// character there when it is not part of a word.
func aiWordAt(lines []aiLine, p aiSelPos) (start, end aiSelPos, ok bool) {
	if p.row < 0 || p.row >= len(lines) || !lines[p.row].sel.ok {
		return start, end, false
	}
	l := lines[p.row].sel
	type cell struct {
		r     rune
		c0, w int
	}
	var cells []cell
	col := l.x
	hit := -1
	for _, r := range l.text {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			continue
		}
		if p.col >= col && p.col < col+w {
			hit = len(cells)
		}
		cells = append(cells, cell{r, col, w})
		col += w
	}
	if hit < 0 || unicode.IsSpace(cells[hit].r) {
		return start, end, false
	}
	word := func(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }
	i, j := hit, hit
	if word(cells[hit].r) {
		for i > 0 && word(cells[i-1].r) {
			i--
		}
		for j+1 < len(cells) && word(cells[j+1].r) {
			j++
		}
	}
	return aiSelPos{p.row, cells[i].c0}, aiSelPos{p.row, cells[j].c0 + cells[j].w}, true
}

// aiLineAt is the whole source line (a wrapped paragraph, list item or
// code line) under p.
func aiLineAt(lines []aiLine, p aiSelPos) (start, end aiSelPos, ok bool) {
	if p.row < 0 || p.row >= len(lines) || !lines[p.row].sel.ok {
		return start, end, false
	}
	r0, r1 := aiLineStart(lines, p.row), aiLineEnd(lines, p.row)
	start = aiSelPos{r0, lines[r0].sel.x}
	end = aiSelPos{r1, aiSelEnd(lines[r1].sel)}
	return start, end, start.less(end)
}

// aiJoins returns, for each row src was wrapped into, the text that
// joined it to the row before in src: the spaces a word wrap dropped, ""
// for a hard break. The first row gets "\n".
func aiJoins(src string, rows []string) []string {
	out := make([]string, len(rows))
	pos := 0
	for i, r := range rows {
		gap := " "
		if j := strings.Index(src[pos:], r); j >= 0 && strings.TrimSpace(src[pos:pos+j]) == "" {
			gap = src[pos : pos+j]
			pos += j + len(r)
		}
		if i == 0 {
			gap = "\n"
		}
		out[i] = gap
	}
	return out
}

// aiSelStyle draws selected cells.
func aiSelStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(theme.LG(theme.BgSelection)).Foreground(theme.LG(theme.TextWhite))
}

// aiHighlight redraws cells [c0, c1) of a drawn row as selected text.
func aiHighlight(row string, c0, c1, width int) string {
	c1 = min(c1, width)
	if c0 >= c1 {
		return row
	}
	return ansi.Cut(row, 0, c0) + aiSelStyle().Render(ansi.Strip(ansi.Cut(row, c0, c1))) + ansi.Cut(row, c1, width)
}

// ── Model side ──────────────────────────────────────────────────────────

// aiSelRange is the live selection range at panel width (false when
// there is none, or it belongs to another width).
func (m Model) aiSelRange(width int) (start, end aiSelPos, ok bool) {
	s := m.ai
	if s == nil || s.sel.mode == aiSelNone || s.sel.width != width {
		return start, end, false
	}
	return s.sel.rng()
}

// aiSelectedText is the selected transcript text ("" = none).
func (m Model) aiSelectedText() string {
	width := m.actionsColumnWidth()
	start, end, ok := m.aiSelRange(width)
	if !ok {
		return ""
	}
	return aiSelectionText(m.aiTranscript(width), start, end)
}

// aiSelPosAt maps screen cell (x, y) to a transcript position, clamped
// to the visible rows. With autoscroll (a drag) a pointer above or below
// the transcript scrolls it one row and selects to the row's edge.
func (m *Model) aiSelPosAt(x, y int, autoscroll bool) (aiSelPos, bool) {
	width := m.actionsColumnWidth()
	g := m.aiPanelGeom(width, m.h)
	if g.transH <= 0 {
		return aiSelPos{}, false
	}
	col := x - (m.w - width)
	switch {
	case y < g.transTop:
		if autoscroll {
			m.scrollAIPanel(1)
		}
		y, col = g.transTop, 0
	case y >= g.transTop+g.transH:
		if autoscroll {
			m.scrollAIPanel(-1)
		}
		y, col = g.transTop+g.transH-1, width
	}
	lines := m.aiTranscript(width)
	start, end := aiVisibleRange(len(lines), g.transH, m.aiScroll())
	if end <= start {
		return aiSelPos{}, false
	}
	row := start + y - g.transTop
	if row >= end {
		row, col = end-1, width
	}
	return aiSelPos{row, max(0, min(col, width))}, true
}

// aiSelPress starts a selection at a left press on the transcript: one
// click starts a drag, two select a word, three the whole line.
func (m *Model) aiSelPress(x, y int) {
	s := m.aiS()
	p, ok := m.aiSelPosAt(x, y, false)
	s.sel.clear()
	if !ok {
		return
	}
	now := time.Now()
	if s.sel.clicks > 0 && s.sel.clicks < 3 && now.Sub(s.sel.lastAt) < aiMultiClick &&
		p.row == s.sel.lastPos.row && p.col-s.sel.lastPos.col <= 1 && s.sel.lastPos.col-p.col <= 1 {
		s.sel.clicks++
	} else {
		s.sel.clicks = 1
	}
	s.sel.lastAt, s.sel.lastPos = now, p
	width := m.actionsColumnWidth()
	s.sel.width, s.sel.dragging = width, true
	switch s.sel.clicks {
	case 1:
		s.sel.mode, s.sel.anchor, s.sel.head = aiSelDrag, p, p
	default:
		find := aiWordAt
		if s.sel.clicks == 3 {
			find = aiLineAt
		}
		if a, b, ok := find(m.aiTranscript(width), p); ok {
			s.sel.mode, s.sel.start, s.sel.end = aiSelFixed, a, b
		}
	}
}

// aiSelDragTo moves the drag's head to (x, y).
func (m *Model) aiSelDragTo(x, y int) {
	s := m.aiS()
	if s.sel.mode != aiSelDrag {
		return
	}
	if p, ok := m.aiSelPosAt(x, y, true); ok {
		s.sel.head = p
	}
}

// aiSelRelease ends a press: a non-empty selection is copied at once.
func (m *Model) aiSelRelease() tea.Cmd {
	s := m.aiS()
	s.sel.dragging = false
	text := m.aiSelectedText()
	if text == "" {
		s.sel.clear()
		return nil
	}
	return m.copyWithToast(text, aiCopiedChars(text))
}

// aiCopiedChars is the "Copied N chars" toast text.
func aiCopiedChars(text string) string {
	n := utf8.RuneCountInString(text)
	if n == 1 {
		return "Copied 1 char"
	}
	return "Copied " + strconv.Itoa(n) + " chars"
}

// aiCopySelection copies the selection (Ctrl+C); false when there is none.
func (m *Model) aiCopySelection() (tea.Cmd, bool) {
	text := m.aiSelectedText()
	if text == "" {
		return nil, false
	}
	return m.copyWithToast(text, aiCopiedChars(text)), true
}

// aiMessageText is the text of message i as stored (Markdown for answers).
func (m *Model) aiMessageText(i int) (string, bool) {
	s := m.aiS()
	if i < 0 || i >= len(s.conv.Messages) {
		return "", false
	}
	return s.conv.Messages[i].Text, s.conv.Messages[i].Text != ""
}

// aiCopyMessage copies message i (its Markdown).
func (m *Model) aiCopyMessage(i int) tea.Cmd {
	text, ok := m.aiMessageText(i)
	if !ok {
		return nil
	}
	return m.copyWithToast(text, "Copied message")
}

// Chat context-menu ids.
const (
	aiMenuCopyMessage   = "ai-chat-copy-message"
	aiMenuCopySelection = "ai-chat-copy-selection"
	aiMenuCopyCode      = "ai-chat-copy-code"
)

// aiChatRightClick opens the transcript context menu for the row at y.
func (m *Model) aiChatRightClick(x, y int) {
	s := m.aiS()
	width := m.actionsColumnWidth()
	g := m.aiPanelGeom(width, m.h)
	if y < g.transTop || y >= g.transTop+g.transH {
		return
	}
	lines := m.aiTranscript(width)
	start, _ := aiVisibleRange(len(lines), g.transH, m.aiScroll())
	s.sel.ctxMsg, s.sel.ctxBlock = 0, 0
	if r := start + y - g.transTop; r >= 0 && r < len(lines) {
		s.sel.ctxMsg, s.sel.ctxBlock = lines[r].msg, lines[r].block
	}
	var items []menu.Item
	if s.sel.ctxMsg > 0 {
		items = append(items, menu.Item{ID: aiMenuCopyMessage, Title: "Copy Message", Icon: "⎘"})
	}
	if m.aiSelectedText() != "" {
		items = append(items, menu.Item{ID: aiMenuCopySelection, Title: "Copy Selection", Icon: "⎘"})
	}
	if s.sel.ctxMsg > 0 && s.sel.ctxBlock > 0 {
		items = append(items, menu.Item{ID: aiMenuCopyCode, Title: "Copy Code Block", Icon: "⎘"})
	}
	if len(items) == 0 {
		return
	}
	m.menu = menu.New(items, x, y)
	m.menu.SetScreenSize(m.w, m.h)
	m.menuOpen = true
	m.menuKind = menuKindAIChat
}

// handleAIChatMenuSelect runs a transcript context-menu entry.
func (m *Model) handleAIChatMenuSelect(id string) tea.Cmd {
	s := m.aiS()
	switch id {
	case aiMenuCopyMessage:
		return m.aiCopyMessage(s.sel.ctxMsg - 1)
	case aiMenuCopySelection:
		cmd, _ := m.aiCopySelection()
		return cmd
	case aiMenuCopyCode:
		text, ok := m.aiMessageText(s.sel.ctxMsg - 1)
		if !ok {
			return nil
		}
		if code, ok := aiCodeBlock(text, s.sel.ctxBlock-1); ok {
			return m.copyWithToast(code, "Copied code block")
		}
	}
	return nil
}

// aiKeepSelection drops the selection when the transcript it points into
// is replaced (another chat, a cleared chat, a new width).
func (s *aiState) aiKeepSelection(old []ai.StoredMessage, oldWidth, width int) {
	if s.sel.mode == aiSelNone {
		return
	}
	cur := s.conv.Messages
	if oldWidth != width || len(cur) < len(old) || (len(old) > 0 && cur[0].Text != old[0].Text) {
		s.sel.clear()
	}
}
