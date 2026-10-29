package profileform

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/theme"
)

// The picker is drawn as its own card in the form's place (same width):
// the title, the search field, the list (it scrolls), then the
// "+ Add another AI" row and the key hints, which stay.

// pickTarget is a clickable row of the picker (k: index into pickRows).
type pickTarget struct{ y, k int }

// pickFixed is the rows of the picker card that do not scroll: title,
// rule, search, note, rule, Add, rule, hints.
const pickFixed = 8

// pickView is how many list rows show.
func (m Model) pickView(list int) int {
	room := m.h - 2 - 2 - pickFixed
	return min(list, max(room, 3))
}

// pickVisible scrolls the list so the cursor row shows.
func (m *Model) pickVisible() {
	rows := m.pickRows()
	list := len(rows) - 1 // the Add row stays below the list
	if m.pickEmpty(rows) {
		list++ // the "no model matches" line
	}
	view := m.pickView(list)
	cur := m.pick.cur
	if m.pickEmpty(rows) {
		cur++
	}
	if m.pick.cur < len(rows)-1 {
		switch {
		case cur < m.pick.scroll:
			m.pick.scroll = cur
		case cur >= m.pick.scroll+view:
			m.pick.scroll = cur - view + 1
		}
	}
	m.pick.scroll = min(max(m.pick.scroll, 0), max(list-view, 0))
}

// pickFrame is the picker card's inner rows and its clickable rows.
func (m Model) pickFrame() ([]string, []pickTarget) {
	innerW := m.width() - 2
	f := theme.Float()
	bg := lipgloss.NewStyle().Background(f.Bg)
	text := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Text)
	sub := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Sub).Bold(true)
	dim := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Dim)
	accent := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Primary)
	danger := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Danger)
	selSt := lipgloss.NewStyle().Background(f.SelBg).Foreground(f.SelFg)
	selDim := lipgloss.NewStyle().Background(f.SelBg).Foreground(f.Dim)
	boxSt := lipgloss.NewStyle().Background(f.InputBg).Foreground(f.InputFg)
	boxDim := lipgloss.NewStyle().Background(f.InputBg).Foreground(f.Dim)
	caretSt := lipgloss.NewStyle().Background(f.InputBg).Foreground(f.Icon)
	rule := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Hairline).Render(strings.Repeat("─", innerW))

	step := m.pick.step
	title := "Model"
	if step >= 0 && step < len(m.steps) {
		t := strings.TrimSpace(m.steps[step].title)
		if t == "" {
			t = m.steps[step].id
		}
		title = fmt.Sprintf("Model for step %d · %s", step+1, t)
	}
	titleSt := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Title).Bold(true)
	out := []string{padTo(bg.Render("  ")+titleSt.Render(runewidth.Truncate(title, innerW-4, "…")), innerW, bg), theme.Hairline(innerW, theme.BgFloat)}

	// Search: typing always goes here.
	const lw = 9
	bw := max(innerW-4-lw, 10)
	var box string
	q := []rune(m.pick.q)
	if len(q) == 0 {
		box = padTo(boxSt.Render(" ")+caretSt.Render("▏")+boxDim.Render(runewidth.Truncate("type a model or AI name", bw-3, "…")), bw, boxSt)
	} else {
		shown := string(q)
		for runewidth.StringWidth(shown) > bw-3 {
			shown = string([]rune(shown)[1:])
		}
		box = padTo(boxSt.Render(" "+shown)+caretSt.Render("▏"), bw, boxSt)
	}
	out = append(out, padTo(bg.Render("  ")+dim.Render(runewidth.FillRight("Search", lw))+box, innerW, bg))
	note := bg.Render("")
	if m.pick.note != "" {
		note = bg.Render("  ") + danger.Render(theme.GlyphFail.String()+" ") + text.Render(runewidth.Truncate(m.pick.note, innerW-6, "…"))
	}
	out = append(out, padTo(note, innerW, bg))

	rows := m.pickRows()
	cur := m.steps[step].ch
	var tg []pickTarget
	var list []string
	var listK []int // the row index of each list line (-1: not a row)
	if m.pickEmpty(rows) {
		list = append(list, padTo(bg.Render("  ")+dim.Render(runewidth.Truncate("No model matches “"+strings.TrimSpace(m.pick.q)+"”.", innerW-4, "…")), innerW, bg))
		listK = append(listK, -1)
	}
	render := func(k int, o pickOpt) string {
		if o.head {
			s := bg.Render("  ") + sub.Render(o.label)
			if o.hint != "" {
				s += bg.Render("  ") + dim.Render(runewidth.Truncate(o.hint, max(innerW-6-lipgloss.Width(o.label), 4), "…"))
			}
			return padTo(s, innerW, bg)
		}
		on := k == m.pick.cur
		st, hs, ms := text, dim, accent
		if on {
			st, hs, ms = selSt, selDim, lipgloss.NewStyle().Background(f.SelBg).Foreground(f.Primary)
		}
		if o.add {
			st = accent
			if on {
				st = selSt.Bold(true)
			}
		}
		mark := "  "
		if !o.add && !o.other && o.ch == cur {
			mark = theme.GlyphOK.String() + " "
		}
		w := innerW - 4
		lead := "  "
		if !o.add {
			lead = "    "
			w -= 2
		}
		label := runewidth.Truncate(o.label, max(w-2, 4), "…")
		cell := ms.Render(mark) + st.Render(label)
		if o.hint != "" && runewidth.StringWidth(label)+4+runewidth.StringWidth(o.hint) < w-2 {
			cell += st.Render("  ") + hs.Render(o.hint)
		}
		fill := st
		if !on {
			fill = bg
		}
		mk := bg.Render(lead)
		if on {
			mk = bg.Render(lead[:len(lead)-2]) + accent.Render("▸ ")
		}
		return padTo(mk+padTo(cell, w, fill), innerW, bg)
	}
	for k, o := range rows[:len(rows)-1] {
		list = append(list, render(k, o))
		listK = append(listK, k)
		if o.head {
			listK[len(listK)-1] = -1
		}
	}
	view := m.pickView(len(list))
	scroll := min(max(m.pick.scroll, 0), max(len(list)-view, 0))
	for n := scroll; n < scroll+view; n++ {
		if listK[n] >= 0 {
			tg = append(tg, pickTarget{y: len(out), k: listK[n]})
		}
		out = append(out, list[n])
	}
	more := ""
	switch {
	case scroll > 0 && scroll+view < len(list):
		more = "▲ ▼"
	case scroll > 0:
		more = "▲"
	case scroll+view < len(list):
		more = "▼"
	}
	if more != "" { // say there is more on the title row
		head := padTo(bg.Render("  ")+titleSt.Render(runewidth.Truncate(title, innerW-10, "…")), innerW-2-runewidth.StringWidth(more), bg)
		out[0] = padTo(head+dim.Render(more), innerW, bg)
	}
	out = append(out, rule)
	addK := len(rows) - 1
	tg = append(tg, pickTarget{y: len(out), k: addK})
	out = append(out, render(addK, rows[addK]))
	out = append(out, rule)
	h := []string{"↑↓", "move", "enter", "choose", "esc", "back", "type", "search"}
	for len(h) > 4 && runewidth.StringWidth(strings.Join(h, "    "))+len(h)*2 > innerW-6 {
		h = h[:len(h)-2]
	}
	out = append(out, padTo(bg.Render("  ")+theme.HintBar(f.Bg, h...), innerW, bg))
	return out, tg
}

