package tabbar

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"termocode/internal/nvim"
	"termocode/internal/theme"
)

// SwitchMsg is emitted when the user clicks a tab body.
type SwitchMsg struct{ ID int }

// CloseMsg is emitted when the user clicks a tab's close glyph.
type CloseMsg struct{ ID int }

// TabContextMsg is emitted when the user right-clicks a tab body. X/Y are
// the screen coordinates where the context menu should anchor.
type TabContextMsg struct {
	ID   int
	X, Y int
}

// tabRegion records hit-test ranges in SCREEN coordinates (already
// adjusted for scrollX and the optional left overflow indicator).
type tabRegion struct {
	start  int // first screen col (inclusive)
	end    int // first screen col after tab (exclusive)
	closeX int // screen col of the close glyph
	id     int
}

type Model struct {
	bufs     []nvim.BufferInfo
	activeID int
	w        int
	scrollX  int // horizontal offset into the virtual tab strip
	regions  []tabRegion
}

func New() Model { return Model{} }

func (m *Model) SetBuffers(bufs []nvim.BufferInfo, active int) {
	m.bufs = bufs
	prev := m.activeID
	m.activeID = active
	// Auto-scroll so the active tab is fully visible. Run on every
	// SetBuffers call — buffer list churn (open / close / reorder) can
	// shift the active tab's column even if its id didn't change.
	_ = prev
	m.clampScroll()
	m.EnsureVisible(active)
}

func (m *Model) SetWidth(w int) {
	m.w = w
	m.clampScroll()
}

// Height returns the row count the tab bar occupies. Single-row layout —
// the active tab is already distinguished by its different bg + bold name
// + left-edge ▎ accent, so an extra top accent strip just reads as a
// blank row in most terminals.
func (m Model) Height() int { return 1 }

// virtualLayout returns each tab's virtual start column (unscrolled) and
// the total virtual width (tabs + separators). Separators are 1 cell
// between consecutive tabs.
func (m Model) virtualLayout() (starts []int, widths []int, totalW int) {
	starts = make([]int, len(m.bufs))
	widths = make([]int, len(m.bufs))
	x := 0
	for i, b := range m.bufs {
		_, w := renderTab(b, b.ID == m.activeID)
		starts[i] = x
		widths[i] = w
		x += w
		if i < len(m.bufs)-1 {
			x++ // separator
		}
	}
	totalW = x
	return
}

// indicatorLayout decides whether overflow indicators are visible and the
// resulting body width (cells available for tab content).
func (m Model) indicatorLayout(totalW int) (leftOverflow, rightOverflow bool, bodyW, leftReserve int) {
	if m.w <= 0 {
		return false, false, 0, 0
	}
	if totalW <= m.w {
		return false, false, m.w, 0
	}
	leftOverflow = m.scrollX > 0
	if leftOverflow {
		leftReserve = 1
	}
	// Tentative body width with left reserve only.
	bodyW = m.w - leftReserve
	if m.scrollX+bodyW < totalW {
		rightOverflow = true
		bodyW--
	}
	if bodyW < 0 {
		bodyW = 0
	}
	return
}

// clampScroll keeps scrollX inside [0, max] so we can't scroll past the
// content. Recomputes when buffers / width change.
func (m *Model) clampScroll() {
	if m.w <= 0 || len(m.bufs) == 0 {
		m.scrollX = 0
		return
	}
	_, _, totalW := m.virtualLayout()
	if totalW <= m.w {
		m.scrollX = 0
		return
	}
	if m.scrollX < 0 {
		m.scrollX = 0
	}
	// Max scroll: enough to put the right edge flush with body edge.
	// Use the bodyW that *would* be active given the new scrollX. We
	// approximate with the indicators-on case so we never clamp short.
	maxBody := m.w - 1 // at minimum a left indicator is shown when scrolled
	if maxBody < 1 {
		maxBody = 1
	}
	max := totalW - maxBody
	if max < 0 {
		max = 0
	}
	if m.scrollX > max {
		m.scrollX = max
	}
}

