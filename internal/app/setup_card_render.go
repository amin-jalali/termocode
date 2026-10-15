package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Drawing of the setup card (setup_card.go) inside the Welcome page.
//
//	S E T   U P   T E R M O C O D E                          1 of 2 done
//
//	●  Icons    Unicode icons — your terminal font is Ubuntu Mono …
//	            Install Nerd Font   Use Nerd icons   Font steps   …
//	●  AI       Not set up. termocode works fine without AI.
//	            Set up AI…   Skip
//	○  Theme    VSCode Dark+ (optional)
//	            Dark+   Tokyo Night   Light (Latte)   More…
//
//	            Done   Hide      Bring it back: F1 → Setup: Show Setup Card

// welcomeSetup is the card the next Welcome render draws (zero = none).
// Model.publishSetupCard sets it before every render / hit test.
var welcomeSetup setupCardView

// welcomeSetupHit is one card button on screen.
type welcomeSetupHit struct {
	Row      int
	ColStart int
	ColEnd   int
	ID       string // button id
	Index    int    // index into setupCardView.buttons()
}

const setupButtonGap = 2

// setupIndent is where status text and buttons start (after mark+title).
func setupIndent(innerW int) int {
	if innerW < welcomeNarrowMin {
		return 3
	}
	return 12
}

// buildSetupCard renders the card at innerW. lineOffset is the row of its
// first line in the page; focus is the focused button (index into
// v.buttons(), out of range = none).
func buildSetupCard(v setupCardView, innerW, lineOffset, focus int, focused, compact bool) ([]string, []welcomeSetupHit) {
	bg := theme.Bg(theme.BgEditor)
	dim := theme.FgBg(theme.TextDim, theme.BgEditor)
	muted := theme.FgBg(theme.TextMuted, theme.BgEditor)
	status := theme.FgBg(theme.TextSecondary, theme.BgEditor)
	title := theme.FgBg(theme.TextPrimary, theme.BgEditor).Bold(true)
	indent := setupIndent(innerW)
	textW := max(innerW-indent, 8)
	pad := func(n int) string { return bg.Render(strings.Repeat(" ", max(n, 0))) }

	var lines []string
	var hits []welcomeSetupHit
	btnIdx := 0

	// Header: name on the left, progress on the right.
	head := sectionHeader("SET UP TERMOCODE")
	prog := dim.Render(v.progress)
	if gap := innerW - textWidth(head) - textWidth(prog); gap >= 2 {
		head += pad(gap) + prog
	}
	lines = append(lines, padRowToWidth(head, innerW, bg))
	if !compact {
		lines = append(lines, blank(innerW))
	}

	// addButtons lays the buttons out from the indent, wrapping rows.
	addButtons := func(bs []setupButton, tail string) {
		if len(bs) == 0 {
			return
		}
		row := pad(indent)
		col := indent
		flush := func() {
			lines = append(lines, padRowToWidth(row, innerW, bg))
			row, col = pad(indent), indent
		}
		for _, b := range bs {
			w := setupButtonWidth(b)
			if col > indent && col+w > innerW {
				flush()
			}
			if col > indent {
				row += pad(setupButtonGap)
				col += setupButtonGap
			}
			row += renderSetupButton(b, btnIdx == focus && focused, min(w, innerW-col))
			hits = append(hits, welcomeSetupHit{Row: lineOffset + len(lines), ColStart: col, ColEnd: min(col+w, innerW), ID: b.id, Index: btnIdx})
			col += w
			btnIdx++
		}
		if tail != "" && col+4+textWidth(tail) <= innerW {
			row += pad(4) + muted.Render(tail)
		}
		flush()
	}

	for _, s := range v.steps {
		mark, markTok := setupMark(s)
		st := wrapWords(s.status, textW)
		first := ""
		if len(st) > 0 {
			first = st[0]
		}
		label := runewidth.Truncate(s.title, indent-4, "")
		row := theme.FgBg(markTok, theme.BgEditor).Bold(true).Render(mark) + pad(2) +
			title.Render(label) + pad(indent-3-runewidth.StringWidth(label)) + status.Render(first)
		if indent < 12 { // narrow: the status goes on its own rows
			row = theme.FgBg(markTok, theme.BgEditor).Bold(true).Render(mark) + pad(2) + title.Render(s.title)
		} else if len(st) > 0 {
			st = st[1:]
		}
		lines = append(lines, padRowToWidth(ansi.Truncate(row, innerW, ""), innerW, bg))
		for _, l := range st {
			lines = append(lines, padRowToWidth(pad(indent)+status.Render(l), innerW, bg))
		}
		for _, n := range s.notes {
			for _, l := range wrapWords(n, textW) {
				lines = append(lines, padRowToWidth(pad(indent)+muted.Render(l), innerW, bg))
			}
		}
		addButtons(s.buttons, "")
	}
	if !compact {
		lines = append(lines, blank(innerW))
	}
	addButtons(v.footer, "Bring it back: F1 → Setup: Show Setup Card")
	return lines, hits
}

