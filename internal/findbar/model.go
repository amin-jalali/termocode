package findbar

import (
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/theme"
)

// SearchMsg fires on every keystroke, carrying the current query.
type SearchMsg struct{ Query string }

// FindNextMsg fires on Enter (when find input is focused).
type FindNextMsg struct{}

// FindPrevMsg fires on Shift+Tab / Up.
type FindPrevMsg struct{}

// CloseMsg fires on Esc.
type CloseMsg struct{}

// ReplaceMsg fires when Enter is pressed in the replace input or the
// replace-one icon is clicked. The host should run a single substitution
// against the next match and advance.
type ReplaceMsg struct {
	Find    string
	Replace string
}

// ReplaceAllMsg fires on Ctrl+Enter in the replace input or click on the
// replace-all icon. The host should substitute every match in the buffer.
type ReplaceAllMsg struct {
	Find    string
	Replace string
}

// blinkMsg toggles cursor visibility on each tick. Unexported — only
// produced by BlinkCmd / Update.
type blinkMsg struct{}

const blinkPeriod = 530 * time.Millisecond

// BlinkCmd kicks off a blink tick. Hosts call this once when opening the
// find bar; each tick re-schedules itself, so blink keeps running while
// the bar is open. In-flight ticks after close are silently dropped.
func BlinkCmd() tea.Cmd {
	return tea.Tick(blinkPeriod, func(time.Time) tea.Msg { return blinkMsg{} })
}

// field selects which input is taking keystrokes when replaceMode is on.
type field int

const (
	fieldFind field = iota
	fieldReplace
)

type Model struct {
	input string
	// cursorPos is the rune offset within input where the caret sits in the
	// FIND input. Mirrors the historical single-input caret.
	cursorPos int

	// replace holds the contents of the second-row Replace input (only used
	// when replaceMode is true).
	replace string
	// replaceCursorPos: caret position inside `replace`.
	replaceCursorPos int

	w int

	// matchCount is the "N/M" string returned by nvim's vim.fn.searchcount()
	// and pushed in via SetMatchCount. Empty when no search has run yet.
	matchCount string

	// caseSensitive toggles whether the search treats letter case as
	// significant. Default false (insensitive). Toggled by clicking the
	// "Aa" glyph in the right cluster.
	caseSensitive bool

	// wholeWord toggles whether the search anchors at word boundaries (\<…\>).
	// Default false. Toggled by the "ab" glyph.
	wholeWord bool

	// useRegex toggles whether the query is treated as a regular
	// expression (true) or a literal substring (false). Default false.
	// Toggled by clicking the ".*" glyph in the right cluster.
	useRegex bool

	// replaceMode controls whether the second (Replace) row is rendered
	// and whether the chevron points down. Toggled by clicking the
	// chevron glyph or via EnableReplace().
	replaceMode bool

	// focus tracks which input absorbs keystrokes when replaceMode is on.
	focus field

	// anchorX/anchorY: on-screen position of the panel's top-left cell.
	// Set by the host (see app/view.go::overlayFindBarInEditor) so
	// HandleMouse can hit-test the button glyphs against screen coords.
	anchorX int
	anchorY int

	// cursorVisible toggles each blink tick. Starts true so the cursor
	// is shown immediately on open; flipped by Update on every blinkMsg.
	cursorVisible bool
}

func New() Model { return Model{cursorVisible: true, focus: fieldFind} }

// Init kicks off the blink tick when the find bar opens.
func (m Model) Init() tea.Cmd { return BlinkCmd() }

func (m *Model) SetWidth(w int) { m.w = w }

// SetSize keeps API compatibility with callers that pass a height too — the
// panel auto-sizes vertically, so the height argument is ignored.
func (m *Model) SetSize(w, _ int) { m.w = w }

// SetAnchor records the on-screen (col, row) of the panel's top-left cell.
func (m *Model) SetAnchor(x, y int) { m.anchorX, m.anchorY = x, y }

// Bounds returns the panel's on-screen rectangle as (x, y, w, h).
func (m Model) Bounds() (x, y, w, h int) {
	return m.anchorX, m.anchorY, m.PanelWidth(), m.PanelHeight()
}

// SetMatchCount stashes the match-count string ("3/12", "0", etc.). Empty
// means "no active search yet" — the slot then renders blank.
func (m *Model) SetMatchCount(s string) { m.matchCount = s }

func (m Model) Query() string { return m.input }

// SetQuery prefills the find input with `s` and parks the caret at the
// end. The host calls this to seed the bar with the current visual
// selection so Ctrl+F / Ctrl+H mirrors the standard IDE behaviour
// ("search for whatever's selected").
func (m *Model) SetQuery(s string) {
	m.input = s
	m.cursorPos = len([]rune(s))
}

