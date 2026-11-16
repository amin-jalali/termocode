// Package nowview draws the TUI's Now page and Review page (Now +
// Review): a full page like the Agent Board, with the web's keys. It
// only draws and reads keys; the app runs each action (done, later,
// mute, undo, open, approve…). Nothing here acts by itself.
package nowview

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Row is one item of the Now list.
type Row struct {
	ID, Kind, Title, Sub string
	Why                  string // the chip: one plain reason
	WhyLong              string // the "why:" line of the selected row
	Rule                 string // the rule's name ("" none)
	Age                  string
	Fix                  bool
}

// Data is what the Now page shows.
type Data struct {
	Project, Branch, CI string
	Running             int
	Turn                []Row
	Overdue             []Row
	Waiting             []Row
	Other               []Row
}

// Action is what a key asks the app to do. Do is "" (nothing), close,
// done, later (Text: when), mute (Repo: the repo), undo, fix, open,
// tomorrow (all overdue), approve / comment (Number, Head, Text), skip,
// openfull, again (Text: the check), log.
type Action struct {
	Do     string
	ID     string
	Text   string
	Repo   bool
	Number int
	Head   string
}

type pal struct {
	bg, sel, text, muted, title, accent, ok, bad, warn, bar, onBar lipgloss.Color
}

func colors() pal {
	fc := theme.Float()
	return pal{bg: fc.Bg, sel: fc.SelBg, text: fc.Text, muted: fc.Sub, title: fc.Title, accent: theme.LG(theme.AIAccent),
		ok: theme.LG(theme.GitAdded), bad: theme.LG(theme.DiagError), warn: theme.LG(theme.DiagWarning), bar: fc.Primary, onBar: fc.PrimaryFg}
}

// line paints text into exactly w cells.
func line(text string, w int, st lipgloss.Style) string {
	if runewidth.StringWidth(text) > w {
		text = runewidth.Truncate(text, w, "…")
	}
	if pad := w - runewidth.StringWidth(text); pad > 0 {
		text += strings.Repeat(" ", pad)
	}
	return st.Render(text)
}

// cell cuts or pads s to n cells.
func cell(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) > n {
		return runewidth.Truncate(s, n, "…")
	}
	return s + strings.Repeat(" ", n-runewidth.StringWidth(s))
}

const (
	tabTurn = iota
	tabWaiting
	tabAll
)

const (
	askNone = iota
	askLater
	askMute
)

// Page is the Now page.
type Page struct {
	d     Data
	w, h  int
	tab   int
	sel   int
	ask   int
	input string
	note  string
	undo  bool
}

// New makes the page for d.
func New(d Data) *Page { return &Page{d: d} }

// SetData shows d (the selection stays in range).
func (p *Page) SetData(d Data) {
	p.d = d
	if n := len(p.rows()); p.sel >= n {
		p.sel = max(n-1, 0)
	}
}

// SetSize sets the page's size.
func (p *Page) SetSize(w, h int) { p.w, p.h = w, h }

// Note shows a line at the bottom (undo: with "u undo").
func (p *Page) Note(s string, undo bool) { p.note, p.undo = s, undo }

func (p *Page) need() int { return len(p.d.Turn) + len(p.d.Overdue) }

// rows are the selectable rows of the open list, in drawing order.
func (p *Page) rows() []Row {
	switch p.tab {
	case tabWaiting:
		return p.d.Waiting
	case tabAll:
		out := append(append(append([]Row{}, p.d.Turn...), p.d.Overdue...), p.d.Waiting...)
		return append(out, p.d.Other...)
	}
	return append(append([]Row{}, p.d.Turn...), p.d.Overdue...)
}

func (p *Page) cur() (Row, bool) {
	r := p.rows()
	if p.sel < 0 || p.sel >= len(r) {
		return Row{}, false
	}
	return r[p.sel], true
}

// Key reads one key.
func (p *Page) Key(k tea.KeyMsg) Action {
	if p.ask != askNone {
		return p.askKey(k)
	}
	row, ok := p.cur()
	act := func(do string) Action {
		if !ok {
			return Action{}
		}
		return Action{Do: do, ID: row.ID}
	}
	switch k.String() {
	case "esc", "q":
		return Action{Do: "close"}
	case "j", "down":
		if p.sel < len(p.rows())-1 {
			p.sel++
		}
	case "k", "up":
		if p.sel > 0 {
			p.sel--
		}
	case "tab":
		p.tab, p.sel = (p.tab+1)%3, 0
	case "shift+tab":
		p.tab, p.sel = (p.tab+2)%3, 0
	case "e":
		return act("done")
	case "u", "z":
		return Action{Do: "undo"}
	case "f":
		if ok && row.Fix {
			return act("fix")
		}
	case "enter":
		return act("open")
	case "T":
		if len(p.d.Overdue) > 0 {
			return Action{Do: "tomorrow"}
		}
	case "h":
		if ok {
			p.ask, p.input = askLater, ""
		}
	case "m":
		if ok {
			p.ask = askMute
		}
	}
	return Action{}
}

