package search

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// SelectMsg fires when the user activates a result.
type SelectMsg struct {
	Path string
	Line int
}

// CloseMsg fires when the user dismisses the overlay.
type CloseMsg struct{}

type tickMsg struct {
	version int
}

type resultsMsg struct {
	version int
	results []Result
	err     error
}

type Model struct {
	input       string
	results     []Result
	cursor      int
	w, h        int
	queryVer    int // increments on every keystroke; latest fires the run
	pending     bool
	rgAvailable bool
	lastErr     string
	// roots is the list of directories ripgrep searches across. Empty
	// means "use cwd" (single-root behaviour). Set via SetRoots before
	// the search overlay opens to enable multi-root search.
	roots []string
}

const debounce = 150 * time.Millisecond

func New() Model {
	return Model{rgAvailable: Available()}
}

func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// SetRoots configures the directory list ripgrep walks. Empty/nil reverts
// to cwd. Each invocation of the search overlay should call SetRoots once
// (before the user starts typing) so the debounced spawn picks up the
// caller's workspace shape.
func (m *Model) SetRoots(roots []string) {
	if len(roots) == 0 {
		m.roots = nil
		return
	}
	m.roots = append(m.roots[:0], roots...)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if msg.version != m.queryVer {
			return m, nil
		}
		query := m.input
		ver := m.queryVer
		// Snapshot roots so the spawned goroutine doesn't race with a
		// concurrent SetRoots call from the UI thread.
		roots := append([]string(nil), m.roots...)
		m.pending = true
		return m, func() tea.Msg {
			if len(roots) == 0 {
				cwd, _ := os.Getwd()
				roots = []string{cwd}
			}
			results, err := RunDirs(roots, query)
			return resultsMsg{version: ver, results: results, err: err}
		}
	case resultsMsg:
		if msg.version != m.queryVer {
			return m, nil
		}
		m.pending = false
		m.results = msg.results
		m.cursor = 0
		if msg.err != nil {
			m.lastErr = msg.err.Error()
		} else {
			m.lastErr = ""
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		return m, func() tea.Msg { return CloseMsg{} }
	case tea.KeyEnter:
		if m.cursor >= 0 && m.cursor < len(m.results) {
			r := m.results[m.cursor]
			return m, func() tea.Msg { return SelectMsg{Path: r.Path, Line: r.Line} }
		}
		return m, nil
	case tea.KeyDown, tea.KeyCtrlN:
		if m.cursor < len(m.results)-1 {
			m.cursor++
		}
		return m, nil
	case tea.KeyUp, tea.KeyCtrlP:
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case tea.KeyBackspace:
		if len(m.input) > 0 {
			r := []rune(m.input)
			m.input = string(r[:len(r)-1])
			return m, m.scheduleSearch()
		}
		return m, nil
	case tea.KeySpace:
		m.input += " "
		return m, m.scheduleSearch()
	case tea.KeyRunes:
		m.input += string(k.Runes)
		return m, m.scheduleSearch()
	}
	return m, nil
}

func (m *Model) scheduleSearch() tea.Cmd {
	m.queryVer++
	v := m.queryVer
	if strings.TrimSpace(m.input) == "" {
		m.results = nil
		m.lastErr = ""
		return nil
	}
	return tea.Tick(debounce, func(time.Time) tea.Msg {
		return tickMsg{version: v}
	})
}