// CaseSensitive reports whether the case-sensitive toggle is on. The host
// reads this when constructing the nvim search pattern.
func (m Model) CaseSensitive() bool { return m.caseSensitive }

// WholeWord reports whether the whole-word toggle is on.
func (m Model) WholeWord() bool { return m.wholeWord }

// UseRegex reports whether the regex toggle is on. The host reads this
// when constructing the nvim search pattern (literal vs regex).
func (m Model) UseRegex() bool { return m.useRegex }

// ReplaceMode reports whether the Replace row is currently expanded.
func (m Model) ReplaceMode() bool { return m.replaceMode }

// ReplaceValue returns the current contents of the Replace input.
func (m Model) ReplaceValue() string { return m.replace }

// EnableReplace expands or collapses the Replace row. When expanding with
// a query already in Find (seeded from the selection, or typed before),
// the input focus moves to the Replace field so Ctrl+H feels like "open
// with the replace input ready". With an empty query the focus stays on
// Find, as in VS Code: the user types what to find first.
func (m *Model) EnableReplace(on bool) {
	m.replaceMode = on
	if on && m.input != "" {
		m.focus = fieldReplace
	} else {
		m.focus = fieldFind
	}
}

// Reset clears the typed query but keeps the configured width and the
// case/regex toggle states (so re-opening the bar feels sticky).
func (m *Model) Reset() {
	m.input = ""
	m.replace = ""
	m.matchCount = ""
	m.cursorPos = 0
	m.replaceCursorPos = 0
}

// activeValue / setActiveValue / activeCursor mediate keypresses against
// whichever input currently has focus.
func (m *Model) activeRunes() []rune {
	if m.focus == fieldReplace {
		return []rune(m.replace)
	}
	return []rune(m.input)
}

func (m *Model) setActiveValue(s string) {
	if m.focus == fieldReplace {
		m.replace = s
	} else {
		m.input = s
	}
}

func (m *Model) activeCursor() int {
	if m.focus == fieldReplace {
		return m.replaceCursorPos
	}
	return m.cursorPos
}

func (m *Model) setActiveCursor(p int) {
	if m.focus == fieldReplace {
		m.replaceCursorPos = p
	} else {
		m.cursorPos = p
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if _, ok := msg.(blinkMsg); ok {
		m.cursorVisible = !m.cursorVisible
		return m, BlinkCmd()
	}
	// Any non-blink event (typing, mouse, etc.) flips the cursor back to
	// visible so it doesn't ghost-hide right after the user types.
	m.cursorVisible = true
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.Type {
	case tea.KeyEsc:
		return m, func() tea.Msg { return CloseMsg{} }
	case tea.KeyTab:
		// Tab swaps focus between find and replace when expanded.
		if m.replaceMode {
			if m.focus == fieldFind {
				m.focus = fieldReplace
			} else {
				m.focus = fieldFind
			}
		}
		return m, nil
	case tea.KeyShiftTab:
		// Shift+Tab on FIND remains "previous match"; on REPLACE it shifts
		// focus back to find for symmetry with Tab.
		if m.replaceMode && m.focus == fieldReplace {
			m.focus = fieldFind
			return m, nil
		}
		return m, func() tea.Msg { return FindPrevMsg{} }
	case tea.KeyEnter:
		// Enter on the replace input fires a single replacement.
		// Ctrl+Enter (when the terminal sends it) fires "replace all".
		if m.focus == fieldReplace {
			find := m.input
			rep := m.replace
			// Ctrl+Enter often arrives as Alt-modified or a literal; treat the
			// alt modifier as "all" since most terminals can't send ctrl+enter.
			if k.Alt {
				return m, func() tea.Msg {
					return ReplaceAllMsg{Find: find, Replace: rep}
				}
			}
			return m, func() tea.Msg {
				return ReplaceMsg{Find: find, Replace: rep}
			}
		}
		return m, func() tea.Msg { return FindNextMsg{} }
	case tea.KeyUp:
		// Up on FIND input keeps "previous match" behaviour. On REPLACE,
		// hop focus to FIND (consistent w/ a vertical layout).
		if m.replaceMode && m.focus == fieldReplace {
			m.focus = fieldFind
			return m, nil
		}
		return m, func() tea.Msg { return FindPrevMsg{} }
	case tea.KeyDown:
		// Down on FIND input keeps "next match". On REPLACE, hop to find.
		if m.replaceMode && m.focus == fieldReplace {
			m.focus = fieldFind
			return m, nil
		}
		return m, func() tea.Msg { return FindNextMsg{} }
	case tea.KeyLeft:
		cp := m.activeCursor()
		if cp > 0 {
			m.setActiveCursor(cp - 1)
		}
		return m, nil
	case tea.KeyRight:
		runes := m.activeRunes()
		cp := m.activeCursor()
		if cp < len(runes) {
			m.setActiveCursor(cp + 1)
		}
		return m, nil
	case tea.KeyHome, tea.KeyCtrlA:
		m.setActiveCursor(0)
		return m, nil
	case tea.KeyEnd, tea.KeyCtrlE:
		m.setActiveCursor(len(m.activeRunes()))
		return m, nil
	case tea.KeyDelete:
		runes := m.activeRunes()
		cp := m.activeCursor()
		if cp < len(runes) {
			m.setActiveValue(string(append(runes[:cp], runes[cp+1:]...)))
			if m.focus == fieldFind {
				return m, fireSearch(m.input)
			}
			return m, nil
		}
		return m, nil
	case tea.KeyBackspace:
		runes := m.activeRunes()
		cp := m.activeCursor()
		if cp > 0 {
			m.setActiveValue(string(append(runes[:cp-1], runes[cp:]...)))
			m.setActiveCursor(cp - 1)
			if m.focus == fieldFind {
				return m, fireSearch(m.input)
			}
			return m, nil
		}
		return m, nil
	case tea.KeySpace:
		cp := m.activeCursor()
		if m.focus == fieldFind {
			m.input = insertAt(m.input, cp, " ")
		} else {
			m.replace = insertAt(m.replace, cp, " ")
		}
		m.setActiveCursor(cp + 1)
		if m.focus == fieldFind {
			return m, fireSearch(m.input)
		}
		return m, nil
	case tea.KeyRunes:
		s := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, ansi.Strip(string(k.Runes)))
		cp := m.activeCursor()
		if m.focus == fieldFind {
			m.input = insertAt(m.input, cp, s)
		} else {
			m.replace = insertAt(m.replace, cp, s)
		}
		m.setActiveCursor(cp + len([]rune(s)))
		if m.focus == fieldFind {
			return m, fireSearch(m.input)
		}
		return m, nil
	}
	return m, nil
}

