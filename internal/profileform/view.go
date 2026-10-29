package profileform

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Look: the shared float card (theme.Float, FloatBox, Hairline,
// HintBar). The body scrolls; the title, the buttons and the key hints
// stay. The card is at most 100 cells wide and never wider than the
// window, so 80x24 shows the same form.

// MaxWidth is the widest the card gets.
const MaxWidth = 100

// target is a click area, relative to the inner area of the card.
type target struct {
	x, y, w int
	it      item
	opt     int // 0 the item; -1 / +1 the arrows of a choice; 10+n the n-th tool preset
}

// rng is the body rows of one item.
type rng struct {
	it     item
	y0, y1 int
}

type line struct {
	s  string
	tg []target // y is filled in when the line is placed
}

type built struct {
	body   []line
	ranges []rng
	foot   []line
}

func (m Model) width() int {
	w := MaxWidth
	if mw := m.w - 4; w > mw {
		w = mw
	}
	return max(w, 30)
}

// viewRows is how many body rows show.
func (m Model) viewRows(b built) int {
	room := m.h - 2 - 2 - 2 - len(b.foot) // border, margin, title and rule, footer
	return min(len(b.body), max(room, 3))
}

func padTo(s string, w int, bg lipgloss.Style) string {
	if cw := lipgloss.Width(s); cw < w {
		return s + bg.Render(strings.Repeat(" ", w-cw))
	}
	return s
}

// build renders the body and the footer rows.
func (m Model) build() built {
	boxW := m.width()
	innerW := boxW - 2
	f := theme.Float()
	bg := lipgloss.NewStyle().Background(f.Bg)
	text := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Text)
	sub := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Sub)
	dim := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Dim)
	accent := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Primary)
	danger := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Danger)
	boxSt := lipgloss.NewStyle().Background(f.InputBg).Foreground(f.InputFg)
	boxDim := lipgloss.NewStyle().Background(f.InputBg).Foreground(f.Dim)
	selSt := lipgloss.NewStyle().Background(f.SelBg).Foreground(f.SelFg)
	caretSt := lipgloss.NewStyle().Background(f.InputBg).Foreground(f.Icon)
	errs := m.Errors()

	var b built
	cur := func() int { return len(b.body) }
	addBody := func(it item, y0 int) { b.ranges = append(b.ranges, rng{it: it, y0: y0, y1: len(b.body) - 1}) }
	row := func(marker bool, s string, tg ...target) {
		mk := bg.Render("  ")
		if marker {
			mk = accent.Render("▸ ")
		}
		l := line{s: padTo(mk+s, innerW, bg), tg: tg}
		b.body = append(b.body, l)
	}
	errRows := func(key string, indent int) {
		msg, ok := errs[key]
		if !ok {
			return
		}
		w := max(innerW-2-indent-2-2, 8)
		for i, l := range wrapText(msg, w) {
			g := "  "
			if i == 0 {
				g = theme.GlyphFail.String() + " "
			}
			b.body = append(b.body, line{s: padTo(bg.Render(strings.Repeat(" ", 2+indent))+danger.Render(g)+text.Render(l), innerW, bg)})
		}
	}
	focusedOn := func(it item) bool { return m.foc == it }

	const lw = 11
	// textBox renders the box of a text item (width bw cells).
	textBox := func(it item, bw int, placeholder string) string {
		v := *(&m).text(it)
		foc := focusedOn(it)
		switch {
		case m.editing && foc:
			r := []rune(v)
			c := min(m.caret, len(r))
			start := 0
			for start < c && runewidth.StringWidth(string(r[start:c]))+2 > bw-1 {
				start++
			}
			before := string(r[start:c])
			after := runewidth.Truncate(string(r[c:]), max(bw-2-runewidth.StringWidth(before), 0), "")
			s := boxSt.Render(" "+before) + caretSt.Render("▏") + boxSt.Render(after)
			return padTo(s, bw, boxSt)
		case v == "":
			st := boxDim
			if foc {
				st = lipgloss.NewStyle().Background(f.SelBg).Foreground(f.Dim)
			}
			return padTo(st.Render(" "+runewidth.Truncate(placeholder, bw-1, "…")), bw, st)
		}
		st := boxSt
		if foc {
			st = selSt
		}
		return padTo(st.Render(" "+runewidth.Truncate(v, bw-2, "…")), bw, st)
	}
	label := func(s string, w int) string { return sub.Render(runewidth.FillRight(s, w)) }

	// A text row: label, box, a click target over the box.
	textRow := func(it item, lbl string, indent int, placeholder, prefix string) {
		y0 := cur()
		bw := max(innerW-2-indent-lw-2, 8)
		pre := ""
		if prefix != "" {
			pre = text.Render(prefix)
			bw -= lipgloss.Width(prefix)
		}
		x := 2 + indent + lw + lipgloss.Width(prefix)
		row(focusedOn(it), bg.Render(strings.Repeat(" ", indent))+label(lbl, lw)+pre+textBox(it, bw, placeholder),
			target{x: x, w: bw, it: it})
		errRows(errKey(it), indent+lw)
		addBody(it, y0)
	}
	// A choice row: ‹ value ›.
	choiceRow := func(it item, lbl string, indent int, value string, ok string) {
		y0 := cur()
		foc := focusedOn(it)
		st := boxSt
		if foc {
			st = selSt
		}
		bw := max(innerW-2-indent-lw-2, 12)
		inner := value
		if ok != "" {
			inner += " " + ok
		}
		inner = runewidth.Truncate(inner, bw-4, "…")
		mid := padTo(st.Render(inner), bw-4, st)
		x := 2 + indent + lw
		s := bg.Render(strings.Repeat(" ", indent)) + label(lbl, lw) + accent.Render("‹ ") + mid + accent.Render(" ›")
		row(foc, s,
			target{x: x, w: 2, it: it, opt: -1},
			target{x: x + 2, w: bw - 4, it: it},
			target{x: x + bw - 2, w: 2, it: it, opt: 1})
		errRows(errKey(it), indent+lw)
		addBody(it, y0)
	}
	slotText := func(name string) (string, string) {
		for _, s := range m.cfg.Slots {
			if s.Name == name {
				if s.KeyOK {
					return name, theme.GlyphOK.String() + " key"
				}
				return name, theme.GlyphFail.String() + " no key"
			}
		}
		return name, ""
	}
	catValue := func(i int) (string, string) {
		if i == 0 {
			return "default", ""
		}
		return slotText(m.cfg.Slots[i-1].Name)
	}

	p := m.Profile()
	textRow(item{kind: iName}, "Name", 0, "letters, digits, - _ .", "")
	textRow(item{kind: iTitle}, "Title", 0, "one line about it (optional)", "")
	textRow(item{kind: iBudget}, "Budget", 0, "no limit; dollars per run", "$ ")
	v, ok := catValue(m.catQ)
	choiceRow(item{kind: iCatQ}, "Quick →", 0, v, ok)
	v, ok = catValue(m.catD)
	choiceRow(item{kind: iCatD}, "Deep →", 0, v, ok)

	b.body = append(b.body, line{s: bg.Render(strings.Repeat(" ", innerW))})
	head := fmt.Sprintf("STEPS  (%d of %d, run in this order", len(m.steps), maxSteps)
	if m.cfg.ParallelMax >= 2 {
		head += "; waits for = side by side"
	}
	head += ")"
	b.body = append(b.body, line{s: padTo(bg.Render("  ")+sub.Render(runewidth.Truncate(head, innerW-4, "…")), innerW, bg)})
	errRows("steps", 0)

	for i, s := range m.steps {
		it := item{iStepHead, i}
		y0 := cur()
		glyph := "▸"
		if i == m.sel {
			glyph = "▾"
		}
		title := strings.TrimSpace(s.title)
		if title == "" {
			title = s.id
		}
		summary := ""
		if i != m.sel {
			name, _ := m.choiceLabel(s.ch)
			summary = " · " + name + " · " + toolPresets[s.tools]
			if need := m.needs(i); need != "" { // say it before the step is opened
				summary += " · " + theme.GlyphFail.String() + " " + need
			}
		}
		hs := text
		if focusedOn(it) {
			hs = selSt
		}
		txt := runewidth.Truncate(fmt.Sprintf(" %s %d  %s%s", glyph, i+1, title, summary), innerW-4, "…")
		row(focusedOn(it), padTo(hs.Render(txt), innerW-4, hs), target{x: 2, w: innerW - 4, it: it})
		if i != m.sel {
			for _, k := range []string{"id", "model", "tools", "prompt", "deps", "step"} {
				if _, bad := errs[fmt.Sprintf("%d:%s", i, k)]; bad {
					b.body = append(b.body, line{s: padTo(bg.Render("      ")+danger.Render(theme.GlyphFail.String()+" "+runewidth.Truncate(errs[fmt.Sprintf("%d:%s", i, k)], innerW-10, "…")), innerW, bg)})
					break
				}
			}
			addBody(it, y0)
			continue
		}
		addBody(it, y0)
		const ind = 3
		textRow(item{iSTitle, i}, "Title", ind, "what the step is called", "")
		textRow(item{iSID, i}, "Id", ind, "step-1", "")
		c := s.ch
		val, kok := m.choiceLabel(c)
		switch {
		case c.cli != "" || c.provider != "":
			if kok != "" {
				kok = "· " + kok
			}
		case c.slot != "":
			val, kok = slotText(c.slot)
		case c.category != "":
			slot := p.SlotFor(p.Steps[i])
			_, kok = slotText(slot)
			val += " → " + slot
		default:
			_, kok = slotText("main")
		}
		choiceRow(item{iSModel, i}, "Model", ind, val, kok)
		m.stepAIRow(&b, i, ind+lw, innerW, bg, text, dim, danger, accent, f)

		// Tools: three radios.
		ty0 := cur()
		tit := item{iSTools, i}
		var tg []target
		ln := bg.Render(strings.Repeat(" ", ind)) + label("Tools", lw)
		x := 2 + ind + lw
		for j, t := range toolPresets {
			gl := theme.GlyphRadioOff.String()
			gs := dim
			st := text
			if s.tools == j {
				gl, gs = theme.GlyphRadioOn.String(), accent
				if focusedOn(tit) {
					st = selSt
				}
			}
			seg := gs.Render(gl) + bg.Render(" ") + st.Render(t)
			tg = append(tg, target{x: x, w: lipgloss.Width(seg), it: tit, opt: 10 + j})
			ln += seg + bg.Render("   ")
			x += lipgloss.Width(seg) + 3
		}
		row(focusedOn(tit), ln, tg...)
		errRows(errKey(tit), ind+lw)
		addBody(tit, ty0)

		// Prompt.
		py0 := cur()
		pit := item{iSPrompt, i}
		bw := max(innerW-2-ind-lw-2, 8)
		rows, ccol, crow := wrapRows([]rune(s.system), bw-3, m.caret)
		if len(rows) == 0 {
			rows = []string{""}
		}
		shown := min(max(len(rows), 3), 6)
		first := 0
		if m.editing && focusedOn(pit) && crow >= shown {
			first = crow - shown + 1
		} else if len(rows) > shown && !m.editing {
			first = 0
		}
		for k := 0; k < shown; k++ {
			lbl := label("", lw)
			if k == 0 {
				lbl = label("Prompt", lw)
			}
			cell := ""
			if first+k < len(rows) {
				cell = rows[first+k]
			}
			st := boxSt
			if focusedOn(pit) && !m.editing {
				st = selSt
			}
			var body string
			switch {
			case s.system == "" && !(m.editing && focusedOn(pit)):
				ph := ""
				if k == 0 {
					ph = "what the model is told (optional)"
				}
				pst := boxDim
				if focusedOn(pit) {
					pst = lipgloss.NewStyle().Background(f.SelBg).Foreground(f.Dim)
				}
				body = padTo(pst.Render(" "+ph), bw, pst)
			case m.editing && focusedOn(pit) && first+k == crow:
				rr := []rune(cell)
				cc := min(ccol, len(rr))
				before, after := string(rr[:cc]), string(rr[cc:])
				body = padTo(boxSt.Render(" "+before)+caretSt.Render("▏")+boxSt.Render(after), bw, boxSt)
			default:
				suffix := ""
				if k == shown-1 && first+shown < len(rows) {
					suffix = " …"
				}
				body = padTo(st.Render(" "+cell+suffix), bw, st)
			}
			row(focusedOn(pit) && k == 0, bg.Render(strings.Repeat(" ", ind))+lbl+body, target{x: 2 + ind + lw, w: bw, it: pit})
		}
		errRows(errKey(pit), ind+lw)
		addBody(pit, py0)

		if m.showDeps(i) {
			textRow(item{iSDeps, i}, "Waits for", ind, "nothing (starts first or after the step before)", "")
		}
		// Planner: a check box.
		cy0 := cur()
		cit := item{iSPlan, i}
		box := "[ ]"
		if s.plan {
			box = "[x]"
		}
		st := text
		if focusedOn(cit) {
			st = selSt
		}
		seg := st.Render(box + " this step plans the run")
		row(focusedOn(cit), bg.Render(strings.Repeat(" ", ind))+label("Planner", lw)+seg,
			target{x: 2 + ind + lw, w: lipgloss.Width(seg), it: cit})
		errRows("plan", ind+lw)
		addBody(cit, cy0)
	}
	if len(m.steps) == 0 {
		b.body = append(b.body, line{s: padTo(bg.Render("    ")+dim.Render("no steps yet: Add step"), innerW, bg)})
	}

	// Footer.
	if _, msg := m.firstProblem(); msg != "" { // why Save… is inactive
		b.foot = append(b.foot, line{s: padTo(bg.Render("  ")+danger.Render(theme.GlyphFail.String()+" ")+text.Render(runewidth.Truncate(msg, innerW-6, "…")), innerW, bg)})
	}
	b.foot = append(b.foot, line{s: lipgloss.NewStyle().Background(f.Bg).Foreground(f.Hairline).Render(strings.Repeat("─", innerW))})
	b.foot = append(b.foot, m.buttonRow(innerW, bg, f, []item{{kind: iAdd}, {kind: iUp}, {kind: iDown}, {kind: iRemove}}, false))
	last := []item{{kind: iSave}, {kind: iCancel}}
	if m.cfg.Editing {
		last = append(last, item{kind: iDelete})
	}
	b.foot = append(b.foot, m.buttonRow(innerW, bg, f, last, true))
	b.foot = append(b.foot, line{s: lipgloss.NewStyle().Background(f.Bg).Foreground(f.Hairline).Render(strings.Repeat("─", innerW))})
	b.foot = append(b.foot, line{s: padTo(bg.Render("  ")+theme.HintBar(f.Bg, m.hints(innerW)...), innerW, bg)})
	return b
}