// pickRects lists the picker's rows on screen (the hit map, tests).
func (m Model) pickRects(ox, oy int) []ButtonRect {
	_, tg := m.pickFrame()
	rows := m.pickRows()
	var out []ButtonRect
	for _, t := range tg {
		out = append(out, ButtonRect{Title: rows[t.k].label, X: ox + 1 + 2, Y: oy + 1 + t.y, W: m.width() - 2 - 4})
	}
	return out
}

// pickMouse: a click on a row chooses it; the wheel scrolls the list.
func (m Model) pickMouse(x, y int, button tea.MouseButton) (Model, tea.Cmd) {
	switch button {
	case tea.MouseButtonWheelUp:
		m.pick.scroll = max(m.pick.scroll-2, 0)
		return m, nil
	case tea.MouseButtonWheelDown:
		m.pick.scroll += 2
		rows := m.pickRows()
		list := len(rows) - 1
		if m.pickEmpty(rows) {
			list++
		}
		m.pick.scroll = min(m.pick.scroll, max(list-m.pickView(list), 0))
		return m, nil
	case tea.MouseButtonLeft:
	default:
		return m, nil
	}
	ox, oy := m.Origin()
	_, tg := m.pickFrame()
	rx, ry := x-ox-1, y-oy-1
	if rx < 2 || rx >= m.width()-2-2 {
		return m, nil
	}
	for _, t := range tg {
		if t.y == ry {
			m.pick.cur = t.k
			return m.pickChoose(t.k)
		}
	}
	return m, nil
}
