package diffview

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/textdiff"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Layout of the card (inner rows, inside the rounded border):
//
//	0        header: ✦ title · file · tag            +x −y · N changes
//	1        labels: − Current  + Proposed     ‹ prev  Change 2 of 5  next ›  Split│Unified
//	2        hairline
//	3..      body: [file list │] diff rows, overview ruler on the right
//	h-3      hairline
//	h-2      footer: status                 [Reject file] [Accept & edit] [Apply] [Accept file]
//	h-1      key hints

const (
	collapseThreshold = 8 // context runs longer than this fold
	collapseContext   = 3 // context lines kept at each side of a fold
	tabWidth          = 4
	splitMinWidth     = 96 // auto mode: split when the diff area is this wide
)

type geom struct {
	x, y, w, h int // box on screen
	iw, ih     int // inner size
	listW      int // file list width, separator included (0: one file)
	diffX      int // inner x of the diff area
	diffW      int // diff area width without the ruler
	bodyTop    int // inner row of the first body row
	bodyH      int
	split      bool
	numW       int // line number width
}

func (m Model) geom() geom {
	var g geom
	g.w = min(m.w-4, 200)
	if g.w < 40 {
		g.w = max(m.w, 20)
	}
	g.h = m.h - 2
	if g.h < 10 {
		g.h = max(m.h, 6)
	}
	g.x, g.y = (m.w-g.w)/2, (m.h-g.h)/2
	g.iw, g.ih = g.w-2, g.h-2
	if len(m.files) > 1 {
		longest := 0
		for _, f := range m.files {
			longest = max(longest, runewidth.StringWidth(f.Name))
		}
		g.listW = min(max(longest+12, 24), g.iw/4) + 1
	}
	g.diffX = g.listW
	g.diffW = g.iw - g.listW - 1
	g.bodyTop = 3
	g.bodyH = max(g.ih-6, 1)
	switch m.mode {
	case ModeSplit:
		g.split = true
	case ModeAuto:
		g.split = g.diffW >= splitMinWidth
	}
	g.numW = 3
	if f := m.file(); f != nil {
		n := max(len(textdiff.SplitLines(f.Old)), len(textdiff.SplitLines(f.New)))
		g.numW = max(3, len(strconv.Itoa(n)))
	}
	return g
}

// splitNow reports whether the current layout is side by side.
func (m Model) splitNow() bool { return m.geom().split }

// ── Display rows ──────────────────────────────────────────────────────────

type vkind uint8

const (
	vHead vkind = iota // a change header (with Accept / Reject)
	vLine              // a diff row (unified) or an old/new pair (split)
	vBand              // a folded run of unchanged lines
	vNote              // a message row ("No changes")
)

type vrow struct {
	kind vkind
	hunk int // vHead; also vLine rows inside a hunk (-1 outside)
	a, b int // vLine: row indexes into diff.Rows (-1: blank side); unified uses a
	n    int // vBand: folded rows; vNote: 0 no change, 1 EOL only
	key  int // vBand: first folded row (expanded key)
}

type cache struct {
	file      *fileState
	split     bool
	expandAll bool
	expanded  int
	rows      []vrow
}

func (c *cache) reset() { c.file = nil; c.rows = nil }

// vrows returns the display rows of the current file (cached).
func (m Model) vrows(g geom) []vrow {
	f := m.file()
	if f == nil {
		return nil
	}
	c := m.cache
	if c == nil {
		c = &cache{}
	}
	if c.file == f && c.split == g.split && c.expandAll == f.expandAll && c.expanded == len(f.expanded) && c.rows != nil {
		return c.rows
	}
	rows := buildRows(f, g.split)
	*c = cache{file: f, split: g.split, expandAll: f.expandAll, expanded: len(f.expanded), rows: rows}
	return rows
}

