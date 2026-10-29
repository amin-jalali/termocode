package mergeview

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Layout of View (h rows):
//
//	row 0      header: file, "Conflict 2 of 5"
//	row 1      pane titles (wide) or the 1 · 2 · 3 pane tabs (narrow)
//	rows 2..   the panes
//	last row   key hints (when h ≥ 5)

// FocusHint is the hint shown while the result (the editor) has the keys.
const FocusHint = "F6 or a click here: merge keys"

const gutterW = 5 // "  12▎"

// row is one displayed line of a pane.
type row struct {
	line  int // 0-based line of the side; -1 for the "nothing here" row
	text  string
	block int // block index, -1 outside every block
	first bool
}

// accent is the pane's colour.
func accent(p Pane) theme.Color256 {
	switch p {
	case PaneOurs:
		return theme.GitAdded
	case PaneTheirs:
		return theme.AccentBlue
	}
	return theme.TextMuted
}

// rows lays out a pane: the side's lines, block lines marked, and one
// "nothing here" row where a block is empty on this side.
func (m Model) rows(p Pane) []row {
	side := m.sides[p]
	blockAt := make([]int, len(side)+1)
	for i := range blockAt {
		blockAt[i] = -1
	}
	empty := map[int][]int{} // line → empty blocks placed before it
	for i, b := range m.blocks {
		r := b.Ranges[p]
		if r[0] == r[1] {
			empty[r[0]] = append(empty[r[0]], i)
			continue
		}
		for l := r[0]; l < r[1] && l < len(side); l++ {
			blockAt[l] = i
		}
	}
	out := make([]row, 0, len(side)+len(empty))
	for l := 0; l <= len(side); l++ {
		for _, bi := range empty[l] {
			out = append(out, row{line: -1, block: bi, first: true})
		}
		if l == len(side) {
			break
		}
		first := blockAt[l] >= 0 && (l == 0 || blockAt[l-1] != blockAt[l])
		out = append(out, row{line: l, text: side[l], block: blockAt[l], first: first})
	}
	return out
}

// top is the first row shown: the current block a third down the pane.
func (m Model) top(rs []row, h int) int {
	at := 0
	for i, r := range rs {
		if r.block == m.cur && r.first {
			at = i
			break
		}
	}
	t := at - h/3 + m.scroll
	return max(min(t, len(rs)-h), 0)
}

// fit truncates or pads plain text to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = strings.ReplaceAll(s, "\t", "    ")
	if runewidth.StringWidth(s) > w {
		s = runewidth.Truncate(s, w-1, "") + "…"
	}
	return s + strings.Repeat(" ", max(w-runewidth.StringWidth(s), 0))
}

// renderPane draws pane p as h rows of w cells.
func (m Model) renderPane(p Pane, w, h int) []string {
	bg := theme.LG(theme.BgEditor)
	acc := accent(p)
	text := lipgloss.NewStyle().Background(bg).Foreground(theme.LG(theme.TextPrimary))
	num := lipgloss.NewStyle().Background(bg).Foreground(theme.LG(theme.TextDim))
	out := make([]string, 0, h)
	if p == PaneBase && m.noBase {
		msg := num.Italic(true).Render(fit(" No base: both sides added this file", w))
		out = append(out, msg)
	}
	rs := m.rows(p)
	start := m.top(rs, h-len(out))
	for i := start; i < len(rs) && len(out) < h; i++ {
		r := rs[i]
		tx, nm := text, num
		mark := " "
		if r.block >= 0 {
			t := 0.12
			if r.block == m.cur {
				t = 0.30
			}
			rbg := theme.Mix(theme.BgEditor, acc, t)
			tx = tx.Background(rbg)
			nm = nm.Background(rbg).Foreground(theme.LG(acc))
			mark = "▎"
		}
		g := ""
		switch {
		case r.first:
			g = fmt.Sprintf("%*s", gutterW-1, "#"+strconv.Itoa(r.block+1))
			nm = nm.Bold(true)
		case r.line >= 0:
			g = fmt.Sprintf("%*d", gutterW-1, r.line+1)
		default:
			g = strings.Repeat(" ", gutterW-1)
		}
		body := r.text
		if r.line < 0 {
			body = "┄ nothing here"
			tx = tx.Foreground(theme.LG(theme.TextDim)).Italic(true)
		}
		out = append(out, nm.Render(fit(g, gutterW-1)+mark)+tx.Render(fit(" "+body, w-gutterW)))
	}
	for len(out) < h {
		out = append(out, text.Render(strings.Repeat(" ", max(w, 0))))
	}
	return out
}

// paneTitle is "Ours · HEAD".
func (m Model) paneTitle(p Pane) string {
	t := p.Title()
	if l := m.labels[p]; l != "" {
		t += " · " + l
	}
	return t
}

// widths splits w into three pane widths and two 1-cell rules.
func widths(w int) [paneCount]int {
	inner := max(w-2, 3)
	a := inner / 3
	return [paneCount]int{a, a, inner - 2*a}
}

