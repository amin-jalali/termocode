// Package recents implements the "Open Recent File" modal — a dedicated
// command-palette-style overlay that replaces the generic picker for the
// recent-files action.
//
// The component renders ONLY its bordered box (Box()); the host app wraps
// that with its standard glass modal_overlay so the body picks up the
// dim-and-tint blend with the editor underneath. Body cells therefore use
// the same panel bg as the generic picker (#262626) — that's what triggers
// the per-cell glass replacement. Other UI cells (input field, selection
// row, chip pills) use distinct bg colors so they pass through the overlay
// untouched and read as crisp dark glass elements.
package recents

import (
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// blinkMsg toggles cursor visibility on each tick. Mirrors findbar's
// blink mechanism so the search input feels identical.
type blinkMsg struct{}

const blinkPeriod = 530 * time.Millisecond

// BlinkCmd kicks off a blink tick. Hosts call this once when opening the
// recents modal; each tick re-schedules itself, so blink keeps running
// while the modal is open.
func BlinkCmd() tea.Cmd {
	return tea.Tick(blinkPeriod, func(time.Time) tea.Msg { return blinkMsg{} })
}

// Entry mirrors app.RecentEntry — duplicated here so this package doesn't
// import the host app package (which would create a cycle). The host
// converts its []RecentEntry into []Entry at construction time.
type Entry struct {
	Path     string
	OpenedAt time.Time
}

// SelectMsg is emitted when the user activates a row.
type SelectMsg struct{ Path string }

// CloseMsg is emitted when the user dismisses the modal.
type CloseMsg struct{}

// Model holds the recents-modal state. Same shape conventions as the
// generic picker: w/h are the screen dims, the renderer centers itself.
type Model struct {
	entries  []Entry
	filtered []int // indices into entries that survive the substring filter
	cursor   int
	input    string
	w, h     int
	now      time.Time

	// cursorVisible toggles each blink tick. Mirrors findbar's blink so the
	// search input feels identical to the Find widget.
	cursorVisible bool
}

// New builds a fully-populated modal with all `entries` initially visible.
// `now` lets tests freeze the relative-time formatting; production callers
// pass time.Now().
func New(entries []Entry, now time.Time) Model {
	m := Model{entries: entries, now: now, cursorVisible: true}
	m.refilter()
	return m
}

// Init kicks off the blink tick when the recents modal opens — same recipe
// as findbar.Init().
func (m Model) Init() tea.Cmd { return BlinkCmd() }

// SetSize stores the screen dimensions used by Box() to center the modal.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// BoxWidth returns the rendered box width in cells (border included).
// Mirrors layoutDims so the host can hit-test the modal in screen coords.
func (m Model) BoxWidth() int {
	bw, _ := m.layoutDims()
	return bw
}

// BoxHeight returns the rendered box height in cells (border + chrome +
// list rows). Must stay in sync with renderBox()'s row composition:
//   2 border + 9 chrome (blank, header, blank, search, hairline,
//   hairline, blank, footer, blank) + rowsVisible.
func (m Model) BoxHeight() int {
	_, rv := m.layoutDims()
	return 11 + rv
}

// HandleMouse routes a mouse event in SCREEN coordinates. Left-click on a
// result row selects it (emits SelectMsg). Wheel scroll moves the cursor.
// Clicks elsewhere inside the box are consumed silently so they don't
// leak to the editor; clicks outside the box return no command.
func (m Model) HandleMouse(x, y int, action tea.MouseAction, button tea.MouseButton) (Model, tea.Cmd) {
	if m.w <= 0 || m.h <= 0 {
		return m, nil
	}
	boxW, rowsVisible := m.layoutDims()
	boxH := 11 + rowsVisible
	leftCol := (m.w - boxW) / 2
	topRow := (m.h - boxH) / 2
	if x < leftCol || x >= leftCol+boxW || y < topRow || y >= topRow+boxH {
		return m, nil
	}
	// Wheel anywhere inside the box scrolls the list.
	if action == tea.MouseActionPress {
		switch button {
		case tea.MouseButtonWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case tea.MouseButtonWheelDown:
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil
		}
	}
	// Left-click on a result row activates it. Row layout inside the box:
	//   border(1) + blank(1) + header(1) + blank(1) + search(1) + hairline(1)
	//   = first list row is at box-local row 6.
	if action != tea.MouseActionPress || button != tea.MouseButtonLeft {
		return m, nil
	}
	const listStartOffset = 6
	listStart := topRow + listStartOffset
	listEnd := listStart + rowsVisible
	if y < listStart || y >= listEnd {
		return m, nil
	}
	rowOffset := y - listStart
	// Same windowing math as renderResults.
	start := 0
	if m.cursor >= rowsVisible {
		start = m.cursor - rowsVisible + 1
	}
	target := start + rowOffset
	if target < 0 || target >= len(m.filtered) {
		return m, nil
	}
	m.cursor = target
	idx := m.filtered[target]
	if idx < 0 || idx >= len(m.entries) {
		return m, nil
	}
	path := m.entries[idx].Path
	return m, func() tea.Msg { return SelectMsg{Path: path} }
}

// Update consumes one bubbletea message and returns the new model + an
// optional command (typically the SelectMsg/CloseMsg that the host catches
// on the way back through Update).
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if _, ok := msg.(blinkMsg); ok {
		m.cursorVisible = !m.cursorVisible
		return m, BlinkCmd()
	}
	// Any non-blink event (typing, etc.) flips the cursor back to visible
	// so it doesn't ghost-hide right after the user types.
	m.cursorVisible = true
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		return m, func() tea.Msg { return CloseMsg{} }
	case tea.KeyEnter:
		if m.cursor >= 0 && m.cursor < len(m.filtered) {
			idx := m.filtered[m.cursor]
			if idx >= 0 && idx < len(m.entries) {
				path := m.entries[idx].Path
				return m, func() tea.Msg { return SelectMsg{Path: path} }
			}
		}
		return m, nil
	case tea.KeyUp, tea.KeyCtrlP:
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case tea.KeyDown, tea.KeyCtrlN:
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
		return m, nil
	case tea.KeyBackspace:
		if r := []rune(m.input); len(r) > 0 {
			m.input = string(r[:len(r)-1])
			m.refilter()
		}
		return m, nil
	case tea.KeySpace:
		m.input += " "
		m.refilter()
		return m, nil
	case tea.KeyRunes:
		// Match the generic picker's mouse-fragment guard so a stray SGR
		// sequence (e.g. "[<35;70;19M") doesn't leak into the search field.
		if isMouseFragmentEvent(key.Runes) {
			return m, nil
		}
		if key.Alt && isMouseCodeRunes(key.Runes) {
			return m, nil
		}
		m.input += string(key.Runes)
		m.refilter()
		return m, nil
	}
	return m, nil
}

