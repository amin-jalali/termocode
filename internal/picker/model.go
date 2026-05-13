package picker

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// Item is one row in the picker. Most items are selectable; rows where
// Header is true render as a non-selectable section heading and are
// skipped during keyboard navigation / Enter-to-select. Group is an
// opaque key callers can use to bucket items so the filter can reinject
// the right header when matches narrow down the visible list.
type Item struct {
	ID     string
	Title  string
	Hint   string
	Header bool   // true → non-selectable section heading row
	Group  string // group key (e.g. "Quick Fix"); shared by header + items
}

// SelectMsg is emitted when the user activates a row.
type SelectMsg struct {
	ID    string
	Title string
}

// CloseMsg is emitted when the user dismisses the picker.
type CloseMsg struct{}

type itemsLoadedMsg struct{ items []Item }

// Loader is an optional async data source.
type Loader func() []Item

type Model struct {
	title   string
	items   []Item
	cursor  int
	input   string
	matches fuzzy.Matches
	w, h    int
	loader  Loader
	loading bool
}

// NewWith returns a picker with a provided loader. The loader runs on Init.
func NewWith(title string, loader Loader) Model {
	return Model{title: title, loader: loader, loading: true}
}

// NewItems returns a picker pre-populated with items.
func NewItems(title string, items []Item) Model {
	m := Model{title: title, items: items}
	m.refilter()
	return m
}

// New is kept for backward compatibility — it builds a file picker that
// walks cwd.
func New() Model {
	return NewWith(" Go to file ", filesLoader)
}

func (m Model) Init() tea.Cmd {
	if m.loader == nil {
		return nil
	}
	loader := m.loader
	return func() tea.Msg { return itemsLoadedMsg{items: loader()} }
}

func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case itemsLoadedMsg:
		m.loading = false
		m.items = msg.items
		m.refilter()
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			return m, func() tea.Msg { return CloseMsg{} }
		case tea.KeyEnter:
			if m.cursor >= 0 && m.cursor < len(m.matches) {
				idx := m.matches[m.cursor].Index
				if idx >= 0 && idx < len(m.items) {
					it := m.items[idx]
					if it.Header {
						return m, nil // header rows are non-selectable
					}
					return m, func() tea.Msg { return SelectMsg{ID: it.ID, Title: it.Title} }
				}
			}
			return m, nil
		case tea.KeyUp, tea.KeyCtrlP:
			m.cursor = m.nextSelectable(m.cursor, -1)
			return m, nil
		case tea.KeyDown, tea.KeyCtrlN:
			m.cursor = m.nextSelectable(m.cursor, +1)
			return m, nil
		case tea.KeyBackspace:
			if len(m.input) > 0 {
				r := []rune(m.input)
				m.input = string(r[:len(r)-1])
				m.refilter()
			}
			return m, nil
		case tea.KeySpace:
			m.input += " "
			m.refilter()
			return m, nil
		case tea.KeyRunes:
			// Bubble Tea sometimes delivers a single mouse-tracking SGR
			// (e.g. "[<35;70;19M") across MULTIPLE KeyRunes events — `[`,
			// `<`, `35`, `;70`, `;19M`. Earlier we ran sanitizeInput over
			// the accumulated buffer, but until the trailing `M` arrived
			// the partial fragment would render and then disappear,
			// flickering on every mouse move. Drop fragment-shaped events
			// up front so nothing transient ever reaches m.input. A real
			// keystroke is almost always a single rune; multi-rune events
			// that start with `[` or `<` and consist solely of mouse-code
			// chars are virtually certain to be a split SGR fragment.
			//
			// EXTRA: Bubble Tea's parser, when it can't recognise an
			// SGR-mouse sequence (e.g. when the trailing `M` lands in the
			// next read), strips the leading ESC and treats the `[` as a
			// rune with Alt=true. That arrives as a SINGLE-rune `[` with
			// Alt set — too short for the multi-rune heuristic. Drop any
			// Alt+mouse-code-rune-set event so the bare `[` / `<` doesn't
			// leak into the search field as the user crosses over the
			// styled input row of an open modal.
			if isMouseFragmentEvent(msg.Runes) {
				return m, nil
			}
			if msg.Alt && isMouseCodeRunes(msg.Runes) {
				return m, nil
			}
			m.input = sanitizeInput(m.input + string(msg.Runes))
			m.refilter()
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) refilter() {
	if m.input == "" {
		// Empty input: every item passes through in its original order
		// (headers included; navigation/select-handlers skip headers via
		// the isSelectable / nextSelectable helpers below).
		m.matches = make(fuzzy.Matches, 0, len(m.items))
		for i, it := range m.items {
			m.matches = append(m.matches, fuzzy.Match{Str: it.Title, Index: i})
		}
		m.cursor = 0
		m.cursor = m.firstSelectable(m.cursor)
		return
	}
	// Non-empty input: fuzzy-match titles of non-header items only. Group
	// headers re-appear before each surviving item from their group so the
	// section labels still bracket the filtered list. Headers whose group
	// has no survivors stay hidden.
	titles := make([]string, 0, len(m.items))
	indexMap := make([]int, 0, len(m.items)) // titles[k] → m.items[indexMap[k]]
	for i, it := range m.items {
		if it.Header {
			continue
		}
		titles = append(titles, it.Title)
		indexMap = append(indexMap, i)
	}
	hits := fuzzy.Find(m.input, titles)
	// Find the most recent header index that precedes each item.
	// Walk hits in their original item order so we can detect group
	// boundaries and inject a header at the start of each new group.
	out := make(fuzzy.Matches, 0, len(hits)+8)
	seenGroups := make(map[string]bool)
	// hits aren't necessarily in input-order; sort by underlying item index
	// so injected headers land in the same stable order as the unfiltered list.
	type indexedHit struct {
		match    fuzzy.Match
		itemIdx  int
	}
	ord := make([]indexedHit, 0, len(hits))
	for _, h := range hits {
		ord = append(ord, indexedHit{match: h, itemIdx: indexMap[h.Index]})
	}
	// stable sort by itemIdx
	for i := 1; i < len(ord); i++ {
		for j := i; j > 0 && ord[j-1].itemIdx > ord[j].itemIdx; j-- {
			ord[j], ord[j-1] = ord[j-1], ord[j]
		}
	}
	for _, h := range ord {
		it := m.items[h.itemIdx]
		if it.Group != "" && !seenGroups[it.Group] {
			// Find the header that owns this group (first Header item
			// with the same Group key) and inject it before this item.
			for hi, hh := range m.items {
				if hh.Header && hh.Group == it.Group {
					out = append(out, fuzzy.Match{Str: hh.Title, Index: hi})
					break
				}
			}
			seenGroups[it.Group] = true
		}
		// Rewrite the match to point to the underlying items index (not
		// the filtered titles index) so renderResultRow + cursor logic
		// keep working.
		out = append(out, fuzzy.Match{
			Str:            h.match.Str,
			Index:          h.itemIdx,
			MatchedIndexes: h.match.MatchedIndexes,
		})
	}
	m.matches = out
	m.cursor = m.firstSelectable(0)
}

