package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/theme"
)

// ─── Welcome page rows ─────────────────────────────────────────────────
//
// The start page is one column of one-line rows (START actions, the setup
// checklist, recent files, workspaces). Every row has the same shape:
//
//	col 0      cursor bar ▎ (blank on other rows; always reserved)
//	col 1-2    blank
//	col 3      text, or a 1-cell glyph + blank and the title at col 5
//	…          status text (dim) after the title column
//	W-1-n…W-2  keycap / action word / time, right-aligned
//	col W-1    blank right margin
//
// The keyboard cursor is the only highlighted thing: the bar plus a wash
// over the whole row.

// welcomeRowState is how a row is drawn.
type welcomeRowState int

const (
	rowRest          welcomeRowState = iota
	rowCursorFocused                 // cursor row, the editor pane has the keys
	rowCursorBlurred                 // cursor row, another pane has the keys
)

// welcomeRow is one line of the start page.
type welcomeRow struct {
	id         string // hit id: "card:N", "setup:<step>", "recent:<path>", "workspace:<path>"
	glyph      string // optional 1-cell glyph at col 3 (setup mark, file icon)
	glyphTok   theme.Color256
	title      string
	titleW     int  // fixed title column (title + gap), 0 = natural width
	titleMuted bool // TextMuted title at rest ("All …", "Hide setup")
	status     string
	statusLeft bool // cut the status from the left (directories)
	right      string
	// rightIsKeycap draws right as a keycap chip; otherwise it is an
	// action word (TextSecondary) or, with rightDim, a time (TextDim).
	rightIsKeycap bool
	rightDim      bool
	interactive   bool // in the cursor cycle and clickable
	muted         bool // a done setup row: all TextMuted, not interactive
}

// welcomeEllipsis is "…", or "..." when the terminal shows ASCII only.
func welcomeEllipsis() string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return "..."
	}
	return "…"
}

// welcomeText makes s safe for the icon tier ("…" → "..." in ASCII).
func welcomeText(s string) string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return strings.ReplaceAll(s, "…", "...")
	}
	return s
}

// welcomeCut cuts s to w cells with the tier's ellipsis.
func welcomeCut(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= w {
		return s
	}
	ell := welcomeEllipsis()
	if w <= runewidth.StringWidth(ell) {
		return runewidth.Truncate(s, w, "")
	}
	return runewidth.Truncate(s, w, ell)
}

// welcomeCutLeft keeps the end of s ("…/internal/app").
func welcomeCutLeft(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= w {
		return s
	}
	ell := welcomeEllipsis()
	ew := runewidth.StringWidth(ell)
	if w <= ew {
		return runewidth.Truncate(s, w, "")
	}
	r := []rune(s)
	for len(r) > 0 && runewidth.StringWidth(string(r))+ew > w {
		r = r[1:]
	}
	return ell + string(r)
}

// welcomeCursorBar is the 1-cell cursor mark.
func welcomeCursorBar() string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return ">"
	}
	return "▎"
}

// renderWelcomeRow draws r exactly w cells wide.
func renderWelcomeRow(r welcomeRow, w int, st welcomeRowState) string {
	if w <= 0 {
		return ""
	}
	cursor := st != rowRest
	bgC := theme.LG(theme.BgEditor)
	switch st {
	case rowCursorFocused:
		bgC = theme.Wash(theme.BgEditor, theme.WashSelectedFocused)
	case rowCursorBlurred:
		bgC = theme.Wash(theme.BgEditor, theme.WashSelected)
	}
	fg := func(c theme.Color256) lipgloss.Style {
		return lipgloss.NewStyle().Background(bgC).Foreground(theme.LG(c))
	}
	bg := lipgloss.NewStyle().Background(bgC)
	pad := func(n int) string {
		if n <= 0 {
			return ""
		}
		return bg.Render(strings.Repeat(" ", n))
	}

	titleTok := theme.TextPrimary
	switch {
	case r.muted, r.titleMuted:
		titleTok = theme.TextMuted
	}
	switch st {
	case rowCursorFocused:
		titleTok = theme.TextWhite
	case rowCursorBlurred:
		titleTok = theme.TextPrimary
	}

	var b strings.Builder
	col := 0
	put := func(s string, n int) { b.WriteString(s); col += max(n, 0) }

	// col 0: the cursor bar.
	if cursor && w >= 3 {
		barTok := theme.BorderFocus
		if st == rowCursorBlurred {
			barTok = theme.TextDimmer
		}
		put(fg(barTok).Render(welcomeCursorBar()), 1)
		put(pad(2), 2)
	} else if w >= 3 {
		put(pad(3), 3)
	}

	// The right side: keycap, action word or time; dropped on tiny rows.
	right, rightW := "", 0
	if r.right != "" && w >= 24 {
		txt := welcomeText(r.right)
		if r.rightIsKeycap {
			right = theme.Keycap(txt, bgC)
			rightW = runewidth.StringWidth(theme.KeyLabel(txt)) + 2
		} else {
			tok := theme.TextSecondary
			if r.rightDim {
				tok = theme.TextDim
			}
			if st == rowCursorFocused {
				tok = theme.TextWhite
			}
			right = fg(tok).Render(txt)
			rightW = runewidth.StringWidth(txt)
		}
	}
	// Text ends here (exclusive): 2 blank cells before the right side,
	// 1 blank margin at the right edge.
	end := w - 1
	if rightW > 0 {
		end = w - 1 - rightW - 2
	}
	if end < col {
		end = col
	}

	if r.glyph != "" && end-col >= 2 {
		g := runewidth.Truncate(welcomeText(r.glyph), 1, "")
		put(fg(r.glyphTok).Render(g), runewidth.StringWidth(g))
		put(pad(1), 1)
	}
	title := welcomeText(r.title)
	if r.titleW > 0 {
		title = welcomeCut(title, min(r.titleW-2, end-col))
		tw := runewidth.StringWidth(title)
		put(fg(titleTok).Render(title), tw)
		if r.status != "" {
			put(pad(min(r.titleW-tw, end-col)), min(r.titleW-tw, end-col))
		}
	} else {
		title = welcomeCut(title, end-col)
		put(fg(titleTok).Render(title), runewidth.StringWidth(title))
		if r.status != "" && end-col > 2 {
			put(pad(2), 2)
		}
	}
	if r.status != "" && end > col {
		s := welcomeText(r.status)
		if r.statusLeft {
			s = welcomeCutLeft(s, end-col)
		} else {
			s = welcomeCut(s, end-col)
		}
		put(fg(theme.TextDim).Render(s), runewidth.StringWidth(s))
	}
	if rightW > 0 {
		put(pad(w-1-rightW-col), w-1-rightW-col)
		put(right, rightW)
	}
	put(pad(w-col), w-col)
	return b.String()
}

// renderWelcomeNote draws a dim note line (font steps, profile hints)
// starting at col indent, w cells wide.
func renderWelcomeNote(text string, indent, w int) string {
	bg := theme.Bg(theme.BgEditor)
	s := welcomeCut(welcomeText(text), w-1-indent)
	out := bg.Render(strings.Repeat(" ", max(indent, 0))) + theme.FgBg(theme.TextDim, theme.BgEditor).Render(s)
	return padRowToWidth(out, w, bg)
}
