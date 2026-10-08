package search

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/theme"
)

// SelectMsg fires when the user activates a result.
type SelectMsg struct {
	Path string
	Line int
}

// CloseMsg fires when the user dismisses the overlay.
type CloseMsg struct{}

type tickMsg struct {
	version int
}

type resultsMsg struct {
	version int
	results []Result
	summary Summary
	err     error
}

// field is one of the overlay's text inputs. Tab / Shift+Tab cycle focus.
type field int

const (
	fieldQuery field = iota
	fieldInclude
	fieldExclude
	fieldContext
	fieldCount
)

// maxContext caps the context-lines field so a stray "99" can't flood the
// overlay with surrounding text.
const maxContext = 9

type Model struct {
	input       string
	results     []Result
	summary     Summary
	cursor      int
	w, h        int
	queryVer    int // increments on every keystroke; latest fires the run
	pending     bool
	rgAvailable bool
	lastErr     string
	// roots is the list of directories ripgrep searches across. Empty
	// means "use cwd" (single-root behaviour). Set via SetRoots before
	// the search overlay opens to enable multi-root search.
	roots []string

	// Search options — the Aa / ab / .* toggles (Alt+C / Alt+W / Alt+R or
	// a click on the glyph) and the filter row (include / exclude globs,
	// context lines). Any change re-runs the search.
	caseSensitive bool
	wholeWord     bool
	useRegex      bool
	include       string
	exclude       string
	contextLines  string
	focus         field
	maxResults    int // 0 = unlimited; see SetMaxResults
}

const debounce = 150 * time.Millisecond

func New() Model {
	return Model{rgAvailable: Available(), maxResults: DefaultMaxResults}
}

func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// SetMaxResults sets the total result cap (the `search_max_results`
// setting). 0 means unlimited; negative values are treated as 0.
func (m *Model) SetMaxResults(n int) {
	if n < 0 {
		n = 0
	}
	m.maxResults = n
}

// SetRoots configures the directory list ripgrep walks. Empty/nil reverts
// to cwd. Each invocation of the search overlay should call SetRoots once
// (before the user starts typing) so the debounced spawn picks up the
// caller's workspace shape.
func (m *Model) SetRoots(roots []string) {
	if len(roots) == 0 {
		m.roots = nil
		return
	}
	m.roots = append(m.roots[:0], roots...)
}

