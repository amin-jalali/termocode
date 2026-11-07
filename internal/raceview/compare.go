package raceview

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Action is what a key on the compare page asks the app to do.
type Action int

const (
	ActNone      Action = iota
	ActReview           // Enter: the lane's diff (read only)
	ActMerge            // m: merge the lane into your files
	ActDiscard          // d: discard the lane (a card asks)
	ActVerify           // v: run the verify command in every finished lane
	ActSummary          // s: ask the quick model for a summary
	ActStop             // x: stop every lane that still works
	ActRefresh          // r: read the lanes' files again
	ActClose            // Esc
	ActBroadcast        // b: type one message into every lane's agent (a card asks)
	ActRunAll           // c: run a command in every open lane (a card asks)
)

// ActionMsg is a key's action; Lane is the selected lane's id.
type ActionMsg struct {
	Action Action
	Lane   string
}

// Compare is the race compare page.
type Compare struct {
	race   agentrace.Race
	cursor int
	w, h   int
	nowMs  int64
	msg    string
	busy   string // a word for the work in progress ("summarizing…")
}

const compareMaxW = 150

// NewCompare opens the page on race r, the first lane selected.
func NewCompare(r agentrace.Race) Compare { return Compare{race: r} }

func (c *Compare) SetSize(w, h int)    { c.w, c.h = w, h }
func (c *Compare) SetNow(ms int64)     { c.nowMs = ms }
func (c *Compare) SetMessage(s string) { c.msg = s }
func (c *Compare) SetBusy(s string)    { c.busy = s }
func (c Compare) Race() agentrace.Race { return c.race }

// SetRace shows a newer copy of the race; the selected lane stays.
func (c *Compare) SetRace(r agentrace.Race) {
	sel := c.Selected()
	c.race = r
	c.cursor = 0
	for i, l := range r.Lanes {
		if l.ID == sel {
			c.cursor = i
		}
	}
}

// Selected is the selected lane's id ("" none).
func (c Compare) Selected() string {
	if c.cursor < 0 || c.cursor >= len(c.race.Lanes) {
		return ""
	}
	return c.race.Lanes[c.cursor].ID
}

func act(a Action, lane string) tea.Cmd { return emit(ActionMsg{Action: a, Lane: lane}) }

// Key reads a key; the action comes back as an ActionMsg.
func (c *Compare) Key(k tea.KeyMsg) tea.Cmd {
	c.msg = ""
	n := len(c.race.Lanes)
	sel := c.Selected()
	var lane agentrace.Lane
	if c.cursor >= 0 && c.cursor < n {
		lane = c.race.Lanes[c.cursor]
	}
	ready := func(word string) bool {
		switch {
		case sel == "":
			return false
		case lane.Closed():
			c.msg = lane.Title + " is " + string(lane.State) + " already"
			return false
		case !lane.Finished():
			c.msg = lane.Title + " is still " + string(lane.State) + ": wait, or stop the race (x), then " + word
			return false
		}
		return true
	}
	switch k.String() {
	case "up", "k":
		c.cursor = max(c.cursor-1, 0)
	case "down", "j":
		c.cursor = min(c.cursor+1, max(n-1, 0))
	case "enter":
		if sel != "" && !lane.Closed() {
			return act(ActReview, sel)
		}
	case "m":
		if ready("merge it") {
			return act(ActMerge, sel)
		}
	case "d":
		if ready("discard it") {
			return act(ActDiscard, sel)
		}
	case "v":
		return act(ActVerify, sel)
	case "b":
		return act(ActBroadcast, sel)
	case "c":
		return act(ActRunAll, sel)
	case "s":
		return act(ActSummary, sel)
	case "x":
		return act(ActStop, sel)
	case "r":
		return act(ActRefresh, sel)
	case "esc", "q":
		return act(ActClose, sel)
	}
	return nil
}

func (c Compare) geom() (x, y, w, h int) {
	w = min(c.w-4, compareMaxW)
	if w < 40 {
		w = c.w
	}
	h = c.h - 2
	if h < 12 {
		h = c.h
	}
	w, h = max(w, 0), max(h, 0)
	return max((c.w-w)/2, 0), max((c.h-h)/2, 0), w, h
}

// Origin is the page's top-left screen cell.
func (c Compare) Origin() (int, int) {
	x, y, _, _ := c.geom()
	return x, y
}

