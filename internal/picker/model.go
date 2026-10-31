package picker

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/amin-jalali/termocode/internal/anim"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/theme"
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
	// File marks Title as a file path: the row gets a file-type icon and
	// the directory part is dimmed.
	File bool
	// Lead is a word that, typed in full as the query (any case), puts
	// this row first (after a row whose title is the query): the main
	// command of a family, e.g. "web" for Web: Open in Browser.
	Lead string
}

// SelectMsg is emitted when the user activates a row.
type SelectMsg struct {
	ID    string
	Title string
}

// ActionMsg is emitted when the user presses the picker's row action key
// (SetRowAction) on a row: Key is the key as set, ID and Title name the
// highlighted row.
type ActionMsg struct {
	Key, ID, Title string
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
	// top is the first visible result row; scroll animates toward it so
	// long jumps (PgDn, wheel, typing) glide instead of snapping.
	top    int
	scroll anim.Value
	// note is a few lines of plain text shown above the search field
	// (SetNote); empty = none.
	note []string
	// width is the box width asked for (SetWidth); 0 = the default 56.
	width int
	// hideEmptyCount drops the "N matches" subtitle while the search is empty.
	hideEmptyCount bool
	// actionKey/actionLabel: one extra row key shown in the footer
	// ("→", "details"); "→" (Right) and "space" are supported.
	actionKey, actionLabel string
	// fc keeps the last filter's work for the next key (filter_cache.go).
	fc filterCache
}

// SetRowAction adds a footer hint and a key that acts on the highlighted
// row without selecting it: key "→" (the Right arrow) or "space" emits
// ActionMsg (with "space" a blank is never typed into the search). The
// footer hint is a button too.
func (m *Model) SetRowAction(key, label string) { m.actionKey, m.actionLabel = key, label }

// rowAction reports the highlighted row as an ActionMsg command.
func (m Model) rowAction() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.matches) {
		return nil
	}
	idx := m.matches[m.cursor].Index
	if idx < 0 || idx >= len(m.items) || m.items[idx].Header {
		return nil
	}
	it, key := m.items[idx], m.actionKey
	return func() tea.Msg { return ActionMsg{Key: key, ID: it.ID, Title: it.Title} }
}

// SetWidth asks for a wider box (rows with a hint column, like the
// Connect-a-service list); it still fits the window.
func (m *Model) SetWidth(w int) { m.width = w }

// HideCountWhenEmpty drops the "N matches" subtitle while nothing is typed,
// for short lists of choices where the count only repeats what is on screen.
func (m *Model) HideCountWhenEmpty() { m.hideEmptyCount = true }

// SetNote shows lines of plain text above the search field, e.g. why a
// choice is needed. Each line should fit the box (about 50 cells).
func (m *Model) SetNote(lines []string) { m.note = lines }

// noteRows is how many box rows the note takes (its lines + a blank).
func (m Model) noteRows() int {
	if len(m.note) == 0 {
		return 0
	}
	return len(m.note) + 1
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

func (m Model) Init() tea.Cmd {
	if m.loader == nil {
		return nil
	}
	loader := m.loader
	return func() tea.Msg { return itemsLoadedMsg{items: loader()} }
}

func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	m, cmd := m.update(msg)
	m.follow()
	return m, cmd
}

