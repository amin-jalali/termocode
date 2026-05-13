package menu

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"termocode/internal/theme"
)

// Item is one row of the menu. Set Sep=true for a horizontal divider.
// Icon is an optional single-glyph cue rendered to the left of the title.
// Hint is an optional accelerator string (e.g. "Ctrl+S") rendered, muted,
// at the right edge of the row.
type Item struct {
	ID    string
	Title string
	Hint  string
	Icon  string
	Sep   bool
}

// SelectMsg is emitted when a non-separator row is activated.
type SelectMsg struct{ ID string }

// CloseMsg is emitted when the menu is dismissed (Esc or click outside).
type CloseMsg struct{}

// Model is the right-click context menu component. It is rendered inline at
// a cursor anchor (clamped to the screen) and is wrapped in a rounded-border
// panel that the host blends through `glassyMenuOverlay` so editor cells
// faintly bleed through the panel.
type Model struct {
	items   []Item
	cursor  int
	anchorX int
	anchorY int
	scrW    int
	scrH    int
	title   string // optional header label; "" hides the header row
	icon    string // optional header glyph; "" falls back to a generic dot
}

// Layout constants. Each row is composed as:
//
//	[LEFT_PAD] [ICON 2col] [TITLE_GAP] [TITLE...] [HINT_GAP+] [HINT] [RIGHT_PAD]
//
// Every row pads to the SAME inner width so lipgloss's rounded border wraps
// a perfectly rectangular block — a prerequisite for `glassyMenuOverlay`'s
// per-cell splice to land on aligned cells.
const (
	menuLeftPad  = 1 // panel-bg cells before the icon column
	menuRightPad = 1 // panel-bg cells after the hint
	menuIconCol  = 2 // single glyph + trailing space (or 2 spaces when empty)
	menuTitleGap = 1 // between icon column and title text
	menuHintGap  = 2 // minimum space between title and hint
	menuMinWidth = 24
)

// New returns a menu anchored at (x, y) with the given items.
func New(items []Item, x, y int) Model {
	m := Model{items: items, anchorX: x, anchorY: y}
	// Move cursor past any leading separators.
	for m.cursor < len(items) && items[m.cursor].Sep {
		m.cursor++
	}
	return m
}

// NewWithItems is an alias for New kept for callers that prefer the more
// explicit name. Both constructors produce identical menus.
func NewWithItems(items []Item, x, y int) Model {
	return New(items, x, y)
}

// SetScreenSize tells the menu the current terminal dimensions so it can
// clamp its anchor inside the visible area.
func (m *Model) SetScreenSize(w, h int) { m.scrW, m.scrH = w, h }

// SetSize is an alias for SetScreenSize, provided for parity with the other
// overlay components (picker, prompt, settings modal).
func (m *Model) SetSize(w, h int) { m.SetScreenSize(w, h) }

// SetPosition repositions the menu anchor (e.g. when the underlying tab/row
// shifts). The next render call re-clamps the new anchor to the screen.
func (m *Model) SetPosition(x, y int) { m.anchorX, m.anchorY = x, y }

// SetTitle attaches an optional header label and icon. Pass "" for either to
// suppress that piece (or both, to skip the header row entirely). No current
// caller sets a header; the field is preserved for API stability.
func (m *Model) SetTitle(title, icon string) { m.title, m.icon = title, icon }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			return m, func() tea.Msg { return CloseMsg{} }
		case tea.KeyEnter:
			if m.cursor >= 0 && m.cursor < len(m.items) {
				it := m.items[m.cursor]
				if !it.Sep {
					return m, func() tea.Msg { return SelectMsg{ID: it.ID} }
				}
			}
		case tea.KeyUp:
			for m.cursor > 0 {
				m.cursor--
				if !m.items[m.cursor].Sep {
					break
				}
			}
		case tea.KeyDown:
			for m.cursor < len(m.items)-1 {
				m.cursor++
				if !m.items[m.cursor].Sep {
					break
				}
			}
		}
	case tea.MouseMsg:
		return m.HandleMouse(msg)
	}
	return m, nil
}