var (
	bgBox    = lipgloss.Color("#262626")
	bgSel    = lipgloss.Color("#094771")
	bgFill   = lipgloss.NewStyle().Background(bgBox)
	border   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#3a3a3a")).Background(bgBox)
	titleS   = lipgloss.NewStyle().Background(bgBox).Foreground(lipgloss.Color("#ffffff")).Bold(true)
	inputS   = lipgloss.NewStyle().Background(lipgloss.Color("#1c1c1c")).Foreground(lipgloss.Color("#ffffff"))
	hintS    = lipgloss.NewStyle().Background(bgBox).Foreground(lipgloss.Color("#858585"))
	pathS    = lipgloss.NewStyle().Background(bgBox).Foreground(lipgloss.Color("#9cdcfe"))
	pathSel  = lipgloss.NewStyle().Background(bgSel).Foreground(lipgloss.Color("#ffffff")).Bold(true)
	prevS    = lipgloss.NewStyle().Background(bgBox).Foreground(lipgloss.Color("#cccccc"))
	prevSel  = lipgloss.NewStyle().Background(bgSel).Foreground(lipgloss.Color("#ffffff"))
	matchS   = lipgloss.NewStyle().Background(bgBox).Foreground(lipgloss.Color("#f9c859")).Bold(true)
	matchSel = lipgloss.NewStyle().Background(bgSel).Foreground(lipgloss.Color("#f9c859")).Bold(true)
	lineS    = lipgloss.NewStyle().Background(bgBox).Foreground(lipgloss.Color("#dcdcaa"))
	lineSel  = lipgloss.NewStyle().Background(bgSel).Foreground(lipgloss.Color("#dcdcaa")).Bold(true)
	errStyle = lipgloss.NewStyle().Background(bgBox).Foreground(lipgloss.Color("#f14c4c")).Bold(true)
)

func (m Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	boxW := m.w - 6
	if boxW > 110 {
		boxW = 110
	}
	if boxW < 40 {
		boxW = 40
	}
	boxH := m.h - 4
	if boxH > 30 {
		boxH = 30
	}
	if boxH < 8 {
		boxH = 8
	}
	box := m.renderBox(boxW, boxH)
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("#1c1c1c")),
	)
}

func (m Model) renderBox(w, h int) string {
	innerW := w - 2

	title := titleS.Width(innerW).Render(" Find in Files")
	cursor := "▍"
	inputBody := " > " + m.input + cursor
	pad := innerW - lipgloss.Width(inputBody)
	if pad < 0 {
		pad = 0
	}
	input := inputS.Render(inputBody + strings.Repeat(" ", pad))

	innerH := h - 2
	available := innerH - 4 // title + input + summary + footer
	if available < 1 {
		available = 1
	}

	var summary string
	switch {
	case !m.rgAvailable:
		summary = errStyle.Render(" ripgrep (rg) not installed ")
	case m.lastErr != "":
		summary = errStyle.Render(" " + m.lastErr)
	case m.pending:
		summary = hintS.Render(" searching... ")
	case strings.TrimSpace(m.input) == "":
		summary = hintS.Render(" type to search ")
	case len(m.results) == 0:
		summary = hintS.Render(" no results ")
	default:
		summary = hintS.Render(fmt.Sprintf(" %d results ", len(m.results)))
	}
	summary = padBg(summary, innerW)

	rows := m.renderResults(available, innerW)

	footer := hintS.Width(innerW).Render(" ↑↓ navigate · enter open · esc close ")

	return border.Render(lipgloss.JoinVertical(lipgloss.Left, title, input, summary, lipgloss.JoinVertical(lipgloss.Left, rows...), footer))
}

func (m Model) renderResults(rows, w int) []string {
	out := make([]string, 0, rows)
	if rows < 1 {
		return out
	}
	if len(m.results) == 0 {
		for i := 0; i < rows; i++ {
			out = append(out, bgFill.Render(strings.Repeat(" ", w)))
		}
		return out
	}

	start := 0
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	end := start + rows
	if end > len(m.results) {
		end = len(m.results)
	}
	for i := start; i < end; i++ {
		out = append(out, m.renderRow(m.results[i], i == m.cursor, w))
	}
	for len(out) < rows {
		out = append(out, bgFill.Render(strings.Repeat(" ", w)))
	}
	return out
}