const maxSteps = 12

var buttonText = map[kind]string{
	iAdd: "Add step", iUp: "Move up", iDown: "Move down", iRemove: "Remove step",
	iSave: "Save…", iCancel: "Cancel", iDelete: "Delete profile…",
}

func (m Model) buttonRow(innerW int, bg lipgloss.Style, f theme.FloatColors, its []item, centre bool) line {
	var sb strings.Builder
	var tg []target
	x := 2
	if centre {
		total := 0
		for _, it := range its {
			total += runewidth.StringWidth(buttonText[it.kind]) + 4 + 2
		}
		x = max((innerW-total+2)/2, 2)
	}
	sb.WriteString(bg.Render(strings.Repeat(" ", x)))
	for i, it := range its {
		if i > 0 {
			sb.WriteString(bg.Render("  "))
			x += 2
		}
		lbl := "  " + buttonText[it.kind] + "  "
		st := lipgloss.NewStyle().Background(f.KeycapBg).Foreground(f.Text)
		switch {
		case it.kind == iSave && !m.Valid():
			st = lipgloss.NewStyle().Background(f.KeycapBg).Foreground(f.Dim)
		case it.kind == iSave:
			st = lipgloss.NewStyle().Background(f.KeycapBg).Foreground(f.Primary)
		case it.kind == iDelete || it.kind == iRemove:
			st = lipgloss.NewStyle().Background(f.KeycapBg).Foreground(f.Danger)
		}
		if m.foc == it {
			st = lipgloss.NewStyle().Background(f.Primary).Foreground(f.PrimaryFg).Bold(true)
		}
		sb.WriteString(st.Render(lbl))
		w := runewidth.StringWidth(lbl)
		tg = append(tg, target{x: x, w: w, it: it})
		x += w
	}
	return line{s: padTo(sb.String(), innerW, bg), tg: tg}
}

