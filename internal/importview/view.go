package importview

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Layout of the card (inner rows, inside the rounded border):
//
//	0        header: ✦ Import from <editor> · <folder>          N of M selected
//	1        labels: ▍In <editor>                 ▍In termocode
//	2        hairline
//	3..      body: section headers and rows ([x] left → right)
//	h-3      hairline
//	h-2      footer: status                      [All] [None] [Apply…] [Cancel]
//	h-1      key hints

type geom struct {
	x, y, w, h int // box on screen
	iw, ih     int // inner size
	bodyH      int
	leftW      int // the other editor's column
	rightW     int // termocode's column
	tiny       bool
}

const (
	checkW = 5 // " [x] "
	arrowW = 3 // " → "
)

func (m Model) geom() geom {
	var g geom
	g.w = min(m.w-4, 150)
	if g.w < 40 {
		g.w = m.w
	}
	g.h = m.h - 2
	if g.h < 12 {
		g.h = m.h
	}
	g.w, g.h = max(g.w, 0), max(g.h, 0)
	g.x, g.y = max((m.w-g.w)/2, 0), max((m.h-g.h)/2, 0)
	g.iw, g.ih = max(g.w-2, 0), max(g.h-2, 0)
	if g.iw < 24 || g.ih < 7 {
		g.tiny = true
		return g
	}
	g.bodyH = g.ih - 6
	avail := g.iw - checkW - arrowW - 1
	g.leftW = avail * 2 / 5
	g.rightW = avail - g.leftW
	return g
}

type pal struct {
	bg, sunken, sel                         lipgloss.Color
	text, muted, dim, title, accent, add    lipgloss.Color
	warn, hair, primary, onPrimary, addTint lipgloss.Color
}

func colors() pal {
	fc := theme.Float()
	return pal{
		bg:        fc.Bg,
		sunken:    theme.Mix(theme.BgFloat, theme.TextPrimary, 0.05),
		sel:       fc.SelBg,
		text:      fc.Text,
		muted:     fc.Sub,
		dim:       fc.Dim,
		title:     fc.Title,
		accent:    theme.LG(theme.AIAccent),
		add:       theme.LG(theme.GitAdded),
		warn:      theme.LG(theme.DiagWarning),
		hair:      fc.Hairline,
		primary:   fc.Primary,
		onPrimary: fc.PrimaryFg,
		addTint:   theme.Mix(theme.BgFloat, theme.GitAdded, 0.16),
	}
}

type glyphs struct{ spark, arrow, bar, dot string }

func icons() glyphs {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return glyphs{"*", ">", "|", "-"}
	}
	return glyphs{"✦", "→", "▍", "·"}
}

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
			t = runewidth.Truncate(t, width-used, "")
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

// cutStart keeps the last w cells of s behind an ellipsis.
func cutStart(s string, w int) string {
	if runewidth.StringWidth(s) <= w {
		return s
	}
	r := []rune(s)
	for i := range r {
		if runewidth.StringWidth(string(r[i:])) <= w-1 {
			return "…" + string(r[i:])
		}
	}
	return "…"
}

type hit struct {
	x0, x1, y int // box-relative cells
	id        string
}

type layoutOut struct {
	x, y, w, h int
	rows       []string
	hits       []hit
}

func (l layoutOut) hitAt(x, y int) string {
	for _, h := range l.hits {
		if y == h.y && x >= h.x0 && x < h.x1 {
			return h.id
		}
	}
	return ""
}

// Box renders the card (the app composites it over the screen).
func (m Model) Box() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	return strings.Join(m.layout(true).rows, "\n")
}

// Origin is the card's top-left screen cell.
func (m Model) Origin() (int, int) {
	g := m.geom()
	return g.x, g.y
}

// status is the footer's left text: why the focused row is off, or what
// the selection will change.
func (m Model) status() string {
	if m.cursor >= 0 && m.cursor < len(m.lines) {
		if ln := m.lines[m.cursor]; ln.row >= 0 {
			r := m.rows[ln.row]
			switch {
			case r.state == stateSkip && r.reason != "":
				return r.reason
			case r.state == stateInfo:
				return "a suggestion only: nothing is installed"
			case r.state == stateToggle && r.conflict:
				return "replaces what you have now"
			}
		}
	}
	c := m.Counts()
	if c.Total() == 0 {
		return "Nothing selected"
	}
	return "Will change: " + c.Summary()
}