// EnsureVisible scrolls horizontally so the tab with the given id becomes
// fully visible. No-op if the id isn't an open buffer or already in view.
func (m *Model) EnsureVisible(id int) {
	if m.w <= 0 || len(m.bufs) == 0 {
		return
	}
	starts, widths, totalW := m.virtualLayout()
	if totalW <= m.w {
		m.scrollX = 0
		return
	}
	idx := -1
	for i, b := range m.bufs {
		if b.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	vStart := starts[idx]
	vEnd := vStart + widths[idx]

	// First pass body width assuming current scrollX.
	_, _, bodyW, leftReserve := m.indicatorLayout(totalW)
	_ = leftReserve
	if bodyW < 1 {
		bodyW = 1
	}

	// If the tab starts before the viewport, scroll left so vStart is at
	// the body's leftmost cell. If it ends after, scroll right so vEnd
	// sits at the right edge.
	if vStart < m.scrollX {
		m.scrollX = vStart
	} else if vEnd > m.scrollX+bodyW {
		m.scrollX = vEnd - bodyW
	}
	m.clampScroll()
}

func (m Model) View() string {
	if m.w <= 0 {
		return ""
	}
	if len(m.bufs) == 0 {
		fill := theme.FgBg(theme.TextDim, theme.BgTitleBar)
		return fill.Render(strings.Repeat(" ", m.w))
	}

	starts, widths, totalW := m.virtualLayout()
	leftOver, rightOver, bodyW, _ := m.indicatorLayout(totalW)

	// Build the virtual row as a flat list of styled cells, one per
	// visual column. This makes cell-level slicing (for clipping at
	// the viewport edges) straightforward — string-level slicing of
	// ANSI escapes is fragile.
	virt := make([]string, 0, totalW)
	for i, b := range m.bufs {
		cells := renderTabCells(b, b.ID == m.activeID)
		// renderTabCells width must match widths[i]
		_ = widths
		virt = append(virt, cells...)
		if i < len(m.bufs)-1 {
			virt = append(virt, theme.FgBg(theme.BorderDefault, theme.BgTitleBar).Render("│"))
		}
	}
	_ = starts

	var sb strings.Builder
	indStyle := theme.FgBg(theme.TextMuted, theme.BgTitleBar)
	if leftOver {
		sb.WriteString(indStyle.Render("◀"))
	}

	// Clip the visible window [scrollX, scrollX+bodyW).
	lo := m.scrollX
	hi := lo + bodyW
	if lo < 0 {
		lo = 0
	}
	if hi > len(virt) {
		hi = len(virt)
	}
	for i := lo; i < hi; i++ {
		sb.WriteString(virt[i])
	}
	emitted := hi - lo
	if emitted < bodyW {
		// Fill the trailing slack with title-bar bg so the row stays
		// at exactly m.w cells (render-width contract).
		sb.WriteString(theme.Bg(theme.BgTitleBar).Render(strings.Repeat(" ", bodyW-emitted)))
	}

	if rightOver {
		sb.WriteString(indStyle.Render("▶"))
	}
	out := sb.String()
	// Defensive width check: if anything drifted, pad / truncate to m.w.
	if w := lipgloss.Width(out); w < m.w {
		out += theme.Bg(theme.BgTitleBar).Render(strings.Repeat(" ", m.w-w))
	}
	return out
}

// scrollByTabs shifts scrollX by `n` whole tabs (negative = left). The
// new scrollX lines up with the start column of a tab so the leftmost
// visible tab is always fully shown — no half-clipped tabs at the
// viewport edge after a wheel tick.
func (m *Model) scrollByTabs(n int) {
	if len(m.bufs) == 0 {
		return
	}
	starts, _, _ := m.virtualLayout()
	// Find the index of the first tab that's at or past scrollX (i.e.
	// the leftmost fully-visible tab in current scroll state).
	cur := 0
	for i, s := range starts {
		if s >= m.scrollX {
			cur = i
			break
		}
		cur = i // keep advancing — last index <= scrollX is the partial one
	}
	target := cur + n
	if target < 0 {
		target = 0
	}
	if target >= len(starts) {
		target = len(starts) - 1
	}
	m.scrollX = starts[target]
	m.clampScroll()
}

// rebuildRegions recomputes click regions in SCREEN coords. Tabs (or
// portions thereof) that fall outside the visible body get no region —
// clicks outside the viewport miss them.
func (m *Model) rebuildRegions() {
	m.regions = m.regions[:0]
	if m.w <= 0 || len(m.bufs) == 0 {
		return
	}
	starts, widths, totalW := m.virtualLayout()
	leftOver, _, bodyW, leftReserve := m.indicatorLayout(totalW)
	_ = leftOver
	bodyLeft := leftReserve            // first screen col of the body
	bodyRight := bodyLeft + bodyW      // first screen col past body
	for i, b := range m.bufs {
		vStart := starts[i]
		vEnd := vStart + widths[i]
		// Translate to screen coords.
		sStart := vStart - m.scrollX + bodyLeft
		sEnd := vEnd - m.scrollX + bodyLeft
		// Clip to body.
		if sEnd <= bodyLeft || sStart >= bodyRight {
			continue
		}
		clampedStart := sStart
		if clampedStart < bodyLeft {
			clampedStart = bodyLeft
		}
		clampedEnd := sEnd
		if clampedEnd > bodyRight {
			clampedEnd = bodyRight
		}
		// Close glyph is at vEnd-2 in virtual coords → vEnd-2-scrollX+bodyLeft
		closeScreenX := vEnd - 2 - m.scrollX + bodyLeft
		// closeX is only meaningful when actually visible; if it got
		// clipped, leave it unreachable (set to -1) so a body click
		// can't accidentally hit it.
		if closeScreenX < clampedStart || closeScreenX >= clampedEnd {
			closeScreenX = -1
		}
		m.regions = append(m.regions, tabRegion{
			start:  clampedStart,
			end:    clampedEnd,
			closeX: closeScreenX,
			id:     b.ID,
		})
		_ = i
	}
}

// HandleMouse reports an action based on click position. x is local to the
// tab bar (0..w-1). screenX/screenY are the absolute screen coords of the
// click — used to anchor the right-click context menu.
func (m Model) HandleMouse(x int, screenX, screenY int, t tea.MouseEventType) (Model, tea.Cmd) {
	// Wheel events scroll the strip horizontally — by TWO tabs per tick
	// (not raw cells). Per-cell scrolling felt jittery because a typical
	// wheel produces several events at once and the user'd zip past
	// half-tabs; stepping in tab boundaries keeps each tick predictable
	// and snaps so the leftmost visible tab always starts at column 0
	// of the viewport.
	switch t {
	case tea.MouseWheelUp:
		m.scrollByTabs(-2)
		return m, nil
	case tea.MouseWheelDown:
		m.scrollByTabs(2)
		return m, nil
	}
	if t != tea.MouseLeft && t != tea.MouseRight {
		return m, nil
	}

	// Indicator hit-tests come before per-tab regions.
	_, _, totalW := m.virtualLayout()
	leftOver, rightOver, bodyW, _ := m.indicatorLayout(totalW)
	if leftOver && x == 0 && t == tea.MouseLeft {
		step := bodyW
		if step < 1 {
			step = 1
		}
		m.scrollX -= step
		m.clampScroll()
		return m, nil
	}
	if rightOver && x == m.w-1 && t == tea.MouseLeft {
		step := bodyW
		if step < 1 {
			step = 1
		}
		m.scrollX += step
		m.clampScroll()
		return m, nil
	}

	m.rebuildRegions()
	for _, r := range m.regions {
		if x < r.start || x >= r.end {
			continue
		}
		id := r.id
		// Close glyph closes on either button.
		if x == r.closeX {
			return m, func() tea.Msg { return CloseMsg{ID: id} }
		}
		if t == tea.MouseRight {
			sx, sy := screenX, screenY
			return m, func() tea.Msg { return TabContextMsg{ID: id, X: sx, Y: sy} }
		}
		return m, func() tea.Msg { return SwitchMsg{ID: id} }
	}
	return m, nil
}

// renderTab returns the rendered tab string and its visible width.
func renderTab(b nvim.BufferInfo, active bool) (string, int) {
	cells := renderTabCells(b, active)
	return strings.Join(cells, ""), len(cells)
}

// renderTabCells returns one styled string per visual column for the tab.
// Width = len(returned slice). Used by View() to enable cell-level
// clipping at viewport edges.
func renderTabCells(b nvim.BufferInfo, active bool) []string {
	name := filepath.Base(b.Path)
	if name == "" {
		name = "[unnamed]"
	}
	if len(name) > 40 {
		name = name[:39] + "…"
	}

	var bg theme.Color256
	var fg theme.Color256
	if active {
		bg = theme.BgEditor
		fg = theme.TextWhite
	} else {
		bg = theme.BgTitleBar
		fg = theme.TextMuted
	}

	bgStyle := theme.Bg(bg)
	textStyle := theme.FgBg(fg, bg)
	textBold := textStyle.Bold(true)
	dirtyStyle := theme.FgBg(theme.SyntaxString, bg).Bold(true)
	closeStyle := theme.FgBg(theme.TextMuted, bg)
	accentStyle := theme.FgBg(theme.AccentLavender, bg).Bold(true)

	cells := make([]string, 0, 7+len(name))
	if active {
		cells = append(cells, accentStyle.Render("▎"))
	} else {
		cells = append(cells, bgStyle.Render(" "))
	}
	if b.Modified {
		cells = append(cells, dirtyStyle.Render("●"))
	} else {
		cells = append(cells, bgStyle.Render(" "))
	}
	cells = append(cells, bgStyle.Render(" "))
	for _, r := range name {
		s := string(r)
		if active {
			cells = append(cells, textBold.Render(s))
		} else {
			cells = append(cells, textStyle.Render(s))
		}
	}
	cells = append(cells, bgStyle.Render(" "))
	cells = append(cells, closeStyle.Render("×"))
	cells = append(cells, bgStyle.Render(" "))
	return cells
}

func (m Model) extensionIcon(name string) string {
	// Reserved for a Nerd-Font-driven extension→icon map. For now keep the
	// indicator slot blank, returning "" so callers can grow the layout
	// without touching renderTab.
	_ = name
	return ""
}

// Width returns the configured pane width.
func (m Model) Width() int { return m.w }

// ScrollX returns the current horizontal scroll offset in virtual cells.
// Exposed for tests and host code that wants to inspect the scroll state.
func (m Model) ScrollX() int { return m.scrollX }
