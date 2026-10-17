// Package connectcard is the floating card of the "Connect a service"
// wizard (internal/app/ai_mcp_connect.go): a title, a body of rows
// (text, a big code, a waiting line, a choice, a text input), a button
// row and a key-hint footer. The app builds a Card for each wizard step
// and the Model handles keys and clicks.
//
// Look: the shared float card (theme.Float, FloatBox, Hairline,
// HintBar); one accent (theme.Float().Primary), the float greys, no
// Faint, one bold in the body at most (the code), at most 76 cells wide
// so every step fits 80x24.
package connectcard

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Kind is what a row shows.
type Kind int

const (
	Text   Kind = iota // body text, wrapped
	Dim                // a quieter note (secondary grey), wrapped
	Code               // a big centred code (the device code), framed
	Status             // a waiting line in the accent ("◷ Waiting for GitHub …  4:51 left")
	OK                 // a success line (✓ …)
	Fail               // a failure line (✗ …), wrapped
	Choice             // a label and options in one row (radio)
	Input              // a one-line text input (Masked hides the value)
	Link               // a URL on its own line (cut in the middle when too long)
	Blank              // an empty row
)

// Row is one body row.
type Row struct {
	Kind     Kind
	Text     string   // Text, Dim, Code, Status, OK, Fail, Link; the label of Choice / Input
	Options  []string // Choice
	Selected int      // Choice: the initial option
	ID       string   // Choice / Input: the id in Choice() and SubmitMsg
	Masked   bool     // Input
	Vertical bool     // Choice: one option per line (accounts)
}

// Button is one button of the row at the bottom.
type Button struct {
	ID      string
	Title   string
	Primary bool
}

// Card is one wizard step.
type Card struct {
	Title   string
	Rows    []Row
	Buttons []Button
	// Keys are extra footer hints and letter keys ("c", "copy link");
	// the letter keys work when the input does not have the focus.
	Keys []string
}

// ButtonMsg: a button was pressed (Enter or a click).
type ButtonMsg struct{ ID string }

// SubmitMsg: Enter in the input.
type SubmitMsg struct{ ID, Value string }

// KeyMsg: one of Card.Keys was pressed.
type KeyMsg struct{ Key string }

// CloseMsg: Esc.
type CloseMsg struct{}

// MaxWidth is the widest the card gets (80x24 fits).
const MaxWidth = 76

// Model is the open card.
type Model struct {
	card   Card
	w, h   int
	focus  int            // index into focusables()
	button int            // the highlighted button
	sel    map[string]int // Choice id → option
	value  string         // the input
}

// New opens a card; the focus starts on the first choice or input, else
// on the primary button.
func New(c Card) Model {
	m := Model{sel: map[string]int{}}
	m.SetCard(c)
	return m
}

// SetCard replaces the card (the next step, or a countdown tick). The
// choices and the input keep their values when the ids stay.
func (m *Model) SetCard(c Card) {
	same := m.card.Title == c.Title && len(m.card.Buttons) == len(c.Buttons) && len(m.focusablesOf(m.card)) == len(m.focusablesOf(c))
	keepInput := false
	for _, r := range c.Rows {
		if r.Kind == Choice {
			if _, ok := m.sel[r.ID]; !ok {
				m.sel[r.ID] = r.Selected
			}
		}
		if r.Kind == Input {
			for _, o := range m.card.Rows {
				if o.Kind == Input && o.ID == r.ID {
					keepInput = true
				}
			}
		}
	}
	m.card = c
	if !keepInput {
		m.value = ""
	}
	if same {
		return
	}
	m.button = -1 // no primary button: none is lit until the user moves
	for i, b := range c.Buttons {
		if b.Primary {
			m.button = i
			break
		}
	}
	m.focus = 0
	fs := m.focusables()
	for i, f := range fs {
		if f >= 0 && c.Rows[f].Kind == Input {
			m.focus = i // the input first: the user types at once
			return
		}
	}
	if len(fs) > 0 && fs[0] >= 0 && c.Rows[fs[0]].Kind == Choice {
		m.focus = 0
		return
	}
	m.focus = len(fs) - 1 // the buttons
}

// Card is the card shown.
func (m Model) Card() Card { return m.card }

// SetSize sets the window size.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// Choice is the selected option of a Choice row.
func (m Model) Choice(id string) int { return m.sel[id] }

// Value is the input's text.
func (m Model) Value() string { return m.value }

// SetValue sets the input's text (tests, a paste).
func (m *Model) SetValue(v string) { m.value = v }