func (m Model) layout(draw bool) layoutOut {
	g := m.geom()
	out := layoutOut{x: g.x, y: g.y, w: g.w, h: g.h}
	if g.w < 4 || g.h < 3 {
		return out
	}
	p := colors()
	ic := icons()
	bg := lipgloss.NewStyle().Background(p.bg)
	if g.tiny {
		if draw {
			inner := make([]string, g.ih)
			for i := range inner {
				inner[i] = paint(nil, g.iw, bg)
			}
			inner[0] = paint([]seg{{" Window too small to review the import", bg.Foreground(p.muted)}}, g.iw, bg)
			out.rows = strings.Split(theme.FloatBox(strings.Join(inner, "\n"), g.w), "\n")
		}
		return out
	}
	addHit := func(x0, w, y int, id string) {
		out.hits = append(out.hits, hit{x0: x0 + 1, x1: x0 + 1 + w, y: y + 1, id: id})
	}
	inner := make([]string, 0, g.ih)

	// Row 0: title and the selection count.
	sel, n, _ := m.tally(m.allRows())
	{
		left := []seg{
			{" " + ic.spark + " ", bg.Foreground(p.accent).Bold(true)},
			{"Import from " + m.title, bg.Foreground(p.title).Bold(true)},
		}
		right := []seg{{fmt.Sprintf("%d of %d selected ", sel, n), bg.Foreground(p.muted)}}
		if segsWidth(left)+segsWidth(right)+1 > g.iw {
			right = nil
		}
		// The folder fills the room left; a long one keeps its end.
		if room := g.iw - segsWidth(left) - segsWidth(right) - 6; m.dir != "" && room >= 8 {
			left = append(left, seg{"  " + ic.dot + "  ", bg.Foreground(p.dim)}, seg{cutStart(m.dir, room), bg.Foreground(p.muted)})
		}
		if draw {
			inner = append(inner, joinLR(left, right, g.iw, bg))
		}
	}
	// Row 1: the column labels.
	if draw {
		chip := func(label string) []seg {
			return []seg{{ic.bar, lipgloss.NewStyle().Background(p.sunken).Foreground(p.accent)},
				{label + " ", lipgloss.NewStyle().Background(p.sunken).Foreground(p.text)}}
		}
		l := chip(fit("In "+m.title, max(g.leftW-1, 1)))
		r := chip(fit("In termocode", max(g.rightW-1, 1)))
		segs := []seg{{strings.Repeat(" ", checkW), bg}}
		segs = append(segs, l...)
		if pad := g.leftW + arrowW - segsWidth(l); pad > 0 {
			segs = append(segs, seg{strings.Repeat(" ", pad), bg})
		}
		segs = append(segs, r...)
		inner = append(inner, paint(segs, g.iw, bg))
		inner = append(inner, theme.Hairline(g.iw, theme.BgFloat))
	}

	// Body.
	scroll := min(max(m.scroll, 0), max(len(m.lines)-g.bodyH, 0))
	for r := 0; r < g.bodyH; r++ {
		k := scroll + r
		y := 3 + r
		if k >= len(m.lines) {
			if draw {
				inner = append(inner, paint(nil, g.iw, bg))
			}
			continue
		}
		addHit(0, g.iw, y, "line:"+strconv.Itoa(k))
		if draw {
			inner = append(inner, m.renderLine(k, g, p, ic))
		}
	}

	// Footer.
	if draw {
		inner = append(inner, theme.Hairline(g.iw, theme.BgFloat))
	}
	{
		type btn struct{ label, id, kind string }
		btns := []btn{{" All ", "all", ""}, {" None ", "none", ""}, {" Apply… ", "apply", "primary"}, {" Cancel ", "cancel", ""}}
		build := func(bs []btn) ([]seg, int) {
			var segs []seg
			for _, b := range bs {
				st := lipgloss.NewStyle().Background(p.sunken).Foreground(p.text)
				if b.kind == "primary" {
					st = lipgloss.NewStyle().Background(p.primary).Foreground(p.onPrimary).Bold(true)
				}
				segs = append(segs, seg{b.label, st}, seg{" ", bg})
			}
			return segs, segsWidth(segs)
		}
		right, rw := build(btns)
		if rw > g.iw {
			btns = btns[2:]
			right, rw = build(btns)
		}
		left := []seg{{" " + m.status(), bg.Foreground(p.muted)}}
		if segsWidth(left)+rw+1 > g.iw {
			left = []seg{{" " + runewidth.Truncate(m.status(), max(g.iw-rw-2, 0), "…"), bg.Foreground(p.muted)}}
		}
		x := max(g.iw-rw, 0)
		for _, b := range btns {
			w := runewidth.StringWidth(b.label)
			addHit(x, w, g.ih-2, b.id)
			x += w + 1
		}
		if draw {
			inner = append(inner, joinLR(left, right, g.iw, bg))
		}
	}
	if draw {
		pairs := []string{"space", "toggle", "a", "all", "n", "none", "↑↓", "move", "enter", "apply", "esc", "cancel"}
		if theme.CurrentIconMode == theme.IconModeASCII {
			pairs[6] = "up/dn"
		}
		hints := theme.HintBar(p.bg, pairs...)
		for _, drop := range []int{6, 4, 2} { // move, none, all
			if lipgloss.Width(hints)+1 <= g.iw {
				break
			}
			pairs = append(pairs[:drop:drop], pairs[drop+2:]...)
			hints = theme.HintBar(p.bg, pairs...)
		}
		inner = append(inner, padStyled(bg.Render(" ")+hints, g.iw, bg))
		out.rows = strings.Split(theme.FloatBox(strings.Join(inner, "\n"), g.w), "\n")
	}
	return out
}