// firstSelectable returns the index of the first non-header match starting
// at `from` (inclusive). Falls back to `from` when the entire match list is
// headers (shouldn't happen — non-empty matches always include at least one
// real item — but the fallback keeps cursor in valid range).
func (m Model) firstSelectable(from int) int {
	if from < 0 {
		from = 0
	}
	for i := from; i < len(m.matches); i++ {
		if !m.matchIsHeader(i) {
			return i
		}
	}
	for i := from - 1; i >= 0; i-- {
		if !m.matchIsHeader(i) {
			return i
		}
	}
	return from
}

// matchIsHeader reports whether the match at `i` points to a header item.
func (m Model) matchIsHeader(i int) bool {
	if i < 0 || i >= len(m.matches) {
		return false
	}
	idx := m.matches[i].Index
	if idx < 0 || idx >= len(m.items) {
		return false
	}
	return m.items[idx].Header
}

// nextSelectable advances past consecutive header rows in the given
// direction. dir = +1 / -1. Returns the input cursor unchanged when no
// selectable row exists in that direction.
func (m Model) nextSelectable(cursor, dir int) int {
	if dir == 0 || len(m.matches) == 0 {
		return cursor
	}
	for i := cursor + dir; i >= 0 && i < len(m.matches); i += dir {
		if !m.matchIsHeader(i) {
			return i
		}
	}
	return cursor
}