// refilter rebuilds the filtered index slice from m.input. Empty input
// keeps every entry; otherwise we substring-match (case-insensitive) on
// the basename + the immediate parent directories. Matches preserve the
// original order so the most-recently-opened file stays at the top.
func (m *Model) refilter() {
	m.filtered = m.filtered[:0]
	q := strings.ToLower(strings.TrimSpace(m.input))
	for i, e := range m.entries {
		if q == "" || strings.Contains(strings.ToLower(e.Path), q) {
			m.filtered = append(m.filtered, i)
		}
	}
	m.cursor = 0
}

// View renders the centered modal at full-screen size. Used for tests and
// any caller that wants a complete frame; in production the host wraps
// Box() with modalOverlay (see app/view.go).
func (m Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	box := m.Box()
	if box == "" {
		return ""
	}
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, box)
}

// Box returns the bordered modal box only, with no surrounding whitespace.
// The host overlays it on the dimmed editor base via modalOverlay.
func (m Model) Box() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	return m.renderBox()
}

// ── Palette ────────────────────────────────────────────────────────────────
//
// PanelBg is the "glass body" surface. It MUST match the value the host's
// modalOverlay recognises (pickerPanelBg = #262626) — that's what triggers
// the per-cell dim-and-tint replacement. Other surfaces use distinct
// values so they pass through the overlay untouched and read as crisp.
var (
	PanelBg     = lipgloss.Color("#262626")
	PanelBorder = lipgloss.Color("#3a3a3a")

	// HairlineFg is the muted line that splits header / list / footer.
	HairlineFg = lipgloss.Color("#363636")

	// Input field — slightly darker than the panel so it reads as a well.
	InputBg     = lipgloss.Color("#1a1a1c")
	InputFg     = lipgloss.Color("#f2f2f2")
	InputCursor = lipgloss.Color("#bb86fc") // matches findbar.AccentFocus
	Placeholder = lipgloss.Color("#5e5e66")

	// Selected row — subtle purple tint (NOT bright cyan/blue).
	SelBg     = lipgloss.Color("#2a1f3a")
	SelDot    = lipgloss.Color("#bb86fc")
	NameFg    = lipgloss.Color("#f2f2f2")
	SelNameFg = lipgloss.Color("#ffffff")

	// Path / muted text on both selected and unselected rows.
	PathFg    = lipgloss.Color("#8c8c8c")
	SelPathFg = lipgloss.Color("#cfc0e6")

	// Header + subtitle text.
	TitleFg    = lipgloss.Color("#e6e6e6")
	HeaderIcon = lipgloss.Color("#bb86fc")

	// Pills: small chips for "20 matches" + the right-aligned time slot.
	PillBg = lipgloss.Color("#2a2a2a")
	PillFg = lipgloss.Color("#9a9a9a")

	// Footer.
	FooterFg    = lipgloss.Color("#8c8c8c")
	FooterChipBg = lipgloss.Color("#2a2a2a")
	FooterChipFg = lipgloss.Color("#cfcfcf")

	// Indicator bullet for unselected rows.
	BulletFg = lipgloss.Color("#5a5a66")
)

