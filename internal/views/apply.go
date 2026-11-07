package views

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// Loc is the time zone of day values ("today", "2026-11-07") and of the
// day a time field groups by: the laptop's own. Tests set it.
var Loc = time.Local

// val is a parsed filter value. A time value is a range [lo, hi): one
// instant (hi = lo + 1) or a whole day.
type val struct {
	s      string
	n      float64
	lo, hi int64
	day    bool
	b      bool
}

var relRe = regexp.MustCompile(`^-?([0-9]{1,6})(m|h|d|w)$`)

// parseValue parses a filter value for a field kind. now is Unix ms; it
// only matters for relative times ("today", "-24h", "7d" = 7 days ago).
func parseValue(kind, v string, now int64) (val, error) {
	switch kind {
	case FieldNum, FieldCost:
		t := strings.TrimSpace(v)
		if kind == FieldCost {
			t = strings.TrimPrefix(t, "$")
		}
		n, err := strconv.ParseFloat(t, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return val{}, fmt.Errorf("%q is not a number", v)
		}
		return val{n: n}, nil
	case FieldBool:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "yes", "1":
			return val{b: true}, nil
		case "false", "no", "0":
			return val{b: false}, nil
		}
		return val{}, fmt.Errorf("%q is not true or false", v)
	case FieldTime:
		return parseTime(strings.TrimSpace(v), now)
	}
	return val{s: v}, nil
}

func parseTime(v string, now int64) (val, error) {
	at := time.UnixMilli(now).In(Loc)
	day := func(t time.Time) val {
		start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, Loc)
		return val{lo: start.UnixMilli(), hi: start.AddDate(0, 0, 1).UnixMilli(), day: true}
	}
	point := func(ms int64) val { return val{lo: ms, hi: ms + 1} }
	switch strings.ToLower(v) {
	case "now":
		return point(now), nil
	case "today":
		return day(at), nil
	case "yesterday":
		return day(at.AddDate(0, 0, -1)), nil
	}
	if m := relRe.FindStringSubmatch(strings.ToLower(v)); m != nil {
		n, _ := strconv.Atoi(m[1])
		unit := map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour}[m[2]]
		return point(at.Add(-time.Duration(n) * unit).UnixMilli()), nil
	}
	if intRe.MatchString(v) {
		ms, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			return point(ms), nil
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", v, Loc); err == nil {
		return day(t), nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return point(t.UnixMilli()), nil
	}
	return val{}, fmt.Errorf("%q is not a time (now, today, yesterday, -24h, 7d, 2026-11-07, RFC 3339 or Unix ms)", v)
}

func isEmpty(kind string, c Cell) bool {
	if c.Null {
		return true
	}
	switch kind {
	case FieldNum, FieldCost, FieldBool:
		return false
	case FieldTime:
		return c.T == 0
	}
	return c.S == ""
}

// match reports whether cell c of a field of kind holds cond.
func match(kind string, cond blocks.ViewCond, c Cell, now int64) bool {
	switch cond.Op {
	case "empty":
		return isEmpty(kind, c)
	case "not_empty":
		return !isEmpty(kind, c)
	case "in":
		for _, v := range cond.In {
			if eqCell(kind, v, c, now) {
				return true
			}
		}
		return false
	case "ne":
		return !eqCell(kind, cond.Value, c, now)
	case "eq":
		return eqCell(kind, cond.Value, c, now)
	}
	if isEmpty(kind, c) {
		return false
	}
	v, err := parseValue(kind, cond.Value, now)
	if err != nil {
		return false
	}
	switch cond.Op {
	case "contains":
		return strings.Contains(strings.ToLower(c.S), strings.ToLower(v.s))
	case "starts":
		return strings.HasPrefix(strings.ToLower(c.S), strings.ToLower(v.s))
	}
	if kind == FieldTime {
		switch cond.Op {
		case "gt":
			return c.T >= v.hi
		case "ge":
			return c.T >= v.lo
		case "lt":
			return c.T < v.lo
		case "le":
			return c.T < v.hi
		}
		return false
	}
	switch cond.Op {
	case "gt":
		return c.N > v.n
	case "ge":
		return c.N >= v.n
	case "lt":
		return c.N < v.n
	case "le":
		return c.N <= v.n
	}
	return false
}

