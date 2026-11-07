package diagram

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

const (
	maxNodes  = 120
	maxLabelW = 32
)

// mermaid draws a Mermaid block: a flowchart (graph / flowchart, TD, TB,
// BT, LR or RL) or a sequence diagram. Other kinds are not drawn yet.
func mermaid(src string, width int) ([]string, bool) {
	lines := mermaidLines(src)
	if len(lines) == 0 {
		return nil, false
	}
	head := strings.Fields(lines[0])
	switch strings.ToLower(head[0]) {
	case "graph", "flowchart":
		dir := "TD"
		if len(head) > 1 {
			dir = strings.ToUpper(strings.SplitN(head[1], ";", 2)[0])
		}
		rest := lines[1:]
		// "graph LR; A-->B" keeps statements on the header line.
		if i := strings.IndexByte(lines[0], ';'); i >= 0 {
			rest = append([]string{lines[0][i+1:]}, rest...)
		}
		f, ok := parseFlow(rest)
		if !ok || len(f.nodes) == 0 {
			return nil, false
		}
		return f.draw(dir == "LR" || dir == "RL", width)
	case "sequencediagram":
		return sequence(lines[1:], width)
	}
	return nil, false
}

// mermaidLines returns the non-empty lines of src without %% comments,
// a front-matter block or an init directive.
func mermaidLines(src string) []string {
	var out []string
	all := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	for i := 0; i < len(all); i++ {
		l := strings.TrimSpace(cleanText(all[i]))
		if l == "---" && len(out) == 0 { // front matter: skip to its end
			for i++; i < len(all) && strings.TrimSpace(all[i]) != "---"; i++ {
			}
			continue
		}
		if l == "" || strings.HasPrefix(l, "%%") {
			continue
		}
		out = append(out, l)
	}
	return out
}

type fnode struct {
	id    string
	label []string
	shape byte
	dummy bool
	rank  int
	pos   int // start on the minor axis
	minor int // size on the minor axis
	major int // size on the major axis
	order float64
}

type fedge struct {
	from, to int
	label    string
	arrow    bool
}

type flow struct {
	nodes []*fnode
	ids   map[string]int
	edges []fedge
}

// parseFlow reads flowchart statements. It skips styling, click and
// subgraph lines (the nodes inside a subgraph are drawn flat).
func parseFlow(lines []string) (*flow, bool) {
	f := &flow{ids: map[string]int{}}
	for _, l := range lines {
		for _, s := range splitStatements(l) {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			w := strings.ToLower(strings.Fields(s)[0])
			switch w {
			case "classdef", "class", "style", "linkstyle", "click", "subgraph", "end", "direction", "acctitle:", "accdescr:", "acctitle", "accdescr":
				continue
			}
			if !f.chain(s) {
				return nil, false
			}
			if len(f.nodes) > maxNodes {
				return nil, false
			}
		}
	}
	return f, true
}

// splitStatements splits a line at ';' outside brackets and quotes.
func splitStatements(l string) []string {
	var out []string
	depth, quote, start := 0, false, 0
	for i, r := range l {
		switch {
		case r == '"':
			quote = !quote
		case quote:
		case strings.ContainsRune("[({", r):
			depth++
		case strings.ContainsRune("])}", r):
			depth = max(depth-1, 0)
		case r == ';' && depth == 0:
			out = append(out, l[start:i])
			start = i + 1
		}
	}
	return append(out, l[start:])
}

// chain reads "A --> B & C -- text --> D".
func (f *flow) chain(s string) bool {
	p := 0
	var prev []int
	var e fedge
	for {
		var ids []int
		for {
			id, np, ok := f.node(s, p)
			if !ok {
				return false
			}
			ids, p = append(ids, id), np
			p = skipSpace(s, p)
			if p < len(s) && s[p] == '&' {
				p++
				continue
			}
			break
		}
		for _, a := range prev {
			for _, b := range ids {
				f.edges = append(f.edges, fedge{from: a, to: b, label: e.label, arrow: e.arrow})
			}
		}
		p = skipSpace(s, p)
		if p >= len(s) {
			return true
		}
		var ok bool
		if e, p, ok = edgeAt(s, p); !ok {
			return false
		}
		prev = ids
	}
}