// visibleWindow picks the rune range of `runes` to render given a query
// area of `width` cells and a cursor at rune index `cp`. It anchors the
// cursor at the right edge of the window when there's overflow on the
// left, then extends the window forward to use any remaining cells —
// matching the "type at the right edge, history scrolls left" behaviour
// of a normal text input.
func visibleWindow(runes []rune, cp, width int) (start, end int) {
	if width < 1 {
		return cp, cp
	}
	// Walk back from cursor filling at most `width` cells.
	accW := 0
	start = cp
	for start > 0 {
		rw := runewidth.RuneWidth(runes[start-1])
		if rw == 0 {
			rw = 1
		}
		if accW+rw > width {
			break
		}
		accW += rw
		start--
	}
	// Then extend forward through any leftover budget.
	end = cp
	for end < len(runes) {
		rw := runewidth.RuneWidth(runes[end])
		if rw == 0 {
			rw = 1
		}
		if accW+rw > width {
			break
		}
		accW += rw
		end++
	}
	return start, end
}

// insertAt inserts s into base at the given rune position. The position
// is clamped into [0, len(runes)] so callers don't have to.
func insertAt(base string, runePos int, s string) string {
	r := []rune(base)
	if runePos < 0 {
		runePos = 0
	}
	if runePos > len(r) {
		runePos = len(r)
	}
	out := make([]rune, 0, len(r)+len([]rune(s)))
	out = append(out, r[:runePos]...)
	out = append(out, []rune(s)...)
	out = append(out, r[runePos:]...)
	return string(out)
}

func fireSearch(q string) tea.Cmd {
	return func() tea.Msg { return SearchMsg{Query: q} }
}