// layoutDims returns the modal box width and the number of result rows
// (each row is 1 cell tall — single-line entries, command-palette style).
func (m Model) layoutDims() (boxW, rowsVisible int) {
	boxW = 64
	if maxW := m.w - 4; boxW > maxW {
		boxW = maxW
	}
	if boxW < 40 {
		boxW = 40
	}

	resultCount := len(m.filtered)
	if resultCount == 0 {
		resultCount = 1
	}
	// Chrome rows inside the bordered box (border itself adds 2 more):
	//   blank(1) + header(1) + blank(1) + search(1) + hairline(1) +
	//   list… + hairline(1) + blank(1) + footer(1) + blank(1) = 9
	// Plus 2 border rows from lipgloss = 11 total chrome.
	const chromeRows = 11
	maxRows := m.h - 4 - chromeRows
	if maxRows < 3 {
		maxRows = 3
	}
	if maxRows > 12 {
		maxRows = 12
	}
	rowsVisible = resultCount
	if rowsVisible > maxRows {
		rowsVisible = maxRows
	}
	if rowsVisible < 1 {
		rowsVisible = 1
	}
	return
}

func (m Model) renderBox() string {
	boxW, rowsVisible := m.layoutDims()
	innerW := boxW - 2

	bgFill := lipgloss.NewStyle().Background(PanelBg)
	hairline := lipgloss.NewStyle().Background(PanelBg).Foreground(HairlineFg).
		Render(strings.Repeat("─", innerW))

	blank := bgFill.Render(strings.Repeat(" ", innerW))
	rows := []string{}

	// Header gets breathing rows above and below so the title doesn't look
	// pinned to the top border.
	rows = append(rows, blank)
	rows = append(rows, padPanel(m.renderHeader(innerW), innerW))
	rows = append(rows, blank)

	// Search input.
	rows = append(rows, padPanel(m.renderSearch(innerW), innerW))

	// Hairline → list → hairline.
	rows = append(rows, hairline)
	rows = append(rows, m.renderResults(innerW, rowsVisible)...)
	rows = append(rows, hairline)

	// Footer gets breathing rows above and below — same treatment as header.
	rows = append(rows, blank)
	rows = append(rows, padPanel(m.renderFooter(innerW), innerW))
	rows = append(rows, blank)

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(PanelBorder).
		BorderBackground(PanelBg).
		Background(PanelBg).
		Width(boxW)
	return border.Render(content)
}