// HandleMouse routes a tea.MouseMsg through the menu. Click outside the
// rendered panel closes the menu; click on an action row selects it; clicks
// on borders/headers/separators are no-ops.
func (m Model) HandleMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	if msg.Type != tea.MouseLeft {
		return m, nil
	}
	w, h := m.size()
	x, y := m.clampedAnchor()
	// Click outside the bordered panel → close.
	if msg.X < x || msg.X >= x+w || msg.Y < y || msg.Y >= y+h {
		return m, func() tea.Msg { return CloseMsg{} }
	}
	// Map screen y to a logical item index (skipping border + optional header).
	idx := m.itemAt(msg.Y - y)
	if idx < 0 || idx >= len(m.items) || m.items[idx].Sep {
		return m, nil
	}
	return m, func() tea.Msg { return SelectMsg{ID: m.items[idx].ID} }
}

// hasHeader reports whether the menu renders a header row above its items.
func (m Model) hasHeader() bool { return m.title != "" || m.icon != "" }

// itemsHaveIcons reports whether any non-separator item has an Icon set.
// When true the renderer reserves an icon column on every row so labels
// align even when individual items lack their own glyph.
func (m Model) itemsHaveIcons() bool {
	for _, it := range m.items {
		if !it.Sep && it.Icon != "" {
			return true
		}
	}
	return false
}

// hasAnyHint reports whether any non-separator item has a Hint set.
// When false the renderer skips the hint gap entirely so menus without
// accelerators don't waste horizontal space.
func (m Model) hasAnyHint() bool {
	for _, it := range m.items {
		if !it.Sep && it.Hint != "" {
			return true
		}
	}
	return false
}

// titleColWidth returns the widest title glyph-width across non-sep items.
func (m Model) titleColWidth() int {
	w := 0
	for _, it := range m.items {
		if it.Sep {
			continue
		}
		if v := runewidth.StringWidth(it.Title); v > w {
			w = v
		}
	}
	return w
}

// hintColWidth returns the widest hint glyph-width across non-sep items.
func (m Model) hintColWidth() int {
	w := 0
	for _, it := range m.items {
		if it.Sep {
			continue
		}
		if v := runewidth.StringWidth(it.Hint); v > w {
			w = v
		}
	}
	return w
}

// panelInnerWidth computes the width of the cell area between the rounded
// border cells. Every row renders to exactly this width before the border
// is wrapped around. This is the single source of truth for menu sizing —
// every other layout helper takes innerW as input.
func (m Model) panelInnerWidth() int {
	icon := 0
	if m.itemsHaveIcons() {
		icon = menuIconCol
	}
	titleW := m.titleColWidth()
	hintW := m.hintColWidth()

	row := menuLeftPad + icon
	if icon > 0 {
		row += menuTitleGap
	}
	row += titleW
	if hintW > 0 {
		row += menuHintGap + hintW
	}
	row += menuRightPad

	if m.hasHeader() {
		hdr := menuLeftPad
		if m.icon != "" {
			hdr += runewidth.StringWidth(m.icon) + 1
		}
		hdr += runewidth.StringWidth(m.title) + menuRightPad
		if hdr > row {
			row = hdr
		}
	}
	if row < menuMinWidth {
		row = menuMinWidth
	}
	return row
}

// innerWidth is preserved as the legacy name — alias for panelInnerWidth.
func (m Model) innerWidth() int { return m.panelInnerWidth() }

// size returns the FULL rendered size of the menu — including the rounded
// border on all four sides and the optional header row + hairline. The
// return value is what callers (e.g. clampedAnchor, mouse hit-test) should
// use as the bounding box.
func (m Model) size() (w, h int) {
	w = m.panelInnerWidth() + 2 // +2 for left/right border cells
	rows := len(m.items)
	if m.hasHeader() {
		rows += 2 // header row + hairline below it
	}
	h = rows + 2 // +2 for top/bottom border cells
	return w, h
}