// StateText is a lane state with its mark.
func StateText(s agentrace.State) string {
	g := map[agentrace.State][2]string{
		agentrace.Starting: {"◌", "o"}, agentrace.Working: {"●", "*"}, agentrace.Waiting: {"◐", "?"},
		agentrace.Done: {"✓", "+"}, agentrace.Stopped: {"■", "#"}, agentrace.Failed: {"✗", "x"},
		agentrace.Merged: {"⑂", "Y"}, agentrace.Discarded: {"–", "-"},
	}[s]
	mark := g[0]
	if ascii() {
		mark = g[1]
	}
	return mark + " " + string(s)
}

func (p pal) state(s agentrace.State) lipgloss.Color {
	switch s {
	case agentrace.Done, agentrace.Merged:
		return p.ok
	case agentrace.Waiting:
		return p.warn
	case agentrace.Failed:
		return p.bad
	case agentrace.Discarded, agentrace.Stopped:
		return p.dim
	}
	return p.text
}

// VerifyText is a lane's verify cell.
func VerifyText(r agentrace.Race, l agentrace.Lane) (string, int) {
	ok, bad := "✓", "✗"
	if ascii() {
		ok, bad = "+", "x"
	}
	switch l.Verify.State {
	case agentrace.VerifyRunning:
		return "running…", 0
	case agentrace.VerifyPass:
		return ok + " passed", 1
	case agentrace.VerifyFail:
		return bad + " exit " + strconv.Itoa(l.Verify.Exit), -1
	}
	if r.Verify == "" {
		return "— none", 0
	}
	return "— not yet", 0
}

// CostText is a lane's cost cell ("—": the CLI does not say).
func CostText(l agentrace.Lane) string {
	if l.Cost == nil {
		return "—"
	}
	return fmt.Sprintf("$%.2f", *l.Cost)
}

func since(startMs, nowMs int64) string {
	m := (nowMs - startMs) / 60000
	switch {
	case startMs == 0 && nowMs == 0, m < 1:
		return "<1 m"
	case m < 60:
		return strconv.FormatInt(m, 10) + " m"
	}
	return strconv.FormatInt(m/60, 10) + " h " + strconv.FormatInt(m%60, 10) + " m"
}

