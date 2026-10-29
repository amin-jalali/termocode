package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ─── START tiles ───────────────────────────────────────────────────────
//
// START is six rounded boxes: glyph, title, a dim subtitle and the keycap
// on the right, in 2 columns × 3 rows (1 column × 6 when the pane is
// narrow). The tiles are rows of the one keyboard cursor: the cursor tile
// takes the accent border, a solid ▌ left edge and a wash. When the tiles
// do not fit the pane the page falls back to one-line rows
// (renderWelcomeRow).

// welcomeTileGap is the blank columns between two tiles of a row.
const welcomeTileGap = 2

// welcomeTileMinW2 / welcomeTileMinW1 are the least content widths for
// two columns and for one column of tiles.
const (
	welcomeTileMinW2 = 66
	welcomeTileMinW1 = 32
)

// welcomeTileModes lists the tile column counts to try for content
// width W, most columns first.
func welcomeTileModes(W int) []int {
	var out []int
	if W >= welcomeTileMinW2 {
		out = append(out, 2)
	}
	if W >= welcomeTileMinW1 {
		out = append(out, 1)
	}
	return out
}

// welcomeTileSize is the width and height of one tile: 4 lines with the
// subtitle, 3 when the tile is too narrow for it.
func welcomeTileSize(W, cols int) (cardW, h int) {
	cardW = (W - (cols-1)*welcomeTileGap) / cols
	h = 4
	if cardW < 32 {
		h = 3
	}
	return cardW, h
}

// welcomeTileIcon is the tile glyph for the icon mode.
func welcomeTileIcon(a quickAction) string {
	if theme.CurrentIconMode != theme.IconModeNerdFont {
		if theme.CurrentIconMode == theme.IconModeASCII && a.icon == "↓" {
			return "v"
		}
		return a.icon
	}
	switch {
	case a.id == welcomeCardClone:
		return "" // nf-fa-github
	case a.id == welcomeCardWorkspace:
		return "" // nf-fa-folder_open
	case a.action == keymap.ActionQuickOpen:
		return "" // nf-fa-file
	case a.action == keymap.ActionCommandPalette:
		return "" // nf-fa-terminal
	case a.action == keymap.ActionWorkspaceSearch:
		return "" // nf-cod-search
	case a.action == keymap.ActionToggleTerminal:
		return "" // nf-oct-terminal
	}
	return a.icon
}

// renderWelcomeTile draws one tile, exactly cardW cells wide and h lines
// tall (3: top, title, bottom; 4: with the subtitle).
func renderWelcomeTile(a quickAction, cardW, h int, st welcomeRowState) []string {
	ascii := theme.CurrentIconMode == theme.IconModeASCII
	bgC := theme.LG(theme.BgEditor)
	borderC := theme.Mix(theme.BgEditor, theme.BorderDefault, 0.85)
	switch st {
	case rowCursorFocused:
		bgC = theme.Wash(theme.BgEditor, theme.WashSelectedFocused)
		borderC = theme.LG(theme.BorderFocus)
	case rowCursorBlurred:
		bgC = theme.Wash(theme.BgEditor, theme.WashSelected)
		borderC = theme.LG(theme.TextDimmer)
	}
	cursor := st != rowRest
	tile := lipgloss.NewStyle().Background(bgC)
	edge := lipgloss.NewStyle().Background(theme.LG(theme.BgEditor)).Foreground(borderC)
	side := tile.Foreground(borderC)

	tl, tr, bl, br, hz, vt, cur := "╭", "╮", "╰", "╯", "─", "│", "▌"
	if ascii {
		tl, tr, bl, br, hz, vt, cur = "+", "+", "+", "+", "-", "|", ">"
	}
	left := side.Render(vt)
	if cursor {
		left = side.Render(cur)
	}

	titleTok := theme.TextPrimary
	if st == rowCursorFocused {
		titleTok = theme.TextWhite
	}
	titleSt := tile.Foreground(theme.LG(titleTok)).Bold(true)
	subSt := tile.Foreground(theme.Readable(theme.TextDim, theme.BgEditor, 3.0))
	iconSt := tile.Foreground(theme.Readable(a.iconTok, theme.BgEditor, 3.0)).Bold(true)

	inner := cardW - 2
	padL := 2
	if inner < 12 {
		padL = 1
	}
	icon := runewidth.Truncate(welcomeTileIcon(a), 2, "")
	iconCells := runewidth.StringWidth(icon) + 2
	key, keyW := "", 0
	if a.shortcut != "" {
		key = theme.Keycap(a.shortcut, bgC)
		keyW = runewidth.StringWidth(theme.KeyLabel(a.shortcut)) + 2
	}
	rightPad := padL
	avail := inner - padL - rightPad
	titleAvail := avail - iconCells - keyW - 1
	if keyW == 0 {
		titleAvail = avail - iconCells
	}
	if titleAvail < 1 { // too narrow for the keycap
		key, keyW = "", 0
		titleAvail = max(avail-iconCells, 1)
	}
	title := welcomeCut(welcomeText(a.title), titleAvail)
	gap := max(avail-iconCells-runewidth.StringWidth(title)-keyW, 0)
	titleRow := tile.Render(strings.Repeat(" ", padL)) + iconSt.Render(icon) + tile.Render("  ") +
		titleSt.Render(title) + tile.Render(strings.Repeat(" ", gap)) + key + tile.Render(strings.Repeat(" ", rightPad))
	titleRow = padRowToWidth(titleRow, inner, tile)

	out := []string{edge.Render(tl + strings.Repeat(hz, inner) + tr), left + titleRow + side.Render(vt)}
	if h >= 4 {
		sub := welcomeCut(welcomeText(a.subtitle), max(avail-iconCells, 1))
		subRow := tile.Render(strings.Repeat(" ", padL+iconCells)) + subSt.Render(sub)
		out = append(out, left+padRowToWidth(subRow, inner, tile)+side.Render(vt))
	}
	return append(out, edge.Render(bl+strings.Repeat(hz, inner)+br))
}