// Options returns the search options built from the overlay's toggles and
// filter fields.
func (m Model) Options() Options {
	n, _ := strconv.Atoi(m.contextLines)
	if n > maxContext {
		n = maxContext
	}
	return Options{
		CaseSensitive: m.caseSensitive,
		WholeWord:     m.wholeWord,
		Regex:         m.useRegex,
		Include:       SplitGlobs(m.include),
		Exclude:       SplitGlobs(m.exclude),
		Context:       n,
		MaxResults:    m.maxResults,
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if msg.version != m.queryVer {
			return m, nil
		}
		query := m.input
		ver := m.queryVer
		opts := m.Options()
		// Snapshot roots so the spawned goroutine doesn't race with a
		// concurrent SetRoots call from the UI thread.
		roots := append([]string(nil), m.roots...)
		m.pending = true
		return m, func() tea.Msg {
			if len(roots) == 0 {
				cwd, _ := os.Getwd()
				roots = []string{cwd}
			}
			results, sum, err := Collect(context.Background(), roots, query, opts)
			return resultsMsg{version: ver, results: results, summary: sum, err: err}
		}
	case resultsMsg:
		if msg.version != m.queryVer {
			return m, nil
		}
		m.pending = false
		m.results = msg.results
		m.summary = msg.summary
		m.cursor = 0
		if msg.err != nil {
			m.lastErr = msg.err.Error()
		} else {
			m.lastErr = ""
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(k tea.KeyMsg) (Model, tea.Cmd) {
	// Alt+C / Alt+W / Alt+R flip the Aa / ab / .* toggles (VS Code keys).
	if k.Alt && k.Type == tea.KeyRunes && len(k.Runes) == 1 {
		switch k.Runes[0] {
		case 'c', 'C':
			m.caseSensitive = !m.caseSensitive
		case 'w', 'W':
			m.wholeWord = !m.wholeWord
		case 'r', 'R':
			m.useRegex = !m.useRegex
		default:
			return m, nil
		}
		return m, m.scheduleSearch()
	}
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		return m, func() tea.Msg { return CloseMsg{} }
	case tea.KeyEnter:
		if m.cursor >= 0 && m.cursor < len(m.results) {
			r := m.results[m.cursor]
			return m, func() tea.Msg { return SelectMsg{Path: r.Path, Line: r.Line} }
		}
		return m, nil
	case tea.KeyTab:
		m.focus = (m.focus + 1) % fieldCount
		return m, nil
	case tea.KeyShiftTab:
		m.focus = (m.focus + fieldCount - 1) % fieldCount
		return m, nil
	case tea.KeyDown, tea.KeyCtrlN:
		if m.cursor < len(m.results)-1 {
			m.cursor++
		}
		return m, nil
	case tea.KeyUp, tea.KeyCtrlP:
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case tea.KeyBackspace:
		f := m.focused()
		if len(*f) > 0 {
			r := []rune(*f)
			*f = string(r[:len(r)-1])
			return m, m.scheduleSearch()
		}
		return m, nil
	case tea.KeySpace:
		if m.focus == fieldContext {
			return m, nil
		}
		*m.focused() += " "
		return m, m.scheduleSearch()
	case tea.KeyRunes:
		if m.focus == fieldContext {
			// Digits only; keep a single digit (0..maxContext).
			for _, r := range k.Runes {
				if r >= '0' && r <= '9' {
					m.contextLines = string(r)
				}
			}
			return m, m.scheduleSearch()
		}
		*m.focused() += string(k.Runes)
		return m, m.scheduleSearch()
	}
	return m, nil
}

// focused returns the text of the input that currently has focus.
func (m *Model) focused() *string {
	switch m.focus {
	case fieldInclude:
		return &m.include
	case fieldExclude:
		return &m.exclude
	case fieldContext:
		return &m.contextLines
	}
	return &m.input
}

func (m *Model) scheduleSearch() tea.Cmd {
	m.queryVer++
	v := m.queryVer
	if strings.TrimSpace(m.input) == "" {
		m.results = nil
		m.summary = Summary{}
		m.lastErr = ""
		m.pending = false
		return nil
	}
	return tea.Tick(debounce, func(time.Time) tea.Msg {
		return tickMsg{version: v}
	})
}

// HandleMouse toggles Aa / ab / .* when the user clicks a glyph in the
// title row, and focuses a filter field when its row cell is clicked.
// x, y are absolute screen cells. Other clicks are ignored.
func (m Model) HandleMouse(x, y int, action tea.MouseAction, button tea.MouseButton) (Model, tea.Cmd) {
	if action != tea.MouseActionPress || button != tea.MouseButtonLeft {
		return m, nil
	}
	bx, by, bw, _ := m.boxRect()
	if bw <= 0 {
		return m, nil
	}
	innerW := bw - 2
	col := x - bx - 1
	switch y - by - 1 {
	case 0: // title row
		c, w, r := toggleCols(innerW)
		switch {
		case col >= c && col < c+2:
			m.caseSensitive = !m.caseSensitive
		case col >= w && col < w+2:
			m.wholeWord = !m.wholeWord
		case col >= r && col < r+2:
			m.useRegex = !m.useRegex
		default:
			return m, nil
		}
		return m, m.scheduleSearch()
	case 1:
		m.focus = fieldQuery
	case 2:
		inc, exc, ctx := filterCols(innerW)
		switch {
		case col >= ctx:
			m.focus = fieldContext
		case col >= exc:
			m.focus = fieldExclude
		case col >= inc:
			m.focus = fieldInclude
		}
	}
	return m, nil
}

// boxSize returns the overlay box width and height for the current screen.
func (m Model) boxSize() (int, int) {
	boxW := m.w - 6
	if boxW > 110 {
		boxW = 110
	}
	if boxW < 40 {
		boxW = 40
	}
	boxH := m.h - 4
	if boxH > 30 {
		boxH = 30
	}
	if boxH < 9 {
		boxH = 9
	}
	return boxW, boxH
}

// boxRect is the box's screen rectangle. lipgloss.Place centres with the
// extra cell going right / down, i.e. the origin is floor(gap/2).
func (m Model) boxRect() (x, y, w, h int) {
	if m.w <= 0 || m.h <= 0 {
		return 0, 0, 0, 0
	}
	w, h = m.boxSize()
	return max(0, (m.w-w)/2), max(0, (m.h-h)/2), w, h
}

// toggleCols returns the inner-box column where each 2-cell glyph starts in
// the title row: "… Aa ab .* ".
func toggleCols(innerW int) (caseCol, wordCol, regexCol int) {
	return innerW - 9, innerW - 6, innerW - 3
}

// filterCols splits the filter row into include / exclude / context
// segments and returns where each segment (label included) starts.
func filterCols(innerW int) (inc, exc, ctx int) {
	const ctxW = 12 // " context [n]"
	half := (innerW - ctxW) / 2
	return 0, half, innerW - ctxW
}

// styles holds the overlay's palette, built from theme tokens on every
// render so a runtime theme switch is picked up immediately.
type styles struct {
	fill, border, title, input, inputFocus, label, hint     lipgloss.Style
	path, pathSel, prev, prevSel, match, matchSel, line     lipgloss.Style
	lineSel, err, ctx, ctxSel, toggleOn, toggleOff, cursorS lipgloss.Style
}

func newStyles() styles {
	bg := theme.BgHover
	sel := theme.BgSelection
	return styles{
		fill:       theme.Bg(bg),
		border:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.LG(theme.BorderDefault)).BorderBackground(theme.LG(bg)).Background(theme.LG(bg)),
		title:      theme.FgBg(theme.TextPrimary, bg).Bold(true),
		input:      theme.FgBg(theme.TextPrimary, theme.BgPanel),
		inputFocus: theme.FgBg(theme.TextWhite, theme.BgPanel),
		label:      theme.FgBg(theme.TextDim, bg),
		hint:       theme.FgBg(theme.TextMuted, bg),
		path:       theme.FgBg(theme.SyntaxVariable, bg),
		pathSel:    theme.FgBg(theme.TextWhite, sel).Bold(true),
		prev:       theme.FgBg(theme.TextSecondary, bg),
		prevSel:    theme.FgBg(theme.TextWhite, sel),
		match:      theme.FgBg(theme.AccentAmber, bg).Bold(true),
		matchSel:   theme.FgBg(theme.AccentAmber, sel).Bold(true),
		line:       theme.FgBg(theme.SyntaxFunction, bg),
		lineSel:    theme.FgBg(theme.SyntaxFunction, sel).Bold(true),
		err:        theme.FgBg(theme.DiagError, bg).Bold(true),
		ctx:        theme.FgBg(theme.TextDim, bg),
		ctxSel:     theme.FgBg(theme.TextSecondary, sel),
		toggleOn:   theme.FgBg(theme.TextWhite, theme.BgInactiveSel).Bold(true),
		toggleOff:  theme.FgBg(theme.TextMuted, bg),
		cursorS:    theme.FgBg(theme.AccentLavender, theme.BgPanel).Bold(true),
	}
}

func (m Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	boxW, boxH := m.boxSize()
	box := m.renderBox(boxW, boxH)
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceChars(" "),
	)
}