// Box draws the page.
func (c Compare) Box() string {
	_, _, w, h := c.geom()
	if w < 4 || h < 3 {
		return ""
	}
	iw, ih := w-2, h-2
	p := colors()
	bg := lipgloss.NewStyle().Background(p.bg)
	wide := iw >= 100
	r := c.race
	var top, bottom []string

	// Title: RACE name · "task"                 N lanes · 4 m
	right := []seg{{strconv.Itoa(len(r.Lanes)) + " lanes · " + since(r.StartedMs, c.nowMs) + " ", bg.Foreground(p.muted)}}
	left := []seg{{" RACE ", bg.Foreground(p.accent).Bold(true)}, {r.Name, bg.Foreground(p.title).Bold(true)}}
	if room := iw - segsWidth(left) - segsWidth(right) - 4; room > 8 {
		left = append(left, seg{" · ", bg.Foreground(p.dim)}, seg{fit(`"`+r.Prompt+`"`, room), bg.Foreground(p.text)})
	}
	top = append(top, joinLR(left, right, iw, bg), hairline(p, iw))

	// The lane table.
	type col struct {
		name string
		w    int
	}
	cols := []col{{"LANE", 18}, {"STATE", 13}, {"FILES", 7}, {"+/-", 13}, {"VERIFY", 14}, {"COST", 9}, {"NOTE", 0}}
	if !wide {
		cols = []col{{"LANE", 18}, {"STATE", 13}, {"FILES", 7}, {"VERIFY", 0}}
	}
	used := 0
	for i := range cols {
		if cols[i].w == 0 {
			cols[i].w = max(iw-used-1, 4)
		}
		used += cols[i].w
	}
	var head []seg
	head = append(head, seg{" ", bg})
	for _, cl := range cols {
		head = append(head, seg{fit(cl.name, cl.w), bg.Foreground(p.dim).Bold(true)})
	}
	top = append(top, paint(head, iw, bg))
	for i, l := range r.Lanes {
		row := bg
		mark := "  "
		if i == c.cursor {
			row = lipgloss.NewStyle().Background(p.sel)
			mark = "▸ "
			if ascii() {
				mark = "> "
			}
		}
		files, add, del := l.Totals()
		fileText, diff := "…", "…"
		if l.Stats {
			fileText, diff = strconv.Itoa(files), "+"+strconv.Itoa(add)+" -"+strconv.Itoa(del)
		}
		vt, vs := VerifyText(r, l)
		vc := p.muted
		switch vs {
		case 1:
			vc = p.ok
		case -1:
			vc = p.bad
		}
		text := p.text
		if l.Closed() {
			text = p.dim
		}
		cells := map[string]seg{
			"LANE":   {mark + l.Title, row.Foreground(text).Bold(i == c.cursor)},
			"STATE":  {StateText(l.State), row.Foreground(p.state(l.State))},
			"FILES":  {fileText, row.Foreground(text)},
			"+/-":    {diff, row.Foreground(text)},
			"VERIFY": {vt, row.Foreground(vc)},
			"COST":   {CostText(l), row.Foreground(p.muted)},
			"NOTE":   {quoteNote(l.Note), row.Foreground(p.muted)},
		}
		segs := []seg{{" ", row}}
		for _, cl := range cols {
			s := cells[cl.name]
			s.text = fit(s.text, cl.w)
			segs = append(segs, s)
		}
		top = append(top, paint(segs, iw, row))
	}
	top = append(top, hairline(p, iw))

	// Bottom: summary, message, hints.
	spark := "✦"
	if ascii() {
		spark = "*"
	}
	if len(r.Summary) > 0 {
		h := []seg{{" " + spark + " SUMMARY", bg.Foreground(p.accent).Bold(true)}}
		if r.SummaryCost != "" {
			h = append(h, seg{" · " + r.SummaryCost, bg.Foreground(p.muted)})
		}
		bottom = append(bottom, paint(h, iw, bg))
		for _, s := range r.Summary {
			bottom = append(bottom, paint([]seg{{"   " + s, bg.Foreground(p.text)}}, iw, bg))
		}
	}
	status := c.msg
	if status == "" {
		status = c.busy
	}
	if status == "" && len(r.Summary) == 0 {
		status = "s " + spark + " summarize: the quick model says how the lanes differ (cost shown)"
	}
	bottom = append(bottom, paint([]seg{{" " + status, bg.Foreground(p.muted)}}, iw, bg), hairline(p, iw))
	if wide {
		bottom = append(bottom, hint(p, iw, "enter", "review diff", "m", "merge this lane", "d", "discard lane", "v", "verify all",
			"b", "message all", "c", "run in all", "s", spark+" summarize", "x", "stop all", "r", "refresh", "esc", "close"))
	} else {
		bottom = append(bottom, hint(p, iw, "enter", "diff", "m", "merge", "d", "discard", "v", "verify", "s", "summary", "esc", "close", "x", "stop", "b", "message", "c", "run"))
	}

	// Middle: the selected lane's files, as many as fit.
	room := ih - len(top) - len(bottom)
	var mid []string
	if c.cursor < len(r.Lanes) && room > 0 {
		l := r.Lanes[c.cursor]
		word := "files"
		if len(l.Files) == 1 {
			word = "file"
		}
		head := l.Title + " · " + strconv.Itoa(len(l.Files)) + " " + word
		switch {
		case !l.Stats && l.Note != "" && l.State == agentrace.Failed:
			head = l.Title + " · " + l.Note
		case !l.Stats:
			head = l.Title + " · the files show when the lane is done"
		case l.Branch != "":
			head += " · " + l.Branch
		}
		mark := "▸ "
		if ascii() {
			mark = "> "
		}
		mid = append(mid, paint([]seg{{" " + mark + head, bg.Foreground(p.title).Bold(true)}}, iw, bg))
		pw := max(min(iw-20, 60), 10)
		for i, f := range l.Files {
			if len(mid) >= room {
				break
			}
			if len(mid) == room-1 && i < len(l.Files)-1 {
				mid = append(mid, paint([]seg{{"   … " + strconv.Itoa(len(l.Files)-i) + " more", bg.Foreground(p.dim)}}, iw, bg))
				break
			}
			d := "+" + strconv.Itoa(f.Add) + " -" + strconv.Itoa(f.Del)
			if f.Binary {
				d = "binary"
			}
			st := bg.Foreground(p.text)
			mid = append(mid, paint([]seg{{"   " + fit(f.Path+statusWord(f.Status), pw) + "  ", st}, {d, bg.Foreground(p.muted)}}, iw, bg))
		}
	}
	for len(mid) < room {
		mid = append(mid, paint(nil, iw, bg))
	}
	inner := append(append(top, mid...), bottom...)
	if len(inner) > ih {
		inner = append(inner[:max(ih-1, 0)], inner[len(inner)-1])
	}
	return theme.FloatBox(strings.Join(inner, "\n"), w)
}

func quoteNote(s string) string {
	if s == "" {
		return ""
	}
	return `"` + s + `"`
}

func statusWord(st string) string {
	switch st {
	case "A":
		return " (new)"
	case "D":
		return " (deleted)"
	}
	return ""
}
