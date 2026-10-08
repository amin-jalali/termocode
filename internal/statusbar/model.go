package statusbar

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"termocode/internal/theme"
)

type State struct {
	Path     string // active buffer path; only used as a "has-a-buffer" probe now
	Project  string // workspace / project basename rendered in the left zone
	Lang     string
	Line     int // 1-based
	Col      int // 1-based
	Dirty    bool
	Err      string
	Errors   int // LSP error count
	Warnings int // LSP warning count
	Branch   string
	Ahead    int
	Behind   int
	Indent   string // e.g. "Spaces: 4" or "Tab Size: 4"
	Encoding string // e.g. "UTF-8"
	Term     bool   // integrated terminal panel is open
	Debug    bool   // a Debug Adapter Protocol session is active

	// LSP chip: ShowLSP turns it on (a file buffer is open); LSP lists the
	// attached client names ("{} gopls"), empty → dim "{} none".
	ShowLSP bool
	LSP     []string

	// Group E: test chip "✓ n ✗ n" in the center (after a run; "◐" while
	// running). Clicking it opens the Testing view (TestsChipSpan).
	ShowTests    bool
	TestsRunning bool
	TestsPassed  int
	TestsFailed  int
}

type Model struct {
	w int
}

// New keeps the legacy constructor signature so existing callers compile;
// the bar styles itself directly from the palette and ignores the argument.
func New(_ theme.Theme) Model { return Model{} }

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) { return m, nil }

func (m *Model) SetWidth(w int) { m.w = w }

// barBgColor is the dark neutral the entire status bar paints. We use
// BgPanel rather than BgStatusBar so the bar tucks into the dark IDE
// chrome instead of glowing VSCode-blue. Every other style on the bar
// layers a foreground color on top of this background.
func barBgColor() theme.Color256 { return theme.BgPanel }

func barBg() lipgloss.Style { return theme.Bg(barBgColor()) }
func barFg(c theme.Color256) lipgloss.Style {
	return lipgloss.NewStyle().Background(theme.LG(barBgColor())).Foreground(theme.LG(c))
}

// dim grey vertical pipe used to subdivide the right-hand metadata
// cluster (Ln/Col │ indent │ encoding │ language).
const sepGlyph = "│"

func sep() string {
	return barFg(theme.BorderDefault).Render(" " + sepGlyph + " ")
}

// barLayout is the fitted content of one status-bar row.
type barLayout struct {
	left, center, right    string
	leftW, centerW, rightW int
	gapL, gapR             int
	chipStart, chipW       int // LSP chip offset inside right (chipW 0 = none)
}

// layout fits left / center / right into the bar width (see View).
func (m Model) layout(s State) barLayout {
	var l barLayout
	l.left = m.renderLeft(s)
	l.center = m.renderCenter(s)
	l.right, l.chipStart, l.chipW = m.renderRightChip(s)

	l.leftW = lipgloss.Width(l.left)
	l.centerW = lipgloss.Width(l.center)
	l.rightW = lipgloss.Width(l.right)

	innerW := m.w - 2 // subtract left+right edge pad
	if innerW < 0 {
		innerW = 0
	}

	// Drop center first if everything won't fit, then truncate right and
	// left in turn so the bar never overflows m.w.
	if l.leftW+l.centerW+l.rightW > innerW {
		l.center, l.centerW = "", 0
	}
	if l.leftW+l.rightW > innerW {
		// Truncate the right cluster first — the filename on the left
		// is more critical than the trailing metadata when space is
		// tight.
		avail := innerW - l.leftW
		if avail < 0 {
			avail = 0
		}
		l.right = truncateStyled(l.right, avail)
		l.rightW = lipgloss.Width(l.right)
	}
	if l.leftW+l.rightW > innerW {
		avail := innerW - l.rightW
		if avail < 0 {
			avail = 0
		}
		l.left = truncateStyled(l.left, avail)
		l.leftW = lipgloss.Width(l.left)
	}
	// A chip cut by truncation is not clickable.
	if l.chipStart+l.chipW > l.rightW {
		l.chipW = 0
	}

	contentW := l.leftW + l.centerW + l.rightW
	gap := innerW - contentW
	if gap < 0 {
		gap = 0
	}
	l.gapL = gap / 2
	l.gapR = gap - l.gapL
	return l
}

func (m Model) View(s State) string {
	if m.w <= 0 {
		return ""
	}
	pad := barBg().Render(" ")
	l := m.layout(s)
	leftSpace := barBg().Render(strings.Repeat(" ", l.gapL))
	rightSpace := barBg().Render(strings.Repeat(" ", l.gapR))

	body := lipgloss.JoinHorizontal(
		lipgloss.Top,
		pad, l.left, leftSpace, l.center, rightSpace, l.right, pad,
	)
	// Force the row to exactly m.w cells from lipgloss's perspective so
	// JoinVertical above can't pad it with default-styled spaces.
	return lipgloss.NewStyle().
		Width(m.w).
		Background(theme.LG(barBgColor())).
		Render(body)
}