func (m Model) renderBox(w, h int) string {
	st := newStyles()
	innerW := w - 2

	// Title row: " Find in Files" on the left, the Aa / ab / .* toggles on
	// the right — same glyphs and on/off look as the Ctrl+F findbar.
	toggle := func(glyph string, on bool) string {
		if on {
			return st.toggleOn.Render(glyph)
		}
		return st.toggleOff.Render(glyph)
	}
	caseCol, _, _ := toggleCols(innerW)
	title := st.title.Render(padRight(" Find in Files", caseCol)) +
		toggle("Aa", m.caseSensitive) + st.fill.Render(" ") +
		toggle("ab", m.wholeWord) + st.fill.Render(" ") +
		toggle(".*", m.useRegex) + st.fill.Render(" ")

	input := m.renderField(st, " > ", m.input, fieldQuery, innerW)
	filters := m.renderFilters(st, innerW)

	innerH := h - 2
	available := innerH - 5 // title + input + filters + summary + footer
	if available < 1 {
		available = 1
	}

	// Without ripgrep we fall back to the built-in walker. It still works,
	// so we show results normally and just flag "basic" mode rather than
	// blocking the panel with a "not installed" error.
	basic := ""
	if !m.rgAvailable {
		basic = " · basic"
	}
	var summary string
	switch {
	case m.lastErr != "":
		summary = st.err.Render(" " + m.lastErr)
	case m.pending:
		summary = st.hint.Render(" searching... ")
	case strings.TrimSpace(m.input) == "":
		if m.rgAvailable {
			summary = st.hint.Render(" type to search ")
		} else {
			summary = st.hint.Render(" type to search · basic (no .gitignore — install rg) ")
		}
	case len(m.results) == 0:
		summary = st.hint.Render(" no results" + basic + " ")
	case m.summary.Truncated():
		summary = st.hint.Render(fmt.Sprintf(" showing %d of %d results%s ", m.summary.Shown, m.summary.Total, basic))
	default:
		summary = st.hint.Render(fmt.Sprintf(" %d results%s ", len(m.results), basic))
	}
	summary = padBg(st, summary, innerW)

	rows := m.renderResults(st, available, innerW)

	footer := st.hint.Width(innerW).Render(" ↑↓ navigate · tab field · alt+c/w/r toggles · enter open · esc close ")

	return st.border.Render(lipgloss.JoinVertical(lipgloss.Left, title, input, filters, summary, lipgloss.JoinVertical(lipgloss.Left, rows...), footer))
}