// HandleMouse routes a mouse event through the find bar. Coordinates are
// in SCREEN cells. Clicks on glyph buttons (chevron, Aa, ab, .*, ↑, ↓,
// ×) emit corresponding commands; clicks on the replace-row icons
// emit ReplaceMsg / ReplaceAllMsg. Clicks elsewhere inside the panel are
// consumed silently; clicks outside return no command.
func (m Model) HandleMouse(x, y int, action tea.MouseAction, button tea.MouseButton) (Model, tea.Cmd) {
	bx, by, bw, bh := m.Bounds()
	if bw <= 0 || bh <= 0 {
		return m, nil
	}
	if x < bx || x >= bx+bw || y < by || y >= by+bh {
		return m, nil
	}
	if action != tea.MouseActionPress || button != tea.MouseButtonLeft {
		return m, nil
	}

	row := y - by
	switch m.ButtonAt(x, y) {
	case "chevron":
		m.replaceMode = !m.replaceMode
		if m.replaceMode {
			m.focus = fieldReplace
		} else {
			m.focus = fieldFind
		}
		return m, nil
	case "case":
		m.caseSensitive = !m.caseSensitive
		return m, fireSearch(m.input)
	case "word":
		m.wholeWord = !m.wholeWord
		return m, fireSearch(m.input)
	case "regex":
		m.useRegex = !m.useRegex
		return m, fireSearch(m.input)
	case "prev":
		return m, func() tea.Msg { return FindPrevMsg{} }
	case "next":
		return m, func() tea.Msg { return FindNextMsg{} }
	case "close":
		return m, func() tea.Msg { return CloseMsg{} }
	case "replace":
		find, rep := m.input, m.replace
		return m, func() tea.Msg { return ReplaceMsg{Find: find, Replace: rep} }
	case "replace-all":
		find, rep := m.input, m.replace
		return m, func() tea.Msg { return ReplaceAllMsg{Find: find, Replace: rep} }
	}
	// Find row sits at by+1; replace row at by+2 (only when expanded).
	switch {
	case row == 1:
		// Click on the find input area moves focus there.
		if m.replaceMode {
			m.focus = fieldFind
		}
	case row == m.replaceRow() && m.replaceMode:
		// Click on the replace input area moves focus there.
		m.focus = fieldReplace
	}
	return m, nil
}

// Button is a clickable glyph of the panel, in screen cells (one row).
type Button struct {
	ID      string // "chevron", "case", "word", "regex", "prev", "next", "close", "replace", "replace-all"
	X, Y, W int
}

// Buttons lists the panel's glyph buttons on screen (see Bounds).
func (m Model) Buttons() []Button {
	bx, by, bw, bh := m.Bounds()
	if bw <= 0 || bh <= 0 {
		return nil
	}
	var out []Button
	add := func(id string, col, row, w int) {
		if col >= 0 {
			out = append(out, Button{ID: id, X: bx + col, Y: by + row, W: w})
		}
	}
	chev, caseCol, word, regex, prev, next, closeCol := m.buttonColumnsFind()
	add("chevron", chev, 1, 1)
	optionRow := 1
	if m.compact() {
		optionRow = 2
	}
	add("case", caseCol, optionRow, 2)
	add("word", word, optionRow, 2)
	add("regex", regex, optionRow, 2)
	add("prev", prev, 1, 1)
	add("next", next, 1, 1)
	add("close", closeCol, 1, 1)
	one, all := m.buttonColumnsReplace()
	add("replace", one, m.replaceRow(), 1)
	add("replace-all", all, m.replaceRow(), 1)
	return out
}

// ButtonAt returns the id of the button at screen cell (x, y), or "".
func (m Model) ButtonAt(x, y int) string {
	for _, b := range m.Buttons() {
		if y == b.Y && x >= b.X && x < b.X+b.W {
			return b.ID
		}
	}
	return ""
}

// Find row layout (columns are panel-local, including the rounded border).
// VS-Code-style: clean groups separated by 2-cell gaps, count is its own
// muted text slot with a leading and trailing space so it never butts up
// against the next group (no more "0Aa").
//
//	border(1) + leadPad(1) + chevron(1) + chevGap(1) +
//	findInput(flex) + cursor(1) +
//	countGap(1) + countSlot(7) + groupGap(2) +
//	options(Aa + 1 + ab + 1 + .* = 8) + groupGap(2) +
//	nav(↑ + 1 + ↓ = 3) + groupGap(2) +
//	close(1) +
//	trailPad(1) + border(1)

const (
	fbLeadPad  = 1
	fbTrailPad = 1
	fbChevW    = 1
	fbChevGap  = 1
	fbCursorW  = 1

	// countGap sits between the cursor cell of the input well and the
	// count slot; countSlot itself renders with its own leading + trailing
	// space, so we keep this gap small (1 cell).
	fbCountGap   = 1
	fbCountSlotW = 7

	// groupGap is the standard 2-cell breather between unrelated control
	// groups. The user explicitly asked for 6–8 px of horizontal padding
	// which translates to 1–2 cells in a TUI; we use 2.
	fbGroupGap = 2

	// Options group: "Aa ab .*" = 2 + 1 + 2 + 1 + 2 = 8 cells.
	fbCaseW    = 2
	fbWordW    = 2
	fbRegexW   = 2
	fbOptInner = 1 // space between option glyphs
	fbOptionsW = fbCaseW + fbOptInner + fbWordW + fbOptInner + fbRegexW

	// Nav group: "↑ ↓" = 1 + 1 + 1 = 3 cells.
	fbPrevW    = 1
	fbNextW    = 1
	fbNavInner = 1
	fbNavW     = fbPrevW + fbNavInner + fbNextW

	fbCloseW = 1
)