// LSPChipSpan returns the [x0, x1) cell range of the LSP chip relative to
// the bar's left edge; ok=false when the chip is hidden or truncated.
func (m Model) LSPChipSpan(s State) (x0, x1 int, ok bool) {
	if m.w <= 0 {
		return 0, 0, false
	}
	l := m.layout(s)
	if l.chipW == 0 {
		return 0, 0, false
	}
	rightX := 1 + l.leftW + l.gapL + l.centerW + l.gapR
	return rightX + l.chipStart, rightX + l.chipStart + l.chipW, true
}

// DiagSpan returns the [x0, x1) cell range of the error / warning
// counters relative to the bar's left edge; ok=false when they are hidden
// (no diagnostics, an error message on show, or no room). Group C.
func (m Model) DiagSpan(s State) (x0, x1 int, ok bool) {
	if m.w <= 0 {
		return 0, 0, false
	}
	l := m.layout(s)
	if l.centerW == 0 {
		return 0, 0, false
	}
	_, start, w := m.renderCenterDiag(s)
	if w == 0 {
		return 0, 0, false
	}
	cx := 1 + l.leftW + l.gapL
	return cx + start, cx + start + w, true
}

// truncateStyled clips a possibly-styled string to width w cells by
// stripping ANSI escapes, truncating with an ellipsis, and re-styling
// the result with the status-bar palette. Good enough for the trailing
// fields where we just need to fit.
func truncateStyled(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	plain := stripCSI(s)
	plain = truncate(plain, w)
	return barFg(theme.TextSecondary).Render(plain)
}

func stripCSI(s string) string {
	var out []byte
	in := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			in = true
			i++
			continue
		}
		if in {
			if c >= 0x40 && c <= 0x7e {
				in = false
			}
			continue
		}
		out = append(out, c)
	}
	return string(out)
}

// renderLeft shows the dirty marker (when a buffer is open + modified) and
// a muted workspace/project name. The filename + directory used to live
// here, but that duplicated the breadcrumb row above the editor; the
// breadcrumbs are now the canonical place for path display, so the status
// bar carries only project-level context.
//
// When Err is set it takes over the whole left segment (errors are loud
// on purpose).
func (m Model) renderLeft(s State) string {
	if s.Err != "" {
		return barFg(theme.DiagError).Bold(true).Render("⚠ " + truncate(s.Err, 60))
	}
	// No buffer open: show the muted "[no file]" placeholder where the
	// project name would normally sit.
	if s.Path == "" && s.Project == "" {
		return barFg(theme.TextDim).Render("[no file]")
	}

	var b strings.Builder
	// Dirty dot only renders when a buffer is actually open AND modified.
	if s.Dirty && s.Path != "" {
		b.WriteString(barFg(theme.SyntaxKeyword).Bold(true).Render(theme.IconDirty.String()))
		b.WriteString(barBg().Render(" "))
	}
	if s.Project != "" {
		b.WriteString(barFg(theme.TextSecondary).Render(s.Project))
	} else {
		// Edge case: no project name resolved but a buffer is open —
		// fall back to the placeholder so the left zone is never empty
		// (the dirty dot alone reads as a stray glyph).
		b.WriteString(barFg(theme.TextDim).Render("[no file]"))
	}
	return b.String()
}

// renderCenter shows git branch + LSP diagnostic counters. These are
// optional accents — when none of them apply (or when Err is set and
// the whole bar is given over to the error message) the center is
// empty.
func (m Model) renderCenter(s State) string {
	out, _, _ := m.renderCenterDiag(s)
	return out
}

// renderCenterDiag is renderCenter plus the cell offset / width of the
// diagnostic counters inside the returned string (width 0 when hidden).
// Group C: the counters are clickable (they open the Problems panel).
func (m Model) renderCenterDiag(s State) (string, int, int) {
	if s.Err != "" {
		return "", 0, 0
	}
	var parts []string
	if s.Branch != "" {
		branchText := theme.IconBranch.String() + " " + s.Branch
		if s.Ahead > 0 {
			branchText += fmt.Sprintf(" ↑%d", s.Ahead)
		}
		if s.Behind > 0 {
			branchText += fmt.Sprintf(" ↓%d", s.Behind)
		}
		parts = append(parts, barFg(theme.TextSecondary).Render(branchText))
	}
	diagIdx := len(parts)
	if s.Errors > 0 {
		parts = append(parts, barFg(theme.DiagError).Bold(true).Render(
			fmt.Sprintf("%s %d", theme.IconError.String(), s.Errors)))
	}
	if s.Warnings > 0 {
		parts = append(parts, barFg(theme.DiagWarning).Bold(true).Render(
			fmt.Sprintf("%s %d", theme.IconWarning.String(), s.Warnings)))
	}
	if chip := renderTestsChip(s); chip != "" { // Group E (always last)
		parts = append(parts, chip)
	}
	if len(parts) == 0 {
		return "", 0, 0
	}
	gap := barBg().Render("  ")
	out := strings.Join(parts, gap)
	if diagIdx == len(parts) {
		return out, 0, 0
	}
	start := 0
	if diagIdx > 0 {
		start = lipgloss.Width(strings.Join(parts[:diagIdx], gap) + gap)
	}
	return out, start, lipgloss.Width(out) - start
}

