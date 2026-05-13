package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

// SettingsSelectMsg is emitted when the user activates a settings row in the
// dedicated settings modal. The ID matches settingsRow.ID (e.g.
// "setting-theme") so handlePromptSubmit can reuse the existing row apply
// logic via promptKindSetting.
type SettingsSelectMsg struct {
	ID string
}

// SettingsCloseMsg dismisses the settings modal without selecting anything.
type SettingsCloseMsg struct{}

// settingsCategoryRow groups one or more settingsRow entries under a
// human-readable category heading (e.g. "Appearance", "Editor"). The order
// of rows is preserved within each category so the rendered modal mirrors
// the structure declared in settingsRows.
type settingsCategoryRow struct {
	Category string
	Row      settingsRow
}

// settingsModalRow is one entry in the modal's flat navigable list. Either
// it is a header (Header == true, just the category label, not selectable)
// or a setting row (Header == false, selectable).
type settingsModalRow struct {
	Header   bool
	Category string
	Row      settingsRow
	Value    string // current value for non-header rows
}

// settingsModal is the dedicated render+input component for the Settings
// overlay. It only knows about settings rows — nothing about the rest of
// the picker ecosystem. The caller wires SettingsSelectMsg back into the
// existing prompt flow (onSettingsRowSelected → applySettingValue).
type settingsModal struct {
	rows   []settingsModalRow
	cursor int    // index into rows (always points at a visible non-header row)
	input  string // search filter
	w, h   int
}

// newSettingsModal builds the ordered, grouped row list from settingsRows.
// Categories come from the static map below; any row without an explicit
// mapping falls into "Editor". Cursor starts on the first selectable row.
func newSettingsModal() settingsModal {
	cats := settingsCategories()

	// Group rows by category, preserving the canonical settingsRows order.
	type bucket struct {
		name string
		rows []settingsRow
	}
	var buckets []bucket
	idx := map[string]int{}
	for _, r := range settingsRows {
		c, ok := cats[r.ID]
		if !ok {
			c = "Editor"
		}
		i, seen := idx[c]
		if !seen {
			idx[c] = len(buckets)
			buckets = append(buckets, bucket{name: c, rows: []settingsRow{r}})
		} else {
			buckets[i].rows = append(buckets[i].rows, r)
		}
	}

	cfg := loadSettings()
	var rows []settingsModalRow
	for _, b := range buckets {
		rows = append(rows, settingsModalRow{Header: true, Category: b.name})
		for _, r := range b.rows {
			rows = append(rows, settingsModalRow{
				Category: b.name,
				Row:      r,
				Value:    currentSettingValue(r, cfg),
			})
		}
	}

	m := settingsModal{rows: rows}
	m.cursor = m.firstSelectable(0, +1)
	return m
}

// settingsCategories maps settingsRow.ID → category label. Centralised here
// (rather than as a Category field on settingsRow) so the canonical
// settingsRows declaration stays untouched and unrelated callers don't need
// to learn about UI grouping.
func settingsCategories() map[string]string {
	return map[string]string{
		"setting-theme":       "Appearance",
		"setting-font-delta":  "Appearance",
		"setting-auto-save":   "Editor",
		"setting-word-wrap":   "Editor",
		"setting-show-hidden": "Editor",
		"setting-tab-size":    "Editor",
	}
}

// SetSize remembers the screen size so Box() can compute its layout.
func (m *settingsModal) SetSize(w, h int) { m.w, m.h = w, h }

// Update consumes key events. Up/Down move the cursor (skipping headers
// and rows hidden by the filter), Enter emits SettingsSelectMsg, Esc/Ctrl-C
// emit SettingsCloseMsg. Printable runes / Backspace / Space edit the
// search filter; the cursor is reseated onto the first visible match.
func (m settingsModal) Update(msg tea.Msg) (settingsModal, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		return m, func() tea.Msg { return SettingsCloseMsg{} }
	case tea.KeyEnter:
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			r := m.rows[m.cursor]
			if !r.Header {
				id := r.Row.ID
				return m, func() tea.Msg { return SettingsSelectMsg{ID: id} }
			}
		}
		return m, nil
	case tea.KeyUp, tea.KeyCtrlP:
		if next := m.prevVisible(m.cursor); next >= 0 {
			m.cursor = next
		}
		return m, nil
	case tea.KeyDown, tea.KeyCtrlN:
		if next := m.nextVisible(m.cursor); next >= 0 {
			m.cursor = next
		}
		return m, nil
	case tea.KeyBackspace:
		if r := []rune(m.input); len(r) > 0 {
			m.input = string(r[:len(r)-1])
			m.refitCursor()
		}
		return m, nil
	case tea.KeySpace:
		m.input += " "
		m.refitCursor()
		return m, nil
	case tea.KeyRunes:
		if isSettingsMouseFragment(k.Runes) {
			return m, nil
		}
		m.input += sanitizeSettingsInput(string(k.Runes))
		m.refitCursor()
		return m, nil
	}
	return m, nil
}

