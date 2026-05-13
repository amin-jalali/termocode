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

func (m Model) View(s State) string {
	if m.w <= 0 {
		return ""
	}
	pad := barBg().Render(" ")

	left := m.renderLeft(s)
	center := m.renderCenter(s)
	right := m.renderRight(s)

	leftW := lipgloss.Width(left)
	centerW := lipgloss.Width(center)
	rightW := lipgloss.Width(right)

	innerW := m.w - 2 // subtract left+right edge pad
	if innerW < 0 {
		innerW = 0
	}

	// Drop center first if everything won't fit, then truncate right and
	// left in turn so the bar never overflows m.w.
	if leftW+centerW+rightW > innerW {
		center, centerW = "", 0
	}
	if leftW+rightW > innerW {
		// Truncate the right cluster first — the filename on the left
		// is more critical than the trailing metadata when space is
		// tight.
		avail := innerW - leftW
		if avail < 0 {
			avail = 0
		}
		right = truncateStyled(right, avail)
		rightW = lipgloss.Width(right)
	}
	if leftW+rightW > innerW {
		avail := innerW - rightW
		if avail < 0 {
			avail = 0
		}
		left = truncateStyled(left, avail)
		leftW = lipgloss.Width(left)
	}

	contentW := leftW + centerW + rightW
	gap := innerW - contentW
	if gap < 0 {
		gap = 0
	}
	gapL := gap / 2
	gapR := gap - gapL
	leftSpace := barBg().Render(strings.Repeat(" ", gapL))
	rightSpace := barBg().Render(strings.Repeat(" ", gapR))

	body := lipgloss.JoinHorizontal(
		lipgloss.Top,
		pad, left, leftSpace, center, rightSpace, right, pad,
	)
	// Force the row to exactly m.w cells from lipgloss's perspective so
	// JoinVertical above can't pad it with default-styled spaces.
	return lipgloss.NewStyle().
		Width(m.w).
		Background(theme.LG(barBgColor())).
		Render(body)
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
	if s.Err != "" {
		return ""
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
	if s.Errors > 0 {
		parts = append(parts, barFg(theme.DiagError).Bold(true).Render(
			fmt.Sprintf("%s %d", theme.IconError.String(), s.Errors)))
	}
	if s.Warnings > 0 {
		parts = append(parts, barFg(theme.DiagWarning).Bold(true).Render(
			fmt.Sprintf("%s %d", theme.IconWarning.String(), s.Warnings)))
	}
	if len(parts) == 0 {
		return ""
	}
	gap := barBg().Render("  ")
	return strings.Join(parts, gap)
}

// renderRight shows cursor position, indent style, encoding, language,
// and (if active) TERM / DEBUG indicators — separated by dim │ pipes.
func (m Model) renderRight(s State) string {
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
	return strings.Join(parts, sep())
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