// focusablesOf lists the rows that take the focus (row index), then -1
// for the button row.
func (m Model) focusablesOf(c Card) []int {
	var out []int
	for i, r := range c.Rows {
		if r.Kind == Choice || r.Kind == Input {
			out = append(out, i)
		}
	}
	if len(c.Buttons) > 0 {
		out = append(out, -1)
	}
	return out
}

func (m Model) focusables() []int { return m.focusablesOf(m.card) }

// focused is the focused row index (-1 the buttons, -2 nothing).
func (m Model) focused() int {
	fs := m.focusables()
	if m.focus < 0 || m.focus >= len(fs) {
		return -2
	}
	return fs[m.focus]
}

func (m Model) inInput() bool {
	f := m.focused()
	return f >= 0 && m.card.Rows[f].Kind == Input
}

func (m *Model) move(d int) {
	n := len(m.focusables())
	if n == 0 {
		return
	}
	m.focus = (m.focus + d + n) % n
	if m.focused() == -1 && m.button < 0 {
		m.button = 0
	}
}

func emit(v tea.Msg) tea.Cmd { return func() tea.Msg { return v } }

// Update handles a key.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	f := m.focused()
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		return m, emit(CloseMsg{})
	case tea.KeyTab, tea.KeyDown:
		m.move(1)
		return m, nil
	case tea.KeyShiftTab, tea.KeyUp:
		m.move(-1)
		return m, nil
	case tea.KeyLeft, tea.KeyRight:
		d := 1
		if k.Type == tea.KeyLeft {
			d = -1
		}
		switch {
		case f == -1 && m.button < 0:
			m.button = 0
		case f == -1:
			m.button = clamp(m.button+d, len(m.card.Buttons))
		case f >= 0 && m.card.Rows[f].Kind == Choice:
			r := m.card.Rows[f]
			m.sel[r.ID] = clamp(m.sel[r.ID]+d, len(r.Options))
		}
		return m, nil
	case tea.KeyEnter:
		switch {
		case f >= 0 && m.card.Rows[f].Kind == Input:
			v := strings.TrimSpace(m.value)
			if v == "" {
				return m, nil
			}
			return m, emit(SubmitMsg{ID: m.card.Rows[f].ID, Value: v})
		case len(m.card.Buttons) > 0:
			b := m.button
			if f != -1 { // Enter on a choice presses the primary button
				b = m.primary()
			}
			if b < 0 || b >= len(m.card.Buttons) {
				return m, nil // nothing lit: Enter does nothing (a waiting step)
			}
			return m, emit(ButtonMsg{ID: m.card.Buttons[b].ID})
		}
		return m, nil
	case tea.KeyBackspace:
		if m.inInput() && m.value != "" {
			r := []rune(m.value)
			m.value = string(r[:len(r)-1])
		}
		return m, nil
	case tea.KeySpace:
		if m.inInput() {
			m.value += " "
		}
		return m, nil
	case tea.KeyRunes:
		if m.inInput() {
			m.value += clean(string(k.Runes))
			return m, nil
		}
		key := string(k.Runes)
		for i := 0; i+1 < len(m.card.Keys); i += 2 {
			if m.card.Keys[i] == key {
				return m, emit(KeyMsg{Key: key})
			}
		}
	}
	return m, nil
}

func (m Model) primary() int {
	for i, b := range m.card.Buttons {
		if b.Primary {
			return i
		}
	}
	return -1
}

