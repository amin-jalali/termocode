package preview

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/mdview"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

// CloseMsg is emitted when the user dismisses the preview.
type CloseMsg struct{}

type Model struct {
	title string
	badge string
	lines []string
	doc   *Doc // set by NewReader: content rendered per width
	top   int
	w, h  int

	icon    string // header icon set by SetHeader (replaces the file icon)
	iconSet bool
	hints   string // footer keys set by SetHints ("" = the default footer)
	cur     int    // focused link (mdview.Link.ID), 0 = none
}

// Doc is content that renders itself for a width (the Markdown reader
// view). It keeps the last render and only renders again when the width,
// the theme, the icon mode or the colour profile changes, so scrolling
// never re-renders. Model values share one Doc through the pointer.
type Doc struct {
	render func(width int) ([]string, mdview.Marks)
	w      int
	key    theme.RenderKey
	lines  []string
	marks  mdview.Marks
}

// NewDoc wraps a render function. Rows it returns should be exactly
// width cells wide.
func NewDoc(render func(width int) []string) *Doc {
	return &Doc{render: func(w int) ([]string, mdview.Marks) { return render(w), mdview.Marks{} }}
}

// NewMarkedDoc wraps a render function that also says where headings
// and links are (mdview.RenderDoc): the reader can then scroll to an
// anchor and follow links.
func NewMarkedDoc(render func(width int) ([]string, mdview.Marks)) *Doc {
	return &Doc{render: render}
}

// Lines returns the rows for width, rendering only when needed.
func (d *Doc) Lines(width int) []string {
	k := theme.CurrentRenderKey()
	if d.lines == nil || d.w != width || d.key != k {
		d.lines, d.marks = d.render(max(width, 1))
		d.w, d.key = width, k
		if d.lines == nil {
			d.lines = []string{}
		}
	}
	return d.lines
}

// Marks returns the marks of the last render (call Lines first).
func (d *Doc) Marks() mdview.Marks { return d.marks }

// NewReader shows doc full screen with title and an optional badge
// ("Preview") in the header.
func NewReader(title, badge string, doc *Doc) Model {
	return Model{title: title, badge: badge, doc: doc}
}

// content is the rows to show: the Doc's render for the current width,
// or the fixed body.
func (m Model) content() []string {
	if m.doc != nil {
		return m.doc.Lines(m.w)
	}
	return m.lines
}

// New takes already-styled content (e.g. a coloured diff) and stores it
// for scrollable display.
func New(title, body string) Model {
	body = strings.TrimRight(body, "\n")
	// Expand tabs up front: lipgloss renders a tab as 4 spaces, so a
	// width measured before rendering would be 3 cells short per tab and
	// the row would end up wider than the screen.
	body = strings.ReplaceAll(body, "\t", "    ")
	return Model{title: title, lines: strings.Split(body, "\n")}
}

func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	m.clamp()
	m.dropHiddenLink()
}

func (m *Model) clamp() {
	max := len(m.content()) - m.contentHeight()
	if max < 0 {
		max = 0
	}
	if m.top > max {
		m.top = max
	}
	if m.top < 0 {
		m.top = 0
	}
}

func (m Model) contentHeight() int {
	// 1 title row + 1 footer row; there are no borders.
	return max(m.h-2, 0)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m, nil
}

func (m Model) handleKey(k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		return m, func() tea.Msg { return CloseMsg{} }
	case tea.KeyDown:
		m.top++
	case tea.KeyUp:
		m.top--
	case tea.KeyPgDown, tea.KeySpace:
		m.top += m.contentHeight()
	case tea.KeyPgUp:
		m.top -= m.contentHeight()
	case tea.KeyHome:
		m.top = 0
	case tea.KeyEnd:
		m.top = len(m.content())
	case tea.KeyTab:
		m.cycleLink(1)
		return m, nil
	case tea.KeyShiftTab:
		m.cycleLink(-1)
		return m, nil
	case tea.KeyEnter:
		if l, ok := m.focusedLink(); ok {
			return m, linkCmd(l.Dest)
		}
		return m, nil
	case tea.KeyBackspace:
		return m, func() tea.Msg { return BackMsg{} }
	case tea.KeyRunes:
		if len(k.Runes) == 1 {
			switch k.Runes[0] {
			case 'q':
				return m, func() tea.Msg { return CloseMsg{} }
			case 'u':
				return m, func() tea.Msg { return BackMsg{} }
			case 'j':
				m.top++
			case 'k':
				m.top--
			case 'g':
				m.top = 0
			case 'G':
				m.top = len(m.content())
			}
		}
	}
	m.clamp()
	m.dropHiddenLink()
	return m, nil
}

func (m Model) handleMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		if l, ok := m.LinkAt(msg.X, msg.Y); ok {
			m.cur = l.ID
			return m, linkCmd(l.Dest)
		}
		return m, nil
	}
	switch msg.Type {
	case tea.MouseWheelUp:
		m.top -= 3
	case tea.MouseWheelDown:
		m.top += 3
	}
	m.clamp()
	m.dropHiddenLink()
	return m, nil
}

var (
	bgPanel    lipgloss.Color
	bgHeader   lipgloss.Color
	titleStyle lipgloss.Style
	hintStyle  lipgloss.Style
	bodyFill   lipgloss.Style
	linkSel    lipgloss.Style
)

func init() { refreshColors() }

