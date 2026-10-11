package preview

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

// CloseMsg is emitted when the user dismisses the preview.
type CloseMsg struct{}

type Model struct {
	title string
	lines []string
	top   int
	w, h  int
}

// New takes already-styled content (e.g. glamour output) and stores it
// for scrollable display.
func New(title, body string) Model {
	body = strings.TrimRight(body, "\n")
	return Model{title: title, lines: strings.Split(body, "\n")}
}

func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	m.clamp()
}

func (m *Model) clamp() {
	max := len(m.lines) - m.contentHeight()
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
		m.top = len(m.lines)
	case tea.KeyRunes:
		if len(k.Runes) == 1 {
			switch k.Runes[0] {
			case 'q':
				return m, func() tea.Msg { return CloseMsg{} }
			case 'j':
				m.top++
			case 'k':
				m.top--
			case 'g':
				m.top = 0
			case 'G':
				m.top = len(m.lines)
			}
		}
	}
	m.clamp()
	return m, nil
}

func (m Model) handleMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.MouseWheelUp:
		m.top -= 3
	case tea.MouseWheelDown:
		m.top += 3
	}
	m.clamp()
	return m, nil
}

var (
	bgPanel lipgloss.Color
	bgHeader lipgloss.Color
	titleStyle lipgloss.Style
	hintStyle lipgloss.Style
	bodyFill lipgloss.Style
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
}

func (m Model) View() string {
	refreshColors()
	if m.w <= 0 || m.h <= 0 {
		return ""
	}

	// Truncate the title so a long "diff · <long/path>" can't wrap the header
	// onto a second row and shift the whole frame down.
	title := " " + m.title + " "
	if ansi.StringWidth(title) > m.w {
		title = ansi.Truncate(title, m.w, "…")
	}
	header := titleStyle.Width(m.w).Render(title)

	contentH := m.contentHeight()
	var contentLines []string
	end := m.top + contentH
	if end > len(m.lines) {
		end = len(m.lines)
	}
	for i := m.top; i < end; i++ {
		line := m.lines[i]
		// glamour's output sometimes leaves an unterminated SGR sequence
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
	if len(m.lines) > 0 {
		visible := end
		progress = scrollIndicator(visible, len(m.lines))
	}
	footerLeft := " esc · q  close"
	footerRight := "↑↓ scroll  · g/G  top/bottom  · " + progress + " "
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
