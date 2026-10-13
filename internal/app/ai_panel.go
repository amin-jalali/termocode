package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// AI chat panel (Group A) — the body of the right-side Actions panel
// (actions_panel.go keeps the header buttons, pin / close, drag-resize and
// persistence). Layout of the body rows (below header + divider):
//
//	transcript  (scrollable: user bubbles, AI answers rendered as Markdown
//	             with code cards and Copy / Apply buttons)
//	chips row   (attachments, or "edit ready: Apply / Discard")
//	input box   (rounded border, 1..aiInputMaxRows rows, grows with the
//	             text; the bottom border carries the key hint)

const (
	aiInputMaxRows = 6
	aiPanelTopRow  = 2 // header + divider drawn by actions_panel.go
	aiIndent       = 2 // left padding of transcript content
)

// aiInputRows is kept for callers that need the minimum input height.
const aiInputRows = 1

// aiLine is one rendered transcript row before padding. Either text +
// style (plain, padded by renderAILine) or styled (already rendered).
type aiLine struct {
	text    string
	style   lipgloss.Style
	bg      theme.Color256
	buttons []aiLineButton
	styled  string
}

// aiLineButton is a clickable span on a transcript / divider row.
type aiLineButton struct {
	x0, x1 int // panel-local columns, half-open
	id     string
}

// aiPanelGeom is the body geometry in panel-local rows.
type aiPanelGeom struct {
	transTop, transH int
	dividerRow       int
	inputTop         int // top border row of the input box
	inputRows        int // text rows inside the box
	footerRow        int // bottom border row of the input box
	compact          bool
}

// aiGeom is the geometry for a one-row input (tests, scrolling math).
func aiGeom(height int) aiPanelGeom { return aiGeomRows(height, 1) }

func aiGeomRows(height, rows int) aiPanelGeom {
	body := height - aiPanelTopRow
	if body < rows+6 {
		return aiPanelGeom{transTop: aiPanelTopRow, transH: maxInt(body, 0), compact: true, dividerRow: -1, inputTop: -1, footerRow: -1}
	}
	g := aiPanelGeom{transTop: aiPanelTopRow, inputRows: rows}
	g.footerRow = height - 1
	g.inputTop = g.footerRow - rows - 1
	g.dividerRow = g.inputTop - 1
	g.transH = g.dividerRow - g.transTop
	return g
}

// aiPanelGeom is the live geometry: the input box grows with its text.
func (m Model) aiPanelGeom(width, height int) aiPanelGeom {
	rows := 1
	if s := m.ai; s != nil && len(s.input) > 0 {
		wrapped, _, _ := aiInputWrapped(s.input, s.caret, aiInputTextW(width))
		rows = len(wrapped)
	}
	if rows > aiInputMaxRows {
		rows = aiInputMaxRows
	}
	if rows < 1 {
		rows = 1
	}
	return aiGeomRows(height, rows)
}

