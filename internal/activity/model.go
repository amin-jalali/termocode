package activity

import (
	"sync"

	"github.com/muesli/termenv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/theme"
)

// View identifies which panel the sidebar should show.
type View int

const (
	ViewFiles View = iota
	ViewSearch
	ViewGit
	ViewRun
	ViewSettings  // bottom group; not yet wired to a sidebar
	ViewTests     // Group E: Testing view (test explorer)
	ViewServices  // dev services: Docker Compose and local commands
	ViewDatabases // database connections and schema browser
)

// Width is the column count taken by the activity bar.
//
// Cell layout per row:
//
//	col 0  accent — cyan ▎ when the row's item is active, else BgActivityBar
//	col 1  left padding (BgActivityBar)
//	col 2  icon glyph — the exact middle of the bar
//	col 3  right padding (BgActivityBar)
//	col 4  right padding (BgActivityBar)
//
// An odd width puts a one-cell icon (and the 3-cell termocode mark) in the
// middle of the bar; with 4 cells they sat half a cell off.
// Visual separation with the next pane comes from the BgActivityBar↔BgSidebar
// contrast — we never draw an explicit divider column.
const Width = 5

// itemPitch is the row count reserved per icon: the bold artwork is
// 2-rows tall (top half + letter half).
const itemPitch = 2

// interItemDivider is the number of rows between consecutive items. With
// the ASCII bold artwork the row holds a short hairline between the 2×2
// blocks; single-cell icons (Nerd / Unicode) already have a blank second
// row, so they need no gap (UI review #9).
func interItemDivider() int {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return 1
	}
	return 0
}

// itemStride is the row count an item occupies including its trailing
// divider. The last item in a group has no trailing divider, so total
// rows per group = len(items)*itemPitch + (len(items)-1)*interItemDivider.
func itemStride() int { return itemPitch + interItemDivider() }

// markRows is the height of the termocode mark at the top of the bar (the
// logo's pixel "T"); topStart leaves one blank row under it.
const (
	markRows = 2
	topStart = markRows + 1
)

// OpenPaletteMsg fires when the user clicks the termocode mark.
type OpenPaletteMsg struct{}

// SwitchMsg fires when the user clicks an icon.
type SwitchMsg struct{ View View }

// ToggleSidebarMsg fires when the user clicks the already-active icon.
type ToggleSidebarMsg struct{}

type item struct {
	view  View
	icon  theme.Icon
	label string
}

// Search and Outline were both removed from the activity bar:
//   - Search was a stub that just reminded the user to press F8 / Ctrl+F.
//   - Outline is now a floating glass-overlay symbol picker invoked via
//     Ctrl+Shift+O / palette / ⋮ menu.
//
// ViewSearch's enum value is retained for back-compat.
var topItems = []item{
	{view: ViewFiles, icon: theme.IconFiles, label: "Explorer"},
	{view: ViewGit, icon: theme.IconGit, label: "Source Control"},
	{view: ViewRun, icon: theme.IconRun, label: "Run"},
	{view: ViewTests, icon: theme.IconTests, label: "Testing"}, // Group E
	{view: ViewServices, icon: theme.IconContainers, label: "Containers"},
	{view: ViewDatabases, icon: theme.IconDatabases, label: "Databases"},
}

var bottomItems = []item{
	{view: ViewSettings, icon: theme.IconSettings, label: "Settings"},
}

type Model struct {
	active View
	h      int
}

func New() Model { return Model{active: ViewFiles} }

func (m *Model) SetHeight(h int)  { m.h = h }
func (m *Model) SetActive(v View) { m.active = v }
func (m Model) Active() View      { return m.active }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) { return m, nil }

// HandleMouse processes a click within the activity bar (x in [0, Width)).
func (m Model) HandleMouse(x, y int, t tea.MouseEventType) (Model, tea.Cmd) {
	if t != tea.MouseLeft {
		return m, nil
	}
	if x < 0 || x >= Width {
		return m, nil
	}
	if y < 0 || y >= m.h {
		return m, nil
	}
	if y < markRows {
		return m, func() tea.Msg { return OpenPaletteMsg{} }
	}
	target, ok := m.itemAt(y)
	if !ok {
		return m, nil
	}
	if target == m.active {
		return m, func() tea.Msg { return ToggleSidebarMsg{} }
	}
	return m, func() tea.Msg { return SwitchMsg{View: target} }
}

