package tabbar

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/charmbracelet/x/ansi"
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
	first    int // index of the first visible tab when the strip overflows
	regions  []tabRegion
	blurred  bool // the editor does not have focus (see SetFocused)
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
	m.EnsureVisible(active)
}

// Buffers returns the tabs in display order and the active buffer id.
func (m Model) Buffers() ([]nvim.BufferInfo, int) {
	return append([]nvim.BufferInfo(nil), m.bufs...), m.activeID
}

// SetFocused tells the bar whether the editor has the keys. Without focus
// the active tab keeps its look but its ▎ accent is dimmed, so the bright
// accent always marks the pane that takes the keys.
func (m *Model) SetFocused(f bool) { m.blurred = !f }

func (m *Model) SetWidth(w int) {
	m.w = w
	m.EnsureVisible(m.activeID)
}

// Height returns the row count the tab bar occupies. Single-row layout —
// the active tab is already distinguished by its different bg + bold name
// + left-edge ▎ accent, so an extra top accent strip just reads as a
// blank row in most terminals.
func (m Model) Height() int { return 1 }

// tabCells renders tab i at its full width.
func (m Model) tabCells(i int) []string {
	b := m.bufs[i]
	return tabCellsCached(b, b.ID == m.activeID, m.suffix(b), m.isWelcome(b), 0)
}

// tabWidths returns each tab's full width in cells.
func (m Model) tabWidths() []int {
	widths := make([]int, len(m.bufs))
	for i := range m.bufs {
		widths[i] = len(m.tabCells(i))
	}
	return widths
}

// totalWidth is the width of every tab plus the 1-cell separators.
func totalWidth(widths []int) int {
	t := 0
	for i, w := range widths {
		t += w
		if i > 0 {
			t++
		}
	}
	return t
}

// strip is the visible part of the tab bar: tabs [first, last] shown whole,
// and the overflow indicators. Tabs are never cut at an edge (VSCode); a
// single tab wider than the bar is shown alone with its name shortened
// with "…" at the end (fitW > 0 is the width it must fit in).
type strip struct {
	first, last         int
	leftOver, rightOver bool
	fitW                int
}

// layoutFrom lays the tabs out starting at tab first.
func (m Model) layoutFrom(first int, widths []int) strip {
	n := len(widths)
	if n == 0 || m.w <= 0 {
		return strip{first: 0, last: -1}
	}
	if totalWidth(widths) <= m.w {
		return strip{first: 0, last: n - 1}
	}
	if first < 0 {
		first = 0
	}
	if first >= n {
		first = n - 1
	}
	st := strip{first: first, leftOver: first > 0}
	avail := m.w
	if st.leftOver {
		avail--
	}
	// Fit tabs from first; reserve the ▶ cell unless the rest all fit.
	fits := func(limit int) int {
		used, last := 0, first-1
		for i := first; i < n; i++ {
			need := widths[i]
			if i > first {
				need++
			}
			if used+need > limit {
				break
			}
			used += need
			last = i
		}
		return last
	}
	last := fits(avail)
	if last < n-1 {
		st.rightOver = true
		last = fits(avail - 1)
	}
	if last < first {
		// The first tab alone is too wide: show it shortened.
		last = first
		st.fitW = avail
		if st.rightOver {
			st.fitW--
		}
		if st.fitW < 1 {
			st.fitW = 1
		}
	}
	st.last = last
	return st
}

// layout is the strip for the current scroll position.
func (m Model) layout() (strip, []int) {
	widths := m.tabWidths()
	return m.layoutFrom(m.first, widths), widths
}

// settle pulls first back while the tabs from an earlier start still show
// everything up to the end, so the bar never scrolls past its content.
func (m *Model) settle(widths []int) {
	for m.first > 0 {
		st := m.layoutFrom(m.first-1, widths)
		if st.rightOver || st.fitW > 0 {
			break
		}
		m.first--
	}
}

// EnsureVisible scrolls so the tab with the given id is fully visible.
// No-op if the id isn't an open buffer.
func (m *Model) EnsureVisible(id int) {
	if m.w <= 0 || len(m.bufs) == 0 {
		m.first = 0
		return
	}
	widths := m.tabWidths()
	if totalWidth(widths) <= m.w {
		m.first = 0
		return
	}
	if m.first >= len(m.bufs) {
		m.first = len(m.bufs) - 1
	}
	idx := -1
	for i, b := range m.bufs {
		if b.ID == id {
			idx = i
			break
		}
	}
	if idx >= 0 {
		if idx < m.first {
			m.first = idx
		}
		for m.first < idx && m.layoutFrom(m.first, widths).last < idx {
			m.first++
		}
	}
	m.settle(widths)
}