// HandleMouse processes a mouse event delivered with absolute screen
// coordinates. Wheel scrolls the cursor; left-click on a visible result
// row selects it; click outside the box closes the modal.
func (m settingsModal) HandleMouse(x, y int, action tea.MouseAction, button tea.MouseButton) (settingsModal, tea.Cmd) {
	if m.w <= 0 || m.h <= 0 {
		return m, nil
	}
	boxW := 56
	if maxW := m.w - 4; boxW > maxW {
		boxW = maxW
	}
	if boxW < 36 {
		boxW = 36
	}
	visible := m.visibleRows()
	innerH := m.layoutInnerHeight(len(visible))
	boxH := innerH + 2 // border top + bottom
	boxLeft := (m.w - boxW) / 2
	boxTop := (m.h - boxH) / 2
	if boxLeft < 0 {
		boxLeft = 0
	}
	if boxTop < 0 {
		boxTop = 0
	}

	if button == tea.MouseButtonWheelUp {
		if next := m.prevVisible(m.cursor); next >= 0 {
			m.cursor = next
		}
		return m, nil
	}
	if button == tea.MouseButtonWheelDown {
		if next := m.nextVisible(m.cursor); next >= 0 {
			m.cursor = next
		}
		return m, nil
	}
	if button != tea.MouseButtonLeft || action != tea.MouseActionPress {
		return m, nil
	}

	// Click outside the box → close.
	if x < boxLeft || x >= boxLeft+boxW || y < boxTop || y >= boxTop+boxH {
		return m, func() tea.Msg { return SettingsCloseMsg{} }
	}
	yRel := y - boxTop - 1 // -1 for top border
	if yRel < 0 || yRel >= innerH {
		return m, nil
	}
	// Find which m.rows index sits at this rendered y, if any.
	idx := m.layoutRowAt(yRel, visible)
	if idx < 0 || idx >= len(m.rows) || m.rows[idx].Header {
		return m, nil
	}
	m.cursor = idx
	id := m.rows[idx].Row.ID
	return m, func() tea.Msg { return SettingsSelectMsg{ID: id} }
}

// refitCursor moves the cursor onto the first visible non-header row, or
// leaves it alone if none exist (no matches).
func (m *settingsModal) refitCursor() {
	visible := m.visibleRows()
	for _, idx := range visible {
		if !m.rows[idx].Header {
			m.cursor = idx
			return
		}
	}
}

// nextVisible returns the index of the next visible non-header row after
// `from`, or -1 if there isn't one.
func (m settingsModal) nextVisible(from int) int {
	visible := m.visibleRows()
	seen := false
	for _, idx := range visible {
		if seen && !m.rows[idx].Header {
			return idx
		}
		if idx == from {
			seen = true
		}
	}
	return -1
}

// prevVisible returns the index of the previous visible non-header row
// before `from`, or -1 if there isn't one.
func (m settingsModal) prevVisible(from int) int {
	visible := m.visibleRows()
	prev := -1
	for _, idx := range visible {
		if idx == from {
			return prev
		}
		if !m.rows[idx].Header {
			prev = idx
		}
	}
	return -1
}

// visibleRows returns the indices of m.rows that should appear under the
// current filter. Headers are included only when they have ≥1 matching
// child — empty categories are hidden.
func (m settingsModal) visibleRows() []int {
	if m.input == "" {
		out := make([]int, len(m.rows))
		for i := range m.rows {
			out[i] = i
		}
		return out
	}
	needle := strings.ToLower(m.input)
	var out []int
	pendingHdr := -1
	for i, r := range m.rows {
		if r.Header {
			pendingHdr = i
			continue
		}
		hay := strings.ToLower(r.Row.Label + " " + r.Value)
		if strings.Contains(hay, needle) {
			if pendingHdr >= 0 {
				out = append(out, pendingHdr)
				pendingHdr = -1
			}
			out = append(out, i)
		}
	}
	return out
}