func eqCell(kind, raw string, c Cell, now int64) bool {
	v, err := parseValue(kind, raw, now)
	if err != nil {
		return false
	}
	if c.Null {
		return false
	}
	switch kind {
	case FieldNum, FieldCost:
		return c.N == v.n
	case FieldBool:
		return c.B == v.b
	case FieldTime:
		return c.T >= v.lo && c.T < v.hi
	}
	return strings.EqualFold(c.S, v.s)
}

// compareCells orders two cells of one kind; text kinds ignore case.
func compareCells(kind string, a, b Cell) int {
	switch kind {
	case FieldNum, FieldCost:
		return cmp.Compare(a.N, b.N)
	case FieldTime:
		return cmp.Compare(a.T, b.T)
	case FieldBool:
		return cmp.Compare(boolInt(a.B), boolInt(b.B))
	}
	if c := cmp.Compare(strings.ToLower(a.S), strings.ToLower(b.S)); c != 0 {
		return c
	}
	return cmp.Compare(a.S, b.S)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// stateTone maps a state word to a tone; the page draws the tone with a
// shape (✓ ● ✗ ◐ ○), never with colour alone.
func stateTone(s string) string {
	switch strings.ToLower(s) {
	case "ok", "pass", "passed", "success", "succeeded", "done", "merged", "clean", "green", "ready", "approved", "open":
		return blocks.ToneOK
	case "fail", "failed", "failure", "error", "errored", "bad", "red", "broken", "conflict", "rejected", "crashloopbackoff":
		return blocks.ToneBad
	case "running", "pending", "queued", "busy", "in_progress", "starting", "working", "building":
		return blocks.ToneBusy
	case "warn", "warning", "needs_you", "waiting", "blocked", "changes_requested", "stale", "dirty":
		return blocks.ToneWarn
	case "":
		return ""
	}
	return blocks.ToneIdle
}

// Text is how a cell reads in a block: the same for the table, the list,
// the group labels and the AI's short text of a view.
func Text(kind string, c Cell) string {
	if c.Null {
		return ""
	}
	switch kind {
	case FieldNum:
		return strconv.FormatFloat(c.N, 'f', -1, 64)
	case FieldCost:
		return fmt.Sprintf("$%.2f", c.N)
	case FieldTime:
		if c.T == 0 {
			return ""
		}
		return strconv.FormatInt(c.T, 10) // the page shows "2 h" from the column kind
	case FieldBool:
		if c.B {
			return "yes"
		}
		return "no"
	}
	return c.S
}

func groupLabel(kind string, c Cell) string {
	if isEmpty(kind, c) {
		return "(none)"
	}
	if kind == FieldTime {
		return time.UnixMilli(c.T).In(Loc).Format("2006-01-02")
	}
	l := strings.TrimSpace(cut(Text(kind, c), blocks.MaxTitle))
	if l == "" {
		return "(none)"
	}
	return l
}

func colKind(kind string) string {
	switch kind {
	case FieldNum:
		return blocks.ColNum
	case FieldTime:
		return blocks.ColTime
	case FieldCost:
		return blocks.ColCost
	case FieldState:
		return blocks.ColTone
	case FieldPath:
		return blocks.ColMono
	}
	return blocks.ColText
}

// cut shortens s to at most n runes, with "…" when it cut. A cell is
// one line of valid text: bad UTF-8 is replaced and every run of white
// space or control characters becomes one space.
func cut(s string, n int) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	if strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) }) >= 0 {
		s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }), " ")
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// orDash keeps a text that must not be empty readable.
func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

var rowIDRe = regexp.MustCompile(`^[A-Za-z0-9_.:\-]{1,64}$`)

