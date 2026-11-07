package raceview

import (
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Option is one agent the race card can use as a lane.
type Option struct {
	ID, Title string
	Hint      string // "claude · signed in", or why it is off
	Disabled  bool   // cannot be a lane (Hint says why)
}

// FocusID is where the keyboard is in the card: the task, a lane (by
// index), the verify field, or a button.
type FocusID int

const (
	FocusTask   FocusID = -1
	FocusVerify FocusID = -2
	FocusCancel FocusID = -3
	FocusStart  FocusID = -4
)

// FocusLane is lane option i.
func FocusLane(i int) FocusID { return FocusID(i) }

// StartMsg: Start was pressed with a task and 2–8 lanes.
type StartMsg struct {
	Task, Verify string
	Lanes        []string // option ids, in list order, once per lane (CMP-18)
}

// CancelMsg: Esc or Cancel; nothing starts.
type CancelMsg struct{}

// Form is the Agent: Race… card.
type Form struct {
	opts         []Option
	count        []int // lanes per option (CMP-18 fan-out count)
	task, verify []rune
	focus        FocusID
	err          string
	w, h         int
	top          int // first lane row shown
	hits         []hit
}

const formMaxW = 76

// NewForm opens the card with the focus on the task.
func NewForm(opts []Option, task, verify string) Form {
	return Form{opts: opts, count: make([]int, len(opts)), task: []rune(task), verify: []rune(verify), focus: FocusTask}
}

func (f *Form) SetSize(w, h int)    { f.w, f.h = w, h }
func (f Form) Focus() FocusID       { return f.focus }
func (f *Form) SetFocus(id FocusID) { f.focus = id; f.err = "" }
func (f Form) Task() string         { return strings.TrimSpace(string(f.task)) }
func (f Form) Verify() string       { return strings.TrimSpace(string(f.verify)) }

// Chosen are the lanes' agent ids: each option as often as its count, in
// list order.
func (f Form) Chosen() []string {
	var out []string
	for i, o := range f.opts {
		for k := 0; k < f.count[i]; k++ {
			out = append(out, o.ID)
		}
	}
	return out
}

// Count is how many lanes run option i.
func (f Form) Count(i int) int {
	if i < 0 || i >= len(f.count) {
		return 0
	}
	return f.count[i]
}

// order is the Tab order.
func (f Form) order() []FocusID {
	out := []FocusID{FocusTask}
	for i := range f.opts {
		out = append(out, FocusLane(i))
	}
	return append(out, FocusVerify, FocusCancel, FocusStart)
}

func (f *Form) move(d int) {
	o := f.order()
	for i, id := range o {
		if id == f.focus {
			f.focus = o[(i+d+len(o))%len(o)]
			return
		}
	}
	f.focus = FocusTask
}

func (f *Form) field() *[]rune {
	switch f.focus {
	case FocusTask:
		return &f.task
	case FocusVerify:
		return &f.verify
	}
	return nil
}

// toggle ticks lane i on (one lane) or off (none, whatever its count).
func (f *Form) toggle(i int) {
	if i < 0 || i >= len(f.opts) {
		return
	}
	if f.count[i] > 0 {
		f.err = ""
		f.count[i] = 0
		return
	}
	f.bump(i, 1)
}

// bump runs option i in d more lanes (d < 0: fewer), within 0 and the
// race's lane limit.
func (f *Form) bump(i, d int) {
	if i < 0 || i >= len(f.opts) {
		return
	}
	f.err = ""
	if f.opts[i].Disabled {
		f.err = f.opts[i].Title + ": " + f.opts[i].Hint
		return
	}
	if d > 0 && len(f.Chosen()) >= agentrace.MaxLanes {
		f.err = "A race has at most " + strconv.Itoa(agentrace.MaxLanes) + " lanes"
		return
	}
	f.count[i] = max(f.count[i]+d, 0)
}

func emit(v tea.Msg) tea.Cmd { return func() tea.Msg { return v } }

// start checks the card and starts, or says what is missing.
func (f Form) start() (Form, tea.Cmd) {
	if f.Task() == "" {
		f.err, f.focus = "Type the task for the agents", FocusTask
		return f, nil
	}
	if n := len(f.Chosen()); n < agentrace.MinLanes || n > agentrace.MaxLanes {
		f.err = "Pick " + strconv.Itoa(agentrace.MinLanes) + " to " + strconv.Itoa(agentrace.MaxLanes) + " lanes"
		f.focus = FocusLane(0)
		return f, nil
	}
	return f, emit(StartMsg{Task: f.Task(), Verify: f.Verify(), Lanes: f.Chosen()})
}

// Update reads a key.
func (f Form) Update(msg tea.Msg) (Form, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}
	fld := f.field()
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		return f, emit(CancelMsg{})
	case tea.KeyTab:
		f.move(1)
	case tea.KeyShiftTab:
		f.move(-1)
	case tea.KeyDown:
		f.move(1)
	case tea.KeyUp:
		f.move(-1)
	case tea.KeyLeft, tea.KeyRight:
		if f.focus >= 0 {
			if k.Type == tea.KeyRight {
				f.bump(int(f.focus), 1)
			} else {
				f.bump(int(f.focus), -1)
			}
		}
		if f.focus == FocusCancel || f.focus == FocusStart {
			if f.focus == FocusCancel {
				f.focus = FocusStart
			} else {
				f.focus = FocusCancel
			}
		}
	case tea.KeyEnter:
		switch {
		case fld != nil:
			f.move(1) // Enter in a field moves on; it never starts
		case f.focus >= 0:
			f.toggle(int(f.focus))
		case f.focus == FocusCancel:
			return f, emit(CancelMsg{})
		case f.focus == FocusStart:
			return f.start()
		}
	case tea.KeySpace:
		switch {
		case fld != nil:
			*fld = append(*fld, ' ')
		case f.focus >= 0:
			f.toggle(int(f.focus))
		}
	case tea.KeyBackspace:
		if fld != nil && len(*fld) > 0 {
			*fld = (*fld)[:len(*fld)-1]
		}
	case tea.KeyCtrlU:
		if fld != nil {
			*fld = nil
		}
	case tea.KeyRunes:
		if fld == nil && f.focus >= 0 && len(k.Runes) == 1 {
			switch k.Runes[0] {
			case '+', '=':
				f.bump(int(f.focus), 1)
			case '-', '_':
				f.bump(int(f.focus), -1)
			}
		}
		if fld != nil {
			for _, r := range k.Runes {
				if r == '\n' || r == '\r' || r == '\t' {
					r = ' '
				}
				if unicode.IsPrint(r) {
					*fld = append(*fld, r)
				}
			}
			if len(*fld) > 2000 {
				*fld = (*fld)[:2000]
			}
		}
	}
	return f, nil
}