// renderRow lays out a single result as two lines:
//
//	  relpath:line:col
//	    snippet (with each matched span bolded)
//
// `active` selects the highlight palette so the cursor row stands out. The
// row is padded to exactly width `w` so background colours fill the box.
func (m Model) renderRow(r Result, active bool, w int) string {
	pathStyle := pathS
	prevStyle := prevS
	lineStyle := lineS
	matchStyle := matchS
	if active {
		pathStyle = pathSel
		prevStyle = prevSel
		lineStyle = lineSel
		matchStyle = matchSel
	}

	// Header line: "  relpath  :line:col"
	loc := fmt.Sprintf("  :%d:%d", r.Line, r.Col)
	maxLeft := w - lipgloss.Width(loc) - 2
	if maxLeft < 8 {
		maxLeft = 8
	}
	relpath := runewidth.Truncate(r.Path, maxLeft, "…")
	body := " " + relpath
	bodyW := lipgloss.Width(body) + lipgloss.Width(loc)
	pad := w - bodyW
	if pad < 0 {
		pad = 0
	}
	header := pathStyle.Render(body) + lineStyle.Render(loc) + pathStyle.Render(strings.Repeat(" ", pad))

	// Snippet line: indent 4, then preview with matched spans bolded.
	indent := "    "
	maxSnippet := w - len(indent)
	if maxSnippet < 4 {
		maxSnippet = 4
	}
	snippet := renderHighlighted(r.Preview, r.Matches, prevStyle, matchStyle, maxSnippet)
	previewLine := prevStyle.Render(indent) + snippet
	pp := w - lipgloss.Width(previewLine)
	if pp > 0 {
		previewLine += prevStyle.Render(strings.Repeat(" ", pp))
	}

	return header + "\n" + previewLine
}

// renderHighlighted paints `preview` so each MatchRange (in raw byte
// offsets) is wrapped with `matchStyle` and the rest with `baseStyle`. The
// result is truncated to `maxW` visual cells with an ellipsis. Ranges that
// fall after truncation are dropped silently.
func renderHighlighted(preview string, matches []MatchRange, baseStyle, matchStyle lipgloss.Style, maxW int) string {
	// Skip leading whitespace for readability; track the offset so byte
	// ranges from rg still line up.
	trimmed := strings.TrimLeft(preview, " \t")
	skipped := len(preview) - len(trimmed)

	if len(matches) == 0 {
		return baseStyle.Render(runewidth.Truncate(trimmed, maxW, "…"))
	}

	var out strings.Builder
	used := 0
	cursor := 0
	for _, mr := range matches {
		s := mr.Start - skipped
		e := mr.End - skipped
		if e <= cursor || s >= len(trimmed) {
			continue
		}
		if s < cursor {
			s = cursor
		}
		if s > e {
			continue
		}
		// Emit base text up to the match.
		if s > cursor {
			seg := trimmed[cursor:s]
			seg, w := truncateBudget(seg, maxW-used)
			out.WriteString(baseStyle.Render(seg))
			used += w
			if used >= maxW {
				return out.String()
			}
		}
		// Emit the match span itself.
		if e > len(trimmed) {
			e = len(trimmed)
		}
		seg := trimmed[s:e]
		seg, w := truncateBudget(seg, maxW-used)
		out.WriteString(matchStyle.Render(seg))
		used += w
		if used >= maxW {
			return out.String()
		}
		cursor = e
	}
	// Tail.
	if cursor < len(trimmed) {
		seg := trimmed[cursor:]
		seg, w := truncateBudget(seg, maxW-used)
		out.WriteString(baseStyle.Render(seg))
		used += w
		_ = used
	}
	return out.String()
}

// truncateBudget returns the longest prefix of `s` that fits in `budget`
// visual cells, plus the actual width of that prefix. When the input
// exceeds the budget the returned string ends with an ellipsis (…).
func truncateBudget(s string, budget int) (string, int) {
	if budget <= 0 {
		return "", 0
	}
	if runewidth.StringWidth(s) <= budget {
		return s, runewidth.StringWidth(s)
	}
	out := runewidth.Truncate(s, budget, "…")
	return out, runewidth.StringWidth(out)
}

func padBg(s string, w int) string {
	pad := w - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	return s + bgFill.Render(strings.Repeat(" ", pad))
}