// aiInputTextW is the text width inside the input box ("│ " … " │"
// inside a one-column margin).
func aiInputTextW(width int) int {
	w := width - 6
	if w < 1 {
		w = 1
	}
	return w
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// renderAIPanelBody renders the panel rows below the header divider.
func (m Model) renderAIPanelBody(width, height int) []string {
	g := m.aiPanelGeom(width, height)
	bg := theme.Bg(theme.BgPanel)
	blank := bg.Render(strings.Repeat(" ", width))
	rows := make([]string, 0, height-aiPanelTopRow)

	lines := m.aiTranscript(width)
	start, end := aiVisibleRange(len(lines), g.transH, m.aiScroll())
	for i := start; i < end; i++ {
		rows = append(rows, renderAILine(lines[i], width))
	}
	for len(rows) < g.transH {
		rows = append(rows, blank)
	}
	if g.compact {
		return rows
	}
	rows = append(rows, renderAILine(m.aiDividerLine(width), width))
	rows = append(rows, m.renderAIInput(width, g.inputRows)...)
	return rows
}

// aiScroll is the clamped-later scroll offset (0 when no state).
func (m Model) aiScroll() int {
	if m.ai == nil {
		return 0
	}
	return m.ai.scroll
}

// aiVisibleRange maps a bottom-anchored scroll offset onto [start, end).
func aiVisibleRange(total, visible, scroll int) (int, int) {
	if visible <= 0 {
		return 0, 0
	}
	maxScroll := total - visible
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	end := total - scroll
	start := end - visible
	if start < 0 {
		start = 0
	}
	return start, end
}

// renderAILine pads one line to width on its background.
func renderAILine(l aiLine, width int) string {
	bgTok := l.bg
	if bgTok == 0 {
		bgTok = theme.BgPanel
	}
	if l.styled != "" {
		return padStyled(l.styled, width, theme.Bg(bgTok))
	}
	txt := runewidth.Truncate(l.text, width, "…")
	pad := width - runewidth.StringWidth(txt)
	if pad < 0 {
		pad = 0
	}
	if len(l.buttons) == 0 {
		return l.style.Background(theme.LG(bgTok)).Render(txt) + theme.Bg(bgTok).Render(strings.Repeat(" ", pad))
	}
	// Buttons: draw each span in the accent color, the rest in l.style.
	var b strings.Builder
	runes := []rune(txt)
	col := 0
	btnStyle := theme.FgBg(theme.AIAccent, bgTok).Bold(true)
	base := l.style.Background(theme.LG(bgTok))
	seg := strings.Builder{}
	segBtn := false
	flush := func() {
		if seg.Len() == 0 {
			return
		}
		if segBtn {
			b.WriteString(btnStyle.Render(seg.String()))
		} else {
			b.WriteString(base.Render(seg.String()))
		}
		seg.Reset()
	}
	for _, r := range runes {
		inBtn := false
		for _, bt := range l.buttons {
			if col >= bt.x0 && col < bt.x1 {
				inBtn = true
				break
			}
		}
		if inBtn != segBtn {
			flush()
			segBtn = inBtn
		}
		seg.WriteRune(r)
		col += runewidth.RuneWidth(r)
	}
	flush()
	return b.String() + theme.Bg(bgTok).Render(strings.Repeat(" ", pad))
}

// aiCleanText expands tabs and drops control characters.
func aiCleanText(s string) string {
	s = strings.ReplaceAll(s, "\t", "    ")
	s = strings.ReplaceAll(s, "\r", "")
	return strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\n') || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// aiWrapCode hard-wraps a code line, keeping its indentation.
func aiWrapCode(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	if runewidth.StringWidth(s) <= w {
		return []string{s}
	}
	var out []string
	for runewidth.StringWidth(s) > w {
		head := runewidth.Truncate(s, w, "")
		if head == "" {
			break
		}
		out = append(out, head)
		s = s[len(head):]
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}

// aiTranscript builds the transcript (panel-local columns): padded
// content, user messages as tinted bubbles on the right, AI answers as
// rendered Markdown under an "✦ AI · model" line.
func (m Model) aiTranscript(width int) []aiLine {
	inner := width - aiIndent - 1
	if inner < 4 {
		inner = 4
	}
	ind := strings.Repeat(" ", aiIndent)
	plain := func(s string, tok theme.Color256) aiLine {
		return aiLine{text: ind + s, style: theme.Fg(tok)}
	}
	var out []aiLine
	s := m.ai
	if s == nil || len(s.conv.Messages) == 0 {
		out = append(out, aiLine{})
		if s == nil || s.provider == nil || !s.provider.Configured() {
			out = append(out, plain("AI is not configured.", theme.TextSecondary))
			out = append(out, aiLine{})
			out = append(out, aiChip("Configure…", "configure", width))
			out = append(out, aiLine{})
			for _, l := range wrapPlain("Or set ANTHROPIC_API_KEY / OPENAI_API_KEY, or TERMOCODE_AI_PROVIDER=ollama.", inner) {
				out = append(out, plain(l, theme.TextDim))
			}
			return out
		}
		out = append(out, aiLine{text: ind + aiSpark() + " Ask about your code", style: theme.Fg(theme.AIAccent).Bold(true)})
		out = append(out, aiLine{})
		for _, sg := range aiSuggestions {
			out = append(out, aiChip(sg.label, "suggest:"+sg.id, width))
		}
		out = append(out, aiLine{})
		for _, l := range []string{
			"Enter send · Shift+Enter newline",
			"Alt+I edit selection with AI",
			"Ctrl+. AI actions (Explain, Fix)",
			"Tab accepts ghost text",
		} {
			for _, w := range wrapPlain(l, inner) {
				out = append(out, plain(w, theme.TextDim))
			}
		}
		return out
	}
	last := len(s.conv.Messages) - 1
	for i, msg := range s.conv.Messages {
		switch msg.Role {
		case ai.StoredUser:
			out = append(out, aiLine{})
			out = append(out, aiUserBubble(aiCleanText(msg.Text), width)...)
			if msg.Context != "" {
				out = append(out, aiRightChip(aiAttachGlyph()+" "+aiContextLabel(msg.Context), width))
			}
		case ai.StoredAssistant:
			out = append(out, aiLine{})
			out = append(out, m.aiAnswerHeader(width))
			text := aiCleanText(msg.Text)
			streaming := s.streaming && i == last
			if strings.TrimSpace(text) == "" && streaming {
				out = append(out, aiLine{text: ind + "thinking…", style: theme.Fg(theme.TextDim).Italic(true)})
				break
			}
			out = append(out, s.renderAnswer(i, text, width, streaming, m.dragKind == dragActionsLeft)...)
		case ai.StoredTool:
			for _, l := range wrapPlain(aiCleanText(msg.Text), inner) {
				out = append(out, plain(l, theme.TextDim))
			}
		case ai.StoredError:
			for _, l := range wrapPlain("⚠ "+aiCleanText(msg.Text), inner) {
				out = append(out, plain(l, theme.DiagError))
			}
		}
	}
	out = append(out, aiLine{})
	return out
}

// aiSuggestions are the empty-state chips.
var aiSuggestions = []struct{ id, label, prompt string }{
	{"explain", "Explain this file", "Explain what this file does, its main parts and how they fit together."},
	{"bugs", "Find bugs", "Review this file for bugs, edge cases and error handling problems. List the issues with fixes."},
	{"tests", "Write tests", "Write unit tests for this file. Cover the main paths and edge cases."},
}

// aiSpark is the AI mark for the icon mode.
func aiSpark() string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return "*"
	}
	return "✦"
}

func aiAttachGlyph() string {
	switch theme.CurrentIconMode {
	case theme.IconModeNerdFont:
		return "\uf0c6" // nf-fa-paperclip
	case theme.IconModeUnicode:
		return "⊕"
	}
	return "+"
}

// aiChip is a clickable chip on its own row (left-aligned).
func aiChip(label, id string, width int) aiLine {
	chip := lipgloss.NewStyle().Background(theme.Mix(theme.BgPanel, theme.AIAccent, 0.16)).
		Foreground(theme.LG(theme.TextPrimary)).Render(" " + label + " ")
	cw := runewidth.StringWidth(label) + 2
	row := theme.Bg(theme.BgPanel).Render(strings.Repeat(" ", aiIndent)) + chip
	return aiLine{styled: row, buttons: []aiLineButton{{aiIndent, aiIndent + cw, id}}}
}

// aiRightChip is a dim chip aligned to the right edge (attachments).
func aiRightChip(label string, width int) aiLine {
	if max := width - aiIndent - 3; runewidth.StringWidth(label) > max && max > 3 {
		label = runewidth.Truncate(label, max, "…")
	}
	chip := lipgloss.NewStyle().Background(theme.Mix(theme.BgPanel, theme.TextPrimary, 0.08)).
		Foreground(theme.LG(theme.TextMuted)).Render(" " + label + " ")
	x := width - 1 - runewidth.StringWidth(label) - 2
	if x < 0 {
		x = 0
	}
	return aiLine{styled: theme.Bg(theme.BgPanel).Render(strings.Repeat(" ", x)) + chip}
}

// aiUserBubble renders a user message as a tinted bubble on the right.
func aiUserBubble(text string, width int) []aiLine {
	maxW := (width - aiIndent - 1) * 85 / 100
	if maxW < 10 {
		maxW = width - aiIndent - 1
	}
	lines := wrapPlain(text, maxW-2)
	bw := 0
	for _, l := range lines {
		if w := runewidth.StringWidth(l); w > bw {
			bw = w
		}
	}
	bw += 2
	x := width - 1 - bw
	if x < aiIndent {
		x = aiIndent
	}
	bub := lipgloss.NewStyle().Background(theme.Mix(theme.BgPanel, theme.BorderFocus, 0.20)).Foreground(theme.LG(theme.TextWhite))
	lead := theme.Bg(theme.BgPanel).Render(strings.Repeat(" ", x))
	out := make([]aiLine, 0, len(lines))
	for _, l := range lines {
		fill := bw - 2 - runewidth.StringWidth(l)
		if fill < 0 {
			fill = 0
		}
		out = append(out, aiLine{styled: lead + bub.Render(" "+l+strings.Repeat(" ", fill)+" ")})
	}
	return out
}

// aiAnswerHeader is the "✦ AI · model" line above an answer.
func (m Model) aiAnswerHeader(width int) aiLine {
	bg := theme.Bg(theme.BgPanel)
	row := bg.Render(strings.Repeat(" ", aiIndent)) +
		theme.FgBg(theme.AIAccent, theme.BgPanel).Bold(true).Render(aiSpark()+" AI")
	if s := m.ai; s != nil && s.provider != nil {
		row += theme.FgBg(theme.TextDim, theme.BgPanel).Render(" · " + s.modelLabel())
	}
	return aiLine{styled: row}
}

// renderAnswer renders (and caches) an assistant message. Finished
// messages are cached by text, width and theme; the streaming one is
// re-rendered only when its text changed.
//
// While the panel's splitter is being dragged (resizing) the last render
// is reused at any width — rows are padded / clipped by renderAILine — so
// a drag does not re-run Markdown + syntax highlighting on every step;
// the release renders once at the final width.
func (s *aiState) renderAnswer(i int, text string, width int, streaming, resizing bool) []aiLine {
	key := aiMDKey{text: text, width: width, theme: theme.Hex(theme.BgPanel) + theme.Hex(theme.TextPrimary), icons: theme.CurrentIconMode, streaming: streaming}
	if s.mdCache == nil {
		s.mdCache = map[int]aiMDEntry{}
	}
	if e, ok := s.mdCache[i]; ok && (e.key == key || (resizing && e.key.text == text && e.key.theme == key.theme)) {
		return e.lines
	}
	lines := renderChatMarkdown(text, width, aiIndent, i, streaming)
	s.mdCache[i] = aiMDEntry{key: key, lines: lines}
	return lines
}

type aiMDKey struct {
	text      string
	width     int
	theme     string
	icons     theme.IconMode
	streaming bool
}

type aiMDEntry struct {
	key   aiMDKey
	lines []aiLine
}

// aiContextLabel is the first line of an attachment block ("main.go:3-9").
func aiContextLabel(ctx string) string {
	first := strings.SplitN(ctx, "\n", 2)[0]
	first = strings.TrimPrefix(first, "Attached ")
	return strings.TrimSuffix(first, ":")
}

// aiDividerLine is the row above the input: proposal banner, attachment
// chips, or empty breathing room.
func (m Model) aiDividerLine(width int) aiLine {
	s := m.ai
	chip := func(label string, bgTok theme.Color256, t float64) string {
		return lipgloss.NewStyle().Background(theme.Mix(theme.BgPanel, bgTok, t)).
			Foreground(theme.LG(theme.TextPrimary)).Render(" " + label + " ")
	}
	bg := theme.Bg(theme.BgPanel)
	if s != nil && s.proposal != nil {
		head := " " + aiSpark() + " edit ready "
		a, d := "Apply", "Discard"
		x := runewidth.StringWidth(head)
		ax := x
		dx := ax + len(a) + 2 + 1
		row := theme.FgBg(theme.AIAccent, theme.BgPanel).Bold(true).Render(head) +
			chip(a, theme.AccentGreen, 0.25) + bg.Render(" ") + chip(d, theme.DiagError, 0.2)
		return aiLine{styled: row, buttons: []aiLineButton{{ax, ax + len(a) + 2, "proposal-apply"}, {dx, dx + len(d) + 2, "proposal-discard"}}}
	}
	if s != nil && len(s.attachments) > 0 {
		row := bg.Render(" ")
		x := 1
		for _, at := range s.attachments {
			lbl := aiAttachGlyph() + " " + at.Label
			row += chip(lbl, theme.TextPrimary, 0.10) + bg.Render(" ")
			x += runewidth.StringWidth(lbl) + 3
		}
		row += theme.FgBg(theme.TextDim, theme.BgPanel).Render("×")
		return aiLine{styled: row, buttons: []aiLineButton{{x, x + 1, "clear-attach"}}}
	}
	return aiLine{}
}

// aiInputHints are the bottom-border hints, longest first (the first
// that fits is shown).
func aiInputHints(s *aiState) []string {
	switch {
	case s != nil && s.streaming:
		return []string{"generating · ctrl+c stop", "ctrl+c stop"}
	case s != nil && s.focused:
		return []string{"enter send · shift+enter newline", "enter send"}
	}
	return []string{"alt+a chat · F6 focus", "F6 focus"}
}

// aiFooterLine is the key hint shown inside the input box's bottom border.
func (m Model) aiFooterLine(width int) aiLine {
	s := m.ai
	hint := "enter send · shift+enter newline"
	switch {
	case s != nil && s.streaming:
		hint = "■ generating — ctrl+c stop"
	case s != nil && !s.focused:
		hint = "alt+a chat · F6 focus"
	}
	return aiLine{text: hint, style: theme.Fg(theme.TextDim)}
}

// aiInputWrapped wraps the input into rows and returns the row / column
// of the caret.
func aiInputWrapped(input []rune, caret, w int) ([]string, int, int) {
	if w < 1 {
		w = 1
	}
	var rows []string
	cr, cc := 0, 0
	line := []rune{}
	lineW := 0
	for i := 0; i <= len(input); i++ {
		if i == caret {
			cr, cc = len(rows), lineW
		}
		if i == len(input) {
			break
		}
		r := input[i]
		if r == '\n' {
			rows = append(rows, string(line))
			line, lineW = line[:0:0], 0
			continue
		}
		rw := runewidth.RuneWidth(r)
		if lineW+rw > w {
			rows = append(rows, string(line))
			line, lineW = line[:0:0], 0
			if i == caret {
				cr, cc = len(rows), 0
			}
		}
		line = append(line, r)
		lineW += rw
	}
	rows = append(rows, string(line))
	if cc >= w { // caret after a full row → start of next visual row
		cr, cc = cr+1, 0
		if cr >= len(rows) {
			rows = append(rows, "")
		}
	}
	return rows, cr, cc
}

// renderAIInput draws the input box: a rounded border (accent while
// focused) with rows text rows and the key hint in the bottom border.
func (m Model) renderAIInput(width, rows int) []string {
	panel := theme.Bg(theme.BgPanel)
	var input []rune
	caret, focused := 0, false
	if s := m.ai; s != nil {
		input, caret, focused = s.input, s.caret, s.focused
	}
	boxBg := theme.Mix(theme.BgPanel, theme.TextPrimary, 0.05)
	borderC := theme.Mix(theme.BgPanel, theme.BorderDefault, 0.9)
	if focused {
		boxBg = theme.Mix(theme.BgPanel, theme.AIAccent, 0.06)
		borderC = theme.LG(theme.AIAccent)
	}
	edge := lipgloss.NewStyle().Background(theme.LG(theme.BgPanel)).Foreground(borderC)
	side := lipgloss.NewStyle().Background(boxBg).Foreground(borderC)
	fill := lipgloss.NewStyle().Background(boxBg)
	txt := lipgloss.NewStyle().Background(boxBg).Foreground(theme.LG(theme.TextPrimary))
	cur := lipgloss.NewStyle().Background(theme.LG(theme.TextPrimary)).Foreground(boxBg)
	tw := aiInputTextW(width)
	inner := tw + 2 // between the side borders
	out := make([]string, 0, rows+2)
	if width < 8 {
		for i := 0; i < rows+2; i++ {
			out = append(out, panel.Render(strings.Repeat(" ", width)))
		}
		return out
	}
	out = append(out, panel.Render(" ")+edge.Render("╭"+strings.Repeat("─", inner)+"╮")+panel.Render(" "))
	line := func(content string, w int) string {
		if w < tw {
			content += fill.Render(strings.Repeat(" ", tw-w))
		}
		return panel.Render(" ") + side.Render("│") + fill.Render(" ") + content + fill.Render(" ") + side.Render("│") + panel.Render(" ")
	}
	if len(input) == 0 {
		ph := "Ask AI anything…"
		c := ""
		w := 0
		if focused {
			c = cur.Render(" ")
			w = 1
		}
		ph = runewidth.Truncate(ph, tw-w, "…")
		c += lipgloss.NewStyle().Background(boxBg).Foreground(theme.LG(theme.TextDim)).Italic(true).Render(ph)
		out = append(out, line(c, w+runewidth.StringWidth(ph)))
		for len(out) < rows+1 {
			out = append(out, line("", 0))
		}
	} else {
		wrapped, cr, cc := aiInputWrapped(input, caret, tw)
		first := 0
		if cr >= rows {
			first = cr - rows + 1
		}
		for i := first; i < len(wrapped) && len(out) < rows+1; i++ {
			rs := []rune(wrapped[i])
			if focused && i == cr {
				idx, wsum := 0, 0
				for idx < len(rs) && wsum+runewidth.RuneWidth(rs[idx]) <= cc {
					wsum += runewidth.RuneWidth(rs[idx])
					idx++
				}
				at, rest := " ", ""
				if idx < len(rs) {
					at, rest = string(rs[idx]), string(rs[idx+1:])
				}
				w := runewidth.StringWidth(string(rs[:idx])) + runewidth.StringWidth(at) + runewidth.StringWidth(rest)
				out = append(out, line(txt.Render(string(rs[:idx]))+cur.Render(at)+txt.Render(rest), w))
				continue
			}
			out = append(out, line(txt.Render(wrapped[i]), runewidth.StringWidth(wrapped[i])))
		}
		for len(out) < rows+1 {
			out = append(out, line("", 0))
		}
	}
	hint := ""
	for _, h := range aiInputHints(m.ai) {
		if runewidth.StringWidth(h)+4 <= inner {
			hint = " " + h + " "
			break
		}
	}
	hw := runewidth.StringWidth(hint)
	bottom := edge.Render("╰─") + lipgloss.NewStyle().Background(theme.LG(theme.BgPanel)).Foreground(theme.LG(theme.TextDim)).Render(hint) +
		edge.Render(strings.Repeat("─", maxInt(inner-1-hw, 0))+"╯")
	out = append(out, panel.Render(" ")+bottom+panel.Render(" "))
	return out
}

// padStyled pads (or cuts) a styled row to exactly width cells.
func padStyled(s string, width int, bg lipgloss.Style) string {
	w := lipgloss.Width(s)
	if w > width {
		return truncRightVisual(s, width)
	}
	return s + bg.Render(strings.Repeat(" ", width-w))
}

// ── Keys ────────────────────────────────────────────────────────────────

// routeAIPanelKey handles keys while the chat panel has focus. Unclaimed
// keys (Alt+A, Ctrl+P, F6, …) fall through to the global keymap.
func (m *Model) routeAIPanelKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	s := m.aiS()
	if !s.focused {
		return nil, false
	}
	if !m.actionsPanelVisible() {
		s.focused = false
		return nil, false
	}
	switch msg.String() {
	case "esc":
		s.focused = false
		m.focus = FocusEditor
		return nil, true
	case "enter":
		return m.submitAIInput(), true
	case "shift+enter", "alt+enter", "ctrl+j":
		m.aiInsert([]rune{'\n'})
		return nil, true
	case "backspace", "ctrl+h":
		if s.caret > 0 {
			s.input = append(s.input[:s.caret-1], s.input[s.caret:]...)
			s.caret--
		}
		return nil, true
	case "delete":
		if s.caret < len(s.input) {
			s.input = append(s.input[:s.caret], s.input[s.caret+1:]...)
		}
		return nil, true
	case "ctrl+w", "alt+backspace":
		i := s.caret
		for i > 0 && s.input[i-1] == ' ' {
			i--
		}
		for i > 0 && s.input[i-1] != ' ' && s.input[i-1] != '\n' {
			i--
		}
		s.input = append(s.input[:i], s.input[s.caret:]...)
		s.caret = i
		return nil, true
	case "ctrl+u":
		s.input, s.caret = nil, 0
		return nil, true
	case "left":
		if s.caret > 0 {
			s.caret--
		}
		return nil, true
	case "right":
		if s.caret < len(s.input) {
			s.caret++
		}
		return nil, true
	case "home", "ctrl+a":
		s.caret = 0
		return nil, true
	case "end", "ctrl+e":
		s.caret = len(s.input)
		return nil, true
	case "up":
		m.scrollAIPanel(1)
		return nil, true
	case "down":
		m.scrollAIPanel(-1)
		return nil, true
	case "pgup":
		m.scrollAIPanel(maxInt(aiGeom(m.h).transH-2, 1))
		return nil, true
	case "pgdown":
		m.scrollAIPanel(-maxInt(aiGeom(m.h).transH-2, 1))
		return nil, true
	case "ctrl+c":
		return m.aiCancelAll(), true
	case "ctrl+n":
		return m.newAIChat(), true
	case "tab", "shift+tab":
		return nil, true
	}
	if msg.Alt {
		return nil, false
	}
	switch msg.Type {
	case tea.KeyRunes:
		m.aiInsert(msg.Runes)
		return nil, true
	case tea.KeySpace:
		m.aiInsert([]rune{' '})
		return nil, true
	}
	return nil, false
}