// renderField draws one text input of exactly `w` cells: a label, the
// value, and the ▍ cursor when the field has focus.
func (m Model) renderField(st styles, label, value string, f field, w int) string {
	focused := m.focus == f
	lbl := st.label
	if focused {
		lbl = st.title
	}
	out := lbl.Render(label)
	room := w - lipgloss.Width(label)
	if room <= 0 {
		return out
	}
	style := st.input
	if focused {
		style = st.inputFocus
	}
	cur := ""
	if focused {
		cur = "▍"
	}
	// Keep the tail of a long value visible — that's where typing happens.
	budget := room - runewidth.StringWidth(cur)
	if runewidth.StringWidth(value) > budget {
		value = runewidth.TruncateLeft(value, runewidth.StringWidth(value)-budget+1, "…")
	}
	body := style.Render(value)
	if focused {
		body += st.cursorS.Render(cur)
	}
	if pad := room - lipgloss.Width(body); pad > 0 {
		body += style.Render(strings.Repeat(" ", pad))
	}
	return out + body
}

// renderFilters draws the include / exclude / context row.
func (m Model) renderFilters(st styles, innerW int) string {
	_, exc, ctx := filterCols(innerW)
	inc := m.renderField(st, " include ", m.include, fieldInclude, exc-1)
	ex := m.renderField(st, " exclude ", m.exclude, fieldExclude, ctx-exc-1)
	cx := m.renderField(st, " context ", m.contextLines, fieldContext, innerW-ctx-1)
	return inc + st.fill.Render(" ") + ex + st.fill.Render(" ") + cx + st.fill.Render(" ")
}

// rowHeight is how many screen lines a result takes: the path header, the
// context lines before, the match itself, and the context lines after.
func rowHeight(r Result) int { return 2 + len(r.Before) + len(r.After) }

func (m Model) renderResults(st styles, rows, w int) []string {
	out := make([]string, 0, rows)
	blank := st.fill.Render(strings.Repeat(" ", w))
	if rows < 1 {
		return out
	}
	if len(m.results) == 0 {
		for i := 0; i < rows; i++ {
			out = append(out, blank)
		}
		return out
	}

	// Scroll so the cursor's whole block is visible: start at the top if
	// it fits, otherwise walk back from the cursor while blocks still fit.
	used := 0
	for i := 0; i <= m.cursor && i < len(m.results); i++ {
		used += rowHeight(m.results[i])
	}
	start := 0
	if used > rows {
		start = m.cursor
		h := rowHeight(m.results[start])
		for start > 0 && h+rowHeight(m.results[start-1]) <= rows {
			start--
			h += rowHeight(m.results[start])
		}
	}
	for i := start; i < len(m.results) && len(out) < rows; i++ {
		out = append(out, m.renderRow(st, m.results[i], i == m.cursor, w)...)
	}
	if len(out) > rows {
		out = out[:rows]
	}
	for len(out) < rows {
		out = append(out, blank)
	}
	return out
}