func (m Model) hints(innerW int) []string {
	var h []string
	if m.editing {
		h = []string{"esc", "done", "tab", "next field"}
		if m.foc.kind == iSPrompt {
			h = append(h, "enter", "new line")
		} else {
			h = append(h, "enter", "done")
		}
	} else {
		h = []string{"tab", "next", "enter", "edit", "?", "keys", "esc", "cancel", "a", "add step"}
		if m.foc.kind == iSModel {
			h[3] = "choose a model"
		}
	}
	for len(h) > 4 && runewidth.StringWidth(strings.Join(h, "    "))+len(h)*2 > innerW-6 {
		h = h[:len(h)-2]
	}
	return h
}

// wrapRows breaks r into rows of at most w cells (a "\n" starts a row).
// It returns the rows, the caret's column in its row (in runes) and that
// row's index.
func wrapRows(r []rune, w, caret int) (rows []string, ccol, crow int) {
	w = max(w, 4)
	var starts []int
	i := 0
	for {
		end := i
		for end < len(r) && r[end] != '\n' {
			end++
		}
		// the raw line r[i:end]
		s := i
		if s == end {
			starts = append(starts, s)
			rows = append(rows, "")
		}
		for s < end {
			cw, e := 0, s
			for e < end && cw+runewidth.RuneWidth(r[e]) <= w {
				cw += runewidth.RuneWidth(r[e])
				e++
			}
			if e == s {
				e = s + 1
			}
			starts = append(starts, s)
			rows = append(rows, string(r[s:e]))
			s = e
		}
		if end >= len(r) {
			break
		}
		i = end + 1
		if i == len(r) { // a trailing newline: one more empty row
			starts = append(starts, i)
			rows = append(rows, "")
			break
		}
	}
	caret = min(max(caret, 0), len(r))
	for k, st := range starts {
		if st <= caret {
			crow, ccol = k, caret-st
		}
	}
	return rows, ccol, crow
}