// rightChromeWidth is the on-row width of everything right of the find
// input cursor: countGap + countSlot + groupGap + options + groupGap + nav
// + groupGap + close.
func rightChromeWidth() int {
	return fbCountGap + fbCountSlotW + fbGroupGap +
		fbOptionsW + fbGroupGap +
		fbNavW + fbGroupGap +
		fbCloseW
}

// buttonColumnsFind returns the panel-local column offsets (relative to
// the panel's left edge, i.e. the rounded border at col 0) for each glyph
// on the find row. Returns -1 across the board when the panel is too small.
//
// Column math MUST stay in lockstep with renderFindRow — the
// TestButtonColumnsMatchRenderedGlyphs guard verifies this.
func (m Model) buttonColumnsFind() (chev, caseCol, word, regex, prev, next, closeCol int) {
	pw := panelWidth(m.w)
	if pw < 12 {
		return -1, -1, -1, -1, -1, -1, -1
	}
	if m.compact() {
		return 1, 1, 4, 7, -1, -1, pw - 2
	}
	innerW := pw - 2
	if innerW < 8 {
		innerW = 8
	}

	rightW := rightChromeWidth()
	// findInputW consumes the leftover after fixed-width chrome.
	findInputW := innerW - fbLeadPad - fbChevW - fbChevGap - fbCursorW - rightW - fbTrailPad
	if findInputW < 1 {
		findInputW = 1
	}

	innerChev := fbLeadPad
	// Cursor sits at the right edge of the input well.
	cursorEnd := fbLeadPad + fbChevW + fbChevGap + findInputW + fbCursorW

	// The count slot itself is 7 cells; renderCount is responsible for the
	// leading/trailing whitespace inside the slot. The slot starts after a
	// 1-cell gap from the cursor, then a 2-cell groupGap before options.
	countStart := cursorEnd + fbCountGap
	optsStart := countStart + fbCountSlotW + fbGroupGap

	innerCase := optsStart
	innerWord := innerCase + fbCaseW + fbOptInner
	innerRegex := innerWord + fbWordW + fbOptInner

	navStart := innerRegex + fbRegexW + fbGroupGap
	innerPrev := navStart
	innerNext := innerPrev + fbPrevW + fbNavInner

	innerClose := innerNext + fbNextW + fbGroupGap
	// +1 shift to account for the rounded border.
	return innerChev + 1,
		innerCase + 1,
		innerWord + 1,
		innerRegex + 1,
		innerPrev + 1,
		innerNext + 1,
		innerClose + 1
}

// buttonColumns is the legacy 5-tuple compatible with old tests. It returns
// case / regex / prev / next / close columns (no chevron, no whole-word,
// no menu). New code should call buttonColumnsFind() instead.
func (m Model) buttonColumns() (caseCol, regexCol, prev, next, closeCol int) {
	_, c, _, r, p, n, x := m.buttonColumnsFind()
	return c, r, p, n, x
}

// buttonColumnsReplace returns panel-local column offsets for the
// replace-one and replace-all icons on the second row. Returns -1 when
// the panel is collapsed or too small.
//
// Layout (must match renderReplaceRow):
//
//	lead(1) + spacer(chevW + chevGap = 2) + replaceInput(=findInputW) +
//	cursor(1) + groupGap(2) + ↵(1) + 1 + ⇒(1) + trail(1)
func (m Model) buttonColumnsReplace() (oneCol, allCol int) {
	if !m.replaceMode {
		return -1, -1
	}
	pw := panelWidth(m.w)
	if pw < 12 {
		return -1, -1
	}
	// The icons sit against the right border in both layouts.
	return pw - 5, pw - 3
}

// ── styling ───────────────────────────────────────────────────────────────
var (
	PanelBg          lipgloss.Color
	PanelBorder      lipgloss.Color
	InputBg          lipgloss.Color
	TextPrimary      lipgloss.Color
	TextMuted        lipgloss.Color
	TextDim          lipgloss.Color
	BtnFg            lipgloss.Color
	BtnDim           lipgloss.Color
	AccentFocus      lipgloss.Color
	CountFg          lipgloss.Color
	WarnFg           lipgloss.Color
	bodyStyle        lipgloss.Style
	chevronStyle     lipgloss.Style
	chevronActive    lipgloss.Style
	inputBgStyle     lipgloss.Style
	queryStyle       lipgloss.Style
	placeholderStyle lipgloss.Style
	cursorStyle      lipgloss.Style
	countStyle       lipgloss.Style
	countWarnStyle   lipgloss.Style
	ctrlStyle        lipgloss.Style
	ctrlDim          lipgloss.Style
	closeStyle       lipgloss.Style
	toggleOff        lipgloss.Style
	toggleOnBg       lipgloss.Color
	toggleOn         lipgloss.Style
)