// Filtered applies a resolved spec's filter and sort to rows, in place
// order kept for equal keys. now is Unix ms (relative times).
func Filtered(d *Def, s *blocks.ViewSpec, rows []Row, now int64) []Row {
	out := make([]Row, 0, len(rows))
next:
	for _, r := range rows {
		for _, c := range s.Filter {
			f, _ := d.Field(c.Field)
			if !match(f.Kind, c, r.Cells[c.Field], now) {
				continue next
			}
		}
		out = append(out, r)
	}
	if len(s.Sort) > 0 {
		slices.SortStableFunc(out, func(a, b Row) int {
			for _, o := range s.Sort {
				f, _ := d.Field(o.Field)
				ca, cb := a.Cells[o.Field], b.Cells[o.Field]
				ea, eb := isEmpty(f.Kind, ca), isEmpty(f.Kind, cb)
				switch {
				case ea && eb:
					continue
				case ea:
					return 1 // no value: last, either way
				case eb:
					return -1
				}
				c := compareCells(f.Kind, ca, cb)
				if o.Desc {
					c = -c
				}
				if c != 0 {
					return c
				}
			}
			return 0
		})
	}
	return out
}

// Render builds the block of a resolved spec from the rows its source
// gave: filter, sort, group, limit, then the layout. fetchCut says the
// source itself stopped early (the block is marked truncated). The block
// always passes blocks.Validate: cells are cut to the caps and rows are
// dropped (truncated) until the block fits.
func Render(id string, d *Def, s blocks.ViewSpec, rows []Row, fetchCut bool, now int64) (blocks.Block, error) {
	if !rowLayouts[s.Layout] {
		return blocks.Block{}, invalid("view %s: layout %s is not built from rows", d.ID, s.Layout)
	}
	rows = Filtered(d, &s, rows, now)
	limit := s.Limit
	for {
		b := build(id, d, s, rows, limit)
		b.Truncated = b.Truncated || fetchCut
		raw, err := json.Marshal(b)
		if err != nil {
			return blocks.Block{}, err
		}
		if len(raw) <= blocks.MaxBlockBytes || limit <= 1 {
			if err := b.Validate(); err != nil {
				return blocks.Block{}, err
			}
			return b, nil
		}
		limit /= 2
	}
}

func build(id string, d *Def, s blocks.ViewSpec, rows []Row, limit int) blocks.Block {
	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = d.Title
	}
	spec := s
	b := blocks.Block{
		ID:     id,
		Kind:   s.Layout,
		Title:  cut(title, blocks.MaxTitle),
		Source: d.ID,
		WS:     s.WS,
		Spec:   &spec,
		Live:   d.Live,
		Total:  len(rows),
	}
	fields := make([]Field, 0, len(s.Fields))
	for _, id := range s.Fields {
		if f, ok := d.Field(id); ok {
			fields = append(fields, f)
		}
	}
	switch s.Layout {
	case blocks.KindCard, blocks.KindDetail:
		if len(rows) == 0 {
			return b
		}
		r := rows[0]
		b.Truncated = len(rows) > 1
		if len(fields) > blocks.MaxFields {
			fields = fields[:blocks.MaxFields]
		}
		for _, f := range fields {
			c := r.Cells[f.ID]
			bf := blocks.BlockField{Label: orDash(cut(f.Label, blocks.MaxTitle)), Value: cut(Text(f.Kind, c), blocks.MaxSub), Kind: colKind(f.Kind)}
			if f.Kind == FieldState {
				bf.Tone = stateTone(c.S)
			}
			b.Fields = append(b.Fields, bf)
		}
		b.Ref = cleanRef(r.Ref)
		b.Actions = rowActions(d, r)
		return b
	}
	if s.Group == "" {
		shown := rows
		if len(shown) > limit {
			shown = shown[:limit]
			b.Truncated = true
		}
		if s.Layout == blocks.KindTable {
			b.Columns = columns(fields)
			b.Rows = tableRows(fields, shown)
		} else {
			b.Items = items(d, s, fields, shown, 0)
		}
		return b
	}
	g, _ := d.Field(s.Group)
	type group struct {
		label string
		rows  []Row
	}
	var groups []*group
	byLabel := map[string]*group{}
	for _, r := range rows {
		l := groupLabel(g.Kind, r.Cells[g.ID])
		gr := byLabel[l]
		if gr == nil {
			gr = &group{label: l}
			byLabel[l] = gr
			groups = append(groups, gr)
		}
		gr.rows = append(gr.rows, r)
	}
	if len(groups) > blocks.MaxGroups {
		groups = groups[:blocks.MaxGroups]
		b.Truncated = true
	}
	if s.Layout == blocks.KindTable {
		b.Columns = columns(fields)
	}
	left := limit
	for gi, gr := range groups {
		shown := gr.rows
		if len(shown) > left {
			shown = shown[:left]
			b.Truncated = true
		}
		left -= len(shown)
		bg := blocks.BlockGroup{Label: gr.label, Count: len(gr.rows)}
		if s.Layout == blocks.KindTable {
			bg.Rows = tableRows(fields, shown)
		} else {
			bg.Items = items(d, s, fields, shown, gi*blocks.MaxItems)
		}
		b.Groups = append(b.Groups, bg)
		if left == 0 {
			if gi < len(groups)-1 {
				b.Truncated = true
			}
			break
		}
	}
	return b
}