func wrapText(s string, w int) []string {
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case runewidth.StringWidth(line)+1+runewidth.StringWidth(word) <= w:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	for i, l := range out {
		out[i] = runewidth.Truncate(l, w, "…")
	}
	return out
}

// frame is the inner rows of the card (title first) and the click
// targets with their rows.
func (m Model) frame() ([]string, []target) {
	boxW := m.width()
	innerW := boxW - 2
	f := theme.Float()
	bg := lipgloss.NewStyle().Background(f.Bg)
	b := m.build()
	view := m.viewRows(b)
	scroll := min(max(m.scroll, 0), max(len(b.body)-view, 0))

	titleSt := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Title).Bold(true)
	title := runewidth.Truncate(m.cfg.Title, innerW-12, "…")
	more := ""
	switch {
	case scroll > 0 && scroll+view < len(b.body):
		more = "▲ ▼"
	case scroll > 0:
		more = "▲"
	case scroll+view < len(b.body):
		more = "▼"
	}
	head := bg.Render("  ") + titleSt.Render(title)
	if gap := innerW - lipgloss.Width(head) - 2 - runewidth.StringWidth(more); gap > 0 {
		head += bg.Render(strings.Repeat(" ", gap)) + lipgloss.NewStyle().Background(f.Bg).Foreground(f.Dim).Render(more) + bg.Render("  ")
	}
	rows := []string{padTo(head, innerW, bg), theme.Hairline(innerW, theme.BgFloat)}
	var tg []target
	place := func(l line) {
		for _, t := range l.tg {
			t.y = len(rows)
			tg = append(tg, t)
		}
		rows = append(rows, padTo(l.s, innerW, bg))
	}
	for _, l := range b.body[scroll : scroll+view] {
		place(l)
	}
	for _, l := range b.foot {
		place(l)
	}
	return rows, tg
}