// View draws the bar; the last result is reused while nothing it reads
// changed (see cache.go).
func (m Model) View() string {
	if m.w <= 0 {
		return ""
	}
	k := viewKey{bufs: m.bufs, activeID: m.activeID, w: m.w, first: m.first, blurred: m.blurred, rk: theme.CurrentRenderKey()}
	viewCache.Lock()
	defer viewCache.Unlock()
	if viewCache.ok && viewCache.key.equal(&k) {
		return viewCache.out
	}
	out := m.draw()
	k.bufs = append([]nvim.BufferInfo(nil), m.bufs...)
	viewCache.ok, viewCache.key, viewCache.out = true, k, out
	return out
}

func (m Model) draw() string {
	if len(m.bufs) == 0 {
		fill := theme.FgBg(theme.TextDim, theme.BgTitleBar)
		return fill.Render(strings.Repeat(" ", m.w))
	}
	st, _ := m.layout()

	var sb strings.Builder
	indStyle := theme.FgBg(theme.TextMuted, theme.BgTitleBar)
	used := 0
	if st.leftOver {
		sb.WriteString(indStyle.Render("◀"))
		used++
	}
	sepCell := lipgloss.NewStyle().Background(theme.LG(theme.BgTitleBar)).
		Foreground(theme.Mix(theme.BgTitleBar, theme.BorderDefault, 0.7)).Render("│")
	for i := st.first; i <= st.last; i++ {
		if i > st.first {
			sb.WriteString(sepCell)
			used++
		}
		b := m.bufs[i]
		cells := tabCellsCached(b, b.ID == m.activeID, m.suffix(b), m.isWelcome(b), st.fitW)
		if b.ID == m.activeID && m.blurred && len(cells) > 0 {
			cells = append([]string(nil), cells...) // shared by the cache
			cells[0] = theme.FgBg(theme.TextDimmer, theme.BgEditor).Render("▎")
		}
		sb.WriteString(strings.Join(cells, ""))
		used += len(cells)
	}
	right := 0
	if st.rightOver {
		right = 1
	}
	if fill := m.w - used - right; fill > 0 {
		// Fill the slack with title-bar bg so the row stays at exactly
		// m.w cells (render-width contract).
		sb.WriteString(theme.Bg(theme.BgTitleBar).Render(strings.Repeat(" ", fill)))
	}
	if st.rightOver {
		sb.WriteString(indStyle.Render("▶"))
	}
	return sb.String()
}

// scrollByTabs shifts the strip by n whole tabs (negative = left).
func (m *Model) scrollByTabs(n int) {
	if len(m.bufs) == 0 {
		return
	}
	widths := m.tabWidths()
	if totalWidth(widths) <= m.w {
		m.first = 0
		return
	}
	m.first += n
	if m.first < 0 {
		m.first = 0
	}
	if m.first >= len(m.bufs) {
		m.first = len(m.bufs) - 1
	}
	m.settle(widths)
}

