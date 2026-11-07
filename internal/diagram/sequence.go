package diagram

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
)

var (
	seqMsg  = regexp.MustCompile(`^(.+?)\s*(-->>|->>|-->|->|--x|-x|--\)|-\))\s*[+-]?\s*(.+?)\s*(?::(.*))?$`)
	seqNote = regexp.MustCompile(`(?i)^note\s+(over|left of|right of)\s+([^:]+?)\s*:(.*)$`)
	seqPart = regexp.MustCompile(`(?i)^(participant|actor)\s+(.+?)(?:\s+as\s+(.+))?$`)
)

type seqEvent struct {
	kind     byte // 'm' message, 'n' note, 'd' divider
	from, to int  // participants (a note over two uses both)
	text     string
	dotted   bool
	head     rune
}

// sequence draws a Mermaid sequence diagram: participant boxes on top,
// lifelines down, one label row and one arrow row per message, notes as
// small boxes and loop / alt / opt blocks as divider rows.
func sequence(lines []string, width int) ([]string, bool) {
	var names, labels []string
	idx := map[string]int{}
	part := func(id string) int {
		id = strings.TrimSpace(id)
		if i, ok := idx[id]; ok {
			return i
		}
		idx[id] = len(names)
		names = append(names, id)
		labels = append(labels, cut(id, maxLabelW))
		return len(names) - 1
	}
	var evs []seqEvent
	number := 0
	for _, l := range lines {
		low := strings.ToLower(l)
		w := strings.Fields(low)[0]
		switch w {
		case "autonumber":
			number = 1
			continue
		case "end", "activate", "deactivate", "title", "box", "create", "destroy", "links", "link":
			continue
		case "loop", "alt", "else", "opt", "par", "and", "critical", "break", "rect", "option":
			evs = append(evs, seqEvent{kind: 'd', text: cut(l, maxLabelW*2)})
			continue
		}
		if m := seqPart.FindStringSubmatch(l); m != nil {
			i := part(m[2])
			if m[3] != "" {
				labels[i] = cut(strings.TrimSpace(m[3]), maxLabelW)
			}
			continue
		}
		if m := seqNote.FindStringSubmatch(l); m != nil {
			ps := strings.Split(m[2], ",")
			a := part(ps[0])
			b := a
			if len(ps) > 1 {
				b = part(ps[1])
			}
			k := byte('n')
			switch strings.ToLower(m[1]) {
			case "left of":
				k = 'l'
			case "right of":
				k = 'r'
			}
			evs = append(evs, seqEvent{kind: k, from: a, to: b, text: cut(strings.TrimSpace(m[3]), maxLabelW)})
			continue
		}
		m := seqMsg.FindStringSubmatch(l)
		if m == nil {
			return nil, false
		}
		e := seqEvent{kind: 'm', from: part(m[1]), to: part(m[3]), text: strings.TrimSpace(m[4]), dotted: strings.HasPrefix(m[2], "--"), head: '▶'}
		switch {
		case strings.HasSuffix(m[2], "x"):
			e.head = '×'
		case strings.HasSuffix(m[2], ")"):
			e.head = '▷'
		case !strings.HasSuffix(m[2], ">>"):
			e.head = '›'
		}
		if number > 0 {
			e.text = strconv.Itoa(number) + ". " + e.text
			number++
		}
		e.text = cut(e.text, maxLabelW*2)
		evs = append(evs, e)
		if len(names) > maxNodes/4 || len(evs) > maxRows/2 {
			return nil, false
		}
	}
	if len(names) == 0 {
		return nil, false
	}
	// Centres: boxes apart by two cells, then wide enough for each label.
	bw := make([]int, len(names))
	cx := make([]int, len(names))
	for i, l := range labels {
		bw[i] = runewidth.StringWidth(l) + 4
		if i == 0 {
			cx[i] = bw[i] / 2
		} else {
			cx[i] = cx[i-1] + (bw[i-1]+1)/2 + bw[i]/2 + 2
		}
	}
	push := func(j, need int) { // move j and the ones after it right
		for k := j; k < len(cx); k++ {
			cx[k] += need
		}
	}
	right := 0 // room needed right of the last lifeline
	for _, e := range evs {
		lw := runewidth.StringWidth(e.text)
		a, b := min(e.from, e.to), max(e.from, e.to)
		switch {
		case e.kind == 'm' && a != b:
			if need := lw + 4 - (cx[b] - cx[a]); need > 0 {
				push(b, need)
			}
		case e.kind == 'm' || e.kind == 'r':
			if a+1 < len(cx) {
				if need := lw + 6 - (cx[a+1] - cx[a]); need > 0 {
					push(a+1, need)
				}
			} else {
				right = max(right, lw+6)
			}
		case e.kind == 'n' && a != b:
			if need := lw + 4 - (cx[b] - cx[a]); need > 0 {
				push(b, need)
			}
		case e.kind == 'n' && a == b:
			if a+1 < len(cx) {
				if need := (lw+4)/2 + 2 + bw[a+1]/2 - (cx[a+1] - cx[a]); need > 0 {
					push(a+1, need)
				}
			}
		}
	}
	// A note left of the first participant, or one centred on it, needs room on the left.
	shift := 0
	for _, e := range evs {
		lw := runewidth.StringWidth(e.text)
		switch {
		case e.kind == 'l':
			shift = max(shift, lw+6-cx[e.from])
		case e.kind == 'n' && e.from == e.to:
			shift = max(shift, (lw+4)/2-cx[e.from])
		}
	}
	if shift > 0 {
		push(0, shift)
	}
	last := len(cx) - 1
	w := max(cx[last]+(bw[last]+1)/2, cx[last]+1+right)
	for _, e := range evs {
		if e.kind == 'd' {
			w = max(w, min(runewidth.StringWidth(e.text)+4, width))
		}
	}
	if w > width {
		return nil, false
	}
	h := 4
	for _, e := range evs {
		switch e.kind {
		case 'm':
			h += 2
			if e.from == e.to {
				h--
			}
		case 'd':
			h++
		default:
			h += 3
		}
	}
	h++
	if h > maxRows {
		return nil, false
	}
	c := newCanvas(w, h)
	for i, l := range labels {
		c.box(cx[i]-bw[i]/2, 0, bw[i], 3, shapeRect, []string{l})
		for y := 3; y < h; y++ {
			c.set(cx[i], y, '│')
		}
		c.set(cx[i], 2, '┬')
	}
	y := 3
	for _, e := range evs {
		switch e.kind {
		case 'm':
			a, b := e.from, e.to
			if a == b {
				c.text(cx[a]+2, y, "↺ "+cut(e.text, w-cx[a]-4))
				y++
				continue
			}
			lo, hi := min(cx[a], cx[b]), max(cx[a], cx[b])
			c.text(lo+2, y, cut(e.text, hi-lo-3))
			dash := '─'
			if e.dotted {
				dash = '╌'
			}
			for x := lo + 1; x < hi; x++ {
				c.set(x, y+1, dash)
			}
			if cx[b] > cx[a] {
				c.set(hi-1, y+1, e.head)
			} else {
				hd := e.head
				switch hd {
				case '▶':
					hd = '◀'
				case '▷':
					hd = '◁'
				case '›':
					hd = '‹'
				}
				c.set(lo+1, y+1, hd)
			}
			y += 2
		case 'd':
			t := "┄ " + cut(e.text, w-4) + " "
			c.text(0, y, t)
			for x := runewidth.StringWidth(t); x < w; x++ {
				if c.cell[y][x] == 0 {
					c.set(x, y, '┄')
				}
			}
			y++
		default:
			lw := runewidth.StringWidth(e.text)
			bx, bwid := cx[e.from]-(lw+4)/2, lw+4
			switch {
			case e.kind == 'n' && e.from != e.to:
				lo, hi := min(cx[e.from], cx[e.to]), max(cx[e.from], cx[e.to])
				bx, bwid = lo-2, hi-lo+5
			case e.kind == 'r':
				bx = cx[e.from] + 2
			case e.kind == 'l':
				bx = cx[e.from] - 2 - bwid
			}
			c.box(max(bx, 0), y, min(bwid, w-max(bx, 0)), 3, shapeRound, []string{e.text})
			y += 3
		}
	}
	return c.rows(), true
}