// topGroupRows returns the row count taken by the top-of-bar group:
// itemPitch per item plus interItemDivider between adjacent items.
func topGroupRows() int {
	items := topAll() // Group I: + extension items
	if len(items) == 0 {
		return 0
	}
	return len(items)*itemPitch + (len(items)-1)*interItemDivider()
}

// bottomGroupRows returns the row count taken by the bottom-of-bar group.
func bottomGroupRows() int {
	if len(bottomItems) == 0 {
		return 0
	}
	return len(bottomItems)*itemPitch + (len(bottomItems)-1)*interItemDivider()
}

// bottomGroupStart returns the first row of the bottom group given the
// current bar height. -1 means there isn't room for a separate bottom group.
func (m Model) bottomGroupStart() int {
	start := m.h - bottomGroupRows()
	if start < topStart+topGroupRows()+1 {
		return -1
	}
	return start
}

// itemAt maps a click row to its item. Inter-item divider rows are NOT
// clickable — they return ok=false so a click in the gap is a no-op
// (matches the visual cue that nothing's there).
func (m Model) itemAt(y int) (View, bool) {
	if y < topStart {
		return 0, false
	}
	if y < topStart+topGroupRows() {
		idx, isItem := strideIndex(y - topStart)
		items := topAll() // Group I: + extension items
		if !isItem || idx >= len(items) {
			return 0, false
		}
		return items[idx].view, true
	}
	bs := m.bottomGroupStart()
	if bs < 0 {
		return 0, false
	}
	if y >= bs && y < bs+bottomGroupRows() {
		idx, isItem := strideIndex(y - bs)
		if !isItem || idx >= len(bottomItems) {
			return 0, false
		}
		return bottomItems[idx].view, true
	}
	return 0, false
}

// strideIndex maps a group-local row to (itemIdx, isItemRow). isItemRow is
// false for the inter-item divider row that sits between adjacent items.
// Rows within an item's pitch (top half + letter half) both return the
// same itemIdx so the click target spans both rows of the icon.
func strideIndex(localY int) (int, bool) {
	idx := localY / itemStride()
	off := localY % itemStride()
	if off < itemPitch {
		return idx, true
	}
	return idx, false
}

type rowKind int

const (
	rowEmpty      rowKind = iota
	rowDivider            // top↔bottom group separator (existing)
	rowInterItem          // thin hairline between two items in the same group
	rowItemTop            // top half of an icon's 2-row pitch — bold[0]
	rowItemBottom         // bottom half of an icon's 2-row pitch — bold[1]
	rowMark               // the termocode mark (rows 0..markRows-1)
)

func (m Model) classify(row int) (rowKind, *item) {
	if row < markRows {
		return rowMark, nil
	}
	if row < topStart {
		return rowEmpty, nil
	}
	if row < topStart+topGroupRows() {
		return classifyInGroup(row-topStart, topAll()) // Group I: + extension items
	}
	bs := m.bottomGroupStart()
	if bs < 0 {
		return rowEmpty, nil
	}
	if row == bs-1 {
		return rowDivider, nil
	}
	if row >= bs && row < bs+bottomGroupRows() {
		return classifyInGroup(row-bs, bottomItems)
	}
	return rowEmpty, nil
}

func classifyInGroup(localY int, items []item) (rowKind, *item) {
	idx := localY / itemStride()
	off := localY % itemStride()
	if idx >= len(items) {
		return rowEmpty, nil
	}
	switch {
	case off == 0:
		return rowItemTop, &items[idx]
	case off == 1:
		return rowItemBottom, &items[idx]
	default:
		return rowInterItem, nil
	}
}

// styleSet bundles every style used by the activity bar. Each style
// explicitly sets BgActivityBar — without that, the segment-level resets
// emitted by lipgloss leak the terminal's default background between
// adjacent styled chunks (visible as black/transparent strips beside the
// cyan accent).
type styleSet struct {
	bgFill   lipgloss.Style
	accent   lipgloss.Style
	muted    lipgloss.Style
	active   lipgloss.Style
	divider  lipgloss.Style
	rowFrame lipgloss.Style
}