// HandleMouse maps an absolute mouse event to picker semantics. The geometry
// must mirror Box() exactly so a click on a visible row resolves to the
// correct match index.
//
// Layout inside the bordered box (rows are 0-indexed within the inner area):
//
//	y=0  header (title + subtitle)
//	y=1  top hairline separator
//	y=2  search input row
//	y=3  result row 0
//	...  result row N
//	     spacer rows
//	     bottom hairline
//	     footer hint row
//
// Wheel events scroll the cursor by 3 (browser-style). Left-click on a
// result row both moves the cursor and emits SelectMsg (VSCode-style
// single-click select). Click outside the box closes the picker.
func (m Model) HandleMouse(x, y int, action tea.MouseAction, button tea.MouseButton) (Model, tea.Cmd) {
	if m.w <= 0 || m.h <= 0 {
		return m, nil
	}
	if button == tea.MouseButtonWheelUp {
		m.cursor -= 3
		if m.cursor < 0 {
			m.cursor = 0
		}
		// Avoid landing on a non-selectable header row.
		if m.matchIsHeader(m.cursor) {
			m.cursor = m.firstSelectable(m.cursor)
		}
		return m, nil
	}
	if button == tea.MouseButtonWheelDown {
		m.cursor += 3
		if m.cursor >= len(m.matches) {
			m.cursor = len(m.matches) - 1
			if m.cursor < 0 {
				m.cursor = 0
			}
		}
		if m.matchIsHeader(m.cursor) {
			m.cursor = m.firstSelectable(m.cursor)
		}
		return m, nil
	}
	if button != tea.MouseButtonLeft || action != tea.MouseActionPress {
		return m, nil
	}

	boxW, boxH, available := m.layoutDims()
	boxLeft := (m.w - boxW) / 2
	boxTop := (m.h - boxH) / 2
	if boxLeft < 0 {
		boxLeft = 0
	}
	if boxTop < 0 {
		boxTop = 0
	}

	// Click outside the box → close.
	if x < boxLeft || x >= boxLeft+boxW || y < boxTop || y >= boxTop+boxH {
		return m, func() tea.Msg { return CloseMsg{} }
	}

	// yRel is the index into the visible result slice (0-based).
	// Inside box: y=0 border, y=1 header, y=2 hairline, y=3 input,
	// y=4 result row 0, ... so subtract boxTop+1+3 = boxTop+4.
	yRel := y - boxTop - 4
	if yRel < 0 || yRel >= available {
		return m, nil
	}
	start := 0
	if m.cursor >= available {
		start = m.cursor - available + 1
	}
	idx := start + yRel
	if idx < 0 || idx >= len(m.matches) {
		return m, nil
	}
	match := m.matches[idx]
	if match.Index < 0 || match.Index >= len(m.items) {
		return m, nil
	}
	it := m.items[match.Index]
	if it.Header {
		// Click on a section header: move the keyboard cursor to the
		// next selectable row and don't fire a SelectMsg.
		m.cursor = m.nextSelectable(idx, +1)
		return m, nil
	}
	m.cursor = idx
	id, title := it.ID, it.Title
	return m, func() tea.Msg { return SelectMsg{ID: id, Title: title} }
}

// View renders the picker as a centered box at full screen size. Used for
// standalone tests and any caller that needs a raw frame; the app wraps
// Box() with modalOverlay for the production look.
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

// Box returns just the picker's bordered box (no surrounding whitespace
// fill). The caller (view.go) overlays it with modalOverlay onto the
// dimmed editor base.
func (m Model) Box() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	return m.renderBox()
}

// ── Palette (mirrors settings_modal.go so all overlays share one design) ──
//
// We intentionally keep the panel bg at #262626, input bg at #1f3447, and
// selection bg at #094771 — modalOverlay matches those exact RGBs to
// re-tint them into the soft-glass look (selection becomes #1f4f63, input
// becomes #1d2632). Direct callers that don't go through modalOverlay see
// the raw VSCode-blue palette, which is the intended fallback.
var (
	pmBoxBg      = lipgloss.Color("#262626")
	pmBorder     = lipgloss.Color("#3a3a3a")
	pmIconBlue   = lipgloss.Color("#569cd6")
	pmTextPri    = lipgloss.Color("#d0d0d0")
	pmTextSec    = lipgloss.Color("#9090a0")
	pmTextDim    = lipgloss.Color("#6c6c6c")
	pmGuide      = lipgloss.Color("#363636")
	pmSubtitleFg = lipgloss.Color("#5a5a66")
	pmInputBg    = lipgloss.Color("#1f3447")
	pmInputFg    = lipgloss.Color("#ffffff")
	pmSelBg      = lipgloss.Color("#094771")
	pmSelFg      = lipgloss.Color("#ffffff")
	pmSelDirFg   = lipgloss.Color("#cce6f4")
	pmDirFg      = lipgloss.Color("#6c8a9c")
	pmHintFg     = lipgloss.Color("#7a7a7a")
	pmSelHintFg  = lipgloss.Color("#cce6f4")
	pmMatchFg    = lipgloss.Color("#ffd700")
)

