package activity

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"termocode/internal/theme"
)

// View identifies which panel the sidebar should show.
type View int

const (
	ViewFiles View = iota
	ViewSearch
	ViewGit
	ViewRun
	ViewSettings // bottom group; not yet wired to a sidebar
)

// Width is the column count taken by the activity bar.
//
// Cell layout per row:
//
//	col 0  accent — cyan ▎ when the row's item is active, else BgActivityBar
//	col 1  left padding (BgActivityBar)
//	col 2  icon glyph
//	col 3  right padding (BgActivityBar)
//
// Visual separation with the next pane comes from the BgActivityBar↔BgSidebar
// contrast — we never draw an explicit divider column.
const Width = 4

// itemPitch is the row count reserved per icon: the bold artwork is
// 2-rows tall (top half + letter half).
const itemPitch = 2

// interItemDivider is the number of rows between consecutive items. The
// row holds a thin faint hairline that doesn't span the full bar width
// (rendered as a short centred segment), giving the eye a soft separation
// between F / G / R without the heaviness of a full-width rule.
const interItemDivider = 1

// itemStride is the row count an item occupies including its trailing
// divider. The last item in a group has no trailing divider, so total
// rows per group = len(items)*itemPitch + (len(items)-1)*interItemDivider.
const itemStride = itemPitch + interItemDivider

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
// ViewSearch's enum value is retained for back-compat.
var topItems = []item{
	{view: ViewFiles, icon: theme.IconFiles, label: "Explorer"},
	{view: ViewGit, icon: theme.IconGit, label: "Source Control"},
	{view: ViewRun, icon: theme.IconRun, label: "Run"},
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
	if len(topItems) == 0 {
		return 0
	}
	return len(topItems)*itemPitch + (len(topItems)-1)*interItemDivider
}

// bottomGroupRows returns the row count taken by the bottom-of-bar group.
func bottomGroupRows() int {
	if len(bottomItems) == 0 {
		return 0
	}
	return len(bottomItems)*itemPitch + (len(bottomItems)-1)*interItemDivider
}

// bottomGroupStart returns the first row of the bottom group given the
// current bar height. -1 means there isn't room for a separate bottom group.
func (m Model) bottomGroupStart() int {
	start := m.h - bottomGroupRows()
	if start < topGroupRows()+1 {
		return -1
	}
	return start
}

// itemAt maps a click row to its item. Inter-item divider rows are NOT
// clickable — they return ok=false so a click in the gap is a no-op
// (matches the visual cue that nothing's there).
func (m Model) itemAt(y int) (View, bool) {
	if y < topGroupRows() {
		idx, isItem := strideIndex(y)
		if !isItem || idx >= len(topItems) {
			return 0, false
		}
		return topItems[idx].view, true
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
	idx := localY / itemStride
	off := localY % itemStride
	if off < itemPitch {
		return idx, true
	}
	return idx, false
}

type rowKind int

const (
	rowEmpty rowKind = iota
	rowDivider          // top↔bottom group separator (existing)
	rowInterItem        // thin hairline between two items in the same group
	rowItemTop          // top half of an icon's 2-row pitch — bold[0]
	rowItemBottom       // bottom half of an icon's 2-row pitch — bold[1]
)

func (m Model) classify(row int) (rowKind, *item) {
	if row < topGroupRows() {
		return classifyInGroup(row, topItems)
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
	idx := localY / itemStride
	off := localY % itemStride
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

func (m Model) View() string {
	if m.h <= 0 {
		return ""
	}
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
	case rowItemTop:
		return renderActivityBoldRow(it.icon, 0, it.view == m.active, st)
	case rowItemBottom:
		return renderActivityBoldRow(it.icon, 1, it.view == m.active, st)
	case rowInterItem:
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

// renderInterItemDivider draws the hairline that sits between two
// consecutive items. `─` (U+2500 LIGHT HORIZONTAL) sits visually at the
// middle of its cell — the closest the unicode block-drawing range
// offers to a "middle 1/8 stroke." Rendered in TextQuaternary +
// Faint(true) so the stroke reads as an almost-imperceptible whisker
// rather than a styled rule. A 2-cell segment centred in the 4-cell
// column keeps the line short of the bar's edges.
func renderInterItemDivider(st styleSet) string {
	const segW = 2
	pad := (Width - segW) / 2
	style := theme.FgBg(theme.TextQuaternary, theme.BgActivityBar).Faint(true)
	out := st.bgFill.Render(strings.Repeat(" ", pad)) +
		style.Render(strings.Repeat("─", segW)) +
		st.bgFill.Render(strings.Repeat(" ", Width-pad-segW))
	return st.rowFrame.Render(out)
}