// HandleMouse: a click on a lane ticks it; on a field focuses it; on a
// button presses it.
func (f Form) HandleMouse(x, y int, action tea.MouseAction, button tea.MouseButton) (Form, tea.Cmd) {
	if action != tea.MouseActionPress || button != tea.MouseButtonLeft {
		return f, nil
	}
	ox, oy := f.Origin()
	f.layout() // hits for this size
	id := hitAt(f.layoutHits(), x-ox, y-oy)
	switch {
	case id == "task":
		f.focus = FocusTask
	case id == "verify":
		f.focus = FocusVerify
	case id == "cancel":
		return f, emit(CancelMsg{})
	case id == "start":
		f.focus = FocusStart
		return f.start()
	case strings.HasPrefix(id, "lane:"):
		i, _ := strconv.Atoi(strings.TrimPrefix(id, "lane:"))
		f.focus = FocusLane(i)
		f.toggle(i)
	}
	return f, nil
}

func (f Form) layoutHits() []hit { _, h := f.layout(); return h }

func (f Form) width() int {
	w := min(f.w-4, formMaxW)
	if w < 40 {
		w = f.w
	}
	return max(w, 0)
}

// Origin is the card's top-left screen cell.
func (f Form) Origin() (int, int) {
	rows, _ := f.layout()
	return max((f.w-f.width())/2, 0), max((f.h-len(rows))/2, 0)
}

// Box draws the card.
func (f Form) Box() string {
	if f.w <= 0 || f.h <= 0 {
		return ""
	}
	rows, _ := f.layout()
	return strings.Join(rows, "\n")
}