func (m *Model) aiInsert(rs []rune) {
	s := m.aiS()
	clean := make([]rune, 0, len(rs))
	for _, r := range rs {
		if r == '\r' {
			r = '\n'
		}
		if r == '\t' {
			r = ' '
		}
		if r < 0x20 && r != '\n' {
			continue
		}
		clean = append(clean, r)
	}
	next := make([]rune, 0, len(s.input)+len(clean))
	next = append(next, s.input[:s.caret]...)
	next = append(next, clean...)
	next = append(next, s.input[s.caret:]...)
	s.input = next
	s.caret += len(clean)
}

// scrollAIPanel scrolls the transcript (positive = up / older).
func (m *Model) scrollAIPanel(delta int) {
	s := m.aiS()
	total := len(m.aiTranscript(m.actionsColumnWidth()))
	maxScroll := total - m.aiPanelGeom(m.actionsColumnWidth(), m.h).transH
	if maxScroll < 0 {
		maxScroll = 0
	}
	s.scroll += delta
	if s.scroll > maxScroll {
		s.scroll = maxScroll
	}
	if s.scroll < 0 {
		s.scroll = 0
	}
	m.persistAIState()
}

// submitAIInput sends the typed message.
func (m *Model) submitAIInput() tea.Cmd {
	s := m.aiS()
	text := strings.TrimSpace(string(s.input))
	if text == "" {
		return nil
	}
	cmd := m.sendAIChat(text)
	if !s.streaming {
		return cmd // not sent (unconfigured / busy) — keep the draft
	}
	s.input, s.caret = nil, 0
	return cmd
}

