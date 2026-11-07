package webproto

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// ViewCell is one typed cell of a view row: S for text, path, state, link
// and md; N for num and cost; T (Unix ms) for time; B for bool. Filter
// and sort read the typed value, never a string compare of a number.
type ViewCell struct {
	S string  `json:"s,omitempty"`
	N float64 `json:"n,omitempty"`
	T int64   `json:"t,omitempty"`
	B bool    `json:"b,omitempty"`
}

// ViewRow is one row an extractor made: cells by field id, the row's
// tone (drawn with a shape on the page), its id and what it stands for.
type ViewRow struct {
	ID    string              `json:"id"`
	Cells map[string]ViewCell `json:"cells"`
	Tone  string              `json:"tone,omitempty"`
	Ref   *BlockRef           `json:"ref,omitempty"`
}

// ApplyView applies a checked spec to the rows of its view and makes the
// block: filter (all conditions), sort (stable, up to three keys), the
// total, the limit, the group and the layout. The block has no id (the
// hub gives one) and passes Block.Validate: a block over the size cap
// loses rows from the end and says Truncated.
func ApplyView(d ViewDef, spec ViewSpec, rows []ViewRow) Block {
	eff := Effective(spec, d)
	kept := make([]ViewRow, 0, len(rows))
	for _, r := range rows {
		if viewMatch(d, eff.Filter, r) {
			kept = append(kept, r)
		}
	}
	if len(eff.Sort) > 0 {
		slices.SortStableFunc(kept, func(a, b ViewRow) int {
			for _, o := range eff.Sort {
				f, _ := d.Field(o.Field)
				c := viewCmp(f.Kind, a.Cells[o.Field], b.Cells[o.Field])
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
	total := len(kept)
	truncated := false
	if len(kept) > eff.Limit {
		kept, truncated = kept[:eff.Limit], true
	}
	title := spec.Title
	if title == "" {
		title = d.Title
	}
	specCopy := spec
	b := Block{ID: "view", Title: viewCut(title, blocks.MaxTitle), Source: d.ID, WS: spec.WS, Spec: &specCopy,
		Live: d.Live, Total: total, Truncated: truncated}
	viewLayout(&b, d, eff, kept)
	b.Actions = nil
	for b.Validate() != nil && len(kept) > 0 {
		kept = kept[:len(kept)/2]
		b.Truncated = true
		viewLayout(&b, d, eff, kept)
	}
	b.ID = ""
	return b
}

// viewLayout fills the payload of b for eff.Layout from rows.
func viewLayout(b *Block, d ViewDef, eff ViewSpec, rows []ViewRow) {
	b.Items, b.Columns, b.Rows, b.Groups, b.Fields, b.MD, b.Board, b.State = nil, nil, nil, nil, nil, nil, nil, ""
	fields := make([]ViewField, 0, len(eff.Fields))
	for _, id := range eff.Fields {
		if f, ok := d.Field(id); ok {
			fields = append(fields, f)
		}
	}
	if len(fields) == 0 {
		fields = d.Fields
	}
	b.Kind = eff.Layout
	switch eff.Layout {
	case blocks.KindTable:
		if len(fields) > blocks.MaxColumns {
			fields = fields[:blocks.MaxColumns]
		}
		for _, f := range fields {
			b.Columns = append(b.Columns, BlockColumn{ID: f.ID, Label: viewCut(f.Label, blocks.MaxTitle), Kind: viewColKind(f.Kind)})
		}
		cells := func(r ViewRow) []string {
			out := make([]string, len(fields))
			for i, f := range fields {
				out[i] = viewCut(viewText(f.Kind, r.Cells[f.ID]), blocks.MaxSub)
			}
			return out
		}
		if eff.Group != "" {
			for _, g := range viewGroups(d, eff.Group, rows) {
				bg := BlockGroup{Label: g.label, Count: len(g.rows)}
				for _, r := range g.rows {
					bg.Rows = append(bg.Rows, cells(r))
				}
				b.Groups = append(b.Groups, bg)
			}
			return
		}
		for _, r := range rows {
			b.Rows = append(b.Rows, cells(r))
		}
	case blocks.KindCard, blocks.KindDetail:
		if len(rows) == 0 {
			b.Kind = blocks.KindList // nothing to show: an empty list says so
			return
		}
		r := rows[0]
		for _, f := range fields {
			if len(b.Fields) == blocks.MaxFields {
				break
			}
			b.Fields = append(b.Fields, BlockField{Label: viewCut(f.Label, blocks.MaxTitle), Value: viewCut(viewText(f.Kind, r.Cells[f.ID]), blocks.MaxSub),
				Kind: viewColKind(f.Kind), Tone: viewTone(f, r)})
		}
		b.Ref = r.Ref
	case blocks.KindStatus:
		b.State = blocks.ToneIdle
		for _, r := range rows { // the worst tone wins
			if viewToneRank(r.Tone) > viewToneRank(b.State) {
				b.State = r.Tone
			}
		}
		b.Items = viewItems(fields, rows)
	case blocks.KindText:
		var lines []string
		for _, r := range rows {
			lines = append(lines, viewText(fields[0].Kind, r.Cells[fields[0].ID]))
		}
		b.MD = &MdNode{K: "doc", C: []MdNode{{K: "codeblock", Text: strings.Join(lines, "\n")}}}
	case blocks.KindBoard:
		group := eff.Group
		if group == "" {
			group = fields[len(fields)-1].ID
		}
		board := &BlockBoard{}
		for i, g := range viewGroups(d, group, rows) {
			if i == blocks.MaxBoardCols {
				break
			}
			board.Columns = append(board.Columns, BlockBoardColumn{ID: "c" + strconv.Itoa(i), Label: viewCut(g.label, blocks.MaxTitle), Items: viewItems(fields, g.rows)})
		}
		b.Board = board
	default: // list
		b.Kind = blocks.KindList
		if eff.Group != "" {
			for _, g := range viewGroups(d, eff.Group, rows) {
				b.Groups = append(b.Groups, BlockGroup{Label: g.label, Count: len(g.rows), Items: viewItems(fields, g.rows)})
			}
			return
		}
		b.Items = viewItems(fields, rows)
	}
}

// viewItems makes list items: the first text field is the title, the
// other shown fields the sub line (" · "), the first time field the time.
func viewItems(fields []ViewField, rows []ViewRow) []BlockItem {
	out := make([]BlockItem, 0, len(rows))
	for i, r := range rows {
		it := BlockItem{ID: r.ID, Tone: r.Tone, Ref: r.Ref}
		if !viewIDRe(it.ID) {
			it.ID = "r" + strconv.Itoa(i)
		}
		var sub []string
		for _, f := range fields {
			c := r.Cells[f.ID]
			switch {
			case f.Kind == FieldTime && it.Time == 0 && c.T > 0:
				it.Time = c.T
			case it.Title == "" && f.Kind != FieldTime && f.Kind != FieldBool:
				it.Title = viewText(f.Kind, c)
			default:
				if t := viewText(f.Kind, c); t != "" && f.Kind != FieldTime {
					if f.Kind == FieldBool {
						t = f.Label
					}
					sub = append(sub, t)
				}
			}
		}
		if it.Title == "" {
			it.Title = "—"
		}
		it.Title = viewCut(it.Title, blocks.MaxTitle)
		it.Sub = viewCut(strings.Join(sub, " · "), blocks.MaxSub)
		out = append(out, it)
		if len(out) == blocks.MaxItems {
			break
		}
	}
	return out
}

type viewGroup struct {
	label string
	rows  []ViewRow
}

// viewGroups splits rows by the shown value of field, in first-seen order;
// past MaxGroups the rest go to "Other".
func viewGroups(d ViewDef, field string, rows []ViewRow) []viewGroup {
	f, _ := d.Field(field)
	var out []viewGroup
	at := map[string]int{}
	for _, r := range rows {
		label := viewText(f.Kind, r.Cells[field])
		if label == "" {
			label = "(none)"
		}
		i, ok := at[label]
		if !ok {
			if len(out) == blocks.MaxGroups-1 {
				label = "Other"
				i, ok = at[label]
			}
			if !ok {
				i = len(out)
				at[label] = i
				out = append(out, viewGroup{label: label})
			}
		}
		out[i].rows = append(out[i].rows, r)
	}
	return out
}

// viewMatch: the row meets every condition.
func viewMatch(d ViewDef, conds []ViewCond, r ViewRow) bool {
	for _, c := range conds {
		f, _ := d.Field(c.Field)
		cell := r.Cells[c.Field]
		text := viewText(f.Kind, cell)
		switch c.Op {
		case "empty":
			if text != "" {
				return false
			}
		case "not_empty":
			if text == "" {
				return false
			}
		case "contains":
			if !strings.Contains(strings.ToLower(text), strings.ToLower(c.Value)) {
				return false
			}
		case "starts":
			if !strings.HasPrefix(strings.ToLower(text), strings.ToLower(c.Value)) {
				return false
			}
		case "in":
			if !slices.ContainsFunc(c.In, func(v string) bool { return viewEq(f.Kind, cell, v) }) {
				return false
			}
		case "eq":
			if !viewEq(f.Kind, cell, c.Value) {
				return false
			}
		case "ne":
			if viewEq(f.Kind, cell, c.Value) {
				return false
			}
		case "gt", "lt", "ge", "le":
			v, err := strconv.ParseFloat(c.Value, 64)
			if err != nil {
				return false
			}
			n := viewNum(f.Kind, cell)
			ok := map[string]bool{"gt": n > v, "lt": n < v, "ge": n >= v, "le": n <= v}[c.Op]
			if !ok {
				return false
			}
		}
	}
	return true
}

// viewEq compares a cell with a value as typed by the field's kind.
func viewEq(kind string, c ViewCell, v string) bool {
	switch kind {
	case FieldNum, FieldCost, FieldTime:
		n, err := strconv.ParseFloat(v, 64)
		return err == nil && viewNum(kind, c) == n
	case FieldBool:
		return c.B == (v == "true" || v == "yes" || v == "1")
	}
	return strings.EqualFold(c.S, v)
}

func viewNum(kind string, c ViewCell) float64 {
	if kind == FieldTime {
		return float64(c.T)
	}
	return c.N
}

func viewCmp(kind string, a, b ViewCell) int {
	switch kind {
	case FieldNum, FieldCost:
		return cmp.Compare(a.N, b.N)
	case FieldTime:
		return cmp.Compare(a.T, b.T)
	case FieldBool:
		return cmp.Compare(boolInt(a.B), boolInt(b.B))
	}
	return cmp.Compare(strings.ToLower(a.S), strings.ToLower(b.S))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// viewText is a cell as the page shows it in a cell or a sub line. A
// time stays Unix ms (the page writes "2 h" for a time column).
func viewText(kind string, c ViewCell) string {
	switch kind {
	case FieldNum:
		if c.N == 0 && c.S == "" {
			return "0"
		}
		return strconv.FormatFloat(c.N, 'f', -1, 64)
	case FieldCost:
		return "$" + strconv.FormatFloat(c.N, 'f', 2, 64)
	case FieldTime:
		if c.T == 0 {
			return ""
		}
		return strconv.FormatInt(c.T, 10)
	case FieldBool:
		if c.B {
			return "yes"
		}
		return ""
	}
	return c.S
}

func viewColKind(kind string) string {
	switch kind {
	case FieldNum:
		return blocks.ColNum
	case FieldTime:
		return blocks.ColTime
	case FieldCost:
		return blocks.ColCost
	case FieldPath:
		return blocks.ColMono
	}
	return blocks.ColText
}

// viewTone is the tone of a state field's value: the row's tone.
func viewTone(f ViewField, r ViewRow) string {
	if f.Kind == FieldState {
		return r.Tone
	}
	return ""
}

func viewToneRank(t string) int {
	return map[string]int{blocks.ToneIdle: 0, blocks.ToneInfo: 1, blocks.ToneOK: 2, blocks.ToneBusy: 3, blocks.ToneWarn: 4, blocks.ToneBad: 5}[t]
}

// viewCut cuts s to max runes ("…" at the end) and drops control runes.
func viewCut(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}