func skipSpace(s string, p int) int {
	for p < len(s) && (s[p] == ' ' || s[p] == '\t') {
		p++
	}
	return p
}

func isIDRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

var shapes = []struct {
	open, close string
	shape       byte
}{
	{"(((", ")))", shapeRound}, {"((", "))", shapeRound}, {"([", "])", shapeRound}, {"[(", ")]", shapeRound},
	{"[[", "]]", shapeRect}, {"{{", "}}", shapeDiamond}, {"[/", "]", shapeRect}, {"[\\", "]", shapeRect},
	{"[", "]", shapeRect}, {"(", ")", shapeRound}, {"{", "}", shapeDiamond}, {">", "]", shapeRect},
}

// node reads one node reference at p: an id with an optional shape and
// label, and an optional ":::class". It returns the node's index.
func (f *flow) node(s string, p int) (int, int, bool) {
	p = skipSpace(s, p)
	st := p
	for p < len(s) {
		r, n := utf8.DecodeRuneInString(s[p:])
		if !isIDRune(r) {
			break
		}
		p += n
	}
	if p == st {
		return 0, p, false
	}
	id := s[st:p]
	var label []string
	shape := shapeRect
	for _, sh := range shapes {
		if !strings.HasPrefix(s[p:], sh.open) {
			continue
		}
		body := s[p+len(sh.open):]
		end := 0
		if t := strings.TrimLeft(body, " "); strings.HasPrefix(t, "\"") { // a quoted label may hold closers
			q := strings.IndexByte(t[1:], '"')
			if q < 0 {
				return 0, p, false
			}
			end = len(body) - len(t) + q + 2
		}
		k := strings.Index(body[end:], sh.close)
		if k < 0 {
			return 0, p, false
		}
		text := strings.TrimSpace(body[:end+k])
		text = strings.TrimRight(strings.Trim(text, "\""), "/\\")
		text = strings.TrimLeft(text, "/\\")
		label = splitLabel(text)
		shape = sh.shape
		p += len(sh.open) + end + k + len(sh.close)
		break
	}
	if strings.HasPrefix(s[p:], ":::") {
		p += 3
		for p < len(s) && (isIDRune(rune(s[p])) || s[p] == '-') {
			p++
		}
	}
	i, seen := f.ids[id]
	if !seen {
		if label == nil {
			label = []string{id}
		}
		i = len(f.nodes)
		f.ids[id] = i
		f.nodes = append(f.nodes, &fnode{id: id, label: label, shape: shape})
	} else if label != nil {
		f.nodes[i].label, f.nodes[i].shape = label, shape
	}
	return i, p, true
}

// splitLabel turns "a<br>b" or "a\nb" into lines and drops simple markup.
func splitLabel(s string) []string {
	for _, br := range []string{"<br/>", "<br />", "<br>", "\\n"} {
		s = strings.ReplaceAll(s, br, "\n")
	}
	s = strings.NewReplacer("**", "", "`", "", "&quot;", "\"", "&amp;", "&", "&lt;", "<", "&gt;", ">").Replace(s)
	var out []string
	for _, l := range strings.Split(s, "\n") {
		out = append(out, cut(strings.TrimSpace(l), maxLabelW))
	}
	return out
}

// edgeAt reads one link at p: "-->", "---", "-.->", "==>", "~~~",
// "-- text -->", "-->|text|" (and the dotted / thick forms).
func edgeAt(s string, p int) (fedge, int, bool) {
	var e fedge
	run := func() string {
		st := p
		if p < len(s) && s[p] == '<' {
			p++
		}
		for p < len(s) && strings.IndexByte("-=.~", s[p]) >= 0 {
			p++
		}
		if p < len(s) && s[p] == '>' {
			p++
		} else if p+1 < len(s) && (s[p] == 'x' || s[p] == 'o') && (s[p+1] == ' ' || s[p+1] == '\t') {
			p++
		}
		return s[st:p]
	}
	r := run()
	n := strings.Count(r, "-") + strings.Count(r, "=") + strings.Count(r, ".") + strings.Count(r, "~")
	if n < 2 {
		return e, p, false
	}
	e.arrow = strings.ContainsAny(r[1:], ">xo") || strings.HasPrefix(r, "<")
	if !e.arrow && (r == "--" || r == "==" || r == "-.") { // "-- text -->"
		rest := s[p:]
		k := -1
		for _, m := range []string{"--", "==", ".-"} {
			if i := strings.Index(rest, m); i >= 0 && (k < 0 || i < k) {
				k = i
			}
		}
		if k < 0 {
			return e, p, false
		}
		e.label = strings.Trim(strings.TrimSpace(rest[:k]), "\"")
		p += k
		r = run()
		e.arrow = strings.ContainsAny(r, ">xo")
	}
	p = skipSpace(s, p)
	if p < len(s) && s[p] == '|' {
		k := strings.IndexByte(s[p+1:], '|')
		if k < 0 {
			return e, p, false
		}
		e.label = strings.Trim(strings.TrimSpace(s[p+1:p+1+k]), "\"")
		p += k + 2
	}
	e.label = cut(strings.Join(splitLabel(e.label), " "), maxLabelW)
	return e, p, true
}