// ensureVisible scrolls the body so the focused item shows.
func (m *Model) ensureVisible() {
	if m.w <= 0 || m.h <= 0 {
		return
	}
	if m.pick.open {
		m.pickVisible()
		return
	}
	b := m.build()
	view := m.viewRows(b)
	for _, r := range b.ranges {
		if r.it != m.foc {
			continue
		}
		switch {
		case r.y0 < m.scroll:
			m.scroll = r.y0
		case r.y1 >= m.scroll+view:
			m.scroll = r.y1 - view + 1
			if r.y0 < m.scroll {
				m.scroll = r.y0
			}
		}
	}
	m.scroll = min(max(m.scroll, 0), max(len(b.body)-view, 0))
}

// Box renders the card (not placed); with the model picker open, the
// picker's card.
func (m Model) Box() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	if m.pick.open {
		rows, _ := m.pickFrame()
		return theme.FloatBox(strings.Join(rows, "\n"), m.width())
	}
	rows, _ := m.frame()
	return theme.FloatBox(strings.Join(rows, "\n"), m.width())
}

// Origin is the card's top-left cell when centred.
func (m Model) Origin() (x, y int) {
	rows, _ := m.frame()
	if m.pick.open {
		rows, _ = m.pickFrame()
	}
	return max((m.w-m.width())/2, 0), max((m.h-(len(rows)+2))/2, 0)
}