// layout draws the card's rows (with the border) and its click targets.
func (f Form) layout() ([]string, []hit) {
	w := f.width()
	iw := max(w-2, 0)
	p := colors()
	bg := lipgloss.NewStyle().Background(p.bg)
	var inner []string
	var hits []hit
	add := func(s string) { inner = append(inner, s) }
	addHit := func(x0, cw int, id string) {
		hits = append(hits, hit{x0: x0 + 1, x1: x0 + 1 + cw, y: len(inner) + 1, id: id})
	}
	spark, bar := "✦", "▏"
	if ascii() {
		spark, bar = "*", "|"
	}
	label := func(text string, on bool) {
		st := bg.Foreground(p.muted)
		if on {
			st = bg.Foreground(p.title).Bold(true)
		}
		add(paint([]seg{{" " + text, st}}, iw, bg))
	}
	input := func(id string, val []rune, on bool, placeholder string) {
		in := lipgloss.NewStyle().Background(p.input).Foreground(p.text)
		fw := max(iw-4, 4)
		text := tail(string(val), fw-1)
		segs := []seg{{" ", bg}, {bar, bg.Foreground(p.dim)}}
		if on {
			segs[1].st = bg.Foreground(p.primary)
		}
		switch {
		case text == "" && !on:
			segs = append(segs, seg{fit(" "+placeholder, fw), in.Foreground(p.dim)})
		case on:
			segs = append(segs, seg{fit(" "+text+"█", fw), in})
		default:
			segs = append(segs, seg{fit(" "+text, fw), in})
		}
		addHit(1, fw+1, id)
		add(paint(segs, iw, bg))
	}

	compact := f.h > 0 && f.h < 30 // no spacer rows, no note
	space := func() {
		if !compact {
			add(paint(nil, iw, bg))
		}
	}
	// Title.
	add(joinLR([]seg{{" " + spark + " ", bg.Foreground(p.accent).Bold(true)}, {"Race", bg.Foreground(p.title).Bold(true)},
		{"  one task, several agents", bg.Foreground(p.muted)}}, nil, iw, bg))
	space()
	label("Task", f.focus == FocusTask)
	input("task", f.task, f.focus == FocusTask, "what every agent should do")
	space()
	n := len(f.Chosen())
	label("Lanes  "+strconv.Itoa(n)+" of "+strconv.Itoa(agentrace.MinLanes)+"–"+strconv.Itoa(agentrace.MaxLanes)+
		"  · +/- sets how many of each agent", f.focus >= 0)

	// The lane rows: as many as fit, scrolled to the focus. fixed is
	// every other row of the card, the border and a free row each side.
	fixed := 13 + 2
	if !compact {
		fixed = 13 + 6 + 2 + 2
	}
	room := len(f.opts)
	if f.h > 0 && room > f.h-fixed {
		room = max(f.h-fixed-1, 1) // one row for "… more"
	}
	top := 0
	if f.focus >= 0 && int(f.focus) >= room {
		top = int(f.focus) - room + 1
	}
	tw := min(20, max(iw/3, 10))
	for i := top; i < len(f.opts) && i < top+room; i++ {
		o := f.opts[i]
		row := bg
		if f.focus == FocusLane(i) {
			row = lipgloss.NewStyle().Background(p.sel)
		}
		box := "[ ]"
		switch c := f.count[i]; {
		case c == 1:
			box = "[x]"
		case c > 1:
			box = "[" + strconv.Itoa(c) + "]"
		}
		if o.Disabled {
			box = "[-]"
		}
		bst, tst, hst := row.Foreground(p.primary), row.Foreground(p.text), row.Foreground(p.muted)
		if o.Disabled {
			bst, tst, hst = row.Foreground(p.dim), row.Foreground(p.dim), row.Foreground(p.dim)
		}
		addHit(0, iw, "lane:"+strconv.Itoa(i))
		add(paint([]seg{{"  ", row}, {box, bst}, {" " + fit(o.Title, tw) + " ", tst}, {o.Hint, hst}}, iw, row))
	}
	if more := len(f.opts) - (top + room); more > 0 {
		add(paint([]seg{{"      … " + strconv.Itoa(more) + " more (↓)", bg.Foreground(p.dim)}}, iw, bg))
	}
	space()
	label("Verify command (optional)", f.focus == FocusVerify)
	input("verify", f.verify, f.focus == FocusVerify, "e.g. go test ./...  (runs in every finished lane)")
	space()
	if !compact {
		note := "Each lane works in its own hidden git worktree from your last commit; your files change only when you merge one."
		for i, l := range wrapText(note, iw-2) {
			if i < 2 {
				add(paint([]seg{{" " + l, bg.Foreground(p.dim)}}, iw, bg))
			}
		}
	}
	// The error, or the room for it.
	add(paint([]seg{{" " + f.err, bg.Foreground(p.warn)}}, iw, bg))
	add(hairline(p, iw))

	// Error (or nothing) and the buttons: Cancel first.
	startTitle := "Start " + strconv.Itoa(n) + " lane"
	if n != 1 {
		startTitle += "s"
	}
	cancel := button(p, "Cancel", f.focus == FocusCancel, false)
	start := button(p, startTitle, f.focus == FocusStart, true)
	right := []seg{cancel, {" ", bg}, start, {" ", bg}}
	rw := segsWidth(right)
	x := iw - rw
	addHit(x, segsWidth([]seg{cancel}), "cancel")
	addHit(x+segsWidth([]seg{cancel})+1, segsWidth([]seg{start}), "start")
	add(joinLR(nil, right, iw, bg))
	add(hint(p, iw, "tab", "next", "space", "tick lane", "enter", "next / press", "esc", "cancel"))
	return strings.Split(theme.FloatBox(strings.Join(inner, "\n"), w), "\n"), hits
}

// wrapText wraps s at spaces into lines of at most w cells.
func wrapText(s string, w int) []string {
	if w <= 4 {
		return []string{s}
	}
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= w:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}
