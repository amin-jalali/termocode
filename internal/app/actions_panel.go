package app

import (
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Right-side Actions panel.
//
// Two states:
//
//  1. Pinned (open):  occupies the rightmost `actionsWidth` columns of the
//     window. Editor pane shrinks to make room. Header has [pin] [close]
//     buttons; body shows a placeholder.
//  2. Unpinned (collapsed): a single tiny launcher chip floats at the
//     bottom-right, just above the status bar. Clicking it re-pins.
//
// `actionsOpen=false` hides BOTH the panel and the chip — the user has to
// use the keybinding / palette to bring it back.
//
// All click hit-zones are centralized at the bottom of this file so
// mouse.go can call `m.hitActionsHeader(...)` / `m.hitActionsLauncher(...)`
// without having to know the panel's internal layout.

// The header's buttons use the shared chrome glyphs (theme.Glyph*).

// actionsLauncherLabel is the launcher chip shown when unpinned.
const actionsLauncherLabel = "▸ ✦ AI"

// actionsPanelVisible reports whether the pinned (column-occupying) panel
// is currently rendered. False when the user closed it OR when it's
// unpinned (in which case only the launcher chip shows up).
func (m Model) actionsPanelVisible() bool {
	return m.actionsOpen && m.actionsPinned && m.actionsColumnWidth() > 0
}

// actionsLauncherVisible reports whether the bottom-right launcher chip
// should be rendered. True only when the panel is "open but not pinned" —
// once the user clicks × the chip goes away too.
func (m Model) actionsLauncherVisible() bool {
	return m.actionsOpen && !m.actionsPinned && !m.zenMode
}

// actionsColumnWidth returns the width in columns the pinned panel
// currently occupies. Zero when the panel is not pinned-and-open — the
// editor / chrome math collapses cleanly with that.
func (m Model) actionsColumnWidth() int {
	if !m.actionsPinned || !m.actionsOpen || m.zenMode {
		return 0 // zen mode shows the editor only
	}
	w := autoActionsWidth(m.w, m.showExp, m.explorerWidth)
	if m.actionsWidthSet {
		w = m.actionsWidth
	}
	// Never wider than the room left of the editor's minimum: a width
	// set in a wide window (drag, saved session) would otherwise push
	// rows past the right edge after the window shrinks.
	room := m.w - activity.Width - editorScrollbarWidth - minEditorPaneWidth
	if m.showExp {
		room -= m.explorerWidth
	}
	if room < actionsMinWidth {
		return 0 // no room: the panel hides until the window grows
	}
	return min(w, room)
}

// minEditorPaneWidth is the editor width the chat panel always leaves.
const minEditorPaneWidth = 10

// autoActionsWidth is the chat panel's width until the user drags its
// edge: about 40% of the window (44–80 columns) so answers with code fit,
// while the editor keeps at least 60 columns.
func autoActionsWidth(w int, showExp bool, explorerW int) int {
	auto := w * 40 / 100
	if auto < 44 {
		auto = 44
	}
	if auto > 80 {
		auto = 80
	}
	left := w - activity.Width - editorScrollbarWidth
	if showExp {
		left -= explorerW
	}
	if room := left - 60; auto > room {
		auto = room
	}
	if auto < defaultActionsWidth {
		auto = defaultActionsWidth
	}
	return auto
}

// renderActionsPanel renders the pinned panel as a (width × height) block.
// Layout:actions
//
//	row 0:    " ACTIONS                ⚐ ×"   (header)
//	row 1:    "─────────────────────────────"  (hairline divider)
//	row 2..n: body — "No actions yet" centered, italic, muted
//
// The full block uses BgPanel as background so it visually separates from
// the editor (which uses BgEditor).
func (m Model) renderActionsPanel(width, height int) string {
	return strings.Join(m.renderActionsPanelLines(width, height), "\n")
}

// renderActionsPanelLines is renderActionsPanel as rows.
func (m Model) renderActionsPanelLines(width, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	bg := theme.Bg(theme.BgPanel)
	rows := make([]string, 0, height)
	// A thin edge on the panel's blank first column, the mirror of the
	// sidebar's ▕, so the panel reads as its own pane next to the editor.
	edge := func(r string) string { return r }
	if width > 1 {
		mark := theme.FgBg(theme.BorderSubtle, theme.BgPanel).Render("▏")
		edge = func(r string) string { return mark + dropFirstCell(r, width) }
	}

	rows = append(rows, m.actionsHeaderEdged(width, edge))
	if height > 1 {
		rows = append(rows, edge(renderActionsDivider(width)))
	}
	switch {
	case height <= aiPanelTopRow:
	case m.agentsShowing():
		rows = append(rows, m.renderAgentsBody(width, height, edge)...) // U20 AP-9: the AGENTS view
	default:
		rows = append(rows, m.renderAIPanelBody(width, height, edge)...) // Group A: AI chat body
	}
	if len(rows) < height {
		blank := edge(bg.Render(strings.Repeat(" ", width)))
		for len(rows) < height {
			rows = append(rows, blank)
		}
	}
	if len(rows) > height {
		rows = rows[:height]
	}
	return rows
}

// dropFirstCell is ansi.Cut(row, 1, width) for rows that start with a
// blank cell: it keeps the leading colour codes and drops the space, with
// no full parse of the row (it runs on every row of every frame).
func dropFirstCell(row string, width int) string {
	i := 0
	for i < len(row) && row[i] == 0x1b {
		j, ok := skipCSI(row, i)
		if !ok {
			return ansi.Cut(row, 1, width)
		}
		i = j
	}
	if i < len(row) && row[i] == ' ' && cellWidth(row) == width {
		return row[:i] + row[i+1:]
	}
	return ansi.Cut(row, 1, width)
}

// renderActionsHeader renders one row: " ACTIONS … ⚐ ×".
//
// "ACTIONS" sits in TextSecondary bold uppercase on the LEFT; the pin and
// close glyphs are right-aligned with 1 col of padding between them and a
// 1-col right pad. The whole row uses BgPanel.
// actionsHeaderMemo keeps the last header row (passed through edge): it
// only changes with the width, pin state, focus, hovered button and theme.
var actionsHeaderMemo struct {
	sync.Mutex
	ok      bool
	width   int
	pinned  bool
	focused bool
	hover   string
	rk      theme.RenderKey
	mcp     int // mcpChipState: -1 no chip, else connected servers
	profile string
	running bool // a pipeline run is active (the profile chip's colour)
	agents  bool // the slot shows the AGENTS view
	usage   bool // ... as the usage tables
	out     string
}

// actionsHeaderEdged is edge(renderActionsHeader(width)) through
// actionsHeaderMemo (edge is the same for the same width and theme).
func (m Model) actionsHeaderEdged(width int, edge func(string) string) string {
	hover, rk := m.actionsHeaderHover(), theme.CurrentRenderKey()
	focused := m.ai != nil && m.ai.focused
	mcpState := m.mcpChipState()
	profile, running := m.aiProfileChipLabel(), m.aiRunActive()
	agents := m.agentsShowing()
	usage := agents && m.ai.agents.usage
	c := &actionsHeaderMemo
	c.Lock()
	defer c.Unlock()
	if c.ok && c.width == width && c.pinned == m.actionsPinned && c.focused == focused && c.hover == hover && c.rk == rk && c.mcp == mcpState &&
		c.profile == profile && c.running == running && c.agents == agents && c.usage == usage {
		return c.out
	}
	out := edge(m.renderActionsHeader(width))
	c.ok, c.width, c.pinned, c.focused, c.hover, c.rk, c.mcp, c.out = true, width, m.actionsPinned, focused, hover, rk, mcpState, out
	c.profile, c.running, c.agents, c.usage = profile, running, agents, usage
	return out
}

// actionsHeaderHover is the header button under the pointer ("" none).
func (m Model) actionsHeaderHover() string {
	if m.hoverY == 0 {
		return m.hitActionsButton(m.hoverX, m.hoverY)
	}
	return ""
}

func (m Model) renderActionsHeader(width int) string {
	bg := theme.Bg(theme.BgPanel)
	labelStyle := theme.FgBg(theme.TextSecondary, theme.BgPanel).Bold(true)

	// The pin glyph reflects current pin state — when pinned, it's brighter
	// (TextPrimary). Both glyphs are buttons so we keep them visually crisp
	// by NOT dimming them.
	pinTok := theme.TextSecondary
	if m.actionsPinned {
		pinTok = theme.AccentLavender
	}
	pinStyle := theme.FgBg(pinTok, theme.BgPanel).Bold(true)
	closeStyle := theme.FgBg(theme.TextSecondary, theme.BgPanel).Bold(true)

	// The AI colour while the chat has the keyboard (UI review #6).
	if m.ai != nil && m.ai.focused {
		labelStyle = theme.FgBg(theme.AIAccent, theme.BgPanel).Bold(true)
	}
	left := bg.Render(" ") + labelStyle.Render(m.actionsHeaderLabel())
	// Header buttons light up under the pointer.
	hover := m.actionsHeaderHover()
	btn := func(id, glyph string, st lipgloss.Style) string {
		if id == hover {
			st = st.Background(theme.Mix(theme.BgPanel, theme.TextPrimary, 0.16)).Foreground(theme.LG(theme.TextWhite))
		}
		return st.Render(glyph)
	}
	// Chips after the label (actionsHeaderChips): the agent profile
	// "✦ <profile> ▾" (ai_profiles_ui.go, a click opens the picker) and
	// MCP "⌁ N MCP" (ai_mcp.go, a click opens AI: MCP Servers…) and
	// "usage" (a click opens the session's AI: Usage tables).
	for _, c := range m.actionsHeaderChips(width) {
		tok := theme.TextSecondary
		if c.id == "profile" {
			tok = m.aiProfileChipTok()
		}
		left += bg.Render(" ") + btn(c.id, c.label, theme.FgBg(tok, theme.BgPanel))
	}
	right := btn("new", theme.GlyphNew.String(), closeStyle) + bg.Render(" ") +
		btn("history", theme.GlyphHistory.String(), closeStyle) + bg.Render(" ") +
		btn("pin", theme.GlyphPin.String(), pinStyle) + bg.Render(" ") +
		btn("close", theme.GlyphClose.String(), closeStyle) + bg.Render(" ")

	leftW := textWidth(left)
	rightW := textWidth(right)
	gap := width - leftW - rightW
	if gap < 1 {
		// Pane too narrow for both label and buttons — drop the label.
		left = bg.Render(" ")
		leftW = textWidth(left)
		gap = width - leftW - rightW
		if gap < 0 {
			gap = 0
		}
	}
	return left + bg.Render(strings.Repeat(" ", gap)) + right
}

// renderActionsDivider renders a 1-row hairline (BorderSubtle on BgPanel).
// '─' is a clean horizontal divider that contrasts with the panel without
// shouting.
func renderActionsDivider(width int) string {
	style := theme.FgBg(theme.BorderSubtle, theme.BgPanel)
	return style.Render(strings.Repeat("─", width))
}

// renderActionsLauncher returns the tiny launcher chip rendered as a
// single styled string, sized exactly the cell-width of the visible
// glyph + label.  Caller is responsible for splicing it into the right
// spot of the rendered base; see view.go::overlayActionsLauncher.
func renderActionsLauncher() string {
	style := theme.FgBg(theme.AccentLavender, theme.BgEditor).Bold(true)
	return style.Render(actionsLauncherLabel)
}

// actionsLauncherWidth returns the visible width of the launcher chip.
// Used by both the hit-test code in mouse.go and the splice math in
// view.go so the two stay in sync.
func actionsLauncherWidth() int {
	return runewidth.StringWidth(actionsLauncherLabel)
}

// ─── Hit-testing helpers ──────────────────────────────────────────────────

// actionsHeaderRect returns the screen rectangle of the panel header row
// (where the pin/close buttons live) when the panel is visible. Returns
// ok=false otherwise.
func (m Model) actionsHeaderRect() (x1, x2, y int, ok bool) {
	if !m.actionsPanelVisible() {
		return 0, 0, 0, false
	}
	w := m.actionsColumnWidth()
	if w <= 0 {
		return 0, 0, 0, false
	}
	x1 = m.w - w
	x2 = m.w
	y = 0
	return x1, x2, y, true
}

// hitActionsButton classifies a click on the panel's header row. Returns
// "pin", "close", or "" if the click is on the row but not on a glyph.
//
// Layout matches renderActionsHeader: " ACTIONS … ⚐ <space> × <space>",
// so the close glyph occupies width-2, and the pin glyph occupies width-4.
func (m Model) hitActionsButton(screenX, screenY int) string {
	x1, x2, y, ok := m.actionsHeaderRect()
	if !ok {
		return ""
	}
	if screenY != y {
		return ""
	}
	if screenX < x1 || screenX >= x2 {
		return ""
	}
	w := x2 - x1
	// Right side: " + ≡ ⚐ × "  →  glyphs at local cols w-8, w-6, w-4, w-2.
	closeCol := x1 + w - 2
	pinCol := x1 + w - 4
	if screenX == closeCol {
		return "close"
	}
	if screenX == pinCol {
		return "pin"
	}
	if screenX == x1+w-6 {
		return "history" // Group A
	}
	if screenX == x1+w-8 {
		return "new" // Group A
	}
	// The chips after " ✦ AI CHAT " (renderActionsHeader).
	for _, c := range m.actionsHeaderChips(w) {
		if c0 := x1 + c.x0; screenX >= c0 && screenX < c0+runewidth.StringWidth(c.label) {
			return c.id
		}
	}
	return ""
}

// agentsShowing reports whether the chat slot shows the AGENTS view.
func (m Model) agentsShowing() bool { return m.ai != nil && m.ai.agents.on }

// actionsHeaderLabel is the slot's title: "✦ AI CHAT" or "✦ AGENTS".
func (m Model) actionsHeaderLabel() string {
	if m.agentsShowing() {
		if m.ai.agents.usage {
			return aiSpark() + " USAGE"
		}
		return aiSpark() + " AGENTS"
	}
	return aiSpark() + " AI CHAT"
}

// actionsChipMinWidth is the narrowest header that still shows a chip
// next to the label and the buttons.
const actionsChipMinWidth = 34

// actionsHeaderButtonsW is the width of the header's right side
// " + ≡ ⚐ × " without its leading gap (hitActionsButton's columns).
const actionsHeaderButtonsW = 8

// actionsHeaderChip is one chip of the header, x0 its first cell from
// the panel's left edge.
type actionsHeaderChip struct {
	id, label string
	x0        int
}

// actionsHeaderChips lays out the header chips after " ✦ AI CHAT": the
// profile chip, then the MCP and usage chips (only from
// actionsChipMinWidth on, usage drops first); a chip that does not fit before the buttons (with a 1-cell gap) is left
// out.
func (m Model) actionsHeaderChips(width int) []actionsHeaderChip {
	x := runewidth.StringWidth(" " + m.actionsHeaderLabel())
	var out []actionsHeaderChip
	dropped := false
	add := func(id, label string) {
		w := runewidth.StringWidth(label)
		if label == "" {
			return
		}
		if x+1+w+1+actionsHeaderButtonsW > width {
			dropped = true
			return
		}
		out = append(out, actionsHeaderChip{id: id, label: label, x0: x + 1})
		x += 1 + w
	}
	if m.agentsShowing() { // the AGENTS view: one chip back to the chat
		back := "‹ chat"
		if theme.CurrentIconMode == theme.IconModeASCII {
			back = "< chat"
		}
		add("chat", back)
		return out
	}
	add("profile", m.aiProfileChipLabel())
	if width >= actionsChipMinWidth {
		add("mcp", m.mcpChipLabel())
		add("usage", "usage") // last: the first chip to drop
	} else if m.mcpChipLabel() != "" || m.aiProfileChipLabel() != "" {
		dropped = true
	}
	if dropped { // a chip that does not fit leaves a trace: ⋯ opens a menu with them
		more := aiMoreLabel()
		mw := runewidth.StringWidth(more)
		// Make room for it: the profile name gets shorter ("✦ rev… ▾",
		// it stays visible), then the last chip steps into the menu.
		for len(out) > 0 && x+1+mw+1+actionsHeaderButtonsW > width {
			last := out[len(out)-1]
			out = out[:len(out)-1]
			x = last.x0 - 1
			if last.id != "profile" {
				continue
			}
			name := []rune(m.aiProfileChipName())
			for k := len(name) - 1; k >= 3; k-- {
				label := aiSpark() + " " + string(name[:k]) + "…" + " " + aiFoldGlyph(true)
				if x+1+runewidth.StringWidth(label)+1+mw+1+actionsHeaderButtonsW <= width {
					out = append(out, actionsHeaderChip{id: "profile", label: label, x0: last.x0})
					x = last.x0 + runewidth.StringWidth(label)
					break
				}
			}
		}
		add("more", more)
	}
	return out
}

// actionsLauncherRect returns the screen rectangle of the bottom-right
// launcher chip when it should be visible. The chip lives one row above
// the status bar (which occupies y == m.h-1), aligned to the right edge
// with a 1-col right pad.
func (m Model) actionsLauncherRect() (x1, x2, y int, ok bool) {
	if !m.actionsLauncherVisible() {
		return 0, 0, 0, false
	}
	if m.h <= 1 || m.w <= 0 {
		return 0, 0, 0, false
	}
	cw := actionsLauncherWidth()
	const rightPad = 1
	x1 = m.w - cw - rightPad
	x2 = x1 + cw
	if x1 < 0 {
		x1 = 0
	}
	if x2 > m.w {
		x2 = m.w
	}
	y = m.h - 2
	if y < 0 {
		y = 0
	}
	return x1, x2, y, true
}

// hitActionsLauncher reports whether (screenX, screenY) lands on the
// bottom-right launcher chip.
func (m Model) hitActionsLauncher(screenX, screenY int) bool {
	x1, x2, y, ok := m.actionsLauncherRect()
	if !ok {
		return false
	}
	return screenY == y && screenX >= x1 && screenX < x2
}

// ─── State transitions ────────────────────────────────────────────────────

// toggleActionsPanel flips actionsOpen. Opening always pins (so the user
// gets the full panel, not just a chip). Closing leaves actionsPinned
// alone so the next open inherits the previous pin state — but since we
// always re-pin on open, the practical net is "Alt+A always lands you in
// the pinned panel."
func (m *Model) toggleActionsPanel() {
	switch {
	case m.actionsPanelVisible() && m.aiS().agents.on:
		// The slot shows AGENTS: Alt+A brings the chat back (U20 AP-9).
		m.ai.agents.on = false
		m.ai.focused = true
		return
	case m.actionsOpen:
		m.actionsOpen = false
		m.aiS().focused = false // Group A
	default:
		m.actionsOpen = true
		m.actionsPinned = true
		m.aiS().focused = true // Group A: Alt+A lands in the chat input
		m.ai.agents.on = false
	}
	m.persistActionsState()
}

// setActionsPinned writes a new pin value and persists.
func (m *Model) setActionsPinned(pinned bool) {
	m.actionsPinned = pinned
	m.persistActionsState()
}

// closeActionsPanel hides BOTH the panel and the launcher chip until the
// user reopens via keybinding/palette. Persists.
func (m *Model) closeActionsPanel() {
	m.actionsOpen = false
	m.persistActionsState()
}

// persistActionsState writes the three Actions-panel fields to session.json.
// Mirrors persistTerminalRows / persistExplorerState.
func (m *Model) persistActionsState() {
	existing := loadSession()
	openVal := m.actionsOpen
	pinnedVal := m.actionsPinned
	existing.ActionsOpen = &openVal
	existing.ActionsPinned = &pinnedVal
	existing.ActionsWidth = 0
	if m.actionsWidthSet { // only a width the user chose sticks
		existing.ActionsWidth = m.actionsWidth
	}
	saveSession(existing)
}