// renderHeader: "◷ Open Recent File" on the left, " N matches " pill on
// the right. Returns the row WITHOUT trailing pad — caller pads to innerW.
func (m Model) renderHeader(innerW int) string {
	icon := lipgloss.NewStyle().Background(PanelBg).Foreground(HeaderIcon).Bold(true).Render("◷")
	title := lipgloss.NewStyle().Background(PanelBg).Foreground(TitleFg).Bold(true).Render(" Open Recent File")
	pill := renderPill(matchCountText(len(m.filtered)))

	leftPad := lipgloss.NewStyle().Background(PanelBg).Render("  ")
	rightPad := lipgloss.NewStyle().Background(PanelBg).Render("  ")

	left := leftPad + icon + title
	right := pill + rightPad
	gap := innerW - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + lipgloss.NewStyle().Background(PanelBg).Render(strings.Repeat(" ", gap)) + right
}

// renderSearch renders the search input row. Layout: 2 cols panel pad, then
// a full-width input "well" (InputBg) with placeholder/text + a purple
// blinking cursor glyph, then 2 cols panel pad.
func (m Model) renderSearch(innerW int) string {
	const sidePad = 2
	wellW := innerW - sidePad*2
	if wellW < 8 {
		wellW = innerW - 2
	}

	wellBg := lipgloss.NewStyle().Background(InputBg)
	textStyle := lipgloss.NewStyle().Background(InputBg).Foreground(InputFg)
	placeholder := lipgloss.NewStyle().Background(InputBg).Foreground(Placeholder).Italic(true)
	cursorGlyph := "▎"
	if !m.cursorVisible {
		cursorGlyph = " "
	}
	cursor := lipgloss.NewStyle().Background(InputBg).Foreground(InputCursor).Bold(true).Render(cursorGlyph)

	// Inner well: " <cursor> <text or placeholder>             "
	var inner string
	leadPad := wellBg.Render(" ")
	inner = leadPad + cursor + wellBg.Render(" ")
	if m.input == "" {
		inner += placeholder.Render("Search recent files…")
	} else {
		inner += textStyle.Render(truncRight(m.input, wellW-4))
	}
	if w := lipgloss.Width(inner); w < wellW {
		inner += wellBg.Render(strings.Repeat(" ", wellW-w))
	}
	// Wrap in a 1-cell purple-tinted left edge to act as a focus marker
	// — same colour story as the cursor itself, gives the well a clear
	// "focused" cue without an actual border-line.
	focusEdge := lipgloss.NewStyle().Background(InputBg).Foreground(InputCursor).Render("│")
	inner = focusEdge + inner

	pad := lipgloss.NewStyle().Background(PanelBg).Render(strings.Repeat(" ", sidePad))
	return pad + inner + pad
}

// renderResults emits exactly `rowsVisible` single-line entries (one cell
// tall each, command-palette style), padded with blank cells if there
// aren't enough matches. Each row spans innerW cells exactly.
func (m Model) renderResults(innerW, rowsVisible int) []string {
	out := make([]string, 0, rowsVisible)

	if len(m.filtered) == 0 {
		empty := lipgloss.NewStyle().Background(PanelBg).Foreground(PathFg).Italic(true).
			Render("    No recent files found")
		out = append(out, padPanel(empty, innerW))
		for i := 1; i < rowsVisible; i++ {
			out = append(out, lipgloss.NewStyle().Background(PanelBg).Render(strings.Repeat(" ", innerW)))
		}
		return out
	}

	start := 0
	if m.cursor >= rowsVisible {
		start = m.cursor - rowsVisible + 1
	}
	end := start + rowsVisible
	if end > len(m.filtered) {
		end = len(m.filtered)
	}
	for i := start; i < end; i++ {
		idx := m.filtered[i]
		entry := m.entries[idx]
		active := i == m.cursor
		out = append(out, m.renderRow(entry, active, innerW))
	}
	missing := rowsVisible - len(out)
	for i := 0; i < missing; i++ {
		out = append(out, lipgloss.NewStyle().Background(PanelBg).Render(strings.Repeat(" ", innerW)))
	}
	return out
}