func buildRows(f *fileState, split bool) []vrow {
	d := f.diff
	if !d.HasChanges() {
		if d.EOLOnly() {
			return []vrow{{kind: vHead, hunk: 0, a: -1, b: -1}, {kind: vNote, n: 1, hunk: 0}}
		}
		return []vrow{{kind: vNote, hunk: -1}}
	}
	var out []vrow
	rows := d.Rows
	hunk := 0
	for i := 0; i < len(rows); {
		if rows[i].Kind != textdiff.Context {
			out = append(out, vrow{kind: vHead, hunk: hunk, a: -1, b: -1})
			end := d.Hunks[hunk].End
			for i < end {
				r := rows[i]
				switch {
				case !split:
					// Unified: the hunk's removed lines, then its added
					// lines (git order), each with its word highlights.
					for k := i; k < end; k++ {
						if rows[k].Kind == textdiff.Removed {
							out = append(out, vrow{kind: vLine, hunk: hunk, a: k, b: -1})
						}
					}
					for k := i; k < end; k++ {
						if rows[k].Kind == textdiff.Added {
							out = append(out, vrow{kind: vLine, hunk: hunk, a: k, b: -1})
						}
					}
					i = end
				case r.Kind == textdiff.Removed && r.Paired && i+1 < end && rows[i+1].Kind == textdiff.Added && rows[i+1].Paired:
					out = append(out, vrow{kind: vLine, hunk: hunk, a: i, b: i + 1})
					i += 2
				case r.Kind == textdiff.Removed:
					out = append(out, vrow{kind: vLine, hunk: hunk, a: i, b: -1})
					i++
				default:
					out = append(out, vrow{kind: vLine, hunk: hunk, a: -1, b: i})
					i++
				}
			}
			hunk++
			continue
		}
		start := i
		for i < len(rows) && rows[i].Kind == textdiff.Context {
			i++
		}
		head, tail := collapseContext, collapseContext
		if start == 0 {
			head = 0
		}
		if i == len(rows) {
			tail = 0
		}
		run := i - start
		ctx := func(k int) vrow { return vrow{kind: vLine, hunk: -1, a: k, b: k} }
		if f.expandAll || f.expanded[start+head] || run <= collapseThreshold || run <= head+tail+1 {
			for k := start; k < i; k++ {
				out = append(out, ctx(k))
			}
			continue
		}
		for k := start; k < start+head; k++ {
			out = append(out, ctx(k))
		}
		out = append(out, vrow{kind: vBand, hunk: -1, n: run - head - tail, key: start + head})
		for k := i - tail; k < i; k++ {
			out = append(out, ctx(k))
		}
	}
	return out
}

// ── Colours ───────────────────────────────────────────────────────────────

type pal struct {
	bg, sunken, addBg, delBg, addWord, delWord, blank  lipgloss.Color
	text, muted, dim, add, del, accent, hair, sel, mod lipgloss.Color
	white, warn, onAccent, primary                     lipgloss.Color
}

func colors() pal {
	fc := theme.Float()
	light := theme.IsLight()
	wash, word := 0.16, 0.42
	if light {
		wash, word = 0.13, 0.32
	}
	return pal{
		bg:       fc.Bg,
		sunken:   theme.Mix(theme.BgFloat, theme.TextPrimary, 0.05),
		addBg:    theme.Mix(theme.BgFloat, theme.GitAdded, wash),
		delBg:    theme.Mix(theme.BgFloat, theme.GitDeleted, wash),
		addWord:  theme.Mix(theme.BgFloat, theme.GitAdded, word),
		delWord:  theme.Mix(theme.BgFloat, theme.GitDeleted, word),
		blank:    theme.Mix(theme.BgFloat, theme.TextPrimary, 0.025),
		text:     fc.Text,
		muted:    fc.Sub,
		dim:      fc.Dim,
		add:      theme.LG(theme.GitAdded),
		del:      theme.LG(theme.GitDeleted),
		mod:      theme.LG(theme.GitModified),
		accent:   theme.LG(theme.AIAccent),
		hair:     fc.Hairline,
		sel:      theme.Wash(theme.BgFloat, theme.WashSelectedFocused),
		white:    fc.Title,
		warn:     theme.LG(theme.DiagWarning),
		onAccent: fc.PrimaryFg,
		primary:  fc.Primary,
	}
}

// ── Painting helpers ──────────────────────────────────────────────────────

// seg is one styled run of plain text.
type seg struct {
	text string
	st   lipgloss.Style
}