func columns(fields []Field) []blocks.BlockColumn {
	cols := make([]blocks.BlockColumn, 0, len(fields))
	for _, f := range fields {
		cols = append(cols, blocks.BlockColumn{ID: f.ID, Label: cut(f.Label, blocks.MaxTitle), Kind: colKind(f.Kind)})
	}
	return cols
}

func tableRows(fields []Field, rows []Row) [][]string {
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		cells := make([]string, len(fields))
		for i, f := range fields {
			cells[i] = cut(Text(f.Kind, r.Cells[f.ID]), blocks.MaxSub)
		}
		out = append(out, cells)
	}
	return out
}

// items builds list items. The first shown field is the title; a time
// field gives the item's time ("2 h"), a state field its tone and badge;
// the rest is the sub line: up to two values when compact, every value
// with its label when detailed.
func items(d *Def, s blocks.ViewSpec, fields []Field, rows []Row, base int) []blocks.BlockItem {
	out := make([]blocks.BlockItem, 0, len(rows))
	for i, r := range rows {
		it := blocks.BlockItem{ID: r.ID, Ref: cleanRef(r.Ref), Actions: rowActions(d, r), Tone: r.Tone}
		if !rowIDRe.MatchString(it.ID) {
			it.ID = "r" + strconv.Itoa(base+i)
		}
		var sub []string
		for fi, f := range fields {
			c := r.Cells[f.ID]
			txt := Text(f.Kind, c)
			switch {
			case fi == 0:
				it.Title = cut(txt, blocks.MaxTitle)
				continue
			case f.Kind == FieldTime && it.Time == 0 && c.T > 0 && !c.Null:
				it.Time = c.T
				continue
			case f.Kind == FieldState && it.Badge == "" && txt != "":
				it.Badge = cut(txt, 24)
				if it.Tone == "" {
					it.Tone = stateTone(txt)
				}
				continue
			}
			if txt == "" {
				continue
			}
			if s.Density == DensityDetailed {
				sub = append(sub, f.Label+": "+txt)
			} else if len(sub) < 2 {
				sub = append(sub, txt)
			}
		}
		it.Title = orDash(it.Title)
		it.Sub = cut(strings.Join(sub, " · "), blocks.MaxSub)
		out = append(out, it)
	}
	return out
}

// rowActions gives a row its own actions, or the view's row actions with
// the row's ref; at most blocks.MaxActions.
func rowActions(d *Def, r Row) []blocks.BlockAction {
	src := r.Actions
	if src == nil {
		if r.Ref == nil {
			return nil
		}
		src = d.Actions
	}
	var out []blocks.BlockAction
	for _, a := range src {
		if len(out) == blocks.MaxActions {
			break
		}
		if a.Ref == nil {
			a.Ref = cleanRef(r.Ref)
		}
		out = append(out, a)
	}
	return out
}

// cleanRef keeps a ref only when its path is relative: a row never sends
// an absolute path to the phone.
func cleanRef(r *blocks.BlockRef) *blocks.BlockRef {
	if r == nil {
		return nil
	}
	if strings.HasPrefix(r.Path, "/") || strings.HasPrefix(r.Path, `\`) || (len(r.Path) > 1 && r.Path[1] == ':') || len(r.Path) > blocks.MaxPath {
		return nil
	}
	c := *r
	return &c
}
