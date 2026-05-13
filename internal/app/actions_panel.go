package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"termocode/internal/theme"
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

// actionsPanelGlyphPin is the pin glyph drawn in the header. ⚐ is a
// "white flag" — at the cell-grid scale it reads as a small flag (i.e.
// "pinned to this corner") which is more intuitive than the other
// candidate glyphs.
const (
	actionsPanelGlyphPin   = "⚐"
	actionsPanelGlyphClose = "×"

	// Launcher chip glyph + label shown when unpinned. "Actions" is a
	// reasonable hint of what the chip does without leaning on tooltips.
	actionsLauncherLabel = "▸ Actions"
)

// actionsPanelVisible reports whether the pinned (column-occupying) panel
// is currently rendered. False when the user closed it OR when it's
// unpinned (in which case only the launcher chip shows up).
func (m Model) actionsPanelVisible() bool {
	return m.actionsOpen && m.actionsPinned
}

// actionsLauncherVisible reports whether the bottom-right launcher chip
// should be rendered. True only when the panel is "open but not pinned" —
// once the user clicks × the chip goes away too.
func (m Model) actionsLauncherVisible() bool {
	return m.actionsOpen && !m.actionsPinned
}

// actionsColumnWidth returns the width in columns the pinned panel
// currently occupies. Zero when the panel is not pinned-and-open — the
// editor / chrome math collapses cleanly with that.
func (m Model) actionsColumnWidth() int {
	if !m.actionsPinned || !m.actionsOpen {
		return 0
	}
	return m.actionsWidth
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
	if width <= 0 || height <= 0 {
		return ""
	}
	bg := theme.Bg(theme.BgPanel)
	rows := make([]string, 0, height)

	rows = append(rows, m.renderActionsHeader(width))
	if height > 1 {
		rows = append(rows, renderActionsDivider(width))
	}
	bodyRows := height - len(rows)
	if bodyRows > 0 {
		rows = append(rows, renderActionsBody(width, bodyRows)...)
	}
	for len(rows) < height {
		rows = append(rows, bg.Render(strings.Repeat(" ", width)))
	}
	if len(rows) > height {
		rows = rows[:height]
	}
	return strings.Join(rows, "\n")
}

// renderActionsHeader renders one row: " ACTIONS … ⚐ ×".
//
// "ACTIONS" sits in TextSecondary bold uppercase on the LEFT; the pin and
// close glyphs are right-aligned with 1 col of padding between them and a
// 1-col right pad. The whole row uses BgPanel.
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

	left := bg.Render(" ") + labelStyle.Render("ACTIONS")
	right := pinStyle.Render(actionsPanelGlyphPin) + bg.Render(" ") +
		closeStyle.Render(actionsPanelGlyphClose) + bg.Render(" ")

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := width - leftW - rightW
	if gap < 1 {
		// Pane too narrow for both label and buttons — drop the label.
		left = bg.Render(" ")
		leftW = lipgloss.Width(left)
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

// renderActionsBody fills `bodyRows` rows with a centered, italic,
// muted "No actions yet" hint vertically centered in the body, and
// pads the rest with BgPanel-colored spaces.
func renderActionsBody(width, bodyRows int) []string {
	bg := theme.Bg(theme.BgPanel)
	hintStyle := theme.FgBg(theme.TextDim, theme.BgPanel).Italic(true)

	rows := make([]string, bodyRows)
	for i := range rows {
		rows[i] = bg.Render(strings.Repeat(" ", width))
	}
	hint := "No actions yet"
	if runewidth.StringWidth(hint) > width-2 {
		// Width too small to render the hint safely — just leave the
		// body fully blank.
		return rows
	}
	hintRow := bodyRows / 2
	if hintRow < 0 || hintRow >= bodyRows {
		return rows
	}
	leftPad := (width - runewidth.StringWidth(hint)) / 2
	rightPad := width - leftPad - runewidth.StringWidth(hint)
	if leftPad < 0 {
		leftPad = 0
	}
	if rightPad < 0 {
		rightPad = 0
	}
	rows[hintRow] = bg.Render(strings.Repeat(" ", leftPad)) +
		hintStyle.Render(hint) +
		bg.Render(strings.Repeat(" ", rightPad))
	return rows
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
	// Right side: " ⚐ × "  →  glyphs at local cols (w-4) and (w-2).
	closeCol := x1 + w - 2
	pinCol := x1 + w - 4
	if screenX == closeCol {
		return "close"
	}
	if screenX == pinCol {
		return "pin"
	}
	return ""
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
	if m.actionsOpen {
		m.actionsOpen = false
	} else {
		m.actionsOpen = true
		m.actionsPinned = true
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
	existing.ActionsWidth = m.actionsWidth
	saveSession(existing)
}