// layoutDims returns the modal box width, total box height, and the number
// of result rows available between input and footer. The math here MUST be
// kept in sync with renderBox() so HandleMouse maps clicks correctly.
//
// Inner row stack (count):
//
//	1  header
//	1  top hairline
//	1  input
//	N  result rows  ← `available`
//	S  spacer (3..12)
//	1  bottom hairline
//	1  footer
//
// boxH = innerH + 2 (top + bottom border).
func (m Model) layoutDims() (boxW, boxH, available int) {
	boxW = 56
	if maxW := m.w - 4; boxW > maxW {
		boxW = maxW
	}
	if boxW < 36 {
		boxW = 36
	}

	resultCount := len(m.matches)
	if m.loading || resultCount == 0 {
		resultCount = 1 // "loading…" / "no results" placeholder row
	}
	// Cap visible rows at a sensible band so the modal stays compact.
	maxRows := m.h - 12 // leave room for header+input+seps+footer+spacer+border
	if maxRows < 1 {
		maxRows = 1
	}
	if maxRows > 18 {
		maxRows = 18
	}
	available = resultCount
	if available > maxRows {
		available = maxRows
	}
	if available < 1 {
		available = 1
	}

	contentRows := 1 /*header*/ + 1 /*top sep*/ + 1 /*input*/ + available
	desiredH := m.h * 80 / 100
	spacer := desiredH - contentRows - 3 // 3 = footer + 2 border
	if spacer < 3 {
		spacer = 3
	}
	if spacer > 12 {
		spacer = 12
	}
	innerH := contentRows + spacer + 1 /*bottom sep*/ + 1 /*footer*/
	boxH = innerH + 2
	return
}

func (m Model) renderBox() string {
	boxW, _, available := m.layoutDims()
	innerW := boxW - 2

	bgFill := lipgloss.NewStyle().Background(pmBoxBg)
	inputBgFill := lipgloss.NewStyle().Background(pmInputBg)
	inputStyle := lipgloss.NewStyle().Background(pmInputBg).Foreground(pmInputFg)
	inputCursorStyle := lipgloss.NewStyle().Background(pmInputBg).Foreground(pmInputFg).Underline(true)
	separatorStyle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmGuide)
	footerStyle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmTextDim)

	headerIcon := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmIconBlue).Bold(true).Render("›")
	titleText := strings.TrimSpace(m.title)
	if titleText == "" {
		titleText = "Picker"
	}
	headerTitle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmTextPri).Bold(true).Render(" " + titleText)
	subtitleText := pickerSubtitle(len(m.matches), m.loading)
	subtitle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmSubtitleFg).Faint(true).Render(subtitleText)

	padBg := func(s string, w int) string {
		used := lipgloss.Width(s)
		if used >= w {
			return s
		}
		return s + bgFill.Render(strings.Repeat(" ", w-used))
	}
	blank := bgFill.Render(strings.Repeat(" ", innerW))

	var rows []string

	// ── Header: title left, subtitle right ─────────────────────────────
	left := bgFill.Render("  ") + headerIcon + headerTitle
	right := subtitle + bgFill.Render("  ")
	gap := innerW - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		// Subtitle didn't fit — drop it and just pad the header.
		rows = append(rows, padBg(left, innerW))
	} else {
		rows = append(rows, left+bgFill.Render(strings.Repeat(" ", gap))+right)
	}

	// ── Top hairline ──────────────────────────────────────────────────
	rows = append(rows, separatorStyle.Render(strings.Repeat("─", innerW)))

	// ── Input row with chevron prefix ─────────────────────────────────
	inputContent := inputBgFill.Render(" › ") + inputStyle.Render(m.input) + inputCursorStyle.Render("▎")
	if w := lipgloss.Width(inputContent); w < innerW {
		inputContent += inputBgFill.Render(strings.Repeat(" ", innerW-w))
	}
	rows = append(rows, inputContent)

	// ── Result rows ───────────────────────────────────────────────────
	if m.loading {
		rows = append(rows, padBg(bgFill.Render("    ")+lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmTextSec).Render("loading…"), innerW))
		// Pad remaining result slots so the spacer math stays consistent.
		for i := 1; i < available; i++ {
			rows = append(rows, blank)
		}
	} else if len(m.matches) == 0 {
		rows = append(rows, padBg(bgFill.Render("    ")+lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmTextSec).Render("no results"), innerW))
		for i := 1; i < available; i++ {
			rows = append(rows, blank)
		}
	} else {
		start := 0
		if m.cursor >= available {
			start = m.cursor - available + 1
		}
		end := start + available
		if end > len(m.matches) {
			end = len(m.matches)
		}
		for i := start; i < end; i++ {
			match := m.matches[i]
			it := Item{}
			if match.Index >= 0 && match.Index < len(m.items) {
				it = m.items[match.Index]
			}
			if it.Header {
				rows = append(rows, m.renderHeaderRow(it, innerW))
				continue
			}
			rows = append(rows, m.renderResultRow(match, it, i == m.cursor, innerW))
		}
		// Pad with blank rows when we have fewer results than slots so
		// the footer keeps its position.
		for i := end - start; i < available; i++ {
			rows = append(rows, blank)
		}
	}

	// ── Spacer ────────────────────────────────────────────────────────
	desiredH := m.h * 80 / 100
	contentH := len(rows)
	spacer := desiredH - contentH - 3
	if spacer < 3 {
		spacer = 3
	}
	if spacer > 12 {
		spacer = 12
	}
	for i := 0; i < spacer; i++ {
		rows = append(rows, blank)
	}

	// ── Bottom hairline + footer ──────────────────────────────────────
	rows = append(rows, separatorStyle.Render(strings.Repeat("─", innerW)))
	footer := footerStyle.Render("↑↓ Navigate    ↵ Select    Esc Close")
	rows = append(rows, padBg(bgFill.Render("  ")+footer, innerW))

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	// Height intentionally NOT set — lipgloss auto-fits the rounded border
	// to the row count. Setting Height(N) would pad below the footer and
	// open a visible gap.
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pmBorder).
		BorderBackground(pmBoxBg).
		Background(pmBoxBg).
		Width(boxW)
	return border.Render(content)
}

