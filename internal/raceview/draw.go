// Package raceview draws the agent race pages (AGX-4…6, plan 2.5): the
// Agent: Race… card (task, lanes, verify command) and the compare page
// (per lane state, files, +/-, verify, cost; merge, discard, summary).
// It only draws and reads keys; the app runs the lanes.
package raceview

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/theme"
)

type pal struct {
	bg, sel, input                   lipgloss.Color
	text, muted, dim, title, accent  lipgloss.Color
	ok, bad, warn, hair, primary, on lipgloss.Color
}

func colors() pal {
	fc := theme.Float()
	return pal{
		bg: fc.Bg, sel: fc.SelBg, input: fc.InputBg,
		text: fc.Text, muted: fc.Sub, dim: fc.Dim, title: fc.Title,
		accent: theme.LG(theme.AIAccent), ok: theme.LG(theme.GitAdded), bad: theme.LG(theme.DiagError),
		warn: theme.LG(theme.DiagWarning), hair: fc.Hairline, primary: fc.Primary, on: fc.PrimaryFg,
	}
}

func ascii() bool { return theme.CurrentIconMode == theme.IconModeASCII }

// seg is one styled run of plain text.
type seg struct {
	text string
	st   lipgloss.Style
}

func segsWidth(s []seg) int {
	w := 0
	for _, x := range s {
		w += runewidth.StringWidth(x.text)
	}
	return w
}

// paint renders segs into exactly width cells (clipped, then padded).
func paint(segs []seg, width int, fill lipgloss.Style) string {
	var b strings.Builder
	used := 0
	for _, s := range segs {
		if used >= width {
			break
		}
		t := s.text
		w := runewidth.StringWidth(t)
		if used+w > width {
			t = runewidth.Truncate(t, width-used, "…")
			w = runewidth.StringWidth(t)
		}
		if t != "" {
			b.WriteString(s.st.Render(t))
		}
		used += w
	}
	if used < width {
		b.WriteString(fill.Render(strings.Repeat(" ", width-used)))
	}
	return b.String()
}

// joinLR renders left, pads, then right, in exactly width cells.
func joinLR(left, right []seg, width int, bg lipgloss.Style) string {
	rw := min(segsWidth(right), width)
	l := paint(left, width-rw, bg)
	if rw == 0 {
		return l
	}
	return l + paint(right, rw, bg)
}

// fit cuts s to w cells with an ellipsis, then pads it to w.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) > w {
		s = runewidth.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", max(w-runewidth.StringWidth(s), 0))
}

// tail keeps the last w cells of s (a text field's caret end).
func tail(s string, w int) string {
	if runewidth.StringWidth(s) <= w {
		return s
	}
	r := []rune(s)
	for i := range r {
		if runewidth.StringWidth(string(r[i:])) <= w-1 {
			return "…" + string(r[i:])
		}
	}
	return ""
}

func padStyled(s string, width int, bg lipgloss.Style) string {
	w := lipgloss.Width(s)
	if w > width {
		s = ansi.Truncate(s, width, "")
		w = lipgloss.Width(s)
	}
	return s + bg.Render(strings.Repeat(" ", max(width-w, 0)))
}

func hairline(p pal, w int) string {
	return lipgloss.NewStyle().Background(p.bg).Foreground(p.hair).Render(strings.Repeat("─", max(w, 0)))
}

// hint fits a hint bar into w cells, dropping pairs from the end.
func hint(p pal, w int, pairs ...string) string {
	bg := lipgloss.NewStyle().Background(p.bg)
	for len(pairs) >= 2 {
		h := theme.HintBar(p.bg, pairs...)
		if lipgloss.Width(h)+1 <= w {
			return padStyled(bg.Render(" ")+h, w, bg)
		}
		pairs = pairs[:len(pairs)-2]
	}
	return paint(nil, w, bg)
}

// hit is a clickable cell run (box-relative).
type hit struct {
	x0, x1, y int
	id        string
}

func hitAt(hs []hit, x, y int) string {
	for _, h := range hs {
		if y == h.y && x >= h.x0 && x < h.x1 {
			return h.id
		}
	}
	return ""
}

// button draws " Title " lit when on (primary) or plain.
func button(p pal, title string, on, primary bool) seg {
	st := lipgloss.NewStyle().Background(theme.Mix(theme.BgFloat, theme.TextPrimary, 0.12)).Foreground(p.text)
	if on {
		st = lipgloss.NewStyle().Background(p.primary).Foreground(p.on).Bold(true)
	} else if primary {
		st = st.Foreground(p.primary).Bold(true)
	}
	return seg{" " + title + " ", st}
}