func (m Model) update(msg tea.Msg) (Model, tea.Cmd) {
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
		case tea.KeyRight:
			if m.actionKey == "→" {
				return m, m.rowAction()
			}
			return m, nil
		case tea.KeyUp, tea.KeyCtrlP:
			m.cursor = m.nextSelectable(m.cursor, -1)
			return m, nil
		case tea.KeyDown, tea.KeyCtrlN:
			m.cursor = m.nextSelectable(m.cursor, +1)
			return m, nil
		case tea.KeyPgDown:
			_, _, avail := m.layoutDims()
			for i := 0; i < avail; i++ {
				m.cursor = m.nextSelectable(m.cursor, +1)
			}
			return m, nil
		case tea.KeyPgUp:
			_, _, avail := m.layoutDims()
			for i := 0; i < avail; i++ {
				m.cursor = m.nextSelectable(m.cursor, -1)
			}
			return m, nil
		case tea.KeyBackspace:
			if len(m.input) > 0 {
				r := []rune(m.input)
				m.input = string(r[:len(r)-1])
				m.refilter()
			}
			return m, nil
		case tea.KeySpace:
			if m.actionKey == "space" { // Space is the row key: no blank in the search
				return m, m.rowAction()
			}
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
	titles, indexMap, raw := m.fc.find(m.items, m.input) // titles[k] → m.items[indexMap[k]]
	hits := bestFirst(m.input, raw, titles, func(k int) string { return m.items[indexMap[k]].Lead })
	if !m.hasGroups() {
		// Flat list: best match first, so Enter runs what was typed.
		m.matches = make(fuzzy.Matches, len(hits))
		for k, h := range hits {
			h.Index = indexMap[h.Index]
			m.matches[k] = h
		}
		m.cursor = 0
		return
	}
	best := -1
	if len(hits) > 0 {
		best = indexMap[hits[0].Index]
	}
	// Find the most recent header index that precedes each item.
	// Walk hits in their original item order so we can detect group
	// boundaries and inject a header at the start of each new group.
	out := make(fuzzy.Matches, 0, len(hits)+8)
	seenGroups := make(map[string]bool)
	// hits aren't necessarily in input-order; sort by underlying item index
	// so injected headers land in the same stable order as the unfiltered list.
	type indexedHit struct {
		match   fuzzy.Match
		itemIdx int
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
	// Grouped list: rows keep their group order, the cursor goes to the
	// best match.
	for k, mt := range out {
		if mt.Index == best {
			m.cursor = k
			break
		}
	}
}

// hasGroups reports whether the list has section headers.
func (m Model) hasGroups() bool {
	for _, it := range m.items {
		if it.Header || it.Group != "" {
			return true
		}
	}
	return false
}

// bestFirst moves titles equal to the query (ignoring case) to the front
// of hits, which fuzzy.Find already sorts by score, then rows whose Lead
// word (lead(k) for titles[k]) is the query. Typing a full title must
// select that row even when a longer title scores as well.
func bestFirst(query string, hits fuzzy.Matches, titles []string, lead func(k int) string) fuzzy.Matches {
	rank := func(h fuzzy.Match) int {
		switch {
		case strings.EqualFold(titles[h.Index], query):
			return 0
		case strings.EqualFold(lead(h.Index), query):
			return 1
		}
		return 2
	}
	out := make(fuzzy.Matches, 0, len(hits))
	for r := 0; r <= 2; r++ {
		for _, h := range hits {
			if rank(h) == r {
				out = append(out, h)
			}
		}
	}
	return out
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
	m, cmd := m.handleMouse(x, y, action, button)
	m.follow()
	return m, cmd
}

func (m Model) handleMouse(x, y int, action tea.MouseAction, button tea.MouseButton) (Model, tea.Cmd) {
	if m.w < 3 || m.h <= 0 {
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
	// Hover: move the highlight onto the result row under the pointer.
	if action == tea.MouseActionMotion {
		if idx, ok := m.RowAt(x, y); ok && !m.matchIsHeader(idx) {
			m.cursor = idx
		}
		return m, nil
	}

	if button != tea.MouseButtonLeft || action != tea.MouseActionPress {
		return m, nil
	}

	// Click outside the box → close.
	if !m.InBox(x, y) {
		return m, func() tea.Msg { return CloseMsg{} }
	}
	// The footer hints are buttons: Enter selects, Esc closes.
	switch m.footerKeyAt(x, y) {
	case "→", "space":
		return m, m.rowAction()
	case "esc":
		return m, func() tea.Msg { return CloseMsg{} }
	case "enter":
		idx := m.cursor
		if idx < 0 || idx >= len(m.matches) {
			return m, nil
		}
		match := m.matches[idx]
		if match.Index < 0 || match.Index >= len(m.items) || m.items[match.Index].Header {
			return m, nil
		}
		it := m.items[match.Index]
		return m, func() tea.Msg { return SelectMsg{ID: it.ID, Title: it.Title} }
	}
	idx, ok := m.RowAt(x, y)
	if !ok {
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
	if m.w < 3 || m.h <= 0 {
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
	if m.w < 3 || m.h <= 0 {
		return ""
	}
	return m.renderBox()
}

// Colours come from theme.Float() (refreshed on every render so a theme
// switch applies at once). Pickers, prompts, confirms and settings share
// that one floating-card look.
var (
	pmBoxBg, pmBorder, pmIconBlue, pmTextPri, pmTextSec, pmTextDim lipgloss.Color
	pmGuide, pmSubtitleFg, pmInputBg, pmInputFg, pmSelBg, pmSelFg  lipgloss.Color
	pmSelDirFg, pmDirFg, pmHintFg, pmSelHintFg, pmMatchFg          lipgloss.Color
)

func refreshColors() {
	f := theme.Float()
	pmBoxBg, pmBorder, pmIconBlue = f.Bg, f.Border, f.Icon
	pmTextPri, pmTextSec, pmTextDim = f.Text, f.Sub, f.Dim
	pmGuide, pmSubtitleFg = f.Hairline, f.Dim
	pmInputBg, pmInputFg = f.InputBg, f.InputFg
	pmSelBg, pmSelFg = f.SelBg, f.SelFg
	pmSelDirFg, pmDirFg = f.Sub, f.Dim
	pmHintFg, pmSelHintFg, pmMatchFg = f.Hint, f.Sub, f.Match
}

// viewTop is the first result row drawn (the animated scroll position,
// clamped so the window never runs past the list).
func (m Model) viewTop(available int) int {
	top := m.scroll.Int()
	if max := len(m.matches) - available; top > max {
		top = max
	}
	if top < 0 {
		top = 0
	}
	return top
}

// follow moves the scroll window just enough to keep the cursor visible.
func (m *Model) follow() {
	_, _, available := m.layoutDims()
	if available < 1 {
		return
	}
	top := m.top
	if m.cursor < top {
		top = m.cursor
	}
	if m.cursor >= top+available {
		top = m.cursor - available + 1
	}
	if max := len(m.matches) - available; top > max {
		top = max
	}
	if top < 0 {
		top = 0
	}
	if top != m.top || m.scroll.Target != float64(top) {
		m.top = top
		// One-row moves (arrow keys) stay crisp; longer jumps glide.
		if d := float64(top) - m.scroll.Pos; d >= -1 && d <= 1 {
			m.scroll.Jump(float64(top))
		} else {
			m.scroll.Set(float64(top))
		}
	}
}

// Origin is the top-left cell of the box: centred horizontally and
// anchored near the top (like VSCode), so the box grows downward as the
// list changes instead of jumping around the centre.
func (m Model) Origin(boxW, boxH int) (x, y int) {
	x = (m.w - boxW) / 2
	y = m.h / 7
	if y+boxH > m.h {
		y = (m.h - boxH) / 2
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

// BoxSize is the rendered box size (for the host's compositor).
func (m Model) BoxSize() (w, h int) {
	w, h, _ = m.layoutDims()
	return w, h
}

// Current returns the item under the cursor (ok=false for none / header).
func (m Model) Current() (Item, bool) {
	if m.cursor < 0 || m.cursor >= len(m.matches) {
		return Item{}, false
	}
	idx := m.matches[m.cursor].Index
	if idx < 0 || idx >= len(m.items) || m.items[idx].Header {
		return Item{}, false
	}
	return m.items[idx], true
}

// SetItems replaces the rows but keeps the search text and the row under
// the cursor (by ID), so a toggle can redraw its row in place.
func (m *Model) SetItems(items []Item) {
	cur, ok := m.Current()
	m.items = items
	m.refilter()
	if ok {
		m.SetCursorID(cur.ID)
	}
}

// SetCursorID moves the cursor onto the item with the given ID.
func (m *Model) SetCursorID(id string) {
	for i, mt := range m.matches {
		if mt.Index >= 0 && mt.Index < len(m.items) && m.items[mt.Index].ID == id {
			m.cursor = i
			m.follow()
			return
		}
	}
}

// Animating reports whether the list is still gliding to its scroll target.
func (m Model) Animating() bool { return m.scroll.Moving() }

// AnimStep advances the scroll animation one frame.
func (m *Model) AnimStep() { m.scroll.Step() }

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
	if m.width > boxW {
		boxW = m.width
	}
	if maxW := m.w - 4; boxW > maxW {
		boxW = maxW
	}
	boxW = max(3, min(boxW, m.w))

	resultCount := len(m.matches)
	if m.loading || resultCount == 0 {
		resultCount = 1 // "loading…" / "no results" placeholder row
	}
	// Cap visible rows at a sensible band so the modal stays compact.
	maxRows := m.h - 12 - m.noteRows() // leave room for header+input+seps+footer+spacer+border
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

	contentRows := 1 /*header*/ + 1 /*top sep*/ + m.noteRows() + 1 /*input*/ + available
	// The list is sized to its content; one blank row before the footer.
	spacer := 1
	innerH := contentRows + spacer + 1 /*bottom sep*/ + 1 /*footer*/
	boxH = innerH + 2
	return
}

func (m Model) renderBox() string {
	refreshColors()
	boxW, _, available := m.layoutDims()
	innerW := boxW - 2

	bgFill := lipgloss.NewStyle().Background(pmBoxBg)
	inputBgFill := lipgloss.NewStyle().Background(pmInputBg)
	inputStyle := lipgloss.NewStyle().Background(pmInputBg).Foreground(pmInputFg)
	placeholder := lipgloss.NewStyle().Background(pmInputBg).Foreground(pmTextDim)
	caret := lipgloss.NewStyle().Background(pmInputBg).Foreground(pmIconBlue).Bold(true)

	headerIcon := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmIconBlue).Bold(true).Render(titleGlyph())
	titleText := strings.TrimSpace(m.title)
	if titleText == "" {
		titleText = "Picker"
	}
	subtitle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmSubtitleFg).Render(m.subtitleText())
	// The title is cut to its one row: a wrapped title would push every
	// list row down and break the mouse hit test (RowRects).
	room := innerW - 2 - lipgloss.Width(headerIcon) - 1 - lipgloss.Width(subtitle) - 2 - 1
	if room < 1 {
		room = 1
	}
	titleText = runewidth.Truncate(titleText, room, "…")
	headerTitle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmTextPri).Bold(true).Render(" " + titleText)

	padBg := func(s string, w int) string {
		used := lipgloss.Width(s)
		if used >= w {
			return s
		}
		return s + bgFill.Render(strings.Repeat(" ", w-used))
	}
	blank := bgFill.Render(strings.Repeat(" ", innerW))

	var rows []string

	// ── Header: icon + title left, match count right ──────────────────
	left := bgFill.Render("  ") + headerIcon + headerTitle
	right := subtitle + bgFill.Render("  ")
	if gap := innerW - lipgloss.Width(left) - lipgloss.Width(right); gap < 1 {
		rows = append(rows, padBg(left, innerW))
	} else {
		rows = append(rows, left+bgFill.Render(strings.Repeat(" ", gap))+right)
	}

	// ── Lit hairline under the title ──────────────────────────────────
	rows = append(rows, theme.Hairline(innerW, theme.BgFloat))

	// ── Note (SetNote) ────────────────────────────────────────────────
	if len(m.note) > 0 {
		noteStyle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmTextSec)
		for _, l := range m.note {
			l = runewidth.Truncate(l, max(innerW-3, 1), "…")
			rows = append(rows, padBg(bgFill.Render("  ")+noteStyle.Render(l), innerW))
		}
		rows = append(rows, blank)
	}

	// ── Search field ──────────────────────────────────────────────────
	field := inputBgFill.Render(" ") + caret.Render(searchGlyph()) + inputBgFill.Render(" ")
	if m.input == "" {
		field += caret.Render("▏") + placeholder.Render("Type to search")
	} else {
		field += inputStyle.Render(inputTail(m.input, max(0, innerW-4))) + caret.Render("▏")
	}
	if w := lipgloss.Width(field); w < innerW {
		field += inputBgFill.Render(strings.Repeat(" ", innerW-w))
	}
	rows = append(rows, field)

	// ── Result rows ───────────────────────────────────────────────────
	msg := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmTextSec)
	switch {
	case m.loading:
		rows = append(rows, padBg(bgFill.Render("    ")+msg.Render("loading…"), innerW))
		for i := 1; i < available; i++ {
			rows = append(rows, blank)
		}
	case len(m.matches) == 0:
		rows = append(rows, padBg(bgFill.Render("    ")+msg.Render("no results"), innerW))
		for i := 1; i < available; i++ {
			rows = append(rows, blank)
		}
	default:
		plainRow, activeRow := newRowStyles(false), newRowStyles(true)
		start := m.viewTop(available)
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
			st := &plainRow
			if i == m.cursor {
				st = &activeRow
			}
			rows = append(rows, m.renderResultRow(match, it, i == m.cursor, innerW, st))
		}
		for i := end - start; i < available; i++ {
			rows = append(rows, blank)
		}
	}

	// ── Spacer ────────────────────────────────────────────────────────
	spacer := 1
	for i := 0; i < spacer; i++ {
		rows = append(rows, blank)
	}

	// ── Footer: keycap hints ──────────────────────────────────────────
	rows = append(rows, lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmGuide).Render(strings.Repeat("─", innerW)))
	footer := theme.HintBar(pmBoxBg, m.footerPairs(innerW)...)
	rows = append(rows, padBg(bgFill.Render("  ")+footer, innerW))

	for i := range rows {
		rows[i] = ansi.Truncate(rows[i], innerW, "")
	}
	return theme.FloatBox(lipgloss.JoinVertical(lipgloss.Left, rows...), boxW)
}