// refreshColors derives this file's colours from the active theme. It runs
// before every render so a theme switch applies immediately.
func refreshColors() {
	bgPanel = theme.LG(theme.BgEditor)
	bgHeader = theme.LG(theme.BgSidebar)
	titleStyle = lipgloss.NewStyle().Background(bgHeader).Foreground(theme.LG(theme.TextWhite)).Bold(true)
	hintStyle = lipgloss.NewStyle().Background(bgHeader).Foreground(theme.LG(theme.TextMuted))
	bodyFill = lipgloss.NewStyle().Background(bgPanel)
	linkSel = lipgloss.NewStyle().Background(theme.Wash(theme.BgEditor, theme.WashSelectedFocused)).
		Foreground(theme.LG(theme.TextWhite)).Underline(true)
}

func (m Model) View() string {
	refreshColors()
	if m.w <= 0 || m.h <= 0 {
		return ""
	}

	header := m.header()
	lines := m.content()
	contentH := m.contentHeight()
	top := min(m.top, max(len(lines)-contentH, 0)) // a re-render may have fewer rows
	var contentLines []string
	end := top + contentH
	if end > len(lines) {
		end = len(lines)
	}
	for i := top; i < end; i++ {
		line := lines[i]
		if m.doc != nil && ansi.StringWidth(line) == m.w {
			if m.cur != 0 {
				line = m.highlightLinks(line, i)
			}
			contentLines = append(contentLines, line) // exact rows: no re-wrap per frame
			continue
		}
		// Styled input sometimes leaves an unterminated SGR sequence
		// at the end of a line (open background, particularly inside a
		// table block), which leaks the last cell's bg colour into the
		// padding we add after it. We bracket every line in a hard
		// reset + body-bg fill so each row stops at the body bg and
		// extends cleanly to m.w cells.
		const reset = "\x1b[0m"
		w := lipgloss.Width(line)
		switch {
		case w > m.w:
			// Truncate over-wide lines (a long minified-code / string diff
			// line) so the row never exceeds m.w cells. An overflowing row
			// wraps in the terminal and corrupts the alt-screen frame —
			// which is what left a stray column / scrollbar gutter showing
			// after the preview closed.
			line = ansi.Truncate(line, m.w, "") + reset
		case w < m.w:
			line = line + reset + bodyFill.Render(strings.Repeat(" ", m.w-w))
		}
		// Wrap the whole row so a stray open-fg from inside the line
		// can't bleed past the right edge into the next row either.
		contentLines = append(contentLines, bodyFill.Render(line+reset))
	}
	for len(contentLines) < contentH {
		contentLines = append(contentLines, bodyFill.Render(strings.Repeat(" ", m.w)))
	}

	progress := ""
	if len(lines) > 0 {
		visible := end
		progress = scrollIndicator(visible, len(lines))
	}
	footerLeft := " esc · q  close"
	footerRight := "↑↓ scroll  · g/G  top/bottom  · " + progress + " "
	if m.hints != "" { // the guide reader
		footerLeft, footerRight = m.hints, "↑↓ scroll · "+progress+" "
		if m.w < 60 {
			footerLeft, footerRight = " esc close", progress+" "
		}
	}
	footerW := m.w - lipgloss.Width(footerLeft) - lipgloss.Width(footerRight)
	if footerW < 0 { // narrow: keep only the position
		footerRight = progress + " "
		footerW = max(m.w-lipgloss.Width(footerLeft)-lipgloss.Width(footerRight), 0)
	}
	footer := hintStyle.Render(footerLeft) + hintStyle.Render(strings.Repeat(" ", footerW)) + hintStyle.Render(footerRight)
	if lipgloss.Width(footer) > m.w { // narrow window: never wrap
		footer = ansi.Truncate(footer, m.w, "")
	}

	rows := append([]string{header}, append(contentLines, footer)...)
	return strings.Join(rows[:min(len(rows), m.h)], "\n")
}

// header is the title row. The reader view shows the file icon, the
// name in bold and a small badge; other content just the title.
func (m Model) header() string {
	if m.doc == nil {
		// Truncate the title so a long "diff · <long/path>" can't wrap the
		// header onto a second row and shift the whole frame down.
		title := " " + m.title + " "
		if ansi.StringWidth(title) > m.w {
			title = ansi.Truncate(title, m.w, "…")
		}
		return titleStyle.Width(m.w).Render(title)
	}
	s := hintStyle.Render(" ")
	if m.iconSet {
		if m.icon != "" {
			s += lipgloss.NewStyle().Background(bgHeader).Foreground(theme.LG(theme.AccentBlue)).Render(m.icon) + hintStyle.Render(" ")
		}
	} else if g, c := theme.FileIconFor(m.title, false, false); g != "" && theme.CurrentIconMode != theme.IconModeASCII {
		s += lipgloss.NewStyle().Background(bgHeader).Foreground(theme.LG(c)).Render(g) + hintStyle.Render(" ")
	}
	s += titleStyle.Render(m.title)
	if m.badge != "" {
		badge := lipgloss.NewStyle().Background(theme.Mix(theme.BgSidebar, theme.AccentBlue, 0.22)).
			Foreground(theme.LG(theme.AccentBlue)).Render(" " + m.badge + " ")
		s += hintStyle.Render("  ") + badge
	}
	w := ansi.StringWidth(s)
	if w > m.w {
		return ansi.Truncate(s, m.w, "") + "\x1b[0m"
	}
	return s + hintStyle.Render(strings.Repeat(" ", m.w-w))
}

func scrollIndicator(visible, total int) string {
	if total == 0 {
		return ""
	}
	pct := visible * 100 / total
	if pct > 100 {
		pct = 100
	}
	return formatPct(pct)
}

func formatPct(p int) string {
	switch {
	case p <= 0:
		return "Top"
	case p >= 100:
		return "Bot"
	}
	return itoa(p) + "%"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