// padStyled pads a styled string to width cells (clipping it).
func padStyled(s string, width int, bg lipgloss.Style) string {
	w := lipgloss.Width(s)
	if w > width {
		s = ansi.Truncate(s, width, "")
		w = lipgloss.Width(s)
	}
	return s + bg.Render(strings.Repeat(" ", max(width-w, 0)))
}

// renderLine draws body line k.
func (m Model) renderLine(k int, g geom, p pal, ic glyphs) string {
	ln := m.lines[k]
	base := p.bg
	if k == m.cursor {
		base = p.sel
	}
	bg := lipgloss.NewStyle().Background(base)
	if ln.row < 0 {
		sec := m.sections[ln.sec]
		sel, n, skipped := m.tally(sec.rows)
		notes := len(sec.rows) - n - skipped
		segs := []seg{{" " + sec.name, bg.Foreground(p.title).Bold(true)}}
		var parts []string
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d of %d selected", sel, n))
		}
		if notes > 0 {
			parts = append(parts, plural(notes, "note"))
		}
		if skipped > 0 {
			parts = append(parts, fmt.Sprintf("%d skipped", skipped))
		}
		info := strings.Join(parts, "  "+ic.dot+"  ")
		if info != "" {
			segs = append(segs, seg{"  " + info, bg.Foreground(p.dim)})
		}
		return paint(segs, g.iw, bg)
	}
	r := m.rows[ln.row]
	var check, left, right lipgloss.Style
	box := "   "
	switch r.state {
	case stateToggle:
		left = bg.Foreground(p.text)
		right = bg.Foreground(p.text)
		box = "[ ]"
		check = bg.Foreground(p.muted)
		if r.on {
			box = "[x]"
			check = bg.Foreground(p.add).Bold(true)
		} else {
			left = bg.Foreground(p.muted)
			right = bg.Foreground(p.muted)
		}
		if r.conflict {
			right = bg.Foreground(p.warn)
		}
	case stateInfo:
		box = " " + ic.dot + " "
		check = bg.Foreground(p.dim)
		left = bg.Foreground(p.muted)
		right = bg.Foreground(p.muted)
	default:
		box = " " + ic.dot + " "
		check = bg.Foreground(p.dim)
		left = bg.Foreground(p.dim)
		right = bg.Foreground(p.dim).Faint(true)
	}
	arrow := " " + ic.arrow + " "
	segs := []seg{
		{" ", bg}, {box, check}, {" ", bg},
		{fit(r.left, g.leftW), left},
		{arrow, bg.Foreground(p.dim)},
		{fit(r.right, g.rightW), right},
	}
	return paint(segs, g.iw, bg)
}