func (p *Page) askKey(k tea.KeyMsg) Action {
	row, _ := p.cur()
	switch p.ask {
	case askMute:
		p.ask = askNone
		switch k.String() {
		case "y":
			return Action{Do: "mute", ID: row.ID}
		case "r":
			return Action{Do: "mute", ID: row.ID, Repo: true}
		}
		return Action{}
	}
	switch k.Type {
	case tea.KeyEsc:
		p.ask = askNone
	case tea.KeyEnter:
		p.ask = askNone
		if t := strings.TrimSpace(p.input); t != "" {
			return Action{Do: "later", ID: row.ID, Text: t}
		}
	case tea.KeyBackspace:
		if r := []rune(p.input); len(r) > 0 {
			p.input = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		p.input += " "
	case tea.KeyRunes:
		if len(p.input) < 40 {
			p.input += string(k.Runes)
		}
	}
	return Action{}
}

// View draws the page: exactly h lines of w cells.
func (p *Page) View() string {
	c := colors()
	bg := lipgloss.NewStyle().Background(c.bg).Foreground(c.text)
	bar := lipgloss.NewStyle().Background(c.bar).Foreground(c.onBar)
	muted := bg.Foreground(c.muted)
	head := bg.Foreground(c.title).Bold(true)
	sel := lipgloss.NewStyle().Background(c.sel).Foreground(c.text)
	w := max(p.w, 20)
	var out []string
	add := func(s string) { out = append(out, s) }
	need := strconv.Itoa(p.need()) + " need you"
	right := strings.TrimSpace(strings.Join([]string{"⎇ " + p.d.Branch, p.d.CI, runningWord(p.d.Running)}, "  "))
	top := "termocode · " + p.d.Project + "   ◉ Now   " + need
	add(line(top+strings.Repeat(" ", max(w-runewidth.StringWidth(top)-runewidth.StringWidth(right), 1))+right, w, bar))
	tabs := []string{"Your turn " + strconv.Itoa(p.need()), "Waiting on others " + strconv.Itoa(len(p.d.Waiting)), "All " + strconv.Itoa(p.need()+len(p.d.Waiting)+len(p.d.Other))}
	var tl []string
	for i, t := range tabs {
		if i == p.tab {
			t = "[" + t + "]"
		}
		tl = append(tl, t)
	}
	add(line(" "+strings.Join(tl, "   ")+"    ? keys", w, muted))
	add(line("", w, bg))
	idx := 0
	section := func(title string, rows []Row, extra string) {
		if len(rows) == 0 {
			return
		}
		t := title + " · " + strconv.Itoa(len(rows))
		if extra != "" {
			t += "   " + extra
		}
		add(line(" "+t, w, head))
		for _, r := range rows {
			st, pre, mark := bg, " ", "  "
			if r.Kind != "" && title != "WAITING ON OTHERS" && title != "EVERYTHING ELSE" {
				mark = "▶ "
			}
			if idx == p.sel {
				st, pre = sel, ">"
			}
			rule := ""
			if r.Rule != "" {
				rule = "[" + r.Rule + "]"
			}
			tw := max(w-80, 24)
			text := pre + mark + cell(r.Title, tw) + "  " + cell(r.Sub, 26) + "  " + cell(r.Why, 20) + "  " + cell(rule, 12) + "  " + r.Age
			add(line(text, w, st))
			if idx == p.sel && r.WhyLong != "" {
				add(line("     why: "+r.WhyLong+whyKeys(r), w, muted))
			}
			idx++
		}
		add(line("", w, bg))
	}
	switch p.tab {
	case tabWaiting:
		section("WAITING ON OTHERS", p.d.Waiting, "")
	case tabAll:
		section("YOUR TURN", p.d.Turn, "")
		section("OVERDUE", p.d.Overdue, "T move all to tomorrow")
		section("WAITING ON OTHERS", p.d.Waiting, "")
		section("EVERYTHING ELSE", p.d.Other, "")
	default:
		if p.need() == 0 {
			add(line("", w, bg))
			add(line(strings.Repeat(" ", max(w/2-20, 1))+"✓ All clear. Nothing waits on you.", w, bg.Foreground(c.ok)))
			add(line(strings.Repeat(" ", max(w/2-20, 1))+"› Waiting on others "+strconv.Itoa(len(p.d.Waiting))+"   › Everything else "+strconv.Itoa(len(p.d.Other)), w, muted))
		} else {
			section("YOUR TURN", p.d.Turn, "")
			section("OVERDUE", p.d.Overdue, "T move all to tomorrow")
			add(line(" › Waiting on others "+strconv.Itoa(len(p.d.Waiting))+"     › Everything else "+strconv.Itoa(len(p.d.Other)), w, muted))
		}
	}
	foot := p.footer(w, bg, bar, muted, c)
	h := max(p.h, len(foot)+1)
	for len(out) < h-len(foot) {
		add(line("", w, bg))
	}
	out = append(out[:h-len(foot)], foot...)
	return strings.Join(out, "\n")
}

func whyKeys(r Row) string {
	k := "   e done · h later · m mute"
	if r.Fix {
		k = "   f ✦ fix with AI ·" + k[2:]
	}
	return k
}

func runningWord(n int) string {
	if n == 0 {
		return ""
	}
	return "▶ " + strconv.Itoa(n) + " running"
}

func (p *Page) footer(w int, bg, bar, muted lipgloss.Style, c pal) []string {
	var out []string
	row, _ := p.cur()
	switch p.ask {
	case askLater:
		out = append(out, line(" Later: "+row.Title, w, bg.Bold(true)))
		out = append(out, line("  "+p.input+"▏   tomorrow 9:00 · next week · in 2 hours · comes back on top at that time", w, muted))
	case askMute:
		out = append(out, line(" Mute "+row.Title+"?  until someone asks you    y mute item   r mute repo   esc cancel", w, bg.Bold(true)))
	}
	if p.note != "" {
		n := " " + p.note
		if p.undo {
			n += "   u undo"
		}
		out = append(out, line(n, w, bg.Foreground(c.ok)))
	}
	out = append(out, line(" j/k move   e done   h later   m mute   f fix   enter open   u undo   tab lists   esc close", w, bar))
	return out
}