func newStyleSet() styleSet {
	return styleSet{
		bgFill: theme.Bg(theme.BgActivityBar),
		accent: theme.FgBg(theme.BorderFocus, theme.BgActivityBar).Bold(true),
		// Activity-bar icons are 1-cell glyphs sitting in a 4-cell column;
		// they read as visually thin without weight. Bold pushes most fonts
		// to render the strokes thicker, which gives the icons more
		// presence without changing layout (still 1 cell wide).
		muted:   theme.FgBg(theme.TextMuted, theme.BgActivityBar).Bold(true),
		active:  theme.FgBg(theme.TextWhite, theme.BgActivityBar).Bold(true),
		divider: theme.FgBg(theme.BorderDefault, theme.BgActivityBar),
		// rowFrame pins each row to exactly Width cells so JoinHorizontal
		// upstream never inserts default-styled padding.
		rowFrame: lipgloss.NewStyle().
			Width(Width).
			Background(theme.LG(theme.BgActivityBar)).
			Inline(true),
	}
}

// viewKey is everything View depends on; viewCache keeps the last result.
// The bar is drawn on every frame but almost never changes, and drawing
// it styled every row again (a tenth of a frame while a key is held).
type viewKey struct {
	active  View
	h       int
	icons   theme.IconMode
	profile termenv.Profile
	colors  [5]string
}

var viewCache struct {
	sync.Mutex
	key viewKey
	out string
}

func (m Model) View() string {
	if m.h <= 0 {
		return ""
	}
	key := viewKey{active: m.active, h: m.h, icons: theme.CurrentIconMode, profile: lipgloss.ColorProfile(),
		colors: [5]string{theme.Hex(theme.BgActivityBar), theme.Hex(theme.BorderFocus), theme.Hex(theme.TextMuted),
			theme.Hex(theme.TextWhite), theme.Hex(theme.BorderDefault)}}
	viewCache.Lock()
	defer viewCache.Unlock()
	if viewCache.out != "" && viewCache.key == key {
		return viewCache.out
	}
	viewCache.key, viewCache.out = key, m.draw()
	return viewCache.out
}

func (m Model) draw() string {
	st := newStyleSet()
	rows := make([]string, m.h)
	for r := 0; r < m.h; r++ {
		rows[r] = m.renderRow(r, st)
	}
	return strings.Join(rows, "\n")
}

func (m Model) renderRow(row int, st styleSet) string {
	emptyRow := st.bgFill.Render(strings.Repeat(" ", Width))
	kind, it := m.classify(row)
	switch kind {
	case rowEmpty:
		return emptyRow
	case rowDivider:
		return st.divider.Render(strings.Repeat("─", Width))
	case rowMark:
		return renderMarkRow(row, st)
	case rowItemTop, rowItemBottom:
		if theme.CurrentIconMode != theme.IconModeASCII {
			return renderActivityGlyphRow(it.icon, kind == rowItemTop, it.view == m.active, st)
		}
		if kind == rowItemTop {
			return renderActivityBoldRow(it.icon, 0, it.view == m.active, st)
		}
		return renderActivityBoldRow(it.icon, 1, it.view == m.active, st)
	case rowInterItem:
		if theme.CurrentIconMode != theme.IconModeASCII {
			return emptyRow
		}
		return renderInterItemDivider(st)
	}
	return emptyRow
}

// renderActivityBoldRow renders one Width-wide row of the activity bar
// using the icon's bold (2×2) artwork. boldRow selects which row of the
// 2-row block to draw (0 = top, 1 = bottom). The accent column (col 0)
// is painted on both rows when the item is active so the cyan bar reads
// as a 2-row tall indicator. The 2-cell bold block is positioned flush
// against the accent column (cols 1–2), with the remaining cell (col 3)
// filled by BgActivityBar — this keeps the artwork close to the accent
// for a tighter visual cluster than centering would.
//
// itemPitch < 2 (defensive — never the case at the moment) still maps a
// single body row to Bold[1], so the icon's lower half remains visible.
func renderActivityBoldRow(ic theme.Icon, boldRow int, isActive bool, st styleSet) string {
	// col 0 — accent column.
	leftCell := st.bgFill.Render(" ")
	if isActive {
		leftCell = st.accent.Render("▎")
	}

	bold := ic.BoldRender()
	row := bold[0]
	if boldRow != 0 {
		row = bold[1]
	}

	// Defensive: pad / truncate to exactly 2 visible cells. The hand-
	// crafted Bold artwork already obeys this contract; the fallback in
	// Icon.BoldRender() does too. This guard catches any future Icon
	// declared with a malformed Bold field without breaking layout.
	rowW := runewidth.StringWidth(row)
	const slotW = 2
	if rowW < slotW {
		row += strings.Repeat(" ", slotW-rowW)
	} else if rowW > slotW {
		row = runewidth.Truncate(row, slotW, "")
		rowW = runewidth.StringWidth(row)
		if rowW < slotW {
			row += strings.Repeat(" ", slotW-rowW)
		}
	}

	style := st.muted
	if isActive {
		style = st.active
	}

	// Body layout: cols 1..Width-1 = (Width-1) cells = 3 cells.
	// Bold artwork is 2 cells flush after the accent; remaining 1 cell
	// is bg-filled padding on the right.
	body := Width - 1
	rightPad := body - slotW
	if rightPad < 0 {
		rightPad = 0
	}

	out := leftCell +
		style.Render(row) +
		st.bgFill.Render(strings.Repeat(" ", rightPad))
	// Last-resort pin so JoinHorizontal sees exactly Width.
	return st.rowFrame.Render(out)
}