// clampedAnchor returns the on-screen position of the menu's top-left corner
// (the rounded border cell), clamped to the screen.
func (m Model) clampedAnchor() (int, int) {
	w, h := m.size()
	x, y := m.anchorX, m.anchorY
	if x+w > m.scrW {
		x = m.scrW - w
	}
	if x < 0 {
		x = 0
	}
	if y+h > m.scrH {
		y = m.scrH - h
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

// itemAt translates a row offset (0 = top border, etc.) within the rendered
// panel back into an items[] index. Returns -1 for non-actionable rows
// (borders, header, hairline, out of range).
func (m Model) itemAt(rowOff int) int {
	first := 1 // row 0 = top border
	if m.hasHeader() {
		first += 2
	}
	if rowOff < first {
		return -1
	}
	idx := rowOff - first
	if idx < 0 || idx >= len(m.items) {
		return -1
	}
	return idx
}

// Overlay draws the menu over the given base view at the (clamped) anchor
// and returns the composed view. Width/height come from size() so the
// caller need not know the menu's internal layout.
func (m Model) Overlay(base string) string {
	x, y := m.clampedAnchor()
	rendered := m.render()
	return overlayString(base, rendered, x, y, m.scrW)
}

// Bounds returns the menu's clamped on-screen rectangle as (x, y, w, h),
// useful for callers that want to apply per-cell effects (e.g. a glass
// blend pass) over the menu region.
func (m Model) Bounds() (x, y, w, h int) {
	x, y = m.clampedAnchor()
	w, h = m.size()
	return
}

// View returns the rendered menu without compositing it onto a base. Useful
// for tests and inspection.
func (m Model) View() string { return m.render() }

// ── Palette ────────────────────────────────────────────────────────────
//
// The host's `glassyMenuOverlay` blends per-cell against an EXACT panel-bg
// constant (`rgbColor{0x26, 0x26, 0x26}`, == #262626 == palette index 235
// == `theme.BgHover`). Cells that don't carry that bg are left untouched
// (border, separators, selected-row tint) — that's the intended behavior.
//
// Selection bg therefore MUST differ from the panel bg, otherwise glass
// blending would also tint the selection (we want it solid). We use
// `theme.BgInactiveSel` (#3a3a3a) — one step lighter — for the soft
// selection bar.

func styleBg(c theme.Color256) lipgloss.Style {
	return theme.Bg(c)
}

func styleFgBg(fg, bg theme.Color256) lipgloss.Style {
	return theme.FgBg(fg, bg)
}

// padToWidth right-pads `s` with bg-styled spaces until its visible width
// equals `w`. NEVER pads with bare whitespace — the host's glass blend
// requires every body cell to carry an explicit bg attribute.
func padToWidth(s string, w int, bg theme.Color256) string {
	used := lipgloss.Width(s)
	if used >= w {
		return s
	}
	return s + styleBg(bg).Render(strings.Repeat(" ", w-used))
}

// renderSeparatorRow renders a hairline divider spanning innerW cells,
// drawn in the subtle border color over the panel background.
func renderSeparatorRow(innerW int) string {
	return styleFgBg(theme.BorderSubtle, theme.BgHover).Render(strings.Repeat("─", innerW))
}

// renderItemRow lays out one action row. The width contract: returned
// string has visible width == innerW. Selected rows use `theme.BgInactiveSel`
// for their bg; unselected use `theme.BgHover` (the panel bg).
func renderItemRow(it Item, selected bool, innerW int, showIcons bool) string {
	bg := theme.BgHover
	if selected {
		bg = theme.BgInactiveSel
	}

	titleStyle := styleFgBg(theme.TextPrimary, bg)
	if selected {
		titleStyle = titleStyle.Bold(true)
	}
	hintStyle := styleFgBg(theme.TextMuted, bg)
	iconFg := theme.TextSecondary
	if selected {
		iconFg = theme.TextPrimary
	}
	iconStyle := styleFgBg(iconFg, bg)
	bgFill := styleBg(bg)

	// LEFT_PAD
	row := bgFill.Render(strings.Repeat(" ", menuLeftPad))

	// ICON column (always menuIconCol cells wide when any item has an icon).
	if showIcons {
		if it.Icon != "" {
			glyphW := runewidth.StringWidth(it.Icon)
			row += iconStyle.Render(it.Icon)
			// Pad the icon column to exactly menuIconCol cells (handles
			// wide glyphs and missing icons uniformly).
			if pad := menuIconCol - glyphW; pad > 0 {
				row += bgFill.Render(strings.Repeat(" ", pad))
			}
		} else {
			row += bgFill.Render(strings.Repeat(" ", menuIconCol))
		}
		// TITLE_GAP
		row += bgFill.Render(strings.Repeat(" ", menuTitleGap))
	}

	// TITLE
	row += titleStyle.Render(it.Title)

	// HINT_GAP + HINT (right-aligned). When the item has no hint we still
	// pad the row to innerW so every row has the same visible width.
	used := lipgloss.Width(row)
	hintW := runewidth.StringWidth(it.Hint)
	if hintW > 0 {
		// Total cells needed: used + gap + hint + RIGHT_PAD == innerW
		gap := innerW - used - hintW - menuRightPad
		if gap < menuHintGap {
			gap = menuHintGap
		}
		row += bgFill.Render(strings.Repeat(" ", gap))
		row += hintStyle.Render(it.Hint)
		row += bgFill.Render(strings.Repeat(" ", menuRightPad))
	} else {
		// No hint: still emit the right pad, then fill the rest with bg.
		// padToWidth handles any residual cells.
	}

	return padToWidth(row, innerW, bg)
}

// renderHeaderRow renders the optional header row (title + icon) followed
// by a hairline separator. Both rows are exactly innerW cells wide.
func (m Model) renderHeaderRow(innerW int) (string, string) {
	bgFill := styleBg(theme.BgHover)
	iconStyle := styleFgBg(theme.AccentLavender, theme.BgHover).Bold(true)
	titleStyle := styleFgBg(theme.TextPrimary, theme.BgHover).Bold(true)

	row := bgFill.Render(strings.Repeat(" ", menuLeftPad))
	if m.icon != "" {
		row += iconStyle.Render(m.icon)
		row += bgFill.Render(" ")
	}
	row += titleStyle.Render(m.title)
	row = padToWidth(row, innerW, theme.BgHover)
	return row, renderSeparatorRow(innerW)
}

// render produces the full menu including its rounded border. Layout
// matches the spec in the package doc: every body row is exactly innerW
// cells of panel-bg-styled output, separators are hairlines, the selected
// row has a subtle solid bg distinct from the panel bg.
func (m Model) render() string {
	innerW := m.panelInnerWidth()
	showIcons := m.itemsHaveIcons()

	var rows []string

	if m.hasHeader() {
		hdr, hr := m.renderHeaderRow(innerW)
		rows = append(rows, hdr, hr)
	}

	for i, it := range m.items {
		if it.Sep {
			rows = append(rows, renderSeparatorRow(innerW))
			continue
		}
		rows = append(rows, renderItemRow(it, i == m.cursor, innerW, showIcons))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)

	// Border background MUST match panel bg so the rounded-corner cells
	// blend with the body. BorderSubtle is the muted grey rim.
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.LG(theme.BorderSubtle)).
		BorderBackground(theme.LG(theme.BgHover)).
		Background(theme.LG(theme.BgHover)).
		Render(content)
}

// overlayString draws top onto base at (x, y), preserving ANSI colors of base
// outside the menu region.
func overlayString(base, top string, x, y, scrW int) string {
	baseLines := strings.Split(base, "\n")
	topLines := strings.Split(top, "\n")
	for i, tl := range topLines {
		ly := y + i
		if ly < 0 || ly >= len(baseLines) {
			continue
		}
		bl := baseLines[ly]
		blW := ansi.StringWidth(bl)

		var left string
		if x <= blW {
			left = ansi.Truncate(bl, x, "")
		} else {
			left = bl + strings.Repeat(" ", x-blW)
		}
		if lw := ansi.StringWidth(left); lw < x {
			left += strings.Repeat(" ", x-lw)
		}

		topW := ansi.StringWidth(tl)
		var right string
		if x+topW < blW {
			right = ansi.TruncateLeft(bl, x+topW, "")
		}
		baseLines[ly] = left + tl + right
	}
	return strings.Join(baseLines, "\n")
}