// layoutInnerHeight returns the inner row count (excluding the outer
// rounded-border) — must mirror exactly what Box() emits so HandleMouse
// can map y → row.
func (m settingsModal) layoutInnerHeight(visibleCount int) int {
	// 1 header + 1 top sep + N visible rows (incl categories + items) +
	// blank rows between non-first categories + spacer + 1 bottom sep + 1 footer.
	// Count categories beyond the first to know breathing-row count.
	visible := m.visibleRows()
	hdrSeen := 0
	breathingRows := 0
	for _, idx := range visible {
		if m.rows[idx].Header {
			hdrSeen++
			if hdrSeen > 1 {
				breathingRows++
			}
		}
	}
	contentRows := 1 /*header*/ + 1 /*top sep*/ + 1 /*input*/ + visibleCount + breathingRows
	// Spacer flexes; the math has to match Box()'s spacer calc exactly.
	desiredH := m.h * 80 / 100
	footerSpacer := desiredH - contentRows - 3
	if footerSpacer < 3 {
		footerSpacer = 3
	}
	if footerSpacer > 12 {
		footerSpacer = 12
	}
	return contentRows + footerSpacer + 1 /*bottom sep*/ + 1 /*footer*/
}

// layoutRowAt translates a rendered inner-y back to an m.rows index. Y=0 is
// the header row; non-clickable rows return -1.
func (m settingsModal) layoutRowAt(yRel int, visible []int) int {
	// Layout (must mirror Box()):
	//   y=0       header
	//   y=1       top separator (dead)
	//   y=2       search input (dead)
	//   y>=3      content rows starting here, with category breathing
	if yRel < 3 {
		return -1
	}
	cy := 3
	hdrSeen := 0
	for _, idx := range visible {
		if m.rows[idx].Header {
			hdrSeen++
			if hdrSeen > 1 {
				if cy == yRel {
					return -1
				}
				cy++
			}
		}
		if cy == yRel {
			return idx
		}
		cy++
	}
	return -1
}