// View draws the merge view: exactly h rows of w cells.
func (m Model) View() string {
	w, h := m.w, m.h
	if w <= 0 || h <= 0 {
		return ""
	}
	bar := theme.LG(theme.BgTitleBar)
	head := lipgloss.NewStyle().Background(bar).Foreground(theme.LG(theme.TextPrimary))
	dim := head.Foreground(theme.LG(theme.TextDim))
	var rows []string

	// Header.
	where := "No conflicts left · r marks it resolved"
	if n := len(m.blocks); n > 0 {
		where = fmt.Sprintf("Conflict %d of %d", m.cur+1, n)
	}
	left := head.Bold(true).Render(" Merge ") + dim.Render(m.path+"  ") + head.Foreground(theme.LG(theme.AccentAmber)).Render(where)
	rows = append(rows, padStyled(left, w, head))
	if h == 1 {
		return rows[0]
	}

	// Titles or tabs.
	hints := h >= 5
	bodyH := h - 2
	if hints {
		bodyH--
	}
	rule := lipgloss.NewStyle().Background(theme.LG(theme.BgEditor)).Foreground(theme.LG(theme.BorderDefault)).Render("│")
	if m.narrow {
		var tabs strings.Builder
		for p := Pane(0); p < paneCount; p++ {
			st := dim
			if p == m.pane {
				st = head.Bold(true).Foreground(theme.LG(accent(p))).Background(theme.Mix(theme.BgTitleBar, accent(p), 0.18))
			}
			tabs.WriteString(st.Render(fmt.Sprintf(" %d %s ", p+1, m.paneTitle(p))))
		}
		rows = append(rows, padStyled(tabs.String(), w, head))
		if bodyH > 0 {
			rows = append(rows, m.renderPane(m.pane, w, bodyH)...)
		}
	} else {
		ws := widths(w)
		var title strings.Builder
		for p := Pane(0); p < paneCount; p++ {
			if p > 0 {
				title.WriteString(head.Render(" "))
			}
			title.WriteString(head.Bold(true).Foreground(theme.LG(accent(p))).Render(fit(" "+m.paneTitle(p), ws[p])))
		}
		rows = append(rows, padStyled(title.String(), w, head))
		if bodyH > 0 {
			cols := [paneCount][]string{}
			for p := Pane(0); p < paneCount; p++ {
				cols[p] = m.renderPane(p, ws[p], bodyH)
			}
			for i := 0; i < bodyH; i++ {
				rows = append(rows, cols[0][i]+rule+cols[1][i]+rule+cols[2][i])
			}
		}
	}
	if hints {
		rows = append(rows, m.hintRow(w))
	}
	return strings.Join(rows[:min(len(rows), h)], "\n")
}

// hintRow is the key-hint row: the merge keys while the view has the
// keys, else how to give it the keys.
func (m Model) hintRow(w int) string {
	bg := theme.LG(theme.BgTitleBar)
	st := lipgloss.NewStyle().Background(bg).Foreground(theme.LG(theme.TextDim))
	if !m.focused {
		return padStyled(st.Render(" "+FocusHint), w, st)
	}
	pairs := []string{"n/p", "conflict", "o", "ours", "t", "theirs", "b", "both", "B", "theirs+ours", "a", "base", "u", "undo", "tab", "edit", "r", "resolved", "esc", "close"}
	if m.narrow {
		// Most needed first: the end is dropped when it does not fit.
		pairs = []string{"o", "ours", "t", "theirs", "b/B", "both", "a", "base", "n/p", "next", "r", "resolved", "tab", "edit", "1-3", "pane", "esc", "close", "u", "undo"}
	}
	// Drop pairs from the end until the row fits.
	for len(pairs) > 2 && ansi.StringWidth(" "+theme.HintBar(bg, pairs...)) > w {
		pairs = pairs[:len(pairs)-2]
	}
	return padStyled(st.Render(" ")+theme.HintBar(bg, pairs...), w, st)
}

// padStyled cuts or pads a styled row to exactly w cells.
func padStyled(s string, w int, fill lipgloss.Style) string {
	if sw := ansi.StringWidth(s); sw > w {
		return ansi.Truncate(s, w, "")
	} else if sw < w {
		return s + fill.Render(strings.Repeat(" ", w-sw))
	}
	return s
}

// Click handles a press at (x, y) relative to the view: a pane tab shows
// that pane; a row of a block makes that block current. It reports
// whether the click hit something.
func (m *Model) Click(x, y int) bool {
	if y == 1 && m.narrow {
		at := 0
		for p := Pane(0); p < paneCount; p++ {
			wd := runewidth.StringWidth(fmt.Sprintf(" %d %s ", p+1, m.paneTitle(p)))
			if x >= at && x < at+wd {
				m.pane = p
				return true
			}
			at += wd
		}
		return false
	}
	bodyH := m.h - 2
	if m.h >= 5 {
		bodyH--
	}
	if y < 2 || y >= 2+bodyH {
		return false
	}
	p := m.pane
	if !m.narrow {
		ws := widths(m.w)
		switch {
		case x < ws[0]:
			p = PaneBase
		case x > ws[0] && x <= ws[0]+ws[1]:
			p = PaneOurs
		case x > ws[0]+ws[1]+1:
			p = PaneTheirs
		default:
			return false // a rule
		}
	}
	rs := m.rows(p)
	off := 0
	if p == PaneBase && m.noBase {
		off = 1
	}
	i := m.top(rs, bodyH-off) + (y - 2 - off)
	if i < 0 || i >= len(rs) || rs[i].block < 0 {
		return false
	}
	m.setCur(rs[i].block)
	return true
}