// pickerSubtitle returns the right-aligned faint text that appears beside
// the title — match count when loaded, "loading…" while the loader runs.
func pickerSubtitle(n int, loading bool) string {
	if loading {
		return "loading…"
	}
	switch n {
	case 0:
		return "no matches"
	case 1:
		return "1 match"
	default:
		return itoa(n) + " matches"
	}
}

// itoa is a tiny int→string helper so we don't pull strconv into this file
// just for the subtitle. Negative inputs are clamped to 0 (the subtitle
// never shows negatives).
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

// renderHeaderRow paints a non-selectable section heading. The label is
// rendered uppercased in a muted fg + box bg, with a faint horizontal
// hairline to the right so the row reads as a section divider as well as
// a label. Width contract: exactly `w` cells.
func (m Model) renderHeaderRow(it Item, w int) string {
	bgFill := lipgloss.NewStyle().Background(pmBoxBg)
	labelStyle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmTextDim).Bold(true)
	dividerStyle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmGuide)

	label := strings.ToUpper(strings.TrimSpace(it.Title))
	// 1-cell left pad so the label doesn't sit flush against the box border.
	left := bgFill.Render(" ") + labelStyle.Render(label) + bgFill.Render(" ")
	leftW := lipgloss.Width(left)
	if leftW >= w {
		// Label longer than the row: drop the trailing pad / truncate
		// so we never overflow innerW. Fallback simple rune trim.
		runes := []rune(label)
		for lipgloss.Width(labelStyle.Render(string(runes))) >= w && len(runes) > 0 {
			runes = runes[:len(runes)-1]
		}
		s := bgFill.Render(" ") + labelStyle.Render(string(runes))
		// Pad to exactly w with bg.
		if pad := w - lipgloss.Width(s); pad > 0 {
			s += bgFill.Render(strings.Repeat(" ", pad))
		}
		return s
	}
	dashes := w - leftW
	if dashes < 0 {
		dashes = 0
	}
	return left + dividerStyle.Render(strings.Repeat("─", dashes))
}