// renderRow lays out a single result:
//
//	relpath:line:col
//	  context before (dim)
//	  snippet (with each matched span bolded)
//	  context after (dim)
//
// `active` selects the highlight palette so the cursor row stands out.
// Every line is padded to exactly width `w` so background colours fill
// the box.
func (m Model) renderRow(st styles, r Result, active bool, w int) []string {
	pathStyle := st.path
	prevStyle := st.prev
	lineStyle := st.line
	matchStyle := st.match
	ctxStyle := st.ctx
	if active {
		pathStyle = st.pathSel
		prevStyle = st.prevSel
		lineStyle = st.lineSel
		matchStyle = st.matchSel
		ctxStyle = st.ctxSel
	}

	// Header line: "  relpath  :line:col"
	loc := fmt.Sprintf("  :%d:%d", r.Line, r.Col)
	maxLeft := w - lipgloss.Width(loc) - 2
	if maxLeft < 8 {
		maxLeft = 8
	}
	relpath := runewidth.Truncate(r.Path, maxLeft, "…")
	body := " " + relpath
	bodyW := lipgloss.Width(body) + lipgloss.Width(loc)
	pad := w - bodyW
	if pad < 0 {
		pad = 0
	}
	header := pathStyle.Render(body) + lineStyle.Render(loc) + pathStyle.Render(strings.Repeat(" ", pad))

	// Snippet lines: indent 4, then text; the match line has its matched
	// spans bolded, context lines are dimmed.
	indent := "    "
	maxSnippet := w - len(indent)
	if maxSnippet < 4 {
		maxSnippet = 4
	}
	fill := func(s string, style lipgloss.Style) string {
		if pp := w - lipgloss.Width(s); pp > 0 {
			s += style.Render(strings.Repeat(" ", pp))
		}
		return s
	}
	ctxLine := func(c ContextLine) string {
		text := runewidth.Truncate(strings.TrimLeft(c.Text, " \t"), maxSnippet, "…")
		return fill(ctxStyle.Render(indent+text), ctxStyle)
	}

	out := []string{header}
	for _, c := range r.Before {
		out = append(out, ctxLine(c))
	}
	snippet := renderHighlighted(r.Preview, r.Matches, prevStyle, matchStyle, maxSnippet)
	out = append(out, fill(prevStyle.Render(indent)+snippet, prevStyle))
	for _, c := range r.After {
		out = append(out, ctxLine(c))
	}
	return out
}

// renderHighlighted paints `preview` so each MatchRange (in raw byte
// offsets) is wrapped with `matchStyle` and the rest with `baseStyle`. The
// result is truncated to `maxW` visual cells with an ellipsis. Ranges that
// fall after truncation are dropped silently.
func renderHighlighted(preview string, matches []MatchRange, baseStyle, matchStyle lipgloss.Style, maxW int) string {
	// Skip leading whitespace for readability; track the offset so byte
	// ranges from rg still line up.
	trimmed := strings.TrimLeft(preview, " \t")
	skipped := len(preview) - len(trimmed)

	if len(matches) == 0 {
		return baseStyle.Render(runewidth.Truncate(trimmed, maxW, "…"))
	}

	var out strings.Builder
	used := 0
	cursor := 0
	for _, mr := range matches {
		s := mr.Start - skipped
		e := mr.End - skipped
		if e <= cursor || s >= len(trimmed) {
			continue
		}
		if s < cursor {
			s = cursor
		}
		if s > e {
			continue
		}
		// Emit base text up to the match.
		if s > cursor {
			seg := trimmed[cursor:s]
			seg, w := truncateBudget(seg, maxW-used)
			out.WriteString(baseStyle.Render(seg))
			used += w
			if used >= maxW {
				return out.String()
			}
		}
		// Emit the match span itself.
		if e > len(trimmed) {
			e = len(trimmed)
		}
		seg := trimmed[s:e]
		seg, w := truncateBudget(seg, maxW-used)
		out.WriteString(matchStyle.Render(seg))
		used += w
		if used >= maxW {
			return out.String()
		}
		cursor = e
	}
	// Tail.
	if cursor < len(trimmed) {
		seg := trimmed[cursor:]
		seg, w := truncateBudget(seg, maxW-used)
		out.WriteString(baseStyle.Render(seg))
		used += w
		_ = used
	}
	return out.String()
}

// truncateBudget returns the longest prefix of `s` that fits in `budget`
// visual cells, plus the actual width of that prefix. When the input
// exceeds the budget the returned string ends with an ellipsis (…).
func truncateBudget(s string, budget int) (string, int) {
	if budget <= 0 {
		return "", 0
	}
	if runewidth.StringWidth(s) <= budget {
		return s, runewidth.StringWidth(s)
	}
	out := runewidth.Truncate(s, budget, "…")
	return out, runewidth.StringWidth(out)
}

// padRight pads s with spaces to w cells (no truncation).
func padRight(s string, w int) string {
	if pad := w - runewidth.StringWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

func padBg(st styles, s string, w int) string {
	pad := w - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	return s + st.fill.Render(strings.Repeat(" ", pad))
}