// isSettingsMouseFragment is the picker/prompt-style guard: drop runes
// events whose payload looks like a fragment of an SGR mouse-tracking
// sequence (e.g. "[<35", ";70", "M") so they don't pollute the filter.
func isSettingsMouseFragment(runes []rune) bool {
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

// sanitizeSettingsInput strips control bytes and full / bare CSI escapes
// from a single-event runes payload.
func sanitizeSettingsInput(s string) string {
	var b []byte
	i := 0
	for i < len(s) {
		c := s[i]
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		if c == '[' && i+1 < len(s) && s[i+1] == '<' {
			j := i + 2
			ok := true
			for j < len(s) && s[j] != 'M' && s[j] != 'm' {
				if (s[j] < '0' || s[j] > '9') && s[j] != ';' {
					ok = false
					break
				}
				j++
			}
			if ok && j < len(s) {
				i = j + 1
				continue
			}
		}
		if c < 0x20 || c == 0x7f {
			i++
			continue
		}
		b = append(b, c)
		i++
	}
	return string(b)
}

// firstSelectable scans `rows` from `start` in direction `step` and returns
// the index of the first non-header row, or -1 if none exists. Used for both
// initial cursor placement and arrow-key skipping over category headers.
func (m settingsModal) firstSelectable(start, step int) int {
	for i := start; i >= 0 && i < len(m.rows); i += step {
		if !m.rows[i].Header {
			return i
		}
	}
	return -1
}

// settings modal palette — chosen to feel cohesive with modalOverlay's soft
// blue tinting. We don't use lipgloss.Color theme constants here because the
// modalOverlay re-tints picker bg colours by exact RGB match; reusing the
// project's "soft selected" RGB lets a future overlay tweak land in one
// place. (See modalOverlay's softSelectedBg / softTitleBg.)
var (
	smBoxBg      = lipgloss.Color("#262626")
	smBorderBlue = lipgloss.Color("#3a3a3a") // thin dark-grey rim
	smIconBlue   = lipgloss.Color("#569cd6") // header icon keeps the blue accent
	smTextPri    = lipgloss.Color("#d0d0d0")
	smTextSec    = lipgloss.Color("#9090a0") // muted labels / subtitle
	smTextDim    = lipgloss.Color("#6c6c6c") // separators, footer hints
	smCategoryFg = lipgloss.Color("#cce6f4") // [ Appearance ] tint
	smSelBg      = lipgloss.Color("#1f4f63") // soft selection bar (matches softSelectedBg)
	smValueFg    = lipgloss.Color("#e6e6e6")
	// smGuideColor matches the editor's indent guide colour + weight
	// (`│` painted in #363636 — see theme_lua.go::c.indent). Using the
	// same colour for the modal's hairline separators ties them visually
	// to the rest of the editor's "secondary scaffolding" lines.
	smGuideColor = lipgloss.Color("#363636")
	// smSubtitleFg is a darker, more faded shade than smTextSec — gives
	// the modal subtitle a "muted byline" feel without competing with
	// the title for attention.
	smSubtitleFg = lipgloss.Color("#5a5a66")
	smInputBg    = lipgloss.Color("#1f3447") // search field bg (matches picker input)
	smInputFg    = lipgloss.Color("#ffffff")
)

// Box renders the structured Settings modal: header (icon + title +
// subtitle), separator, grouped rows with [ Category ] labels and aligned
// label/value pairs, separator, footer hint row. Width is fixed in a
// reasonable band so very wide terminals don't blow out the layout; height
// auto-fits to the row count.
func (m settingsModal) Box() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	// Width: comfortable for label+value but bounded so the modal stays
	// visually compact. Min keeps it usable on narrow terminals; max keeps
	// it from looking adrift on ultrawide.
	boxW := 56
	if maxW := m.w - 4; boxW > maxW {
		boxW = maxW
	}
	if boxW < 36 {
		boxW = 36
	}
	innerW := boxW - 2 // discount left/right border cells

	bgFill := lipgloss.NewStyle().Background(smBoxBg)
	inputBgFill := lipgloss.NewStyle().Background(smInputBg)
	inputStyle := lipgloss.NewStyle().Background(smInputBg).Foreground(smInputFg)
	inputCursorStyle := lipgloss.NewStyle().Background(smInputBg).Foreground(smInputFg).Underline(true)
	headerIcon := lipgloss.NewStyle().Background(smBoxBg).Foreground(smIconBlue).Bold(true).Render("⚙")
	headerTitle := lipgloss.NewStyle().Background(smBoxBg).Foreground(smTextPri).Bold(true).Render(" Settings")
	categoryStyle := lipgloss.NewStyle().Background(smBoxBg).Foreground(smCategoryFg).Bold(true)
	labelStyle := lipgloss.NewStyle().Background(smBoxBg).Foreground(smTextSec)
	valueStyle := lipgloss.NewStyle().Background(smBoxBg).Foreground(smValueFg)
	selRowBg := lipgloss.NewStyle().Background(smSelBg)
	selLabel := lipgloss.NewStyle().Background(smSelBg).Foreground(lipgloss.Color("#cce6f4"))
	selValue := lipgloss.NewStyle().Background(smSelBg).Foreground(lipgloss.Color("#ffffff")).Bold(true)
	selChevron := lipgloss.NewStyle().Background(smSelBg).Foreground(lipgloss.Color("#ffffff")).Bold(true)
	separatorStyle := lipgloss.NewStyle().Background(smBoxBg).Foreground(smGuideColor)
	footerStyle := lipgloss.NewStyle().Background(smBoxBg).Foreground(smTextDim)
	footerSepStyle := lipgloss.NewStyle().Background(smBoxBg).Foreground(smGuideColor)

	// Pad helper: render `s` and right-pad with bg-coloured spaces to width w.
	padBg := func(s string, w int) string {
		used := lipgloss.Width(s)
		if used >= w {
			return s
		}
		return s + bgFill.Render(strings.Repeat(" ", w-used))
	}

	blank := bgFill.Render(strings.Repeat(" ", innerW))

	var rows []string

	// ── Header ────────────────────────────────────────────────────────
	// Title on the left; right side just bg-fills the row so the modal
	// header keeps its full width.
	left := bgFill.Render("  ") + headerIcon + headerTitle
	rows = append(rows, padBg(left, innerW))

	// ── Top separator ────────────────────────────────────────────────
	// Ultra-thin 1/8 hairline (matches the footer separator below).
	rows = append(rows, separatorStyle.Render(strings.Repeat("─", innerW)))

	// ── Search input row ─────────────────────────────────────────────
	// (Reserved as the first row of the visible content so HandleMouse
	// can map clicks correctly — but we render it INLINE with the
	// header to keep the layout tight; the input visually replaces the
	// would-be padding row above content.)
	// NOTE: layoutRowAt() expects content to start at yRel=2 — that
	// matches "header(1) + top-sep(1) + input(1)" since this input row
	// is now the FIRST content row.
	inputContent := inputBgFill.Render(" › ") + inputStyle.Render(m.input) + inputCursorStyle.Render("▎")
	if w := lipgloss.Width(inputContent); w < innerW {
		inputContent += inputBgFill.Render(strings.Repeat(" ", innerW-w))
	}
	rows = append(rows, inputContent)

	// ── Grouped rows ─────────────────────────────────────────────────
	const labelW = 14
	prevCategory := ""
	visible := m.visibleRows()
	for _, i := range visible {
		r := m.rows[i]
		if r.Header {
			if prevCategory != "" {
				rows = append(rows, blank)
			}
			label := categoryStyle.Render("[ " + r.Category + " ]")
			rows = append(rows, padBg(bgFill.Render("  ")+label, innerW))
			prevCategory = r.Category
			continue
		}

		// Selected row gets the soft highlight bar with a leading chevron;
		// unselected rows get the normal modal bg.
		selected := i == m.cursor

		labelText := r.Row.Label
		valueText := truncateMiddle(r.Value, innerW-labelW-8)

		if selected {
			// Layout (selected): "▶ <label padded to labelW>  <value>"
			// Whole row painted with selBg so it reads as one bar.
			lbl := selLabel.Render(padRight(labelText, labelW))
			val := selValue.Render(valueText)
			content := selChevron.Render(" ▶ ") + lbl + selRowBg.Render("  ") + val
			rows = append(rows, padSelToWidth(content, innerW, smSelBg))
		} else {
			lbl := labelStyle.Render(padRight(labelText, labelW))
			val := valueStyle.Render(valueText)
			content := bgFill.Render("    ") + lbl + bgFill.Render("  ") + val
			rows = append(rows, padBg(content, innerW))
		}
	}

	// ── Spacer + Footer ──────────────────────────────────────────────
	// Push the footer down with breathing room above it. Spacer count
	// flexes with terminal height so the footer always sits near the
	// bottom border on tall terminals while staying compact on short
	// ones — minimum 3 rows of gap, ideally fills until 80% of m.h.
	desiredH := m.h * 80 / 100
	contentH := len(rows)
	footerSpacer := desiredH - contentH - 3 // 3 = footer + 2 border
	if footerSpacer < 3 {
		footerSpacer = 3
	}
	if footerSpacer > 12 {
		footerSpacer = 12
	}
	for i := 0; i < footerSpacer; i++ {
		rows = append(rows, blank)
	}
	// Ultra-thin 1/8 separator line above the footer. `▁` paints just
	// the bottom 1/8 of its cell, so a row of these reads as a hairline
	// directly above the next row (= the footer).
	sepRow := footerSepStyle.Render(strings.Repeat("─", innerW))
	rows = append(rows, sepRow)

	footer := footerStyle.Render("↑↓ Navigate    ↵ Select    Esc Close")
	rows = append(rows, padBg(bgFill.Render("  ")+footer, innerW))

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	// Height intentionally NOT set — lipgloss auto-fits the border to the
	// content row count. Setting Height(N) makes lipgloss pad the inner
	// content area to N rows, which inserts blank rows BELOW the footer
	// before the bottom border line — visible as a gap between the
	// footer hints and the form's bottom edge.
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(smBorderBlue).
		BorderBackground(smBoxBg).
		Background(smBoxBg).
		Width(boxW)
	return border.Render(content)
}

// padRight pads s with trailing spaces so its visible width is exactly w.
// If s is wider it's truncated with an ellipsis (single rune).
func padRight(s string, w int) string {
	used := runewidth.StringWidth(s)
	if used == w {
		return s
	}
	if used > w {
		return runewidth.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", w-used)
}

// padSelToWidth pads `s` (already styled) with selection-bg spaces up to
// width w. Used so the soft selection bar extends across the whole inner
// row, not just the label+value text.
func padSelToWidth(s string, w int, bg lipgloss.Color) string {
	used := lipgloss.Width(s)
	if used >= w {
		return s
	}
	return s + lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", w-used))
}

// truncateMiddle clips a string to maxW visible cells. We choose the simple
// "trailing ellipsis" form because settings values (theme names, ints, bools)
// are most informative at their start.
func truncateMiddle(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= maxW {
		return s
	}
	return runewidth.Truncate(s, maxW, "…")
}