// ButtonRect is a button on screen (tests, the hit map).
type ButtonRect struct {
	Title   string
	X, Y, W int
}

// ButtonRects lists the buttons with their screen cells.
func (m Model) ButtonRects() []ButtonRect {
	ox, oy := m.Origin()
	if m.pick.open {
		return m.pickRects(ox, oy)
	}
	_, tg := m.frame()
	var out []ButtonRect
	for _, t := range tg {
		if t.it.kind.button() {
			out = append(out, ButtonRect{Title: buttonText[t.it.kind], X: ox + 1 + t.x, Y: oy + 1 + t.y, W: t.w})
		}
	}
	return out
}

// HandleMouse: a click on a field focuses it (a text field starts
// editing), on a choice changes it, on a button presses it; the wheel
// scrolls the body.
func (m Model) HandleMouse(x, y int, action tea.MouseAction, button tea.MouseButton) (Model, tea.Cmd) {
	if action != tea.MouseActionPress {
		return m, nil
	}
	if m.pick.open {
		return m.pickMouse(x, y, button)
	}
	switch button {
	case tea.MouseButtonWheelUp:
		m.scroll = max(m.scroll-2, 0)
		return m, nil
	case tea.MouseButtonWheelDown:
		b := m.build()
		m.scroll = min(m.scroll+2, max(len(b.body)-m.viewRows(b), 0))
		return m, nil
	case tea.MouseButtonLeft:
	default:
		return m, nil
	}
	ox, oy := m.Origin()
	_, tg := m.frame()
	rx, ry := x-ox-1, y-oy-1
	for _, t := range tg {
		if ry != t.y || rx < t.x || rx >= t.x+t.w {
			continue
		}
		var cmd tea.Cmd
		it := t.it
		switch {
		case it.kind.text():
			m.setFocus(it)
			m.editing = true
			m.caret = len([]rune(*m.text(it)))
		case it.kind == iSModel && t.opt == 0:
			m.OpenPicker(it.step)
		case it.kind.choice():
			m.setFocus(it)
			switch {
			case t.opt >= 10:
				m.steps[it.step].tools = t.opt - 10
			case t.opt != 0:
				m.cycle(it, t.opt)
			default:
				m.cycle(it, 1)
			}
		case it.kind == iSPlan:
			m.setFocus(it)
			m.cycle(it, 1)
		case it.kind == iStepHead:
			m.setFocus(it)
		default:
			m.setFocus(it)
			m, cmd = m.press(it.kind)
		}
		m.ensureVisible()
		return m, cmd
	}
	return m, nil
}