// renderRow returns a SINGLE line per entry — bullet, file name (bold),
// dir path (muted, left-truncated), time pill (right-aligned). Active row
// gets a subtle purple-tinted bg across the full inner width.
func (m Model) renderRow(e Entry, active bool, innerW int) string {
	bg := PanelBg
	bullet := BulletFg
	nameFg := NameFg
	pathFg := PathFg
	if active {
		bg = SelBg
		bullet = SelDot
		nameFg = SelNameFg
		pathFg = SelPathFg
	}
	rowBg := lipgloss.NewStyle().Background(bg)

	name := filepath.Base(e.Path)
	dir := filepath.Dir(e.Path)
	if dir == "." {
		dir = ""
	}

	const leftPad, rightPad = 2, 2
	const bulletWidth = 1
	const nameDirGap = 2 // spaces between name and dir
	const dirPillGap = 2 // spaces between dir and pill

	bulletStyle := lipgloss.NewStyle().Background(bg).Foreground(bullet).Bold(true)
	nameStyle := lipgloss.NewStyle().Background(bg).Foreground(nameFg).Bold(true)
	pathStyle := lipgloss.NewStyle().Background(bg).Foreground(pathFg).Faint(!active)

	pill := renderTimeChip(formatRelative(e.OpenedAt, m.now), bg)
	pillW := lipgloss.Width(pill)

	leftBlock := rowBg.Render(strings.Repeat(" ", leftPad)) +
		bulletStyle.Render("•") +
		rowBg.Render(strings.Repeat(" ", bulletWidth))
	leftBlockW := lipgloss.Width(leftBlock)
	rightPadStr := rowBg.Render(strings.Repeat(" ", rightPad))

	// Budget: innerW = leftBlock + name + gap + dir + gap + pill + rightPad
	avail := innerW - leftBlockW - pillW - rightPad - nameDirGap - dirPillGap
	if avail < 12 {
		avail = innerW - leftBlockW - pillW - rightPad
	}

	// Give name up to half the available, dir takes whatever is left.
	nameMax := avail / 2
	if nameMax < 8 {
		nameMax = avail
	}
	nameTrunc := truncRight(name, nameMax)
	nameW := runewidth.StringWidth(nameTrunc)

	dirAvail := avail - nameW
	if dirAvail < 0 {
		dirAvail = 0
	}
	dirText := dir
	if dirText == "" {
		dirText = "—"
	}
	dirTrunc := truncLeft(dirText, dirAvail)
	dirW := runewidth.StringWidth(dirTrunc)

	gap := innerW - leftBlockW - nameW - nameDirGap - dirW - dirPillGap - pillW - rightPad
	if gap < 0 {
		gap = 0
	}

	row := leftBlock +
		nameStyle.Render(nameTrunc) +
		rowBg.Render(strings.Repeat(" ", nameDirGap)) +
		pathStyle.Render(dirTrunc) +
		rowBg.Render(strings.Repeat(" ", dirPillGap+gap)) +
		pill +
		rightPadStr
	return padBg(row, innerW, bg)
}

// renderFooter produces the bottom hint row with keycap chips:
// "  ↑↓ Navigate    ↵ Select    Esc Close  "
func (m Model) renderFooter(innerW int) string {
	bgFill := lipgloss.NewStyle().Background(PanelBg)
	label := lipgloss.NewStyle().Background(PanelBg).Foreground(FooterFg)
	chip := lipgloss.NewStyle().Background(FooterChipBg).Foreground(FooterChipFg).Bold(true).Padding(0, 1)

	pad := bgFill.Render("  ")
	gap := bgFill.Render("   ")
	parts := []string{
		pad,
		chip.Render("↑↓"), bgFill.Render(" "), label.Render("Navigate"),
		gap,
		chip.Render("↵"), bgFill.Render(" "), label.Render("Select"),
		gap,
		chip.Render("Esc"), bgFill.Render(" "), label.Render("Close"),
		pad,
	}
	out := strings.Join(parts, "")
	if w := lipgloss.Width(out); w > innerW {
		// Compact fallback — just the chips.
		out = pad +
			chip.Render("↑↓") + bgFill.Render(" ") +
			chip.Render("↵") + bgFill.Render(" ") +
			chip.Render("Esc") + pad
	}
	return out
}