// renderResultRow renders one fuzzy match as a row inside the results list.
// Selected rows get a soft selection bar with a leading "▶ " chevron and
// bold white title; unselected rows live on the panel bg with a 4-space
// indent. The directory portion of a path-style title is dimmed and match
// positions are highlighted in yellow.
func (m Model) renderResultRow(match fuzzy.Match, it Item, active bool, w int) string {
	bg := pmBoxBg
	if active {
		bg = pmSelBg
	}
	rowBgFill := lipgloss.NewStyle().Background(bg)

	titleFg := pmTextPri
	hintFg := pmHintFg
	dirFg := pmDirFg
	if active {
		titleFg = pmSelFg
		hintFg = pmSelHintFg
		dirFg = pmSelDirFg
	}
	titleStyle := lipgloss.NewStyle().Background(bg).Foreground(titleFg)
	if active {
		titleStyle = titleStyle.Bold(true)
	}
	dirStyle := lipgloss.NewStyle().Background(bg).Foreground(dirFg)
	hintStyle := lipgloss.NewStyle().Background(bg).Foreground(hintFg)
	matchStyle := lipgloss.NewStyle().Background(bg).Foreground(pmMatchFg).Bold(true)

	pos := make(map[int]bool, len(match.MatchedIndexes))
	for _, i := range match.MatchedIndexes {
		pos[i] = true
	}

	// Find the LAST '/' so we can dim the directory portion.
	runes := []rune(it.Title)
	splitAt := -1
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] == '/' {
			splitAt = i
			break
		}
	}

	var sb strings.Builder
	if active {
		// "▶ " chevron + 2-space indent → 4 cells before content, matches
		// the unselected 4-space indent so the rows visually align.
		sb.WriteString(lipgloss.NewStyle().Background(bg).Foreground(pmSelFg).Bold(true).Render(" ▶  "))
	} else {
		sb.WriteString(rowBgFill.Render("    "))
	}

	for i, r := range runes {
		switch {
		case pos[i]:
			sb.WriteString(matchStyle.Render(string(r)))
		case i <= splitAt:
			sb.WriteString(dirStyle.Render(string(r)))
		default:
			sb.WriteString(titleStyle.Render(string(r)))
		}
	}
	if it.Hint != "" {
		sb.WriteString(rowBgFill.Render("  "))
		sb.WriteString(hintStyle.Render(it.Hint))
	}

	rendered := sb.String()
	if pad := w - lipgloss.Width(rendered); pad > 0 {
		rendered += rowBgFill.Render(strings.Repeat(" ", pad))
	}
	return rendered
}

// filesLoader walks cwd and returns one Item per file.
func filesLoader() []Item {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var items []Item
	skipDirs := map[string]bool{
		".git":         true,
		"node_modules": true,
		"vendor":       true,
		"dist":         true,
		"build":        true,
		"target":       true,
		"__pycache__":  true,
		".idea":        true,
		".vscode":      true,
		".cache":       true,
	}
	_ = filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if path != cwd && (skipDirs[name] || (strings.HasPrefix(name, ".") && name != ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if rel, err := filepath.Rel(cwd, path); err == nil {
			items = append(items, Item{ID: rel, Title: rel})
		}
		return nil
	})
	return items
}

// isMouseCodeRunes reports whether `runes` consists ENTIRELY of bytes
// that can appear inside an SGR mouse sequence — digits, `;`, `<`, `[`,
// `M`, `m`. Used together with msg.Alt to drop the single-rune `[`
// Bubble Tea emits when an `\x1b[<…M` mouse SGR is split across reads
// and the parser falls back to the alt-prefix rune scanner (which only
// keeps one rune per event).
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

// isMouseFragmentEvent reports whether a KeyRunes event payload is
// almost certainly a fragment of a mouse-tracking SGR sequence (e.g.
// "[<35", ";70", ";19M") rather than something the user typed.
//
// Heuristic:
//   - 2+ runes (single-rune `[` / `<` / `M` are valid keystrokes)
//   - first rune is `[` or `<` (real typing rarely starts a multi-char
//     burst with these — they ride at the head of the SGR fragment, OR
//     the chunk after a `;` split looks like ";70" / ";19M" so we also
//     accept a leading `;`)
//   - every rune is in the mouse-code charset (digits, `;`, `<`, `[`,
//     `M`, `m`)
//
// Persian / non-ASCII text always contains runes outside the charset,
// so it falls through unchanged. Typing `[hello]` falls through too —
// `h` is not in the charset.
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

// sanitizeInput strips ANSI control sequences and other non-printable
// bytes that can sneak into a text-input field — typically when the
// terminal emits a mouse-tracking SGR code that the program's input
// parser didn't intercept (e.g. mid-toggle of mouse mode).
//
// Drops:
//   - full CSI escapes: \x1b[<params><final 0x40..0x7E>
//   - bare mouse-SGR fragments [<digits;digits;digits{M|m}] (ESC already
//     stripped by the upstream parser)
//   - control bytes (< 0x20 or 0x7F)
func sanitizeInput(s string) string {
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