// renderRight shows cursor position, indent style, encoding, language,
// the LSP chip, and (if active) TERM / DEBUG indicators — separated by dim
// │ pipes.
func (m Model) renderRight(s State) string {
	out, _, _ := m.renderRightChip(s)
	return out
}

// renderRightChip is renderRight plus the LSP chip's cell offset / width
// inside the returned string (width 0 when the chip is hidden).
func (m Model) renderRightChip(s State) (string, int, int) {
	var parts []string
	parts = append(parts, barFg(theme.TextSecondary).Render(
		fmt.Sprintf("Ln %d, Col %d", s.Line, s.Col)))
	if s.Indent != "" {
		parts = append(parts, barFg(theme.TextSecondary).Render(s.Indent))
	}
	enc := s.Encoding
	if enc == "" {
		enc = "UTF-8"
	}
	parts = append(parts, barFg(theme.TextSecondary).Render(enc))
	if s.Lang != "" {
		parts = append(parts, barFg(theme.TextSecondary).Render(s.Lang))
	}
	chipIdx := -1
	if s.ShowLSP {
		chipIdx = len(parts)
		parts = append(parts, renderLSPChip(s.LSP))
	}
	if s.Term {
		// Subtle "TERM" indicator so the user can tell at a glance
		// whether the integrated terminal panel is currently open.
		parts = append(parts, barFg(theme.SyntaxString).Bold(true).Render("● TERM"))
	}
	if s.Debug {
		// "● DEBUG" badge mirrors the "● TERM" pattern. Rendered in
		// the diagnostic-error palette so it stands out — an active
		// debug session is high-attention.
		parts = append(parts, barFg(theme.DiagError).Bold(true).Render("● DEBUG"))
	}
	chipStart, chipW := 0, 0
	if chipIdx >= 0 {
		sepW := lipgloss.Width(sep())
		for _, p := range parts[:chipIdx] {
			chipStart += lipgloss.Width(p) + sepW
		}
		chipW = lipgloss.Width(parts[chipIdx])
	}
	return strings.Join(parts, sep()), chipStart, chipW
}

// renderTestsChip is the Group E test chip: "✓ 12 ✗ 1" (◐ prefix while a
// run is in progress), or "" when hidden.
func renderTestsChip(s State) string {
	if !s.ShowTests {
		return ""
	}
	var b strings.Builder
	if s.TestsRunning {
		b.WriteString(barFg(theme.AccentAmber).Render("◐ "))
	}
	b.WriteString(barFg(theme.AccentGreen).Render(fmt.Sprintf("✓ %d", s.TestsPassed)))
	b.WriteString(barBg().Render(" "))
	failFg := theme.TextSecondary
	if s.TestsFailed > 0 {
		failFg = theme.DiagError
	}
	b.WriteString(barFg(failFg).Bold(s.TestsFailed > 0).Render(fmt.Sprintf("✗ %d", s.TestsFailed)))
	return b.String()
}

// TestsChipSpan returns the [x0, x1) cell range of the test chip relative
// to the bar's left edge; ok=false when hidden or dropped for width. The
// chip is always the last part of the center cluster.
func (m Model) TestsChipSpan(s State) (x0, x1 int, ok bool) {
	chip := renderTestsChip(s)
	if m.w <= 0 || chip == "" || s.Err != "" {
		return 0, 0, false
	}
	l := m.layout(s)
	if l.centerW == 0 {
		return 0, 0, false
	}
	cw := lipgloss.Width(chip)
	end := 1 + l.leftW + l.gapL + l.centerW
	return end - cw, end, true
}

// renderLSPChip is "{} gopls" (attached clients) or a dim "{} none".
func renderLSPChip(clients []string) string {
	if len(clients) == 0 {
		return barFg(theme.TextDim).Render("{} none")
	}
	return barFg(theme.TextSecondary).Render("{} " + strings.Join(clients, ", "))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