// expandTabs replaces tabs (and drops \r and control characters).
func expandTabs(s string) string {
	if !strings.ContainsAny(s, "\t\r\x1b") {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch {
		case r == '\t':
			n := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
		case r < 0x20 || r == 0x7f:
		default:
			b.WriteRune(r)
			col += runewidth.RuneWidth(r)
		}
	}
	return b.String()
}

// paint renders segs into exactly width cells: the first skip cells are
// dropped (horizontal scroll), the rest is clipped, and fill pads.
func paint(segs []seg, skip, width int, fill lipgloss.Style) string {
	var b strings.Builder
	used := 0
	for _, s := range segs {
		if used >= width {
			break
		}
		t := s.text
		if skip > 0 {
			w := runewidth.StringWidth(t)
			if w <= skip {
				skip -= w
				continue
			}
			t = cutLeft(t, skip)
			skip = 0
		}
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

// cutLeft drops the first n cells of s (a wide rune cut in half becomes a
// space).
func cutLeft(s string, n int) string {
	w := 0
	for i, r := range s {
		if w >= n {
			return s[i:]
		}
		rw := runewidth.RuneWidth(r)
		if w+rw > n {
			return strings.Repeat(" ", w+rw-n) + s[i+len(string(r)):]
		}
		w += rw
	}
	return ""
}

func fit(s string, w int) string {
	if runewidth.StringWidth(s) > w {
		return runewidth.Truncate(s, w, "…")
	}
	return s
}

// ── Glyphs ────────────────────────────────────────────────────────────────

type glyphs struct{ spark, check, cross, pend, part, left, right, fold, bar, ruler string }

func icons() glyphs {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return glyphs{"*", "+", "x", "o", "~", "<", ">", "...", "|", "="}
	}
	return glyphs{"✦", "✓", "✗", "●", "◐", "‹", "›", "⋯", "▍", "▐"}
}

// ── Layout + render ───────────────────────────────────────────────────────

type hit struct {
	x0, x1, y int // inner coords + 1 (border) — box-relative cells
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
	if m.w <= 0 || m.h <= 0 || len(m.files) == 0 {
		return ""
	}
	return strings.Join(m.layout(true).rows, "\n")
}

// Origin is the card's top-left screen cell.
func (m Model) Origin() (int, int) {
	g := m.geom()
	return g.x, g.y
}

// layout builds the card rows (when draw) and the click targets.
func (m Model) layout(draw bool) layoutOut {
	g := m.geom()
	out := layoutOut{x: g.x, y: g.y, w: g.w, h: g.h}
	f := m.file()
	if f == nil || g.iw < 10 {
		return out
	}
	p := colors()
	ic := icons()
	bg := lipgloss.NewStyle().Background(p.bg)
	inner := make([]string, 0, g.ih)
	addHit := func(x0, w, y int, id string) {
		out.hits = append(out.hits, hit{x0: x0 + 1, x1: x0 + 1 + w, y: y + 1, id: id})
	}

	// Row 0: title · file · tag, then change nav and the view switch.
	{
		left := []seg{
			{" " + ic.spark + " ", lipgloss.NewStyle().Background(p.bg).Foreground(p.accent).Bold(true)},
			{m.title, lipgloss.NewStyle().Background(p.bg).Foreground(p.white).Bold(true)},
			{"  ·  ", bg.Foreground(p.dim)},
			{f.Name, lipgloss.NewStyle().Background(p.bg).Foreground(p.text).Bold(true)},
		}
		if f.Tag != "" {
			left = append(left, seg{"  " + f.Tag, bg.Foreground(p.warn)})
		}
		n := len(f.diff.Hunks)
		pos := "No changes"
		if n > 0 {
			pos = fmt.Sprintf("Change %d of %d", f.focus+1, n)
		}
		btn := lipgloss.NewStyle().Background(p.sunken).Foreground(p.text)
		on := lipgloss.NewStyle().Background(p.sel).Foreground(p.white).Bold(true)
		off := lipgloss.NewStyle().Background(p.sunken).Foreground(p.dim)
		splitSt, uniSt := off, on
		if g.split {
			splitSt, uniSt = on, off
		}
		prev, next := " "+ic.left+" ", " "+ic.right+" "
		mid := " " + pos + " "
		right := []seg{{prev, btn}, {mid, bg.Foreground(p.muted)}, {next, btn},
			{"  ", bg}, {" Split ", splitSt}, {" Unified ", uniSt}, {" ", bg}}
		// Narrow: drop the title, then the file name keeps what is left.
		if segsWidth(left)+segsWidth(right)+2 > g.iw {
			left = append([]seg{left[0]}, left[3:]...)
		}
		x := g.iw - segsWidth(right)
		addHit(x, runewidth.StringWidth(prev), 0, "prev")
		x += runewidth.StringWidth(prev) + runewidth.StringWidth(mid)
		addHit(x, runewidth.StringWidth(next), 0, "next")
		x += runewidth.StringWidth(next) + 2
		addHit(x, 7, 0, "mode-split")
		addHit(x+7, 9, 0, "mode-unified")
		if draw {
			inner = append(inner, joinLR(left, right, g.iw, bg))
		}
	}

	// Row 1: the side labels over the panes, the file's stats on the right.
	{
		chip := func(label string, c, tint lipgloss.Color) []seg {
			return []seg{{ic.bar, lipgloss.NewStyle().Background(tint).Foreground(c)},
				{label + " ", lipgloss.NewStyle().Background(tint).Foreground(p.text)}}
		}
		oldC, newC := chip("− Current", p.del, p.delBg), chip("+ Proposed", p.add, p.addBg)
		if !m.review {
			oldC, newC = chip("− Old", p.del, p.delBg), chip("+ New", p.add, p.addBg)
		}
		var left []seg
		left = append(left, seg{strings.Repeat(" ", g.diffX+1), bg})
		left = append(left, oldC...)
		if g.split {
			half := (g.diffW - 1) / 2
			if pad := half + 1 - segsWidth(oldC); pad > 0 {
				left = append(left, seg{strings.Repeat(" ", pad), bg})
			}
		} else {
			left = append(left, seg{"  ", bg})
		}
		left = append(left, newC...)
		n := len(f.diff.Hunks)
		word := "changes"
		if n == 1 {
			word = "change"
		}
		right := []seg{
			{fmt.Sprintf("+%d", f.diff.Added), bg.Foreground(p.add).Bold(true)},
			{" ", bg},
			{fmt.Sprintf("−%d", f.diff.Removed), bg.Foreground(p.del).Bold(true)},
			{fmt.Sprintf(" · %d %s ", n, word), bg.Foreground(p.dim)},
		}
		if segsWidth(left)+segsWidth(right)+1 > g.iw {
			right = nil
		}
		if draw {
			inner = append(inner, joinLR(left, right, g.iw, bg))
		}
	}
	if draw {
		inner = append(inner, theme.Hairline(g.iw, theme.BgFloat))
	}

	// Body.
	vr := m.vrows(g)
	scroll := f.scroll
	if mx := len(vr) - g.bodyH; scroll > mx {
		scroll = max(mx, 0)
	}
	list := m.fileList(g, p, ic)
	ruler := rulerCells(vr, scroll, g.bodyH, f, p)
	sep := lipgloss.NewStyle().Background(p.bg).Foreground(p.hair).Render("│")
	for r := 0; r < g.bodyH; r++ {
		y := g.bodyTop + r
		var line string
		if g.listW > 0 {
			if r < len(list) {
				addHit(0, g.listW-1, y, list[r].id)
			}
			if draw {
				line = listRow(list, r, g.listW-1, p) + sep
			}
		}
		k := scroll + r
		if k < len(vr) {
			row := vr[k]
			switch row.kind {
			case vHead:
				if m.review && !f.done && len(f.dec) > 0 {
					bx := g.diffX + g.diffW - headButtonsW(f.dec[row.hunk])
					a, rj := headButtons(f.dec[row.hunk])
					addHit(bx, runewidth.StringWidth(a), y, "hacc:"+strconv.Itoa(row.hunk))
					addHit(bx+runewidth.StringWidth(a)+1, runewidth.StringWidth(rj), y, "hrej:"+strconv.Itoa(row.hunk))
				}
				addHit(g.diffX, g.diffW/2, y, "hunk:"+strconv.Itoa(row.hunk))
			case vBand:
				addHit(g.diffX, g.diffW, y, "band:"+strconv.Itoa(row.key))
			}
			if draw {
				line += m.renderRow(row, g, f, p, ic)
			}
		} else if draw {
			line += bg.Render(strings.Repeat(" ", g.diffW))
		}
		if draw {
			line += ruler[r]
			inner = append(inner, line)
		}
	}

	// Footer.
	if draw {
		inner = append(inner, theme.Hairline(g.iw, theme.BgFloat))
	}
	{
		var left []seg
		var right []seg
		var ids []string
		if m.review {
			acc, rej := 0, 0
			for _, d := range f.dec {
				switch d {
				case Accepted:
					acc++
				case Rejected:
					rej++
				}
			}
			switch {
			case f.done:
				left = []seg{{" " + ic.check + " " + f.summary, bg.Foreground(p.add).Bold(true)}}
				if m.Pending() > 0 {
					right = append(right, seg{" Next file " + ic.right + " ", buttonStyle(p, "primary")}, seg{" ", bg})
					ids = append(ids, "next-file")
				}
			default:
				status := fmt.Sprintf(" %d of %d accepted", acc, len(f.dec))
				if rej > 0 {
					status += fmt.Sprintf(" · %d rejected", rej)
				}
				if len(m.files) > 1 {
					status += fmt.Sprintf(" · file %d of %d", m.cur+1, len(m.files))
				}
				left = []seg{{status, bg.Foreground(p.muted)}}
				for _, b := range []struct{ label, id, kind string }{
					{" " + ic.cross + " Reject file ", "reject-file", "danger"},
					{" Accept & edit ", "edit", ""},
					{" Apply selected ", "apply", ""},
					{" " + ic.check + " Accept file ", "accept-file", "primary"},
				} {
					right = append(right, seg{b.label, buttonStyle(p, b.kind)}, seg{" ", bg})
					ids = append(ids, b.id)
				}
			}
		} else {
			left = []seg{{" " + f.Name, bg.Foreground(p.muted)}}
			right = []seg{{" Close ", buttonStyle(p, "")}, {" ", bg}}
			ids = []string{"hide"}
		}
		rw := segsWidth(right)
		x := g.iw - rw
		if x < segsWidth(left)+1 {
			// Too narrow: keep the buttons, drop the status.
			left = nil
			x = max(g.iw-rw, 0)
		}
		for i, id := range ids {
			w := runewidth.StringWidth(right[2*i].text)
			addHit(x, w, g.ih-2, id)
			x += w + 1
		}
		if draw {
			inner = append(inner, joinLR(left, right, g.iw, bg))
		}
	}
	if draw {
		var hints string
		if m.review {
			hints = theme.HintBar(p.bg, "a/r", "accept/reject change", "n/p", "next/prev", "A/R", "whole file", "↵", "apply", "e", "edit", "t", "view", "esc", "hide")
		} else {
			hints = theme.HintBar(p.bg, "n/p", "next/prev change", "t", "split/unified", "z", "unfold", "←→", "scroll", "esc", "close")
		}
		if len(m.files) > 1 {
			hints = theme.HintBar(p.bg, "tab", "next file") + bg.Render("   ") + hints
		}
		inner = append(inner, padStyled(bg.Render(" ")+hints, g.iw, bg))
		out.rows = strings.Split(theme.FloatBox(strings.Join(inner, "\n"), g.w), "\n")
	}
	return out
}

func buttonStyle(p pal, kind string) lipgloss.Style {
	switch kind {
	case "primary":
		return lipgloss.NewStyle().Background(p.primary).Foreground(p.onAccent).Bold(true)
	case "danger":
		return lipgloss.NewStyle().Background(p.delBg).Foreground(p.text)
	}
	return lipgloss.NewStyle().Background(p.sunken).Foreground(p.text)
}

func segsWidth(s []seg) int {
	w := 0
	for _, x := range s {
		w += runewidth.StringWidth(x.text)
	}
	return w
}

// joinLR renders left, pads, then right, in exactly width cells.
func joinLR(left, right []seg, width int, bg lipgloss.Style) string {
	rw := segsWidth(right)
	l := paint(left, 0, max(width-rw, 0), bg)
	if rw == 0 {
		return l
	}
	return l + paint(right, 0, rw, bg)
}

// padStyled pads a styled string to width cells (clipping it).
func padStyled(s string, width int, bg lipgloss.Style) string {
	w := lipgloss.Width(s)
	if w > width {
		s = ansi.Truncate(s, width, "")
		w = lipgloss.Width(s)
	}
	return s + bg.Render(strings.Repeat(" ", width-w))
}

// ── File list ─────────────────────────────────────────────────────────────

type listItem struct {
	text  []seg
	id    string
	sel   bool
	title bool
}

func (m Model) fileList(g geom, p pal, ic glyphs) []listItem {
	if g.listW == 0 {
		return nil
	}
	bg := lipgloss.NewStyle().Background(p.bg)
	w := g.listW - 1
	out := []listItem{{text: []seg{{fmt.Sprintf(" FILES  %d", len(m.files)), bg.Foreground(p.dim).Bold(true)}}, title: true}}
	for i, f := range m.files {
		glyph, gc := ic.pend, p.accent
		if !m.review {
			glyph, gc = "•", p.dim
		} else if f.done {
			acc := 0
			for _, d := range f.dec {
				if d == Accepted {
					acc++
				}
			}
			switch {
			case acc == 0:
				glyph, gc = ic.cross, p.del
			case acc < len(f.dec):
				glyph, gc = ic.part, p.mod
			default:
				glyph, gc = ic.check, p.add
			}
		}
		sbg := p.bg
		if i == m.cur {
			sbg = p.sel
		}
		st := lipgloss.NewStyle().Background(sbg)
		stats := fmt.Sprintf("+%d −%d ", f.diff.Added, f.diff.Removed)
		nameW := w - 3 - runewidth.StringWidth(stats) - 1
		name := fit(f.Name, max(nameW, 4))
		pad := max(w-3-runewidth.StringWidth(name)-runewidth.StringWidth(stats), 1)
		fg := p.text
		if f.done {
			fg = p.dim
		}
		out = append(out, listItem{
			id:  "file:" + strconv.Itoa(i),
			sel: i == m.cur,
			text: []seg{{" " + glyph + " ", st.Foreground(gc)}, {name, st.Foreground(fg).Bold(i == m.cur)},
				{strings.Repeat(" ", pad), st}, {stats, st.Foreground(p.dim)}},
		})
	}
	return out
}

func listRow(list []listItem, r, w int, p pal) string {
	bg := lipgloss.NewStyle().Background(p.bg)
	if r >= len(list) {
		return bg.Render(strings.Repeat(" ", w))
	}
	fill := bg
	if list[r].sel {
		fill = lipgloss.NewStyle().Background(p.sel)
	}
	return paint(list[r].text, 0, w, fill)
}

// ── Diff rows ─────────────────────────────────────────────────────────────

func headButtons(d Decision) (string, string) {
	ic := icons()
	switch d {
	case Accepted:
		return " " + ic.check + " Accepted ", " Undo "
	case Rejected:
		return " Undo ", " " + ic.cross + " Rejected "
	}
	return " " + ic.check + " Accept ", " " + ic.cross + " Reject "
}

func headButtonsW(d Decision) int {
	a, r := headButtons(d)
	return runewidth.StringWidth(a) + 1 + runewidth.StringWidth(r) + 1
}

func (m Model) renderRow(row vrow, g geom, f *fileState, p pal, ic glyphs) string {
	bg := lipgloss.NewStyle().Background(p.bg)
	switch row.kind {
	case vNote:
		msg := "No changes: the proposed text is the same as the current text."
		if row.n == 1 {
			msg = "Only the final newline differs."
		}
		return paint([]seg{{"   " + msg, bg.Foreground(p.dim).Italic(true)}}, 0, g.diffW, bg)
	case vBand:
		st := lipgloss.NewStyle().Background(p.sunken)
		word := "lines"
		if row.n == 1 {
			word = "line"
		}
		return paint([]seg{
			{"  " + ic.fold + "  ", st.Foreground(p.dim)},
			{fmt.Sprintf("%d unchanged %s", row.n, word), st.Foreground(p.muted)},
			{"   click or z to unfold", st.Foreground(p.dim)},
		}, 0, g.diffW, st)
	case vHead:
		return m.renderHead(row, g, f, p, ic)
	}
	rej := m.review && row.hunk >= 0 && row.hunk < len(f.dec) && f.dec[row.hunk] == Rejected
	rows := f.diff.Rows
	if !g.split {
		return renderCell(rows[row.a], 0, g.numW, g.diffW, f.hscroll, p, rej)
	}
	lw := (g.diffW - 1) / 2
	rw := g.diffW - 1 - lw
	var l, r string
	if row.a >= 0 {
		l = renderCell(rows[row.a], 1, g.numW, lw, f.hscroll, p, rej)
	} else {
		l = blankCell(lw, p)
	}
	if row.b >= 0 {
		r = renderCell(rows[row.b], 2, g.numW, rw, f.hscroll, p, rej)
	} else {
		r = blankCell(rw, p)
	}
	return l + lipgloss.NewStyle().Background(p.bg).Foreground(p.hair).Render("│") + r
}

func (m Model) renderHead(row vrow, g geom, f *fileState, p pal, ic glyphs) string {
	focus := row.hunk == f.focus
	hbg := p.bg
	if focus {
		hbg = p.sel
	}
	st := lipgloss.NewStyle().Background(hbg)
	n := max(len(f.diff.Hunks), 1)
	lead := "  "
	if focus {
		lead = ic.bar + " "
	}
	segs := []seg{{lead, st.Foreground(p.accent)}, {fmt.Sprintf("Change %d of %d", row.hunk+1, n), st.Foreground(p.white).Bold(true)}}
	if row.hunk < len(f.diff.Hunks) {
		h := f.diff.Hunks[row.hunk]
		segs = append(segs, seg{" · ", st.Foreground(p.dim)},
			seg{fmt.Sprintf("+%d", h.NewCount), st.Foreground(p.add)}, seg{" ", st},
			seg{fmt.Sprintf("−%d", h.OldCount), st.Foreground(p.del)},
			seg{" · " + hunkLines(h), st.Foreground(p.dim)})
	}
	if !m.review || len(f.dec) == 0 {
		return paint(segs, 0, g.diffW, st)
	}
	d := f.dec[row.hunk]
	var right []seg
	if !f.done {
		a, r := headButtons(d)
		as, rs := buttonStyle(p, ""), buttonStyle(p, "")
		switch d {
		case Accepted:
			as = lipgloss.NewStyle().Background(p.addBg).Foreground(p.add).Bold(true)
		case Rejected:
			rs = lipgloss.NewStyle().Background(p.delBg).Foreground(p.del).Bold(true)
		default:
			as = lipgloss.NewStyle().Background(p.addWord).Foreground(p.white).Bold(true)
			rs = lipgloss.NewStyle().Background(p.delBg).Foreground(p.text)
		}
		right = []seg{{a, as}, {" ", st}, {r, rs}, {" ", st}}
	} else {
		switch d {
		case Accepted:
			right = []seg{{ic.check + " applied ", st.Foreground(p.add)}}
		default:
			right = []seg{{ic.cross + " not applied ", st.Foreground(p.dim)}}
		}
	}
	return joinLR(segs, right, g.diffW, st)
}

func hunkLines(h textdiff.Hunk) string {
	if h.NewCount == 0 {
		return fmt.Sprintf("removes line %d", h.OldStart+1) + plural(h.OldCount, h.OldStart)
	}
	if h.NewCount == 1 {
		return fmt.Sprintf("line %d", h.NewStart+1)
	}
	return fmt.Sprintf("lines %d–%d", h.NewStart+1, h.NewStart+h.NewCount)
}

func plural(n, start int) string {
	if n <= 1 {
		return ""
	}
	return fmt.Sprintf("–%d", start+n)
}

func blankCell(w int, p pal) string {
	return lipgloss.NewStyle().Background(p.blank).Render(strings.Repeat(" ", max(w, 0)))
}

// renderCell renders one diff row in width cells. side: 0 unified (two
// number columns), 1 old pane, 2 new pane.
func renderCell(r textdiff.Row, side int, numW, width, hscroll int, p pal, rejected bool) string {
	var rowBg, wordBg, mark lipgloss.Color
	marker := " "
	textFg := p.muted
	switch r.Kind {
	case textdiff.Added:
		rowBg, wordBg, mark, marker, textFg = p.addBg, p.addWord, p.add, "+", p.text
	case textdiff.Removed:
		rowBg, wordBg, mark, marker, textFg = p.delBg, p.delWord, p.del, "-", p.text
	default:
		rowBg, mark = p.bg, p.dim
	}
	if rejected && r.Kind != textdiff.Context {
		// A rejected change is drawn faded: it will not be applied.
		rowBg, wordBg, textFg = p.blank, p.blank, p.dim
	}
	st := lipgloss.NewStyle().Background(rowBg)
	num := func(n int) string {
		if n == 0 {
			return strings.Repeat(" ", numW)
		}
		s := strconv.Itoa(n)
		return strings.Repeat(" ", max(numW-len(s), 0)) + s
	}
	var gutter []seg
	switch side {
	case 0:
		gutter = []seg{{" " + num(r.OldLine) + " " + num(r.NewLine) + " ", st.Foreground(p.dim)}}
	case 1:
		gutter = []seg{{" " + num(r.OldLine) + " ", st.Foreground(p.dim)}}
	default:
		gutter = []seg{{" " + num(r.NewLine) + " ", st.Foreground(p.dim)}}
	}
	gutter = append(gutter, seg{marker + " ", st.Foreground(mark).Bold(true)})
	gw := segsWidth(gutter)
	var code []seg
	base := st.Foreground(textFg)
	if r.Paired && len(r.Spans) > 0 && !rejected {
		hi := lipgloss.NewStyle().Background(wordBg).Foreground(p.white).Bold(true)
		for _, s := range r.Spans {
			t := expandTabs(s.Text)
			if s.Changed {
				code = append(code, seg{t, hi})
			} else {
				code = append(code, seg{t, base})
			}
		}
	} else {
		code = []seg{{expandTabs(r.Text), base}}
	}
	return paint(gutter, 0, gw, st) + paint(code, hscroll, max(width-gw, 0), st)
}

// rulerCells is the overview ruler: a tick where changes are, a lighter
// thumb where the view is.
func rulerCells(vr []vrow, scroll, h int, f *fileState, p pal) []string {
	out := make([]string, h)
	n := len(vr)
	track := lipgloss.NewStyle().Background(p.sunken)
	thumb := lipgloss.NewStyle().Background(theme.Mix(theme.BgFloat, theme.TextPrimary, 0.16))
	ic := icons()
	for r := 0; r < h; r++ {
		st := track
		lo, hi := r, r+1
		if n > h {
			lo, hi = r*n/h, (r+1)*n/h
			if hi <= lo {
				hi = lo + 1
			}
		}
		if lo < scroll+h && hi > scroll && n > h {
			st = thumb
		}
		var add, del bool
		for k := lo; k < hi && k < n; k++ {
			v := vr[k]
			if v.kind != vLine || v.hunk < 0 {
				continue
			}
			for _, i := range []int{v.a, v.b} {
				if i < 0 {
					continue
				}
				switch f.diff.Rows[i].Kind {
				case textdiff.Added:
					add = true
				case textdiff.Removed:
					del = true
				}
			}
		}
		switch {
		case add && del:
			out[r] = st.Foreground(p.mod).Render(ic.ruler)
		case add:
			out[r] = st.Foreground(p.add).Render(ic.ruler)
		case del:
			out[r] = st.Foreground(p.del).Render(ic.ruler)
		default:
			out[r] = st.Render(" ")
		}
	}
	return out
}