// ── Mouse ──────────────────────────────────────────────────────────────

// aiPanelTargetAt returns what a left click at screen cell (x, y) hits in
// the chat panel: "ai:new", "ai:history", "ai:btn:<id>" (a transcript or
// divider span), "ai:transcript", "ai:input", "ai:body" (the rest of the
// panel), or "" when the panel does not take the click (outside it, or
// the pin / close header buttons, which mouse.go handles).
func (m Model) aiPanelTargetAt(x, y int) string {
	if !m.actionsPanelVisible() || m.dragKind != dragNone {
		return ""
	}
	x0 := m.w - m.actionsColumnWidth()
	if x < x0 || (x == x0 && y > 0) {
		return ""
	}
	switch y {
	case 0:
		switch id := m.hitActionsButton(x, y); id {
		case "new", "history":
			return "ai:" + id
		}
		return ""
	case 1:
		return "ai:body"
	}
	localX := x - x0
	width := m.actionsColumnWidth()
	g := m.aiPanelGeom(width, m.h)
	switch {
	case y >= g.transTop && y < g.transTop+g.transH:
		lines := m.aiTranscript(width)
		scroll := 0
		if m.ai != nil {
			scroll = m.ai.scroll
		}
		start, _ := aiVisibleRange(len(lines), g.transH, scroll)
		if idx := start + y - g.transTop; idx >= 0 && idx < len(lines) {
			for _, b := range lines[idx].buttons {
				if localX >= b.x0 && localX < b.x1 {
					return "ai:btn:" + b.id
				}
			}
		}
		return "ai:transcript"
	case !g.compact && y == g.dividerRow:
		for _, b := range m.aiDividerLine(width).buttons {
			if localX >= b.x0 && localX < b.x1 {
				return "ai:btn:" + b.id
			}
		}
	case !g.compact && y >= g.inputTop && y <= g.footerRow:
		return "ai:input"
	}
	return "ai:body"
}