func init() { refreshColors() }

// refreshColors derives this file's colours from the active theme. It runs
// before every render so a theme switch applies immediately.
func refreshColors() {
	PanelBg = theme.Float().Bg
	PanelBorder = theme.Float().Border
	InputBg = theme.Float().InputBg
	TextPrimary = theme.Float().Title
	TextMuted = theme.Float().Hint
	TextDim = theme.Float().Dim
	BtnFg = theme.Float().Sub
	BtnDim = theme.Float().Dim
	AccentFocus = theme.Float().Icon
	CountFg = theme.Float().Sub
	WarnFg = theme.Float().Danger
	bodyStyle = lipgloss.NewStyle().Background(PanelBg)
	chevronStyle = lipgloss.NewStyle().
		Background(PanelBg).
		Foreground(BtnFg)
	chevronActive = lipgloss.NewStyle().
		Background(PanelBg).
		Foreground(AccentFocus).
		Bold(true)
	inputBgStyle = lipgloss.NewStyle().Background(InputBg)
	queryStyle = lipgloss.NewStyle().
		Background(InputBg).
		Foreground(TextPrimary)
	placeholderStyle = lipgloss.NewStyle().
		Background(InputBg).
		Foreground(TextDim).
		Italic(true)
	cursorStyle = lipgloss.NewStyle().
		Background(InputBg).
		Foreground(AccentFocus).
		Bold(true)
	countStyle = lipgloss.NewStyle().
		Background(PanelBg).
		Foreground(CountFg)
	countWarnStyle = lipgloss.NewStyle().
		Background(PanelBg).
		Foreground(WarnFg)
	ctrlStyle = lipgloss.NewStyle().
		Background(PanelBg).
		Foreground(BtnFg)
	ctrlDim = lipgloss.NewStyle().
		Background(PanelBg).
		Foreground(BtnDim)
	closeStyle = lipgloss.NewStyle().
		Background(PanelBg).
		Foreground(BtnFg)
	toggleOff = lipgloss.NewStyle().
		Background(PanelBg).
		Foreground(BtnFg)
	toggleOnBg = theme.Wash(theme.BgFloat, theme.WashPressed)
	toggleOn = lipgloss.NewStyle().
		Background(toggleOnBg).
		Foreground(TextPrimary).
		Bold(true)
}

// PanelWidth returns the cell width of the rendered floating panel.
func (m Model) PanelWidth() int { return panelWidth(m.w) }

// PanelHeight returns the cell height of the rendered floating panel.
//
//	Collapsed: 3 rows (top border + find row + bottom border)
//	Expanded:  5 rows (top border + find row + blank + replace row + bottom border)
func (m Model) compact() bool { return panelWidth(m.w) < 34 }
func (m Model) replaceRow() int {
	if m.compact() {
		return 4
	}
	return 3
}
func (m Model) PanelHeight() int {
	h := 3
	if m.compact() {
		h++
	}
	if m.replaceMode {
		h += 2
	}
	return h
}

// panelWidth picks a compact width: target ~64 cells (≈ 520-560px), but
// never wider than the available editor pane width minus a couple cells
// of breathing room.
func panelWidth(avail int) int {
	const target = 64
	const minW = 36
	if avail <= 0 {
		return target
	}
	w := target
	if w > avail-2 {
		w = avail - 2
	}
	if w < minW {
		w = minW
	}
	if w > avail {
		w = avail
	}
	return w
}