// ── helpers ──────────────────────────────────────────────────────────────

func renderPill(text string) string {
	return lipgloss.NewStyle().
		Background(PillBg).
		Foreground(PillFg).
		Padding(0, 1).
		Render(text)
}

// renderTimeChip is the right-aligned time pill on each row. Uses the row's
// bg to "punch through" so the chip floats over selected rows correctly —
// a chip on the bright purple SelBg uses a slightly lifted bg of its own
// for separation; on the panel it just uses PillBg.
func renderTimeChip(text string, rowBg lipgloss.Color) string {
	chipBg := PillBg
	// On selected rows, lift the chip's bg one notch lighter than SelBg so
	// the time still reads as a distinct pill (not a fade-out).
	if rowBg == SelBg {
		chipBg = lipgloss.Color("#3a2a4a")
	}
	return lipgloss.NewStyle().
		Background(chipBg).
		Foreground(PillFg).
		Padding(0, 1).
		Render(text)
}

// padPanel pads the row up to width w with PanelBg-styled spaces.
func padPanel(s string, w int) string {
	used := lipgloss.Width(s)
	if used >= w {
		return s
	}
	return s + lipgloss.NewStyle().Background(PanelBg).Render(strings.Repeat(" ", w-used))
}

// padBg pads the row up to width w with the given bg color so a wider
// selection-highlighted row stays uniformly tinted across its full span.
func padBg(s string, w int, bg lipgloss.Color) string {
	used := lipgloss.Width(s)
	if used >= w {
		return s
	}
	return s + lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", w-used))
}

// truncRight clips s to max visible cells, appending "…" when truncated.
func truncRight(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= max {
		return s
	}
	return runewidth.Truncate(s, max, "…")
}

// truncLeft keeps the tail of s and prepends "…" when too wide. Path-style
// truncation: the basename (rightmost segment) stays visible.
func truncLeft(s string, max int) string {
	if max <= 0 {
		return ""
	}
	w := runewidth.StringWidth(s)
	if w <= max {
		return s
	}
	r := []rune(s)
	target := max - 1
	keep := r
	for runewidth.StringWidth(string(keep)) > target && len(keep) > 0 {
		keep = keep[1:]
	}
	return "…" + string(keep)
}

// matchCountText returns the right-side header pill copy.
func matchCountText(n int) string {
	switch n {
	case 0:
		return "no matches"
	case 1:
		return "1 match"
	default:
		return itoa(n) + " matches"
	}
}

// formatRelative renders a short "Nh ago" / "Nd ago" / "Jan 2" string,
// matching the existing formatRecentTime convention used elsewhere in the
// app. Duplicated here to avoid an import cycle into the host app package.
func formatRelative(t, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		return "just now"
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return itoa(int(d.Hours())) + "h ago"
	case d < 7*24*time.Hour:
		return itoa(int(d.Hours()/24)) + "d ago"
	default:
		return t.Format("Jan 2")
	}
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// isMouseFragmentEvent / isMouseCodeRunes mirror the heuristics in the
// generic picker — keeps stray mouse-tracking SGR fragments out of the
// search input field. See picker/model.go for the full rationale.
func isMouseFragmentEvent(runes []rune) bool {
	if len(runes) < 2 {
		return false
	}
	first := runes[0]
	if first != '[' && first != '<' && first != ';' {
		return false
	}
	for _, r := range runes {
		switch {
		case r >= '0' && r <= '9':
		case r == ';' || r == '<' || r == '[' || r == 'M' || r == 'm':
		default:
			return false
		}
	}
	return true
}

func isMouseCodeRunes(runes []rune) bool {
	if len(runes) == 0 {
		return false
	}
	for _, r := range runes {
		switch {
		case r >= '0' && r <= '9':
		case r == ';' || r == '<' || r == '[' || r == 'M' || r == 'm':
		default:
			return false
		}
	}
	return true
}