func clamp(v, n int) int {
	if n <= 0 {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v >= n {
		return n - 1
	}
	return v
}

// clean drops control bytes from typed or pasted text.
func clean(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r != 0x7f {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ── Layout ──────────────────────────────────────────────────────────────

type target struct {
	x, y, w int
	button  string // a button id
	row     int    // a Choice row …
	option  int    // … and its option
}

// width is the card's outer width.
func (m Model) width() int {
	w := MaxWidth
	if mw := m.w - 4; w > mw {
		w = mw
	}
	if w < 36 {
		w = 36
	}
	return w
}

// lines renders the inner rows (each innerW wide) and the click targets
// relative to the inner area.
func (m Model) lines() ([]string, []target) {
	boxW := m.width()
	innerW := boxW - 2
	bodyW := innerW - 4
	f := theme.Float()
	bg := lipgloss.NewStyle().Background(f.Bg)
	text := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Text)
	sub := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Sub)
	dim := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Dim)
	accent := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Primary)
	danger := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Danger)
	title := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Title).Bold(true)

	pad := func(s string) string {
		if w := lipgloss.Width(s); w < innerW {
			return s + bg.Render(strings.Repeat(" ", innerW-w))
		}
		return s
	}
	ind := bg.Render("  ")
	var rows []string
	var tg []target
	add := func(s string) { rows = append(rows, pad(ind+s)) }
	blank := bg.Render(strings.Repeat(" ", innerW))

	rows = append(rows, pad(ind+title.Render(m.card.Title)))
	rows = append(rows, theme.Hairline(innerW, theme.BgFloat))
	rows = append(rows, blank)
	fRow := m.focused()
	for i, r := range m.card.Rows {
		switch r.Kind {
		case Blank:
			rows = append(rows, blank)
		case Text:
			for _, l := range wrap(r.Text, bodyW) {
				add(text.Render(l))
			}
		case Dim:
			for _, l := range wrap(r.Text, bodyW) {
				add(sub.Render(l))
			}
		case Link:
			add(accent.Render(cutMiddle(r.Text, bodyW)))
		case Status:
			add(accent.Render(runewidth.Truncate(r.Text, bodyW, "…")))
		case OK:
			ls := wrap(r.Text, bodyW-2)
			for j, l := range ls {
				g := "  "
				if j == 0 {
					g = theme.GlyphOK.String() + " "
				}
				add(accent.Render(g) + text.Render(l))
			}
		case Fail:
			ls := wrap(r.Text, bodyW-2)
			for j, l := range ls {
				g := "  "
				if j == 0 {
					g = theme.GlyphFail.String() + " "
				}
				add(danger.Render(g) + text.Render(l))
			}
		case Code:
			code := " " + r.Text + " "
			cw := runewidth.StringWidth(code)
			left := max((bodyW-cw-2)/2, 0)
			frame := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Hairline)
			big := lipgloss.NewStyle().Background(f.InputBg).Foreground(f.Title).Bold(true)
			sp := bg.Render(strings.Repeat(" ", left))
			add(sp + frame.Render("╭"+strings.Repeat("─", cw)+"╮"))
			add(sp + frame.Render("│") + big.Render(code) + frame.Render("│"))
			add(sp + frame.Render("╰"+strings.Repeat("─", cw)+"╯"))
		case Choice:
			focused := fRow == i
			label := r.Text
			lw := 10
			if label == "" {
				lw = 0
			}
			head := sub.Render(runewidth.FillRight(label, lw))
			x := 2 + lw
			opt := func(j int, o string) string {
				on := m.sel[r.ID] == j
				g := theme.GlyphRadioOff.String()
				st := text
				if on {
					g = theme.GlyphRadioOn.String()
				}
				gs := dim
				if on {
					gs = accent
				}
				if on && focused {
					st = lipgloss.NewStyle().Background(f.SelBg).Foreground(f.SelFg)
				}
				return gs.Render(g) + bg.Render(" ") + st.Render(o)
			}
			if r.Vertical {
				for j, o := range r.Options {
					h := head
					if j > 0 {
						h = bg.Render(strings.Repeat(" ", lw))
					}
					s := opt(j, runewidth.Truncate(o, bodyW-lw-2, "…"))
					tg = append(tg, target{x: x, y: len(rows), w: lipgloss.Width(s), row: i, option: j})
					add(h + s)
				}
				continue
			}
			line := head
			for j, o := range r.Options {
				if j > 0 {
					line += bg.Render("    ")
				}
				s := opt(j, o)
				tg = append(tg, target{x: 2 + lipgloss.Width(line), y: len(rows), w: lipgloss.Width(s), row: i, option: j})
				line += s
			}
			add(line)
		case Input:
			if r.Text != "" {
				for _, l := range wrap(r.Text, bodyW) {
					add(text.Render(l))
				}
			}
			v := m.value
			if r.Masked {
				v = strings.Repeat("•", len([]rune(v)))
			}
			fw := bodyW - 2
			if runewidth.StringWidth(v) > fw-1 {
				rr := []rune(v)
				for runewidth.StringWidth(string(rr)) > fw-2 && len(rr) > 0 {
					rr = rr[1:]
				}
				v = "…" + string(rr)
			}
			in := lipgloss.NewStyle().Background(f.InputBg).Foreground(f.InputFg)
			caret := " "
			if fRow == i {
				caret = "▏"
			}
			field := in.Render(" "+v) + lipgloss.NewStyle().Background(f.InputBg).Foreground(f.Icon).Render(caret)
			if w := lipgloss.Width(field); w < fw {
				field += in.Render(strings.Repeat(" ", fw-w))
			}
			add(field)
		}
	}
	rows = append(rows, blank)
	if len(m.card.Buttons) > 0 {
		var labels []string
		total := 0
		for _, b := range m.card.Buttons {
			l := "  " + b.Title + "  "
			labels = append(labels, l)
			total += runewidth.StringWidth(l)
		}
		total += 2 * (len(labels) - 1)
		left := max((innerW-total)/2, 0)
		var line strings.Builder
		line.WriteString(bg.Render(strings.Repeat(" ", left)))
		x := left
		for j, b := range m.card.Buttons {
			if j > 0 {
				line.WriteString(bg.Render("  "))
				x += 2
			}
			line.WriteString(m.renderButton(b, labels[j], fRow == -1 && j == m.button))
			tg = append(tg, target{x: x, y: len(rows), w: runewidth.StringWidth(labels[j]), button: b.ID})
			x += runewidth.StringWidth(labels[j])
		}
		rows = append(rows, pad(line.String()))
	}
	rows = append(rows, blank)
	rows = append(rows, lipgloss.NewStyle().Background(f.Bg).Foreground(f.Hairline).Render(strings.Repeat("─", innerW)))
	rows = append(rows, pad(ind+theme.HintBar(f.Bg, m.hints()...)))
	return rows, tg
}