// handleAIPanelMouse handles wheel / clicks inside the chat panel body
// and the extra header buttons (+ new chat, ≡ history). A left click
// outside the panel while it has focus blurs it (and is not consumed).
func (m *Model) handleAIPanelMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if !m.actionsPanelVisible() || m.dragKind != dragNone {
		return nil, false
	}
	s := m.aiS()
	x0 := m.w - m.actionsColumnWidth()
	if msg.X < x0 || (msg.X == x0 && msg.Y > 0) {
		if msg.Type == tea.MouseLeft && s.focused {
			s.focused = false
		}
		return nil, false
	}
	switch msg.Type {
	case tea.MouseWheelUp:
		m.scrollAIPanel(3)
		return nil, true
	case tea.MouseWheelDown:
		m.scrollAIPanel(-3)
		return nil, true
	case tea.MouseLeft:
	default:
		return nil, msg.Y > 0
	}
	switch id := m.aiPanelTargetAt(msg.X, msg.Y); {
	case id == "":
		return nil, false // pin / close handled by mouse.go
	case id == "ai:new":
		return m.newAIChat(), true
	case id == "ai:history":
		return m.openAIHistory(), true
	case strings.HasPrefix(id, "ai:btn:"):
		return m.onAIButton(strings.TrimPrefix(id, "ai:btn:")), true
	case id == "ai:transcript":
		s.focused = true
	case id == "ai:input":
		s.focused = true
		s.caret = len(s.input)
	}
	return nil, true
}

