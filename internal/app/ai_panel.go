package app

import (
	"slices"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/ai"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/rtl"
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
	text string
	// style draws text when styled is "" (nil: no colour). A pointer:
	// a lipgloss.Style is ~0.5 KB and a long answer has tens of
	// thousands of rows, most of them pre-styled.
	style   *lipgloss.Style
	bg      theme.Color256
	buttons []aiLineButton
	styled  string
	sel     aiSel // selectable text (ai_select.go)
	// msg and block are the message and code block the row belongs to,
	// index+1 (0 = none): the right-click menu's target.
	msg, block int
}

// aiStyle is st for aiLine.style.
func aiStyle(st lipgloss.Style) *lipgloss.Style { return &st }

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

// renderAIPanelBody renders the panel rows below the header divider,
// each passed through edge (the panel's left-edge marker, see
// renderActionsPanel). The visible transcript rows come from the
// transcript cache, so an unchanged chat costs no styling per frame.
func (m Model) renderAIPanelBody(width, height int, edge func(string) string) []string {
	g := m.aiPanelGeom(width, height)
	rows := make([]string, 0, height-aiPanelTopRow)

	tc := m.aiTranscriptCached(width)
	start, end := aiVisibleRange(len(tc.lines), g.transH, m.aiScroll())
	// The selection is drawn over the cached rows: only selected rows
	// are drawn again, the cache stays as it is.
	s0, s1, hasSel := m.aiSelRange(width)
	for i := start; i < end; i++ {
		if hasSel && i >= s0.row && i <= s1.row {
			if c0, c1, ok := aiSelRowSpan(tc.lines[i], i, s0, s1); ok {
				rows = append(rows, edge(aiHighlight(renderAILine(tc.lines[i], width), c0, c1, width)))
				continue
			}
		}
		rows = append(rows, tc.row(i, width, edge))
	}
	if len(rows) < g.transH {
		blank := tc.blankRow(width, edge)
		for len(rows) < g.transH {
			rows = append(rows, blank)
		}
	}
	if g.compact {
		return rows
	}
	rows = append(rows, edge(renderAILine(m.aiDividerLine(width), width)))
	return append(rows, m.aiInputEdged(width, g.inputRows, edge)...)
}

// aiInputCache keeps the input box rows (passed through edge) between
// frames. The key is everything renderAIInput reads.
type aiInputCache struct {
	ok                 bool
	width, rows, caret int
	input              []rune
	focused, streaming bool
	rk                 theme.RenderKey
	rtl                rtl.Mode // rtlMode the rows were laid out for
	out                []string
}