func (m Model) View() string {
	refreshColors()
	if m.w <= 0 {
		return ""
	}
	pw := panelWidth(m.w)
	if pw < 12 {
		return ""
	}
	innerW := pw - 2
	if innerW < 8 {
		innerW = 8
	}

	findRow := m.renderFindRow(innerW)
	if m.compact() {
		glyph := "▸"
		if m.replaceMode {
			glyph = "▾"
		}
		findRow = chevronStyle.Render(glyph) + m.renderInput(m.input, m.cursorPos, innerW-3, "Find", m.focus == fieldFind) + closeStyle.Render("×")
		option := func(text string, on bool) string {
			if on {
				return toggleOn.Render(text)
			}
			return toggleOff.Render(text)
		}
		opts := option("Aa", m.caseSensitive) + bodyStyle.Render(" ") + option("ab", m.wholeWord) + bodyStyle.Render(" ") + option(".*", m.useRegex)
		findRow += "\n" + opts + bodyStyle.Render(strings.Repeat(" ", innerW-8))
	}

	var inner string
	if m.replaceMode {
		blank := bodyStyle.Render(strings.Repeat(" ", innerW))
		replaceRow := m.renderReplaceRow(innerW)
		if m.compact() {
			replaceRow = m.renderInput(m.replace, m.replaceCursorPos, innerW-6, "Replace", m.focus == fieldReplace) + bodyStyle.Render(" ") + ctrlStyle.Render("↵ ⇒") + bodyStyle.Render(" ")
		}
		inner = findRow + "\n" + blank + "\n" + replaceRow
	} else {
		inner = findRow
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(PanelBorder).
		BorderBackground(PanelBg).
		Background(PanelBg).
		Render(inner)

	return box
}

// renderFindRow renders the top input row.
//
//	leadPad + chevron + chevGap + findInput(flex+cursor) +
//	countGap + countSlot + groupGap + options + groupGap + nav +
//	groupGap + close + trailPad
//
// Each region is rendered with a styled background (PanelBg or InputBg) so
// no bare spaces leak the terminal default colour through the overlay.
func (m Model) renderFindRow(innerW int) string {
	rightW := rightChromeWidth()

	findInputW := innerW - fbLeadPad - fbChevW - fbChevGap - fbCursorW - rightW - fbTrailPad
	if findInputW < 1 {
		findInputW = 1
	}

	// Chevron — points right when collapsed, down when expanded.
	chevGlyph := "▸"
	chevSty := chevronStyle
	if m.replaceMode {
		chevGlyph = "▾"
		chevSty = chevronActive
	}
	chev := chevSty.Render(chevGlyph)

	// Option toggles
	caseSty := toggleOff
	if m.caseSensitive {
		caseSty = toggleOn
	}
	wordSty := toggleOff
	if m.wholeWord {
		wordSty = toggleOn
	}
	regexSty := toggleOff
	if m.useRegex {
		regexSty = toggleOn
	}

	// Nav arrows dim when there's nothing to navigate.
	navSty := ctrlStyle
	if m.input == "" || m.matchCount == "0" || m.matchCount == "0/0" {
		navSty = ctrlDim
	}

	caseGlyph := caseSty.Render("Aa")
	wordGlyph := wordSty.Render("ab")
	regexGlyph := regexSty.Render(".*")
	prev := navSty.Render("↑")
	next := navSty.Render("↓")
	closeX := closeStyle.Render("×")

	optInner := bodyStyle.Render(strings.Repeat(" ", fbOptInner))
	navInner := bodyStyle.Render(strings.Repeat(" ", fbNavInner))
	groupGap := bodyStyle.Render(strings.Repeat(" ", fbGroupGap))

	optionsGroup := caseGlyph + optInner + wordGlyph + optInner + regexGlyph
	navGroup := prev + navInner + next

	// Find input region. renderInput returns exactly findInputW + cursor(1).
	inputRegion := m.renderInput(m.input, m.cursorPos, findInputW, "Find", m.focus == fieldFind)

	countCell := m.renderCount()

	leadPad := bodyStyle.Render(strings.Repeat(" ", fbLeadPad))
	chevGap := bodyStyle.Render(strings.Repeat(" ", fbChevGap))
	countGap := bodyStyle.Render(strings.Repeat(" ", fbCountGap))
	trailPad := bodyStyle.Render(strings.Repeat(" ", fbTrailPad))

	row := leadPad +
		chev + chevGap +
		inputRegion +
		countGap + countCell + groupGap +
		optionsGroup + groupGap +
		navGroup + groupGap +
		closeX +
		trailPad

	if w := lipgloss.Width(row); w < innerW {
		row += bodyStyle.Render(strings.Repeat(" ", innerW-w))
	}
	return row
}

// renderReplaceRow renders the second row. The replace input has the same
// width as the find input (so the wells column-align), and the icons (↵ ⇒)
// are pushed all the way to the right edge of the panel — flexible gap
// between input and icons fills whatever space is left.
//
//	leadPad + spacer(chevW+chevGap) + replaceInput(+cursor) +
//	flexGap + ↵ + 1 + ⇒ + trailPad
func (m Model) renderReplaceRow(innerW int) string {
	const replOneW = 1
	const replAllW = 1
	const innerIconGap = 1

	leftSpacerW := fbChevW + fbChevGap

	rightW := rightChromeWidth()
	findInputW := innerW - fbLeadPad - fbChevW - fbChevGap - fbCursorW - rightW - fbTrailPad
	if findInputW < 1 {
		findInputW = 1
	}

	disabled := m.input == "" || m.matchCount == "0" || m.matchCount == "0/0"
	iconSty := ctrlStyle
	iconAllSty := ctrlStyle.Bold(true)
	if disabled {
		iconSty = ctrlDim
		iconAllSty = ctrlDim.Bold(true)
	}
	replOne := iconSty.Render("↵")
	replAll := iconAllSty.Render("⇒")

	inputRegion := m.renderInput(m.replace, m.replaceCursorPos, findInputW, "Replace", m.focus == fieldReplace)
	inputRegionW := lipgloss.Width(inputRegion)

	leadPad := bodyStyle.Render(strings.Repeat(" ", fbLeadPad))
	leftSpacer := bodyStyle.Render(strings.Repeat(" ", leftSpacerW))
	iconGap := bodyStyle.Render(strings.Repeat(" ", innerIconGap))
	trailPad := bodyStyle.Render(strings.Repeat(" ", fbTrailPad))

	// Flex gap pushes the icons hard to the right edge (just before trailPad).
	iconsW := replOneW + innerIconGap + replAllW
	flex := innerW - fbLeadPad - leftSpacerW - inputRegionW - iconsW - fbTrailPad
	if flex < 1 {
		flex = 1
	}
	flexGap := bodyStyle.Render(strings.Repeat(" ", flex))

	row := leadPad + leftSpacer + inputRegion +
		flexGap + replOne + iconGap + replAll +
		trailPad

	if w := lipgloss.Width(row); w < innerW {
		row += bodyStyle.Render(strings.Repeat(" ", innerW-w))
	}
	return row
}

// renderInput draws an embedded input "well": placeholder when empty, or the
// query with a cursor positioned at `cp`. Width contract: returns exactly
// `areaW + 1` cells (the +1 is the cursor cell sitting inside the well).
func (m Model) renderInput(value string, cp, areaW int, placeholder string, focused bool) string {
	regionW := areaW + fbCursorW

	cursorGlyph := "▎"
	if !focused || !m.cursorVisible {
		cursorGlyph = " "
	}
	cursor := cursorStyle.Render(cursorGlyph)

	if value == "" {
		ph := placeholder
		if runewidth.StringWidth(ph) > areaW {
			ph = runewidth.Truncate(ph, areaW, "")
		}
		region := cursor + placeholderStyle.Render(ph)
		if pad := regionW - lipgloss.Width(region); pad > 0 {
			region += inputBgStyle.Render(strings.Repeat(" ", pad))
		}
		return region
	}

	runes := []rune(value)
	if cp < 0 {
		cp = 0
	}
	if cp > len(runes) {
		cp = len(runes)
	}
	winStart, winEnd := visibleWindow(runes, cp, areaW)
	before := string(runes[winStart:cp])
	after := string(runes[cp:winEnd])
	region := queryStyle.Render(before) + cursor + queryStyle.Render(after)
	if pad := regionW - lipgloss.Width(region); pad > 0 {
		region += inputBgStyle.Render(strings.Repeat(" ", pad))
	}
	return region
}

// renderCount renders the match-count slot, fixed at fbCountSlotW cells.
// The text is centred-ish with at least 1 cell of padding on each side so
// it never visually butts against neighbouring chrome (that's the bug the
// user reported as "0Aa"). Empty match count → an empty slot of bg cells.
// "0" / "0/0" / "0/00…" render in WarnFg; non-zero in CountFg.
func (m Model) renderCount() string {
	slot := bodyStyle.Render(strings.Repeat(" ", fbCountSlotW))
	if m.matchCount == "" {
		return slot
	}
	shown := m.matchCount
	// Reserve 1 cell of leading + trailing whitespace inside the slot.
	maxText := fbCountSlotW - 2
	if maxText < 1 {
		maxText = 1
	}
	if runewidth.StringWidth(shown) > maxText {
		shown = runewidth.Truncate(shown, maxText, "")
	}
	style := countStyle
	if shown == "0" || strings.HasPrefix(shown, "0/") {
		style = countWarnStyle
	}
	textW := runewidth.StringWidth(shown)
	// Right-align text within the slot, but ALWAYS keep at least 1 trailing
	// space so the count never touches the next group's first glyph.
	leftPad := fbCountSlotW - textW - 1
	if leftPad < 1 {
		leftPad = 1
	}
	rightPad := fbCountSlotW - textW - leftPad
	if rightPad < 1 {
		rightPad = 1
		leftPad = fbCountSlotW - textW - rightPad
		if leftPad < 0 {
			leftPad = 0
		}
	}
	return bodyStyle.Render(strings.Repeat(" ", leftPad)) +
		style.Render(shown) +
		bodyStyle.Render(strings.Repeat(" ", rightPad))
}