// onAIButton runs a clickable transcript / divider span.
func (m *Model) onAIButton(id string) tea.Cmd {
	switch {
	case id == "configure":
		return m.openAIConfigure()
	case id == "proposal-apply":
		return m.applyAIProposal()
	case id == "proposal-discard":
		return m.discardAIProposal(true)
	case id == "clear-attach":
		m.aiS().attachments = nil
		return nil
	case strings.HasPrefix(id, "suggest:"):
		for _, sg := range aiSuggestions {
			if "suggest:"+sg.id == id {
				return m.aiSuggest(sg.prompt)
			}
		}
		return nil
	case strings.HasPrefix(id, "copy:"), strings.HasPrefix(id, "apply:"):
		parts := strings.Split(id, ":")
		i, err := strconv.Atoi(parts[1])
		s := m.aiS()
		if err != nil || i < 0 || i >= len(s.conv.Messages) {
			return nil
		}
		block := 0
		if len(parts) > 2 {
			block, _ = strconv.Atoi(parts[2])
		}
		code, ok := aiCodeBlock(s.conv.Messages[i].Text, block)
		if !ok {
			return nil
		}
		if strings.HasPrefix(id, "copy:") {
			m.copyToClipboard(code)
			return m.aiInfo("Copied code block")
		}
		return m.proposeWholeFile(code, "chat code block")
	}
	return nil
}

// aiCodeBlock returns the n-th fenced code block of a message.
func aiCodeBlock(text string, n int) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	k := -1
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
			continue
		}
		k++
		var code []string
		for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
			code = append(code, lines[i])
		}
		if k == n {
			return strings.Join(code, "\n"), true
		}
	}
	return "", false
}

// aiSuggest sends an empty-state suggestion, with the open file attached
// when there is one.
func (m *Model) aiSuggest(prompt string) tea.Cmd {
	s := m.aiS()
	if m.editor.Path() != "" && len(s.attachments) == 0 {
		_ = m.aiAttachCurrent()
	}
	s.input, s.caret = []rune(prompt), len([]rune(prompt))
	return m.submitAIInput()
}