// setupMark is the step's state glyph: done, to do, optional.
func setupMark(s setupStep) (string, theme.Color256) {
	ascii := theme.CurrentIconMode == theme.IconModeASCII
	switch {
	case s.done:
		if ascii {
			return "+", theme.AccentGreen
		}
		return "✓", theme.AccentGreen
	case s.optional:
		if ascii {
			return "-", theme.TextDim
		}
		return "○", theme.TextDim
	}
	if ascii {
		return "*", theme.AccentAmber
	}
	return "●", theme.AccentAmber
}

func setupButtonText(b setupButton) string {
	if b.active {
		if theme.CurrentIconMode == theme.IconModeASCII {
			return "* " + b.label
		}
		return "✓ " + b.label
	}
	return b.label
}

func setupButtonWidth(b setupButton) int { return runewidth.StringWidth(setupButtonText(b)) + 2 }

// renderSetupButton draws " label " as a key-cap; the focused one gets
// the accent and a ▌ edge (visible without colour too), a disabled one
// dim text.
func renderSetupButton(b setupButton, focused bool, w int) string {
	bgC := theme.Mix(theme.BgEditor, theme.TextPrimary, 0.10)
	fgC := theme.LG(theme.TextSecondary)
	left := " "
	switch {
	case focused:
		bgC = theme.Wash(theme.BgEditor, theme.WashPressed)
		fgC = theme.LG(theme.TextWhite)
		left = "▌"
	case b.disabled:
		fgC = theme.LG(theme.TextQuaternary)
	case b.active:
		fgC = theme.LG(theme.AccentGreen)
	}
	st := lipgloss.NewStyle().Background(bgC).Foreground(fgC)
	if focused {
		st = st.Bold(true)
	}
	edge := lipgloss.NewStyle().Background(bgC).Foreground(theme.LG(theme.BorderFocus))
	text := setupButtonText(b)
	if w < setupButtonWidth(b) {
		text = runewidth.Truncate(text, max(w-2, 1), "…")
	}
	return edge.Render(left) + st.Render(text) + st.Render(" ")
}

// wrapWords word-wraps plain text to w cells (a longer word is cut).
func wrapWords(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	lead := len(s) - len(strings.TrimLeft(s, " "))
	var out []string
	line := strings.Repeat(" ", lead)
	for _, word := range strings.Fields(s) {
		if runewidth.StringWidth(word) > w {
			word = runewidth.Truncate(word, w, "…")
		}
		switch {
		case strings.TrimSpace(line) == "":
			line += word
		case runewidth.StringWidth(line)+1+runewidth.StringWidth(word) > w:
			out = append(out, line)
			line = strings.Repeat(" ", lead) + word
		default:
			line += " " + word
		}
	}
	if strings.TrimSpace(line) != "" {
		out = append(out, line)
	}
	return out
}