// rebuildRegions recomputes click regions in SCREEN coords for the tabs
// on show (hidden tabs get no region).
func (m *Model) rebuildRegions() {
	m.regions = m.regions[:0]
	if m.w <= 0 || len(m.bufs) == 0 {
		return
	}
	st, widths := m.layout()
	x := 0
	if st.leftOver {
		x = 1
	}
	for i := st.first; i <= st.last; i++ {
		if i > st.first {
			x++ // separator
		}
		w := widths[i]
		if st.fitW > 0 {
			b := m.bufs[i]
			w = len(tabCellsCached(b, b.ID == m.activeID, m.suffix(b), m.isWelcome(b), st.fitW))
		}
		m.regions = append(m.regions, tabRegion{
			start:  x,
			end:    x + w,
			closeX: x + w - 2, // the close glyph sits before the trailing pad
			id:     m.bufs[i].ID,
		})
		x += w
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

	// Indicator hit-tests come before per-tab regions: each pages by
	// the number of tabs on show.
	st, _ := m.layout()
	page := st.last - st.first + 1
	if page < 1 {
		page = 1
	}
	tg, ok := m.TargetAt(x)
	if !ok {
		return m, nil
	}
	id := tg.ID
	switch tg.Kind {
	case TargetScrollLeft:
		if t == tea.MouseLeft {
			m.scrollByTabs(-page)
		}
		return m, nil
	case TargetScrollRight:
		if t == tea.MouseLeft {
			m.scrollByTabs(page)
		}
		return m, nil
	case TargetClose:
		// Close glyph closes on either button.
		return m, func() tea.Msg { return CloseMsg{ID: id} }
	}
	if t == tea.MouseRight {
		sx, sy := screenX, screenY
		return m, func() tea.Msg { return TabContextMsg{ID: id, X: sx, Y: sy} }
	}
	return m, func() tea.Msg { return SwitchMsg{ID: id} }
}

// Target kinds of the tab bar.
const (
	TargetTab         = "tab"
	TargetClose       = "close"
	TargetScrollLeft  = "scroll-left"
	TargetScrollRight = "scroll-right"
)

// Target is one click target of the tab bar, in bar-local columns.
type Target struct {
	Kind string // TargetTab, TargetClose, TargetScrollLeft or TargetScrollRight
	ID   int    // buffer id (tab and close targets)
	X, W int
}

// Targets lists the click targets on show, in the order HandleMouse
// tests them. A tab target spans the whole tab, its close glyph too.
func (m Model) Targets() []Target {
	if m.w <= 0 || len(m.bufs) == 0 {
		return nil
	}
	var out []Target
	st, _ := m.layout()
	if st.leftOver {
		out = append(out, Target{Kind: TargetScrollLeft, X: 0, W: 1})
	}
	if st.rightOver {
		out = append(out, Target{Kind: TargetScrollRight, X: m.w - 1, W: 1})
	}
	m.rebuildRegions()
	for _, r := range m.regions {
		out = append(out, Target{Kind: TargetClose, ID: r.id, X: r.closeX, W: 1})
		out = append(out, Target{Kind: TargetTab, ID: r.id, X: r.start, W: r.end - r.start})
	}
	return out
}

// TargetAt returns the click target at bar-local column x.
func (m Model) TargetAt(x int) (Target, bool) {
	for _, tg := range m.Targets() {
		if x >= tg.X && x < tg.X+tg.W {
			return tg, true
		}
	}
	return Target{}, false
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
	return renderTabCellsSfx(b, active, "", isWelcomeBuf(b), 0)
}

// suffix is the dim parent-folder hint shown after a tab name when
// another open buffer has the same file name (VSCode-style).
func (m Model) suffix(b nvim.BufferInfo) string {
	base := filepath.Base(b.Path)
	dup := false
	for _, o := range m.bufs {
		if o.ID != b.ID && filepath.Base(o.Path) == base && o.Path != b.Path {
			dup = true
			break
		}
	}
	if !dup || isWelcomeBuf(b) {
		return ""
	}
	return filepath.Base(filepath.Dir(b.Path))
}

// isWelcome: the directory buffer, or the lone unnamed buffer termocode
// starts with — both show the dashboard.
func (m Model) isWelcome(b nvim.BufferInfo) bool {
	return isWelcomeBuf(b) || (b.Path == "" && len(m.bufs) <= 1)
}

var dirCache = map[string]bool{}

// isWelcomeBuf reports whether the buffer is the start-up directory
// buffer, which shows the dashboard and is labelled "Welcome".
func isWelcomeBuf(b nvim.BufferInfo) bool {
	if b.Path == "" || b.Path == "." {
		return b.Path == "."
	}
	isDir, ok := dirCache[b.Path]
	if !ok {
		st, err := os.Stat(b.Path)
		isDir = err == nil && st.IsDir()
		dirCache[b.Path] = isDir
	}
	return isDir
}

// renderTabCellsSfx renders one tab. maxW > 0 is the most cells it may
// take: the folder hint goes first, then the name is shortened with "…"
// at its end.
func renderTabCellsSfx(b nvim.BufferInfo, active bool, suffix string, welcome bool, maxW int) []string {
	name := filepath.Base(b.Path)
	if b.Title != "" {
		name = b.Title
	}
	if welcome {
		name = "Welcome"
	}
	if name == "" {
		name = "[unnamed]"
	}
	name = ansi.Truncate(name, 40, "…")
	suffix = ansi.Truncate(suffix, 16, "…")
	if maxW > 0 {
		cells := renderTabCellsSfx(b, active, suffix, welcome, 0)
		if len(cells) <= maxW {
			return cells
		}
		if suffix != "" {
			return renderTabCellsSfx(b, active, "", welcome, maxW)
		}
		over := len(cells) - maxW
		keep := ansi.StringWidth(name) - over
		if keep < 1 {
			keep = 1
		}
		name = ansi.Truncate(name, keep, "…")
	}

	bg, fg := theme.BgTitleBar, theme.TextMuted
	if active {
		bg, fg = theme.BgEditor, theme.TextWhite
	}

	bgStyle := theme.Bg(bg)
	textStyle := theme.FgBg(fg, bg).Bold(active)
	dirtyStyle := theme.FgBg(theme.AccentAmber, bg).Bold(true)
	closeStyle := theme.FgBg(theme.TextMuted, bg)
	accentStyle := theme.FgBg(theme.BorderFocus, bg).Bold(true)

	cells := make([]string, 0, 8+len(name))
	if active {
		cells = append(cells, accentStyle.Render("▎"))
	} else {
		cells = append(cells, bgStyle.Render(" "))
	}
	cells = append(cells, bgStyle.Render(" "))
	if theme.CurrentIconMode != theme.IconModeASCII {
		g, c := theme.FileIconFor(name, false, false)
		if welcome {
			g, c = "\uf015", theme.BorderFocus // nf-fa-home
			if theme.CurrentIconMode == theme.IconModeUnicode {
				g = "⌂"
			}
		}
		if !active {
			// Inactive tabs keep a hint of the type colour.
			cells = append(cells, lipgloss.NewStyle().Background(theme.LG(bg)).
				Foreground(theme.Mix(bg, c, 0.7)).Render(g))
		} else {
			cells = append(cells, theme.FgBg(c, bg).Render(g))
		}
		cells = append(cells, bgStyle.Render(" "))
	}
	cells = appendGlyphCells(cells, name, textStyle)
	if suffix != "" {
		dim := theme.FgBg(theme.TextDim, bg)
		cells = append(cells, bgStyle.Render(" "))
		cells = appendGlyphCells(cells, suffix, dim)
	}
	cells = append(cells, bgStyle.Render(" "))
	// A modified buffer shows the dirty dot where the close glyph sits
	// (VSCode); the slot still closes the tab on click. Only the active
	// tab shows ×: on every tab it was a row of noise (UI review #8).
	switch {
	case b.Modified:
		cells = append(cells, dirtyStyle.Render("●"))
	case active:
		cells = append(cells, closeStyle.Render(closeGlyph()))
	default:
		cells = append(cells, bgStyle.Render(" "))
	}
	cells = append(cells, bgStyle.Render(" "))
	return cells
}

// appendGlyphCells adds s to cells one glyph (grapheme) at a time: a
// glyph two cells wide (CJK, emoji, a flag) is followed by an empty
// cell, so len(cells) stays the width; a mark with no letter before it
// joins the cell before.
func appendGlyphCells(cells []string, s string, st lipgloss.Style) []string {
	for s != "" {
		g, w := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if g == "" {
			break
		}
		s = s[len(g):]
		if w == 0 && len(cells) > 0 {
			cells[len(cells)-1] += st.Render(g)
			continue
		}
		cells = append(cells, st.Render(g))
		for i := 1; i < w; i++ {
			cells = append(cells, "")
		}
	}
	return cells
}

// closeGlyph is the tab close button: the shared close glyph.
func closeGlyph() string { return theme.GlyphClose.String() }

func (m Model) extensionIcon(name string) string {
	// Reserved for a Nerd-Font-driven extension→icon map. For now keep the
	// indicator slot blank, returning "" so callers can grow the layout
	// without touching renderTab.
	_ = name
	return ""
}

// Width returns the configured pane width.
func (m Model) Width() int { return m.w }

// ScrollX returns the current horizontal scroll offset in cells (the
// start of the first visible tab in the full strip). Exposed for tests.
func (m Model) ScrollX() int {
	x := 0
	for i, w := range m.tabWidths() {
		if i >= m.first {
			break
		}
		x += w + 1
	}
	return x
}