func (m Model) hints() []string {
	h := []string{}
	if len(m.focusables()) > 1 {
		h = append(h, "tab", "next")
	}
	h = append(h, "enter", "ok")
	h = append(h, m.card.Keys...)
	h = append(h, "esc", "close")
	// Drop key hints that do not fit (the extra keys still work).
	for len(h) > 4 && runewidth.StringWidth(strings.Join(h, "    "))+len(h)*2 > m.width()-6 {
		h = append(h[:len(h)-4], h[len(h)-2:]...)
	}
	return h
}

func (m Model) renderButton(b Button, label string, active bool) string {
	f := theme.Float()
	if active {
		return lipgloss.NewStyle().Background(f.Primary).Foreground(f.PrimaryFg).Bold(true).Render(label)
	}
	fg := f.Text
	if b.Primary {
		fg = f.Primary
	}
	return lipgloss.NewStyle().Background(f.KeycapBg).Foreground(fg).Render(label)
}

// Box renders the card (not placed).
func (m Model) Box() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	rows, _ := m.lines()
	return theme.FloatBox(strings.Join(rows, "\n"), m.width())
}

// Origin is the card's top-left cell when centred.
func (m Model) Origin() (x, y int) {
	rows, _ := m.lines()
	h := len(rows) + 2
	return max((m.w-m.width())/2, 0), max((m.h-h)/2, 0)
}

// HandleMouse: a click on a button presses it, a click on an option
// selects it.
func (m Model) HandleMouse(x, y int, action tea.MouseAction, button tea.MouseButton) (Model, tea.Cmd) {
	if action != tea.MouseActionPress || button != tea.MouseButtonLeft {
		return m, nil
	}
	ox, oy := m.Origin()
	_, tg := m.lines()
	rx, ry := x-ox-1, y-oy-1
	for _, t := range tg {
		if ry != t.y || rx < t.x || rx >= t.x+t.w {
			continue
		}
		if t.button != "" {
			return m, emit(ButtonMsg{ID: t.button})
		}
		r := m.card.Rows[t.row]
		m.sel[r.ID] = t.option
		for i, f := range m.focusables() {
			if f == t.row {
				m.focus = i
			}
		}
		return m, nil
	}
	return m, nil
}

// ButtonRect is a button on screen (for tests and the hit map).
type ButtonRect struct {
	ID, Title string
	X, Y, W   int
}

// ButtonRects lists the buttons with their screen cells.
func (m Model) ButtonRects() []ButtonRect {
	ox, oy := m.Origin()
	_, tg := m.lines()
	var out []ButtonRect
	for _, t := range tg {
		if t.button == "" {
			continue
		}
		title := ""
		for _, b := range m.card.Buttons {
			if b.ID == t.button {
				title = b.Title
			}
		}
		out = append(out, ButtonRect{ID: t.button, Title: title, X: ox + 1 + t.x, Y: oy + 1 + t.y, W: t.w})
	}
	return out
}

// wrap word-wraps s to w cells, keeping manual newlines; a word longer
// than w is cut.
func wrap(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			for runewidth.StringWidth(word) > w {
				if line != "" {
					out = append(out, line)
					line = ""
				}
				cut := runewidth.Truncate(word, w, "")
				out = append(out, cut)
				word = word[len(cut):]
			}
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
		out = append(out, line)
	}
	return out
}

// cutMiddle shortens s to w cells around a … in the middle.
func cutMiddle(s string, w int) string {
	if runewidth.StringWidth(s) <= w || w < 5 {
		return runewidth.Truncate(s, w, "…")
	}
	r := []rune(s)
	head := (w - 1) * 2 / 3
	tail := w - 1 - head
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}