// footerPairs are the footer hints that fit innerW cells.
func (m Model) footerPairs(innerW int) []string {
	fits := func(p []string) bool {
		sp := theme.HintSpans(p...)
		return sp[len(sp)-1].X1+2 <= innerW
	}
	full := []string{"↑↓", "navigate", "enter", "select", "esc", "close"}
	if m.actionKey != "" {
		with := []string{"↑↓", "navigate", m.actionKey, m.actionLabel, "enter", "select", "esc", "close"}
		for _, p := range [][]string{with, with[2:], {"enter", "select", "esc", "close"}} {
			if fits(p) {
				return p
			}
		}
	}
	if !fits(full) {
		return full[2:]
	}
	return full
}

// footerKeyAt is the footer hint key under screen cell (x, y) ("" none).
// The footer is the last row inside the border, after a 2-cell margin.
func (m Model) footerKeyAt(x, y int) string {
	bx, by, bw, bh := m.boxRect()
	if y != by+bh-2 {
		return ""
	}
	rel := x - (bx + 1 + 2)
	for _, sp := range theme.HintSpans(m.footerPairs(bw - 2)...) {
		if rel >= sp.X0 && rel < sp.X1 {
			return sp.Key
		}
	}
	return ""
}

// titleGlyph / searchGlyph follow the icon mode (Nerd Font, Unicode,
// ASCII).
func titleGlyph() string {
	switch theme.CurrentIconMode {
	case theme.IconModeNerdFont:
		return "\uf120" // nf-fa-terminal
	case theme.IconModeUnicode:
		return "❯"
	}
	return ">"
}