// seg is one rank-to-rank piece of an edge (long edges pass dummies).
type seg struct {
	from, to int
	label    string
	arrow    bool
}

// draw lays the flowchart out in ranks (longest path from the roots),
// orders each rank under its parents and routes the links in the gaps.
// lr turns the ranks into columns. Links back up (cycles) and labels that
// find no room are listed under the drawing.
func (f *flow) draw(lr bool, width int) ([]string, bool) {
	n := len(f.nodes)
	// Back links: the ones a depth-first walk finds closing a cycle.
	state := make([]int, n) // 0 new, 1 on stack, 2 done
	out := make([][]int, n)
	for i, e := range f.edges {
		out[e.from] = append(out[e.from], i)
	}
	back := make([]bool, len(f.edges))
	var dfs func(int)
	dfs = func(u int) {
		state[u] = 1
		for _, ei := range out[u] {
			v := f.edges[ei].to
			switch state[v] {
			case 0:
				dfs(v)
			case 1:
				back[ei] = true
			}
		}
		state[u] = 2
	}
	for u := range n {
		if state[u] == 0 {
			dfs(u)
		}
	}
	// Ranks: longest path over the forward links (Kahn order).
	indeg := make([]int, n)
	for i, e := range f.edges {
		if !back[i] {
			indeg[e.to]++
		}
	}
	var queue []int
	for u := range n {
		if indeg[u] == 0 {
			queue = append(queue, u)
		}
	}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, ei := range out[u] {
			if back[ei] {
				continue
			}
			v := f.edges[ei].to
			f.nodes[v].rank = max(f.nodes[v].rank, f.nodes[u].rank+1)
			if indeg[v]--; indeg[v] == 0 {
				queue = append(queue, v)
			}
		}
	}
	var legend []string
	name := func(i int) string { return strings.Join(f.nodes[i].label, " ") }
	// Segments; a link over several ranks passes one dummy per rank.
	var segs []seg
	for i, e := range f.edges {
		if back[i] {
			l := "↺ " + name(e.from) + " → " + name(e.to)
			if e.label != "" {
				l += ": " + e.label
			}
			legend = append(legend, l)
			continue
		}
		u := e.from
		for r := f.nodes[e.from].rank + 1; r < f.nodes[e.to].rank; r++ {
			f.nodes = append(f.nodes, &fnode{dummy: true, rank: r})
			d := len(f.nodes) - 1
			segs = append(segs, seg{from: u, to: d})
			u = d
		}
		segs = append(segs, seg{from: u, to: e.to, label: e.label, arrow: e.arrow})
	}
	if len(f.nodes) > 4*maxNodes {
		return nil, false
	}
	// Sizes on the minor (across ranks) and major (along links) axes.
	ranks := 0
	for _, nd := range f.nodes {
		ranks = max(ranks, nd.rank+1)
		if nd.dummy {
			nd.minor = 1
			continue
		}
		lw := 0
		for _, l := range nd.label {
			lw = max(lw, runewidth.StringWidth(l))
		}
		w, h := lw+4, len(nd.label)+2
		if lr {
			nd.minor, nd.major = h, w
		} else {
			nd.minor, nd.major = w, h
		}
	}
	byRank := make([][]int, ranks)
	for i, nd := range f.nodes {
		byRank[nd.rank] = append(byRank[nd.rank], i)
	}
	parents := make([][]int, len(f.nodes))
	for _, s := range segs {
		parents[s.to] = append(parents[s.to], s.from)
	}
	center := func(i int) int { return f.nodes[i].pos + f.nodes[i].minor/2 }
	gapMinor := 2
	if lr {
		gapMinor = 1
	}
	// Place each rank under the mean centre of its parents.
	for r, ids := range byRank {
		for k, i := range ids {
			nd := f.nodes[i]
			nd.order = float64(k) + 1e6 // no parents: keep the written order, after the others
			if len(parents[i]) > 0 {
				sum := 0
				for _, p := range parents[i] {
					sum += center(p)
				}
				nd.order = float64(sum)/float64(len(parents[i])) + float64(k)*1e-6
			}
		}
		sortByOrder(ids, f.nodes)
		end := -1 << 30
		for _, i := range ids {
			nd := f.nodes[i]
			want := end + gapMinor
			if r > 0 && len(parents[i]) > 0 {
				want = max(want, int(nd.order)-nd.minor/2)
			} else if end == -1<<30 {
				want = 0
			}
			nd.pos = want
			end = want + nd.minor
		}
	}
	lo, hi := 1<<30, 0
	for _, nd := range f.nodes {
		lo = min(lo, nd.pos)
	}
	for _, nd := range f.nodes {
		nd.pos -= lo
		hi = max(hi, nd.pos+nd.minor)
	}
	// Major layout: rank r spans [start[r], start[r]+size[r]); the gap after it
	// holds an exit row, one row per track, (TD) a label row, and the arrow row.
	size := make([]int, ranks)
	for _, nd := range f.nodes {
		size[nd.rank] = max(size[nd.rank], nd.major)
	}
	for r := range size {
		size[r] = max(size[r], 1)
	}
	type gap struct {
		tracks map[int]int // source node → track
		n      int
		label  int // label room: TD rows (0 or 1), LR columns
	}
	gaps := make([]gap, ranks)
	for r := range gaps {
		gaps[r].tracks = map[int]int{}
	}
	type span struct{ lo, hi int }
	groups := map[int]*span{}
	var order []int
	for _, s := range segs {
		g := &gaps[f.nodes[s.from].rank]
		if lw := runewidth.StringWidth(s.label); lw > 0 {
			if lr {
				g.label = max(g.label, lw+2)
			} else {
				g.label = 1
			}
		}
		cu, cv := center(s.from), center(s.to)
		if cu == cv {
			continue
		}
		sp, ok := groups[s.from]
		if !ok {
			sp = &span{cu, cu}
			groups[s.from] = sp
			order = append(order, s.from)
		}
		sp.lo, sp.hi = min(sp.lo, cv), max(sp.hi, cv)
	}
	sortBySpan(order, func(i int) int { return groups[i].lo })
	lastHi := make([][]int, ranks)
	for _, u := range order {
		r, sp := f.nodes[u].rank, groups[u]
		t := 0
		for t < len(lastHi[r]) && lastHi[r][t] >= sp.lo-1 {
			t++
		}
		if t == len(lastHi[r]) {
			lastHi[r] = append(lastHi[r], 0)
		}
		lastHi[r][t] = sp.hi
		gaps[r].tracks[u] = t
		gaps[r].n = max(gaps[r].n, t+1)
	}
	start := make([]int, ranks)
	// exit, tracks, label room, arrow
	gapLen := func(r int) int { return 1 + gaps[r].n + gaps[r].label + 1 }
	total := 0
	for r := range ranks {
		start[r] = total
		total += size[r]
		if r < ranks-1 {
			total += gapLen(r)
		}
	}
	w, h := hi, total
	if lr {
		w, h = total, hi
	} else { // room on the right for the labels of the last column
		need := hi
		for _, s := range segs {
			if s.label != "" {
				need = max(need, center(s.to)+2+runewidth.StringWidth(s.label))
			}
		}
		w = max(hi, min(need, width))
	}
	if w > width || h > maxRows {
		return nil, false
	}
	c := newCanvas(w, h)
	// pt maps (major, minor) to the screen.
	line := func(m1, n1, m2, n2 int) {
		if lr {
			c.line(m1, n1, m2, n2)
		} else {
			c.line(n1, m1, n2, m2)
		}
	}
	put := func(m, n int, r rune) {
		if lr {
			c.set(m, n, r)
		} else {
			c.set(n, m, r)
		}
	}
	type entry struct{ m, n int }
	var exits, plainIn []entry
	var heads []entry
	type lab struct {
		m, n int
		text string
		e    seg
	}
	var labels []lab
	for _, s := range segs {
		r := f.nodes[s.from].rank
		u, v := f.nodes[s.from], f.nodes[s.to]
		cu, cv := center(s.from), center(s.to)
		from := start[r] + u.major - 1 // the box border
		if u.dummy {
			from = start[r]
		} else {
			exits = append(exits, entry{from, cu})
		}
		gs := start[r] + size[r] // first gap cell
		in := start[r+1] - 1     // the arrow cell
		if v.dummy {
			in = start[r+1]
		}
		if cu == cv {
			line(from, cu, in, cv)
		} else {
			tm := gs + 1 + gaps[r].tracks[s.from]
			line(from, cu, tm, cu)
			line(tm, cu, tm, cv)
			line(tm, cv, in, cv)
		}
		if !v.dummy {
			if s.arrow {
				heads = append(heads, entry{in, cv})
			} else {
				plainIn = append(plainIn, entry{in + 1, cv})
			}
		}
		if s.label != "" {
			if lr {
				labels = append(labels, lab{gs + 1 + gaps[r].n + 1, cv, s.label, s})
			} else {
				labels = append(labels, lab{gs + 1 + gaps[r].n, cv + 2, s.label, s})
			}
		}
	}
	// A dummy is a straight line through its rank.
	for _, nd := range f.nodes {
		if nd.dummy {
			line(start[nd.rank], center0(nd), start[nd.rank]+size[nd.rank]-1, center0(nd))
		}
	}
	for _, nd := range f.nodes {
		if nd.dummy {
			continue
		}
		x, y, bw, bh := nd.pos, start[nd.rank], nd.minor, nd.major
		if lr {
			x, y, bw, bh = start[nd.rank], nd.pos, nd.major, nd.minor
		}
		c.box(x, y, bw, bh, nd.shape, nd.label)
	}
	ex, in := '┬', '┴'
	head := '▼'
	if lr {
		ex, in, head = '├', '┤', '▶'
	}
	for _, e := range exits {
		put(e.m, e.n, ex)
	}
	for _, e := range plainIn {
		put(e.m, e.n, in)
	}
	for _, e := range heads {
		put(e.m, e.n, head)
	}
	for _, l := range labels {
		lw := runewidth.StringWidth(l.text)
		free := true
		if lr { // over the line on the entry row: only line cells may go
			for k := range lw {
				if !c.in(l.m+k, l.n) || c.cell[l.n][l.m+k] != 0 {
					free = false
				}
			}
		} else {
			for k := -1; k <= lw; k++ {
				if x := l.n + k; x < c.w && !c.empty(x, l.m) {
					free = false
				}
			}
			free = free && l.n+lw <= c.w
		}
		if free {
			if lr {
				c.text(l.m, l.n, l.text)
			} else {
				c.text(l.n, l.m, l.text)
			}
			continue
		}
		legend = append(legend, name(realFrom(l.e, segs, f))+" → "+name(l.e.to)+": "+l.text)
	}
	rows := c.rows()
	if len(legend) > 0 {
		rows = append(rows, "")
		for _, l := range legend {
			rows = append(rows, cut(l, width))
		}
	}
	return rows, true
}

func center0(nd *fnode) int { return nd.pos + nd.minor/2 }

// realFrom walks a segment back over dummies to the node the link left.
func realFrom(s seg, segs []seg, f *flow) int {
	u := s.from
	for f.nodes[u].dummy {
		found := false
		for _, p := range segs {
			if p.to == u {
				u, found = p.from, true
				break
			}
		}
		if !found {
			break
		}
	}
	return u
}

func sortByOrder(ids []int, nodes []*fnode) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && nodes[ids[j]].order < nodes[ids[j-1]].order; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}

func sortBySpan(ids []int, key func(int) int) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && key(ids[j]) < key(ids[j-1]); j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}