// aiInputEdged is renderAIInput passed through edge, cached in aiState.
func (m Model) aiInputEdged(width, rows int, edge func(string) string) []string {
	draw := func() []string {
		out := m.renderAIInput(width, rows)
		for i, r := range out {
			out[i] = edge(r)
		}
		return out
	}
	s := m.ai
	if s == nil {
		return draw()
	}
	rk := theme.CurrentRenderKey()
	c := &s.inputCache
	if c.ok && c.width == width && c.rows == rows && c.caret == s.caret && c.focused == s.focused &&
		c.streaming == s.streaming && c.rk == rk && c.rtl == rtlMode && slices.Equal(c.input, s.input) {
		return c.out
	}
	*c = aiInputCache{ok: true, width: width, rows: rows, caret: s.caret, input: slices.Clone(s.input),
		focused: s.focused, streaming: s.streaming, rk: rk, rtl: rtlMode, out: draw()}
	return c.out
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
	st := lipgloss.NewStyle()
	if l.style != nil {
		st = *l.style
	}
	if len(l.buttons) == 0 {
		return st.Background(theme.LG(bgTok)).Render(txt) + theme.Bg(bgTok).Render(strings.Repeat(" ", pad))
	}
	// Buttons: draw each span in the accent color, the rest in l.style.
	var b strings.Builder
	runes := []rune(txt)
	col := 0
	btnStyle := theme.FgBg(theme.AIAccent, bgTok).Bold(true)
	base := st.Background(theme.LG(bgTok))
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

// aiCleanText expands tabs and drops what must never reach the screen
// from model or tool text: whole escape sequences (CSI, OSC such as
// OSC 52 clipboard writes; not just the ESC byte, or "[2J" stays behind),
// C0/C1 control characters, and the bidi overrides and isolates
// (U+202A–202E, U+2066–2069) that can make "exe.txt" read as "txt.exe".
// The plain marks U+200E / U+200F stay: they do not reorder text.
func aiCleanText(s string) string {
	if strings.IndexByte(s, 0x1b) >= 0 {
		s = outputANSIRe.ReplaceAllString(s, "")
	}
	s = strings.ReplaceAll(s, "\t", "    ")
	s = strings.ReplaceAll(s, "\r", "")
	return strings.Map(func(r rune) rune {
		if r != '\n' && unsafeScreenRune(r) {
			return -1
		}
		return r
	}, s)
}

// unsafeScreenRune reports runes that outside text (model output, tool
// results, task output, extensions) must not put on the screen: C0 / C1
// controls and the bidi overrides and isolates.
func unsafeScreenRune(r rune) bool {
	return r < 0x20 || (r >= 0x7f && r <= 0x9f) ||
		(r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
}

// aiTranscriptCache keeps the last transcript (aiTranscript) and its
// drawn rows between frames. The key is everything the transcript reads:
// width, theme / icons / colour profile, provider state, streaming,
// resizing and the messages (strings compare by pointer first, so an
// unchanged chat is a cheap check; any edit makes a new string).
type aiTranscriptCache struct {
	ok          bool
	width       int
	rk          theme.RenderKey
	rtl         rtl.Mode // rtlMode the rows were laid out for
	resizing    bool
	streaming   bool
	hasProvider bool
	configured  bool
	label       string
	restoreGen  int
	runGen      int
	act         aiActivityView
	stall       string // stalled run nodes (agentsStallSig)
	msgs        []ai.StoredMessage
	lines       []aiLine
	rows        []string // edged rows by line index; "" = not drawn yet
	blank       string
}

// matches reports whether the cache is valid for this state.
func (c *aiTranscriptCache) matches(s *aiState, width int, rk theme.RenderKey, resizing bool, act aiActivityView, stall string) bool {
	if !c.ok || c.width != width || c.rk != rk || c.rtl != rtlMode || c.resizing != resizing || c.streaming != s.streaming || c.act != act || c.stall != stall ||
		c.hasProvider != (s.provider != nil) || len(c.msgs) != len(s.conv.Messages) || c.restoreGen != s.restoreGen ||
		c.runGen != s.runGen {
		return false
	}
	if s.provider != nil && (c.configured != s.provider.Configured() || c.label != s.modelLabel()) {
		return false
	}
	for i := range c.msgs {
		a, b := &c.msgs[i], &s.conv.Messages[i]
		if a.Role != b.Role || a.Text != b.Text || a.Context != b.Context || a.Model != b.Model || a.Lessons != b.Lessons || a.Memory != b.Memory {
			return false
		}
	}
	return true
}

// row returns transcript line i drawn at width and passed through edge.
func (c *aiTranscriptCache) row(i, width int, edge func(string) string) string {
	if c.rows == nil {
		return edge(renderAILine(c.lines[i], width))
	}
	if c.rows[i] == "" {
		c.rows[i] = edge(renderAILine(c.lines[i], width))
	}
	return c.rows[i]
}

// blankRow is an empty transcript row passed through edge.
func (c *aiTranscriptCache) blankRow(width int, edge func(string) string) string {
	if c.rows == nil {
		return edge(theme.Bg(theme.BgPanel).Render(strings.Repeat(" ", width)))
	}
	if c.blank == "" {
		c.blank = edge(theme.Bg(theme.BgPanel).Render(strings.Repeat(" ", width)))
	}
	return c.blank
}

// aiTranscript returns the transcript lines (see buildAITranscript),
// reusing the last build while nothing it reads has changed. Callers
// must not modify the result.
func (m Model) aiTranscript(width int) []aiLine {
	return m.aiTranscriptCached(width).lines
}

// aiTranscriptCached returns the transcript cache entry for width,
// rebuilding it when stale. Without AI state nothing is cached (the
// empty "not configured" page is short).
func (m Model) aiTranscriptCached(width int) *aiTranscriptCache {
	s := m.ai
	if s == nil {
		return &aiTranscriptCache{lines: m.buildAITranscript(width)}
	}
	rk := theme.CurrentRenderKey()
	resizing := m.dragKind == dragActionsLeft
	c := &s.transcript
	now := clock.Now()
	act, stall := s.activityView(now), s.agentsStallSig(now)
	if c.matches(s, width, rk, resizing, act, stall) {
		return c
	}
	canKeepRows := len(c.lines) > 0 && len(s.streamTranscript.lines) > 0 && &c.lines[0] == &s.streamTranscript.lines[0]
	lines := m.buildAITranscriptAt(width, act)
	if c.ok {
		s.aiKeepSelection(c.msgs, c.width, width)
	}
	rows := make([]string, 0)
	kept := s.streamTranscript.reused
	if canKeepRows && kept > 0 && kept <= len(c.rows) {
		rows = append(c.rows[:kept], make([]string, len(lines)-kept)...)
	} else {
		rows = make([]string, len(lines))
	}
	*c = aiTranscriptCache{ok: true, width: width, rk: rk, rtl: rtlMode, resizing: resizing, streaming: s.streaming, act: act, stall: stall,
		hasProvider: s.provider != nil, restoreGen: s.restoreGen, runGen: s.runGen, msgs: append([]ai.StoredMessage(nil), s.conv.Messages...),
		lines: lines, rows: rows}
	if s.provider != nil {
		c.configured, c.label = s.provider.Configured(), s.modelLabel()
	}
	s.pruneMDCache(len(s.conv.Messages))
	s.pruneSegCache(len(s.conv.Messages))
	return c
}

// buildAITranscript builds the transcript (panel-local columns): padded
// content, user messages as tinted bubbles on the right, AI answers as
// rendered Markdown under an "✦ AI · model" line.
func (m Model) buildAITranscript(width int) []aiLine {
	return m.buildAITranscriptAt(width, m.aiActivityView(clock.Now()))
}

// buildAITranscriptAt builds the transcript with the activity line act.
func (m Model) buildAITranscriptAt(width int, act aiActivityView) []aiLine {
	inner := width - aiIndent - 1
	if inner < 4 {
		inner = 4
	}
	ind := strings.Repeat(" ", aiIndent)
	plain := func(s string, tok theme.Color256) aiLine {
		return aiLine{text: ind + s, style: aiStyle(theme.Fg(tok))}
	}
	// selPlain wraps text into selectable rows of message msgIdx.
	selPlain := func(text string, tok theme.Color256, msgIdx int) []aiLine {
		rows, joins, maps, right := aiBidiWrap(text, inner)
		out := make([]aiLine, len(rows))
		for k, r := range rows {
			gap := 0
			if right[k] {
				gap = max(inner-runewidth.StringWidth(r), 0)
			}
			out[k] = plain(strings.Repeat(" ", gap)+r, tok)
			out[k].msg = msgIdx + 1
			out[k].sel = aiSel{ok: true, x: aiIndent + gap, text: r, join: joins[k], bidi: maps[k]}
		}
		return out
	}
	var out []aiLine
	s := m.ai
	reuse := false
	if s != nil {
		reuse = s.streamTranscript.prepare(m, width, m.dragKind == dragActionsLeft)
	}
	if s != nil && !reuse {
		// About as long as the last build: no regrowing a long transcript.
		out = make([]aiLine, 0, len(s.transcript.lines)+64)
	}
	if s != nil && s.conv.ForkedFrom != nil { // G-FORK: "Forked from <title> at message N"
		out = append(out, aiLine{})
		for _, l := range wrapPlain(s.conv.ForkNote(), inner) {
			out = append(out, plain(l, theme.TextDim))
		}
	}
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
		out = append(out, aiLine{text: ind + aiSpark() + " Ask about your code", style: aiStyle(theme.Fg(theme.AIAccent).Bold(true))})
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
	turn := -1 // user message of the turn being drawn
	undo := func() {
		// R3: an "Undo AI changes" button under a finished turn that
		// changed files.
		if turn < 0 || (s.streaming && turn == s.turnMsg) {
			return
		}
		if n := m.aiRestoreFiles(turn); n > 0 {
			out = append(out, aiChip("↶ Undo AI changes ("+strconv.Itoa(n)+" file"+plural(n)+")", "undo-turn:"+strconv.Itoa(turn), width))
		}
	}
	resizing := m.dragKind == dragActionsLeft
	first := 0
	if reuse {
		out = s.streamTranscript.lines[:s.streamTranscript.start]
		turn = s.streamTranscript.turn
		first = last
	}
	for i := first; i < len(s.conv.Messages); i++ {
		msg := s.conv.Messages[i]
		if msg.Role == ai.StoredUser {
			undo()
			turn = i
		}
		streaming := s.streaming && i == last
		var key aiSegKey
		cacheable := !resizing && !streaming && msg.Run == nil
		if cacheable {
			key = m.aiSegKeyFor(i, width)
			if e, ok := s.segCache[i]; ok && e.key == key {
				out = append(out, e.lines...)
				continue
			}
		}
		start := len(out)
		if i == last && s.streamTranscript.active {
			s.streamTranscript.start = start
			s.streamTranscript.turn = turn
		}
		switch msg.Role {
		case ai.StoredUser:
			if msg.Summary != "" { // OMX-3
				out = append(out, aiSummaryLine(msg.Summarised, width))
			}
			out = append(out, aiLine{sel: aiSel{ok: true, join: "\n"}})
			out = append(out, aiUserBubble(aiCleanText(msg.Text), width, i)...)
			if msg.Context != "" {
				out = append(out, aiRightChip(aiAttachGlyph()+" "+aiContextLabel(msg.Context), width))
			}
			if msg.KeepGoing > 0 { // OMX-7: the keep-going loop sent this
				out = append(out, aiRightChip(s.aiKeepGoingChip(msg.KeepGoing), width))
			}
			if msg.Rules != "" || msg.Lessons > 0 { // OM-1/OM-2: "rules: AGENTS.md (4 KB) · memory: 3 lessons"
				out = append(out, aiRulesLine(msg.Rules, msg.Lessons, width))
			}
			if i+1 < len(s.conv.Messages) { // G-API: the AI wrote this task for a run it started
				if by := aiRunByLine(s.conv.Messages[i+1].Run); by != "" {
					out = append(out, aiRightChip(aiSpark()+" "+by, width))
				}
			}
		case ai.StoredAssistant:
			if msg.Run != nil { // a pipeline run: step cards (ai_run_cards.go)
				out = append(out, m.aiRunLines(i, msg, width, inner)...)
				break
			}
			out = append(out, aiLine{sel: aiSel{ok: true, join: "\n"}})
			text := ""
			if streaming {
				if s.cleanAnswerIndex != i {
					s.answerCleaner = aiStreamCleaner{}
					s.cleanAnswerIndex = i
				}
				text = s.answerCleaner.clean(msg.Text)
			} else {
				text = aiCleanText(msg.Text)
			}
			out = append(out, m.aiAnswerHeader(width, i, !streaming))
			if strings.TrimSpace(text) == "" && streaming {
				break // the activity line below says what the model does
			}
			stable := 0
			st := &s.mdStream
			if reuse && st.ok && st.i == i && st.width == width && st.theme == theme.CurrentRenderKey() && st.rtl == rtlMode && st.startsWith(text) {
				stable = len(trimBlankAILines(st.lines))
			}
			answer := s.renderAnswer(i, text, width, streaming, resizing)
			stable = min(stable, len(answer))
			if stable > 0 {
				out = out[:len(out)+stable]
				s.streamTranscript.reused = len(out)
			}
			out = append(out, answer[stable:]...)
		case ai.StoredTool:
			out = append(out, selPlain(aiCleanText(msg.Text), theme.TextDim, i)...)
			out = append(out, s.aiToolDetailLines(i, width)...) // MCPX-33
		case ai.StoredError:
			out = append(out, selPlain("⚠ "+aiCleanText(msg.Text), theme.DiagError, i)...)
		case ai.StoredNote: // OMX-7
			out = append(out, selPlain(aiCleanText(msg.Text), theme.TextDim, i)...)
		}
		if msg.Memory > 0 { // OM-2: "memory: 2 lessons saved · view"
			out = append(out, aiMemorySavedLine(msg.Memory, width))
		}
		if cacheable {
			if s.segCache == nil {
				s.segCache = map[int]aiSegEntry{}
			}
			s.segCache[i] = aiSegEntry{key: key, lines: slices.Clip(out[start:])}
		}
	}
	out = append(out, m.aiTodoCard(width)...)              // OMX-7
	out = append(out, m.aiMemoryAskLines(width, inner)...) // OM-2 ask-once card
	undo()
	out = append(out, act.lines(ind, inner)...)
	out = append(out, aiLine{sel: aiSel{ok: true, join: "\n"}})
	if s.streamTranscript.active {
		s.streamTranscript.lines = out
	}
	return out
}

// aiSegKey is everything the transcript lines of one finished message
// depend on (buildAITranscriptAt): the message, the "run by" line of the
// next one, the width, the theme and the model label of its header.
type aiSegKey struct {
	role, text, context, rules, model, runBy, label string
	lessons, memory, width, summarised, keepGoing   int
	detail                                          int // MCP call details: 0 none, 1 closed, 2 open
	rk                                              theme.RenderKey
	rtl                                             rtl.Mode
}

type aiSegEntry struct {
	key   aiSegKey
	lines []aiLine
}

// aiSegKeyFor is the segment key of message i at width.
func (m Model) aiSegKeyFor(i, width int) aiSegKey {
	s := m.ai
	msg := &s.conv.Messages[i]
	k := aiSegKey{role: msg.Role, text: msg.Text, context: msg.Context, rules: msg.Rules, model: msg.Model,
		lessons: msg.Lessons, memory: msg.Memory, summarised: msg.Summarised, keepGoing: msg.KeepGoing, width: width, rk: theme.CurrentRenderKey(), rtl: rtlMode,
		detail: s.aiToolDetailState(i)}
	if i+1 < len(s.conv.Messages) {
		k.runBy = aiRunByLine(s.conv.Messages[i+1].Run)
	}
	if s.provider != nil {
		k.label = s.modelLabel()
	}
	return k
}

// pruneSegCache drops the segments of messages past n (a new or
// shortened chat).
func (s *aiState) pruneSegCache(n int) {
	for i := range s.segCache {
		if i >= n {
			delete(s.segCache, i)
		}
	}
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
func aiUserBubble(text string, width, msgIdx int) []aiLine {
	maxW := (width - aiIndent - 1) * 85 / 100
	if maxW < 10 {
		maxW = width - aiIndent - 1
	}
	lines, joins, maps, right := aiBidiWrap(text, maxW-2)
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
	bub := lipgloss.NewStyle().Background(theme.Wash(theme.BgPanel, theme.WashSelectedFocused)).Foreground(theme.LG(theme.TextWhite))
	lead := theme.Bg(theme.BgPanel).Render(strings.Repeat(" ", x))
	out := make([]aiLine, 0, len(lines))
	for k, l := range lines {
		fill := bw - 2 - runewidth.StringWidth(l)
		if fill < 0 {
			fill = 0
		}
		body := " " + l + strings.Repeat(" ", fill) + " "
		tx := x + 1
		if right[k] { // a right-to-left row ends at the bubble's right side
			body = " " + strings.Repeat(" ", fill) + l + " "
			tx += fill
		}
		row := aiLine{styled: lead + bub.Render(body), msg: msgIdx + 1,
			sel: aiSel{ok: true, x: tx, text: l, join: joins[k], bidi: maps[k]}}
		// A copy button left of the bubble's first row.
		if k == 0 && x >= 3 {
			g := aiCopyGlyph()
			gx := x - 1 - runewidth.StringWidth(g)
			row.styled = theme.Bg(theme.BgPanel).Render(strings.Repeat(" ", gx)) +
				theme.FgBg(theme.TextDim, theme.BgPanel).Render(g) + theme.Bg(theme.BgPanel).Render(" ") + bub.Render(body)
			row.buttons = []aiLineButton{{gx, gx + runewidth.StringWidth(g), "copymsg:" + strconv.Itoa(msgIdx)}}
		}
		out = append(out, row)
	}
	return out
}

// aiCopyGlyph is the icon of a message's copy button.
func aiCopyGlyph() string {
	switch theme.CurrentIconMode {
	case theme.IconModeNerdFont:
		return "\uf0c5" // nf-fa-copy
	case theme.IconModeUnicode:
		return "⧉"
	}
	return "c"
}

// aiCopyLabel is the label of the copy buttons ("⧉ Copy").
func aiCopyLabel() string {
	switch theme.CurrentIconMode {
	case theme.IconModeNerdFont:
		return "\uf0c5 Copy"
	case theme.IconModeUnicode:
		return "⧉ Copy"
	}
	return "Copy"
}

// aiAnswerHeader is the "✦ AI · model" line above answer msgIdx, with a
// Copy button (the whole answer) on the right once it is done.
func (m Model) aiAnswerHeader(width, msgIdx int, done bool) aiLine {
	label := ""
	if s := m.ai; s != nil && s.provider != nil {
		label = s.modelLabel()
		if msgIdx >= 0 && msgIdx < len(s.conv.Messages) && s.conv.Messages[msgIdx].Model != "" {
			label = s.conv.Messages[msgIdx].Model // the step's slot model, not the main one
		}
	}
	return m.aiAnswerHeaderModel(width, msgIdx, done, label)
}

// aiAnswerHeaderModel is aiAnswerHeader naming model ("" = none).
func (m Model) aiAnswerHeaderModel(width, msgIdx int, done bool, model string) aiLine {
	bg := theme.Bg(theme.BgPanel)
	row := bg.Render(strings.Repeat(" ", aiIndent)) +
		theme.FgBg(theme.AIAccent, theme.BgPanel).Bold(true).Render(aiSpark()+" AI")
	if model != "" {
		row += theme.FgBg(theme.TextDim, theme.BgPanel).Render(" · " + model)
	}
	l := aiLine{styled: row, msg: msgIdx + 1}
	if !done {
		return l
	}
	label := aiCopyLabel()
	bw, used := runewidth.StringWidth(label), textWidth(row)
	if x := width - 2 - bw; x >= used+2 {
		l.styled += bg.Render(strings.Repeat(" ", x-used)) + theme.FgBg(theme.TextDim, theme.BgPanel).Render(label)
		l.buttons = []aiLineButton{{x, x + bw, "copymsg:" + strconv.Itoa(msgIdx)}}
	}
	return l
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
	key := aiMDKey{text: text, width: width, theme: theme.CurrentRenderKey(), streaming: streaming, rtl: rtlMode}
	if s.mdCache == nil {
		s.mdCache = map[int]aiMDEntry{}
	}
	if e, ok := s.mdCache[i]; ok && (e.key == key || (resizing && e.key.text == text && e.key.theme == key.theme)) {
		return e.lines
	}
	lines := s.renderAnswerParts(i, text, width, streaming)
	s.mdCache[i] = aiMDEntry{key: key, lines: lines}
	return lines
}

type aiMDKey struct {
	text      string
	width     int
	theme     theme.RenderKey
	streaming bool
	rtl       rtl.Mode
}

// dropRenderCaches frees the drawn transcript, rendered answers and
// input box (the panel was hidden); the next draw builds them again.
func (s *aiState) dropRenderCaches() {
	if !s.transcript.ok && !s.inputCache.ok && len(s.mdCache) == 0 {
		return
	}
	s.transcript, s.inputCache, s.mdCache = aiTranscriptCache{}, aiInputCache{}, nil
	s.mdStream = aiMDStream{}
	s.streamTranscript = aiStreamTranscript{}
	s.answerCleaner = aiStreamCleaner{}
}

// pruneMDCache drops rendered answers past the last message (a cleared
// or switched chat), so the cache never holds more than the open chat.
func (s *aiState) pruneMDCache(n int) {
	for i := range s.mdCache {
		if i >= n {
			delete(s.mdCache, i)
		}
	}
	if s.mdStream.i >= n {
		s.mdStream = aiMDStream{}
	}
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
	if n := m.aiReviewPending(); n > 0 && !m.reviewOpen {
		head := " " + aiSpark() + " " + strconv.Itoa(n) + " file" + plural(n) + " to review "
		a := "Review"
		ax := runewidth.StringWidth(head)
		row := theme.FgBg(theme.AIAccent, theme.BgPanel).Bold(true).Render(head) + chip(a, theme.AccentGreen, 0.25)
		return aiLine{styled: row, buttons: []aiLineButton{{ax, ax + len(a) + 2, "review-open"}}}
	}
	if s != nil && len(s.attachments) == 0 {
		if l, ok := m.mcpSignInBar(); ok {
			return l
		}
	}
	level, word := "", ""
	if s != nil {
		level, word = s.aiInputThink() // OMX-5: "think" / "ultrathink" in the input
	}
	kw, hasKw := s.aiInputKeyword() // OMX-17: ulw and friends
	if s != nil && (len(s.attachments) > 0 || word != "" || hasKw) {
		// Each chip is " ⊕ label × "; its "× " removes it.
		row := bg.Render(" ")
		x := 1
		var buttons []aiLineButton
		if word != "" { // the keyword's reasoning level for this message
			lbl := aiSpark() + " " + word + " · " + level + " ×"
			row += chip(lbl, theme.AIAccent, 0.22) + bg.Render(" ")
			w := runewidth.StringWidth(lbl)
			buttons = append(buttons, aiLineButton{x + w, x + w + 2, "rm-think"})
			x += w + 3
		}
		if hasKw { // OMX-17: " ⚡ ulw → Work until done × Alt+Q "
			lbl := "⚡ " + kw.word + " → " + kw.does + " ×"
			row += chip(lbl, theme.AIAccent, 0.25) + bg.Render(" ")
			w := runewidth.StringWidth(lbl)
			buttons = append(buttons, aiLineButton{x + w, x + w + 2, "rm-keyword"})
			x += w + 3
			hint := "Alt+Q plain text "
			row += theme.FgBg(theme.TextDim, theme.BgPanel).Render(hint)
			x += runewidth.StringWidth(hint)
		}
		for i, at := range s.attachments {
			lbl := aiAttachGlyph() + " " + at.Label + " ×"
			row += chip(lbl, theme.TextPrimary, 0.10) + bg.Render(" ")
			w := runewidth.StringWidth(lbl)
			buttons = append(buttons, aiLineButton{x + w, x + w + 2, "rm-attach:" + strconv.Itoa(i)})
			x += w + 3
		}
		return aiLine{styled: row, buttons: buttons}
	}
	return aiLine{}
}

// aiInputHints are the bottom-border hints, longest first (the first
// that fits is shown).
func aiInputHints(s *aiState) []string {
	switch {
	case s != nil && s.streaming:
		return []string{"generating · Ctrl+C stop", "Ctrl+C stop"}
	case s != nil && s.focused:
		return []string{"Enter send · Shift+Enter newline", "Enter send"}
	}
	return []string{"Alt+A chat · F6 focus", "F6 focus"}
}

// aiInputWrapped wraps the input into rows and returns the row / column
// of the caret.
func aiInputWrapped(input []rune, caret, w int) ([]string, int, int) {
	rows, _, cr, cc := aiInputRowStarts(input, caret, w)
	return rows, cr, cc
}

// aiInputRowStarts is aiInputWrapped plus the input index where each row
// starts.
func aiInputRowStarts(input []rune, caret, w int) (rows []string, starts []int, cr, cc int) {
	if w < 1 {
		w = 1
	}
	line := []rune{}
	lineW, start := 0, 0
	for i := 0; i <= len(input); i++ {
		if i == caret {
			cr, cc = len(rows), lineW
		}
		if i == len(input) {
			break
		}
		r := input[i]
		if r == '\n' {
			rows, starts = append(rows, string(line)), append(starts, start)
			line, lineW, start = line[:0:0], 0, i+1
			continue
		}
		rw := runewidth.RuneWidth(r)
		if lineW+rw > w {
			rows, starts = append(rows, string(line)), append(starts, start)
			line, lineW, start = line[:0:0], 0, i
			if i == caret {
				cr, cc = len(rows), 0
			}
		}
		line = append(line, r)
		lineW += rw
	}
	rows, starts = append(rows, string(line)), append(starts, start)
	if cc >= w { // caret after a full row → start of next visual row
		cr, cc = cr+1, 0
		if cr >= len(rows) {
			rows, starts = append(rows, ""), append(starts, len(input))
		}
	}
	return rows, starts, cr, cc
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
		wrapped, starts, cr, cc := aiInputRowStarts(input, caret, tw)
		first := 0
		if cr >= rows {
			first = cr - rows + 1
		}
		for i := first; i < len(wrapped) && len(out) < rows+1; i++ {
			rs := []rune(wrapped[i])
			if on, dir := rtlPara(aiInputPara(input, starts[i])); on {
				// Right-to-left: drawn shaped and in reading order; the
				// caret stays logical (on the glyph of the rune at it).
				co := -1
				if focused && i == cr {
					co = caret - starts[i]
				}
				c, w := aiInputBidiRow(rs, dir, co, txt, cur)
				if dir == rtl.RTL && w < tw {
					c, w = fill.Render(strings.Repeat(" ", tw-w))+c, tw
				}
				out = append(out, line(c, w))
				continue
			}
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
	// The attach button sits at the right end of the bottom border.
	btn, btnW := "", 0
	if _, _, l, ok := aiAttachButton(width); ok {
		btn, btnW = l, runewidth.StringWidth(l)
	}
	hint := ""
	for _, h := range aiInputHints(m.ai) {
		if runewidth.StringWidth(h)+4+btnW+1 <= inner {
			hint = " " + h + " "
			break
		}
	}
	hw := runewidth.StringWidth(hint)
	bottom := edge.Render("╰─") + lipgloss.NewStyle().Background(theme.LG(theme.BgPanel)).Foreground(theme.LG(theme.TextDim)).Render(hint)
	if btn != "" {
		bottom += edge.Render(strings.Repeat("─", maxInt(inner-2-hw-btnW, 0))) +
			theme.FgBg(theme.AIAccent, theme.BgPanel).Bold(true).Render(btn) + edge.Render("─╯")
	} else {
		bottom += edge.Render(strings.Repeat("─", maxInt(inner-1-hw, 0)) + "╯")
	}
	out = append(out, panel.Render(" ")+bottom+panel.Render(" "))
	return out
}

// padStyled pads (or cuts) a styled row to exactly width cells.
func padStyled(s string, width int, bg lipgloss.Style) string {
	w := textWidth(s)
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
	if s.agents.on { // U20 AP-9: the AGENTS view has its own keys
		return m.routeAgentsKey(msg)
	}
	switch msg.String() {
	case "esc":
		if s.cardSel != "" { // drop the step card selection first
			s.cardSel = ""
			s.runGen++
			return nil, true
		}
		if _, _, ok := s.sel.rng(); ok {
			s.sel.clear() // the first Esc drops the selection
			return nil, true
		}
		if s.streaming { // then Esc stops a running answer (B1)
			return m.aiCancelAll(), true
		}
		s.focused = false
		m.focus = FocusEditor
		return nil, true
	case "enter":
		if s.cardSel != "" && len(s.input) == 0 { // Enter toggles the selected step card
			m.aiToggleCard(s.cardSel)
			return nil, true
		}
		return m.submitAIInput(), true
	case "alt+q": // OMX-17: remove the keyword chip
		if _, ok := s.aiInputKeyword(); ok {
			s.kwOff = true
			return nil, true
		}
		return nil, false
	case "shift+enter", "alt+enter", "ctrl+j":
		m.aiInsert([]rune{'\n'})
		return nil, true
	case "backspace", "ctrl+h":
		if s.caret == 0 && len(s.input) == 0 && len(s.attachments) > 0 {
			s.attachments = s.attachments[:len(s.attachments)-1]
			return nil, true
		}
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
	case "ctrl+c", "ctrl+shift+c":
		// With a selection: copy it. Without: Ctrl+C stops the AI.
		if cmd, ok := m.aiCopySelection(); ok {
			return cmd, true
		}
		if msg.String() == "ctrl+shift+c" {
			return nil, false
		}
		return m.aiCancelAll(), true
	case "ctrl+n":
		return m.newAIChat(), true
	case aiForkKey: // G-FORK: pick the message to fork from
		return m.openAIForkPicker(), true
	case aiMCPBackgroundKey: // a running MCP call goes to the background (MCPX-19)
		return m.aiMCPDetach()
	case "tab", "shift+tab": // select a step card (Enter toggles it)
		if msg.String() == "tab" && strings.HasPrefix(string(s.input), "/") { // MCP prompt (MCPX-13)
			return m.aiMCPPromptTab(), true
		}
		dir := 1
		if msg.String() == "shift+tab" {
			dir = -1
		}
		m.aiCycleCard(dir)
		return nil, true
	}
	if msg.Alt {
		return nil, false
	}
	switch msg.Type {
	case tea.KeyRunes:
		if msg.Paste { // a dropped file arrives as a pasted path
			if paths, ok := aiDroppedPaths(string(msg.Runes), aiWorkspaceRoot()); ok {
				return m.aiAttachPaths(paths), true
			}
		}
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
	if _, ok := m.aiReady(); ok && !s.streaming {
		if cmd, ok := m.aiMCPPromptSubmit(text); ok { // "/server.prompt" (MCPX-13)
			return cmd
		}
	}
	var attach tea.Cmd
	if _, ok := m.aiReady(); ok && !s.streaming {
		attach = m.aiAttachMentions(text) // "@path" words
	}
	if kw, ok := s.aiInputKeyword(); ok && !s.streaming {
		s.kwProfile = kw.profile // OMX-17
	}
	cmd := tea.Batch(attach, m.sendAIChat(text))
	s.kwProfile = ""
	if !s.streaming {
		return cmd // not sent (unconfigured / busy) — keep the draft
	}
	s.input, s.caret, s.kwOff = nil, 0, false
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
		case "new", "history", "mcp", "profile", "think", "usage", "chat", "more":
			return "ai:" + id
		}
		return ""
	case 1:
		return "ai:body"
	}
	localX := x - x0
	if m.agentsShowing() {
		return m.agentsTargetAt(localX, y)
	}
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
		if bx0, bx1, _, ok := aiAttachButton(width); ok && y == g.footerRow && localX >= bx0 && localX < bx1 {
			return "ai:btn:attach-file"
		}
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
	// A selection drag follows the pointer anywhere (and scrolls the
	// transcript above / below it) until the button is released.
	if s.sel.dragging {
		switch {
		case msg.Action == tea.MouseActionMotion:
			m.aiSelDragTo(msg.X, msg.Y)
			return nil, true
		case msg.Action == tea.MouseActionRelease:
			return m.aiSelRelease(), true
		}
	}
	x0 := m.w - m.actionsColumnWidth()
	if msg.X < x0 || (msg.X == x0 && msg.Y > 0) {
		if msg.Type == tea.MouseLeft {
			s.sel.clear()
			if s.focused {
				s.focused = false
			}
		}
		return nil, false
	}
	switch msg.Type {
	case tea.MouseWheelUp:
		if s.agents.on {
			m.agentsScroll(-3)
			return nil, true
		}
		m.scrollAIPanel(3)
		return nil, true
	case tea.MouseWheelDown:
		if s.agents.on {
			m.agentsScroll(3)
			return nil, true
		}
		m.scrollAIPanel(-3)
		return nil, true
	case tea.MouseLeft:
	case tea.MouseRight:
		if msg.Action == tea.MouseActionPress && msg.Y > 1 {
			m.aiChatRightClick(msg.X, msg.Y)
		}
		return nil, msg.Y > 0
	default:
		return nil, msg.Y > 0
	}
	id := m.aiPanelTargetAt(msg.X, msg.Y)
	if id != "ai:transcript" && id != "" {
		s.sel.clear() // a click elsewhere in the panel drops the selection
	}
	switch {
	case id == "":
		return nil, false // pin / close handled by mouse.go
	case id == "ai:new":
		s.agents.on = false
		return m.newAIChat(), true
	case id == "ai:history":
		s.agents.on = false
		return m.openAIHistory(), true
	case id == "ai:chat":
		s.agents.on, s.focused = false, true
	case strings.HasPrefix(id, "ai:agents"):
		s.focused = true
		if node := strings.TrimPrefix(id, "ai:agents:"); node != id {
			s.agents.sel, s.agents.manual, s.agents.free = node, true, false
		}
	case id == "ai:mcp":
		return m.onMCPChip(), true
	case id == "ai:profile":
		return m.openAIProfilePicker(), true
	case id == "ai:usage":
		return m.showAIUsage(""), true
	case id == "ai:think":
		return m.cycleAIReasoning(), true
	case id == "ai:more":
		return m.openAIMoreMenu(), true
	case strings.HasPrefix(id, "ai:btn:"):
		return m.onAIButton(strings.TrimPrefix(id, "ai:btn:")), true
	case id == "ai:transcript":
		s.focused = true
		m.aiSelPress(msg.X, msg.Y)
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
	case strings.HasPrefix(id, "mcp-signin:"):
		return m.mcpConnectServer(strings.TrimPrefix(id, "mcp-signin:"))
	case strings.HasPrefix(id, "mcp-notnow:"):
		s := m.aiS()
		if s.mcpNotNow == nil {
			s.mcpNotNow = map[string]bool{}
		}
		s.mcpNotNow[strings.TrimPrefix(id, "mcp-notnow:")] = true
		return nil
	case id == "review-open":
		return m.showAIReview()
	case strings.HasPrefix(id, "step:"):
		m.aiToggleCard(id)
		return nil
	case strings.HasPrefix(id, "usage:"): // a run row's tokens and cost
		return m.showAIUsage(strings.TrimPrefix(id, "usage:"))
	case strings.HasPrefix(id, "agents:"): // a planned run's row: show it in AGENTS
		return m.showAgents(strings.TrimPrefix(id, "agents:"))
	case strings.HasPrefix(id, "wt-"): // G-WT: a step's worktree (ai_worktree_actions.go)
		return m.onAIWorktreeButton(id)
	case strings.HasPrefix(id, "run-retry:"):
		if i, err := strconv.Atoi(strings.TrimPrefix(id, "run-retry:")); err == nil {
			return m.aiRetryRun(i)
		}
		return nil
	case strings.HasPrefix(id, "plan-open:"): // G-PLAN (ai_plan_approval.go)
		return m.openAIPlanCard()
	case strings.HasPrefix(id, "plan-deny:"):
		return m.answerAIPlan("deny")
	case strings.HasPrefix(id, "run-continue:"):
		return m.answerAIBudget("continue")
	case strings.HasPrefix(id, "run-stop:"):
		return m.answerAIBudget("stop")
	case strings.HasPrefix(id, "undo-turn:"):
		if i, err := strconv.Atoi(strings.TrimPrefix(id, "undo-turn:")); err == nil {
			return m.aiUndoTurn(i)
		}
		return nil
	case id == "memask-on":
		return m.answerAIMemoryAsk("on")
	case id == "memask-notnow":
		return m.answerAIMemoryAsk("notnow")
	case strings.HasPrefix(id, "tooldetail:"): // MCPX-33: open / close an MCP call's details
		m.aiS().aiToggleToolDetail(id)
		return nil
	case id == "memory-view": // OM-2: the memory line opens AI: Memory…
		return m.openAIMemory()
	case strings.HasPrefix(id, "rules-open:"):
		return m.openAIRulesFile(strings.TrimPrefix(id, "rules-open:"))
	case id == "attach-file":
		return m.openAIAttachPicker()
	case id == "rm-think": // OMX-5: send this message without the keyword's level
		m.aiS().thinkOff = true
		return nil
	case id == "rm-keyword": // OMX-17: send the draft as plain text
		m.aiS().kwOff = true
		return nil
	case strings.HasPrefix(id, "rm-attach:"):
		s := m.aiS()
		if i, err := strconv.Atoi(strings.TrimPrefix(id, "rm-attach:")); err == nil && i >= 0 && i < len(s.attachments) {
			s.attachments = append(s.attachments[:i], s.attachments[i+1:]...)
		}
		return nil
	case strings.HasPrefix(id, "save:"):
		return m.aiSaveCodeBlockAs(id)
	case strings.HasPrefix(id, "copymsg:"):
		i, err := strconv.Atoi(strings.TrimPrefix(id, "copymsg:"))
		if err != nil {
			return nil
		}
		return m.aiCopyMessage(i)
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
			return m.copyWithToast(code, "Copied code block")
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
	var attach tea.Cmd // the attach toast's expiry timer
	if m.editor.Path() != "" && len(s.attachments) == 0 {
		attach = m.aiAttachCurrent()
	}
	s.input, s.caret = []rune(prompt), len([]rune(prompt))
	return tea.Batch(attach, m.submitAIInput())
}
