package app

import (
	"fmt"
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
//	transcript  (scrollable, wrapped, with clickable Copy / Apply rows)
//	──── divider (attachments chip or "edit ready: Apply / Discard") ────
//	input       (aiInputRows rows, caret shown when focused)
//	footer      (key hints; non-interactive)

const (
	aiInputRows   = 3
	aiPanelTopRow = 2 // header + divider drawn by actions_panel.go
)

// aiLine is one rendered transcript row before padding.
type aiLine struct {
	text    string
	style   lipgloss.Style
	bg      theme.Color256
	buttons []aiLineButton
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
	inputTop         int
	footerRow        int
	compact          bool // too short for input / footer
}

func aiGeom(height int) aiPanelGeom {
	body := height - aiPanelTopRow
	if body < aiInputRows+4 {
		return aiPanelGeom{transTop: aiPanelTopRow, transH: maxInt(body, 0), compact: true, dividerRow: -1, inputTop: -1, footerRow: -1}
	}
	g := aiPanelGeom{transTop: aiPanelTopRow}
	g.footerRow = height - 1
	g.inputTop = g.footerRow - aiInputRows
	g.dividerRow = g.inputTop - 1
	g.transH = g.dividerRow - g.transTop
	return g
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// renderAIPanelBody renders the panel rows below the header divider.
func (m Model) renderAIPanelBody(width, height int) []string {
	g := aiGeom(height)
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
	rows = append(rows, m.renderAIInput(width)...)
	rows = append(rows, renderAILine(m.aiFooterLine(width), width))
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

// aiTranscript builds the wrapped transcript (panel-local columns).
func (m Model) aiTranscript(width int) []aiLine {
	inner := width - 2
	if inner < 4 {
		inner = 4
	}
	label := func(s string) aiLine {
		return aiLine{text: " " + s, style: theme.Fg(theme.AIAccent).Bold(true)}
	}
	plain := func(s string, tok theme.Color256) aiLine {
		return aiLine{text: " " + s, style: theme.Fg(tok)}
	}
	var out []aiLine
	s := m.ai
	if s == nil || len(s.conv.Messages) == 0 {
		out = append(out, aiLine{})
		if s == nil || s.provider == nil || !s.provider.Configured() {
			out = append(out, plain("AI is not configured.", theme.TextSecondary))
			btn := " [ Configure… ]"
			out = append(out, aiLine{text: btn, style: theme.Fg(theme.TextSecondary), buttons: []aiLineButton{{1, runewidth.StringWidth(btn), "configure"}}})
			for _, l := range wrapPlain("Or set ANTHROPIC_API_KEY / OPENAI_API_KEY, or TERMOCODE_AI_PROVIDER=ollama.", inner) {
				out = append(out, plain(l, theme.TextDim))
			}
			return out
		}
		for _, l := range []string{
			"✦ Ask about your code.",
			"",
			"Enter send · Shift+Enter newline",
			"Alt+I edit selection with AI",
			"Ctrl+. → AI actions (Explain, Fix)",
			"Tab accepts ghost text",
		} {
			out = append(out, plain(l, theme.TextDim))
		}
		return out
	}
	for i, msg := range s.conv.Messages {
		switch msg.Role {
		case ai.StoredUser:
			out = append(out, label("You"))
			for _, l := range wrapPlain(aiCleanText(msg.Text), inner) {
				out = append(out, plain(l, theme.TextPrimary))
			}
			if msg.Context != "" {
				out = append(out, plain("📎 "+aiContextLabel(msg.Context), theme.TextDim))
			}
		case ai.StoredAssistant:
			out = append(out, label("✦ AI"))
			text := aiCleanText(msg.Text)
			if strings.TrimSpace(text) == "" && s.streaming && i == len(s.conv.Messages)-1 {
				out = append(out, aiLine{text: " thinking…", style: theme.Fg(theme.TextDim).Italic(true)})
				break
			}
			inCode := false
			for _, raw := range strings.Split(text, "\n") {
				if strings.HasPrefix(strings.TrimSpace(raw), "```") {
					inCode = !inCode
					out = append(out, aiLine{text: " " + strings.TrimSpace(raw), style: theme.Fg(theme.TextDim), bg: theme.BgEditor})
					continue
				}
				if inCode {
					for _, l := range aiWrapCode(raw, inner) {
						out = append(out, aiLine{text: " " + l, style: theme.Fg(theme.SyntaxString), bg: theme.BgEditor})
					}
					continue
				}
				for _, l := range wrapPlain(raw, inner) {
					out = append(out, plain(l, theme.TextPrimary))
				}
			}
			if _, ok := ai.FirstCodeBlock(msg.Text); ok && !(s.streaming && i == len(s.conv.Messages)-1) {
				copyLbl, applyLbl := "⧉ Copy", "✓ Apply to file"
				x := 1
				btns := []aiLineButton{{x, x + runewidth.StringWidth(copyLbl), "copy:" + strconv.Itoa(i)}}
				x += runewidth.StringWidth(copyLbl) + 3
				btns = append(btns, aiLineButton{x, x + runewidth.StringWidth(applyLbl), "apply:" + strconv.Itoa(i)})
				out = append(out, aiLine{text: " " + copyLbl + "   " + applyLbl, style: theme.Fg(theme.TextSecondary), buttons: btns})
			}
		case ai.StoredTool:
			for _, l := range wrapPlain(aiCleanText(msg.Text), inner) {
				out = append(out, plain(l, theme.TextDim))
			}
		case ai.StoredError:
			for _, l := range wrapPlain("⚠ "+aiCleanText(msg.Text), inner) {
				out = append(out, plain(l, theme.DiagError))
			}
		}
		out = append(out, aiLine{})
	}
	return out
}

// aiContextLabel is the first line of an attachment block ("main.go:3-9").
func aiContextLabel(ctx string) string {
	first := strings.SplitN(ctx, "\n", 2)[0]
	first = strings.TrimPrefix(first, "Attached ")
	return strings.TrimSuffix(first, ":")
}

// aiDividerLine is the row above the input: proposal banner, attachment
// chip, or a plain rule.
func (m Model) aiDividerLine(width int) aiLine {
	s := m.ai
	rule := func(text string, btns []aiLineButton) aiLine {
		w := runewidth.StringWidth(text)
		if w < width {
			text += " " + strings.Repeat("─", maxInt(width-w-1, 0))
		}
		return aiLine{text: text, style: theme.Fg(theme.BorderSubtle), buttons: btns}
	}
	if s != nil && s.proposal != nil {
		head := "─ ✦ edit ready "
		a, d := "[Apply]", "[Discard]"
		x := runewidth.StringWidth(head)
		btns := []aiLineButton{{x, x + len(a), "proposal-apply"}, {x + len(a) + 1, x + len(a) + 1 + len(d), "proposal-discard"}}
		return rule(head+a+" "+d, btns)
	}
	if s != nil && len(s.attachments) > 0 {
		head := fmt.Sprintf("─ 📎 %d attached ", len(s.attachments))
		x := runewidth.StringWidth(head)
		return rule(head+"[×]", []aiLineButton{{x, x + 3, "clear-attach"}})
	}
	return aiLine{text: strings.Repeat("─", width), style: theme.Fg(theme.BorderSubtle)}
}

// aiFooterLine is the dim key-hint row.
func (m Model) aiFooterLine(width int) aiLine {
	s := m.ai
	hint := " Alt+A chat · F6 focus"
	switch {
	case s != nil && s.streaming:
		hint = " ■ generating — Ctrl+C stop"
	case s != nil && s.focused:
		hint = " Enter send · Esc editor · PgUp/PgDn"
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

// renderAIInput draws the aiInputRows input rows.
func (m Model) renderAIInput(width int) []string {
	bgTok := theme.BgEditor
	bg := theme.Bg(bgTok)
	prefixFirst, prefix := " › ", "   "
	inner := width - runewidth.StringWidth(prefixFirst) - 1
	if inner < 1 {
		inner = 1
	}
	var input []rune
	caret, focused := 0, false
	if s := m.ai; s != nil {
		input, caret, focused = s.input, s.caret, s.focused
	}
	out := make([]string, 0, aiInputRows)
	pre := theme.FgBg(theme.AIAccent, bgTok).Bold(true)
	if len(input) == 0 {
		ph := "Ask AI…"
		if focused {
			ph = "Ask AI…  (Enter to send)"
		}
		row := pre.Render(prefixFirst)
		if focused {
			row += theme.FgBg(theme.BgEditor, theme.TextPrimary).Render(" ")
		}
		row += theme.FgBg(theme.TextDim, bgTok).Italic(true).Render(runewidth.Truncate(ph, inner, "…"))
		out = append(out, padStyled(row, width, bg))
		for len(out) < aiInputRows {
			out = append(out, bg.Render(strings.Repeat(" ", width)))
		}
		return out
	}
	rows, cr, cc := aiInputWrapped(input, caret, inner)
	first := 0
	if cr >= aiInputRows {
		first = cr - aiInputRows + 1
	}
	txt := theme.FgBg(theme.TextPrimary, bgTok)
	cur := theme.FgBg(theme.BgEditor, theme.TextPrimary)
	for i := first; i < len(rows) && len(out) < aiInputRows; i++ {
		p := prefix
		if i == 0 {
			p = prefixFirst
		}
		line := pre.Render(p)
		if focused && i == cr {
			rs := []rune(rows[i])
			// cc is a cell column; map to a rune index.
			idx, wsum := 0, 0
			for idx < len(rs) && wsum+runewidth.RuneWidth(rs[idx]) <= cc {
				wsum += runewidth.RuneWidth(rs[idx])
				idx++
			}
			at := " "
			rest := ""
			if idx < len(rs) {
				at = string(rs[idx])
				rest = string(rs[idx+1:])
			}
			line += txt.Render(string(rs[:idx])) + cur.Render(at) + txt.Render(rest)
		} else {
			line += txt.Render(rows[i])
		}
		out = append(out, padStyled(line, width, bg))
	}
	for len(out) < aiInputRows {
		out = append(out, bg.Render(strings.Repeat(" ", width)))
	}
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
	maxScroll := total - aiGeom(m.h).transH
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
	if msg.Y == 0 {
		switch m.hitActionsButton(msg.X, msg.Y) {
		case "new":
			return m.newAIChat(), true
		case "history":
			return m.openAIHistory(), true
		}
		return nil, false // pin / close handled by mouse.go
	}
	if msg.Y == 1 {
		return nil, true
	}
	localX := msg.X - x0
	width := m.actionsColumnWidth()
	g := aiGeom(m.h)
	switch {
	case msg.Y >= g.transTop && msg.Y < g.transTop+g.transH:
		lines := m.aiTranscript(width)
		start, _ := aiVisibleRange(len(lines), g.transH, s.scroll)
		idx := start + msg.Y - g.transTop
		if idx >= 0 && idx < len(lines) {
			for _, b := range lines[idx].buttons {
				if localX >= b.x0 && localX < b.x1 {
					return m.onAIButton(b.id), true
				}
			}
		}
		s.focused = true
		return nil, true
	case !g.compact && msg.Y == g.dividerRow:
		for _, b := range m.aiDividerLine(width).buttons {
			if localX >= b.x0 && localX < b.x1 {
				return m.onAIButton(b.id), true
			}
		}
		return nil, true
	case !g.compact && msg.Y >= g.inputTop && msg.Y < g.inputTop+aiInputRows:
		s.focused = true
		s.caret = len(s.input)
		return nil, true
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
	case strings.HasPrefix(id, "copy:"), strings.HasPrefix(id, "apply:"):
		i, err := strconv.Atoi(id[strings.IndexByte(id, ':')+1:])
		s := m.aiS()
		if err != nil || i < 0 || i >= len(s.conv.Messages) {
			return nil
		}
		code, ok := ai.FirstCodeBlock(s.conv.Messages[i].Text)
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