func searchGlyph() string {
	switch theme.CurrentIconMode {
	case theme.IconModeNerdFont:
		return "\uea6d" // nf-cod-search
	case theme.IconModeUnicode:
		return "⌕"
	}
	return "/"
}

// subtitleText is the subtitle for the current state of the picker.
func (m Model) subtitleText() string {
	if m.hideEmptyCount && m.input == "" && !m.loading {
		return ""
	}
	return pickerSubtitle(len(m.matches), m.loading)
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

// renderHeaderRow paints a non-selectable section heading: a small
// uppercase label followed by a hairline. Exactly w cells.
func (m Model) renderHeaderRow(it Item, w int) string {
	bgFill := lipgloss.NewStyle().Background(pmBoxBg)
	labelStyle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmTextDim).Bold(true)
	dividerStyle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmGuide)

	label := strings.ToUpper(strings.TrimSpace(it.Title))
	if max := w - 4; runewidth.StringWidth(label) > max && max > 0 {
		label = runewidth.Truncate(label, max, "…")
	}
	left := bgFill.Render("  ") + labelStyle.Render(label) + bgFill.Render(" ")
	dashes := w - lipgloss.Width(left)
	if dashes < 0 {
		dashes = 0
	}
	return left + dividerStyle.Render(strings.Repeat("─", dashes))
}