// renderActivityGlyphRow draws one row of an item with a single-cell icon
// (Nerd Font or Unicode set): the icon sits on the first row, the second
// row is breathing room. The active item gets the accent edge and a soft
// accent wash across both rows.
func renderActivityGlyphRow(ic theme.Icon, iconRow, isActive bool, st styleSet) string {
	bg := theme.LG(theme.BgActivityBar)
	if isActive {
		bg = theme.Wash(theme.BgActivityBar, theme.WashSelected)
	}
	fill := lipgloss.NewStyle().Background(bg)
	lead := st.bgFill.Render(" ")
	if isActive {
		lead = lipgloss.NewStyle().Background(theme.LG(theme.BgActivityBar)).
			Foreground(theme.LG(theme.BorderFocus)).Render("▎")
	}
	g := " "
	if iconRow {
		g = ic.String()
		// Unicode mode: plain letters (F, G, R, T …) read better than
		// thin, font-dependent symbols. Nerd mode keeps its glyphs.
		if theme.CurrentIconMode == theme.IconModeUnicode || runewidth.StringWidth(g) != 1 {
			g = ic.ASCII
		}
	}
	fg := theme.LG(theme.TextMuted)
	if isActive {
		fg = theme.LG(theme.BorderFocus)
	}
	icon := lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(true).Render(g)
	return st.rowFrame.Render(lead + fill.Render(" ") + icon + fill.Render(strings.Repeat(" ", Width-3)))
}

// renderInterItemDivider draws the hairline that sits between two
// consecutive items. `─` (U+2500 LIGHT HORIZONTAL) sits visually at the
// middle of its cell — the closest the unicode block-drawing range
// offers to a "middle 1/8 stroke." Rendered in TextQuaternary (no Faint:
// terminals differ too much on SGR 2) so the stroke reads as a quiet
// whisker rather than a styled rule. A 2-cell segment centred in the 4-cell
// column keeps the line short of the bar's edges.
func renderInterItemDivider(st styleSet) string {
	const segW = 2
	pad := (Width - segW) / 2
	style := theme.FgBg(theme.TextQuaternary, theme.BgActivityBar)
	out := st.bgFill.Render(strings.Repeat(" ", pad)) +
		style.Render(strings.Repeat("─", segW)) +
		st.bgFill.Render(strings.Repeat(" ", Width-pad-segW))
	return st.rowFrame.Render(out)
}

// renderMarkRow draws one row of the termocode mark: the pixel "T" of the
// start-page logo in half blocks (top bar, then the stem), in the accent
// colours. ASCII mode shows a plain "T".
func renderMarkRow(row int, st styleSet) string {
	bg := theme.LG(theme.BgActivityBar)
	cell := func(c theme.Color256, s string) string {
		return lipgloss.NewStyle().Background(bg).Foreground(theme.LG(c)).Render(s)
	}
	// The mark is 3 cells wide, centred on col 2 (the icons' column).
	if theme.CurrentIconMode == theme.IconModeASCII {
		if row == 0 {
			return st.rowFrame.Render(st.bgFill.Render("  ") + cell(theme.BorderFocus, "T") + st.bgFill.Render(strings.Repeat(" ", Width-3)))
		}
		return st.bgFill.Render(strings.Repeat(" ", Width))
	}
	if row == 0 {
		return st.rowFrame.Render(st.bgFill.Render(" ") +
			cell(theme.BorderFocus, "▀") + cell(theme.AccentLavender, "█") + cell(theme.AccentMagenta, "▀") +
			st.bgFill.Render(strings.Repeat(" ", Width-4)))
	}
	return st.rowFrame.Render(st.bgFill.Render("  ") + cell(theme.AccentLavender, "▀") + st.bgFill.Render(strings.Repeat(" ", Width-3)))
}