// rowStyles are the styles of result rows, made once per frame: one set
// for the active row, one for the others.
type rowStyles struct {
	bg                                  lipgloss.Color
	fill, title, dir, hint, match, edge theme.Fast
}

func newRowStyles(active bool) rowStyles {
	bg := pmBoxBg
	titleFg, hintFg, dirFg := pmTextPri, pmHintFg, pmDirFg
	if active {
		bg = pmSelBg
		titleFg, hintFg, dirFg = pmSelFg, pmSelHintFg, pmSelDirFg
	}
	return rowStyles{
		bg:    bg,
		fill:  theme.NewFast(lipgloss.NewStyle().Background(bg)),
		title: theme.NewFast(lipgloss.NewStyle().Background(bg).Foreground(titleFg).Bold(active)),
		dir:   theme.NewFast(lipgloss.NewStyle().Background(bg).Foreground(dirFg)),
		hint:  theme.NewFast(lipgloss.NewStyle().Background(bg).Foreground(hintFg)),
		match: theme.NewFast(lipgloss.NewStyle().Background(bg).Foreground(pmMatchFg).Bold(true)),
		edge:  theme.NewFast(lipgloss.NewStyle().Background(bg).Foreground(pmIconBlue)),
	}
}

// renderResultRow renders one match. The active row is a soft accent bar
// with a left accent edge; matched characters are highlighted; for file
// items the type icon leads and the directory part is dimmed. Exactly w
// cells.
func (m Model) renderResultRow(match fuzzy.Match, it Item, active bool, w int, st *rowStyles) string {
	bg := st.bg
	fill, titleStyle, dirStyle, hintStyle, matchStyle := st.fill, st.title, st.dir, st.hint, st.match

	pos := make(map[int]bool, len(match.MatchedIndexes))
	for _, i := range match.MatchedIndexes {
		pos[i] = true
	}
	runes := []rune(it.Title)
	splitAt := -1
	if it.File || strings.ContainsRune(it.Title, '/') {
		for i := len(runes) - 1; i >= 0; i-- {
			if runes[i] == '/' {
				splitAt = i
				break
			}
		}
	}

	var sb strings.Builder
	if active {
		sb.WriteString(st.edge.Render("▌"))
		sb.WriteString(fill.Render(" "))
	} else {
		sb.WriteString(fill.Render("  "))
	}
	if it.File {
		g, c := theme.FileIconFor(it.Title, false, false)
		sb.WriteString(lipgloss.NewStyle().Background(bg).Foreground(theme.LG(c)).Render(g))
		sb.WriteString(fill.Render(" "))
	}
	// Room for the title: leave space for the hint and a right margin.
	hint := it.Hint
	avail := w - lipgloss.Width(sb.String()) - 2
	if hint != "" {
		if hw := runewidth.StringWidth(hint); hw > avail/2 {
			hint = runewidth.Truncate(hint, avail/2, "…")
		}
		avail -= runewidth.StringWidth(hint) + 2
	}
	// Runs of runes that share a style are rendered at once (one Render
	// per run, not per rune: a row is a handful of runs).
	styles := [3]theme.Fast{titleStyle, dirStyle, matchStyle}
	run, runKind := make([]rune, 0, len(runes)), -1
	flush := func() {
		if len(run) > 0 {
			sb.WriteString(styles[runKind].Render(string(run)))
			run = run[:0]
		}
	}
	used := 0
	for i, r := range runes {
		rw := runewidth.RuneWidth(r)
		if used+rw > avail {
			flush()
			sb.WriteString(titleStyle.Render("…"))
			break
		}
		used += rw
		kind := 0
		switch {
		case pos[i]:
			kind = 2
		case i <= splitAt:
			kind = 1
		}
		if kind != runKind {
			flush()
			runKind = kind
		}
		run = append(run, r)
	}
	flush()
	rendered := sb.String()
	if hint != "" {
		gap := w - lipgloss.Width(rendered) - runewidth.StringWidth(hint) - 2
		if gap < 1 {
			gap = 1
		}
		rendered += fill.Render(strings.Repeat(" ", gap)) + hintStyle.Render(hint)
	}
	if pad := w - lipgloss.Width(rendered); pad > 0 {
		rendered += fill.Render(strings.Repeat(" ", pad))
	}
	return rendered
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

// boxRect is the box's screen rect.
func (m Model) boxRect() (x, y, w, h int) {
	w, h, _ = m.layoutDims()
	x, y = m.Origin(w, h)
	return x, y, w, h
}

// InBox reports whether screen cell (x, y) is inside the box.
func (m Model) InBox(x, y int) bool {
	bx, by, bw, bh := m.boxRect()
	return x >= bx && x < bx+bw && y >= by && y < by+bh
}

// RowRect is where a visible result row sits on screen (one row tall).
type RowRect struct {
	Index   int // index into the matches (what the cursor addresses)
	X, Y, W int
}

// RowRects lists the visible result rows with the screen cells
// HandleMouse hit tests for them.
func (m Model) RowRects() []RowRect {
	if m.w < 3 || m.h <= 0 {
		return nil
	}
	bx, by, bw, _ := m.boxRect()
	_, _, available := m.layoutDims()
	top := m.viewTop(available)
	var out []RowRect
	// Inside the box: y=0 border, y=1 header, y=2 hairline, the note,
	// then the input and result row 0, …
	first := by + 4 + m.noteRows()
	for i := 0; i < available && top+i < len(m.matches); i++ {
		out = append(out, RowRect{Index: top + i, X: bx, Y: first + i, W: bw})
	}
	return out
}

// RowAt returns the match index of the result row at screen cell (x, y).
func (m Model) RowAt(x, y int) (int, bool) {
	for _, r := range m.RowRects() {
		if y == r.Y && x >= r.X && x < r.X+r.W {
			return r.Index, true
		}
	}
	return 0, false
}

// inputTail keeps the caret and the most recent query text visible.
func inputTail(s string, width int) string {
	r := []rune(s)
	for len(r) > 0 && runewidth.StringWidth(string(r)) > width {
		r = r[1:]
	}
	return string(r)
}
