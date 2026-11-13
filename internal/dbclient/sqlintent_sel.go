package dbclient

import (
	"strings"
)

// SQL intentions on a selection (DBX-42):
//   - Extract Expression into Subquery Column: the block's one table
//     becomes (SELECT t.*, <expr> AS expr FROM t) t and every same
//     expression of the block reads expr.
//   - Extract Expression into CTE: the same, as a CTE.
//   - Extract the Selected Subquery as CTE: a selected ( SELECT … )
//     outside FROM becomes a CTE, read as (SELECT * FROM sub).
// They are only offered when the rewrite keeps the meaning: no
// aggregates or windows, one plain table, no names of an outer query.

// SQLIntentOpts are the facts beyond the cursor.
type SQLIntentOpts struct {
	SelStart, SelEnd int // the selected bytes of the statement; SelEnd <= SelStart = no selection
}

// The selection intention ids.
const (
	IntentExtractExprSub = "extract-expr-subquery"
	IntentExtractExprCTE = "extract-expr-cte"
	IntentExtractSelCTE  = "extract-selection-cte"
)

var intentAggWords = map[string]bool{
	"count": true, "sum": true, "avg": true, "min": true, "max": true, "array_agg": true, "string_agg": true,
	"group_concat": true, "json_agg": true, "jsonb_agg": true, "json_object_agg": true, "jsonb_object_agg": true,
	"bool_and": true, "bool_or": true, "every": true, "stddev": true, "variance": true, "listagg": true,
	"json_arrayagg": true, "json_objectagg": true,
}

// selToks is the token range [a, b) that covers the selection exactly
// (white space around it left out) and is balanced in parentheses.
func (c *intentCtx) selToks() (int, int, bool) {
	s, e := c.opts.SelStart, c.opts.SelEnd
	if e <= s || s < 0 || e > len(c.src) {
		return 0, 0, false
	}
	for s < e && strings.IndexByte(" \t\r\n\f\v", c.src[s]) >= 0 {
		s++
	}
	for e > s && strings.IndexByte(" \t\r\n\f\v", c.src[e-1]) >= 0 {
		e--
	}
	a, b := -1, -1
	for j, t := range c.toks {
		if t.pos == s {
			a = j
		}
		if t.end == e {
			b = j + 1
		}
	}
	if a < 0 || b <= a {
		return 0, 0, false
	}
	open := 0
	for j := a; j < b; j++ {
		switch {
		case isIPunct(c.toks[j], "("):
			open++
		case isIPunct(c.toks[j], ")"):
			open--
		}
		if open < 0 {
			return 0, 0, false
		}
	}
	return a, b, open == 0
}

// exprBefore: the token before an expression lets it stand alone (no
// operator binds into it).
func exprBefore(t itok) bool {
	switch strings.ToLower(t.text) {
	case ",", "(", "=", "<>", "!=", "<", ">", "<=", ">=", "select", "distinct", "where", "and", "or", "not",
		"when", "then", "else", "on", "by", "having", "all":
		return true
	}
	return false
}

// exprAfter: the token after an expression ends it.
func exprAfter(t itok) bool {
	switch strings.ToLower(t.text) {
	case ",", ")", ";", "=", "<>", "!=", "<", ">", "<=", ">=", "and", "or", "as", "asc", "desc", "from", "where",
		"group", "order", "having", "limit", "offset", "when", "then", "else", "end", "is", "like", "ilike",
		"between", "in", "not", "nulls", "union", "intersect", "except", "fetch", "for", "window":
		return true
	}
	return false
}

// exprPlan is a selected expression that can move into its table.
type exprPlan struct {
	it    fromItem
	q     string // the table's qualifier: its alias, else its name
	name  string // the new column
	inner string // SELECT q.*, expr AS name FROM table [alias]
	eds   []sqlEdit
	short string
}

// standsAlone: the tokens [j, j+n) of block b are a whole expression,
// or the selected item ends before the alias of a select item.
func (c *intentCtx) standsAlone(b *qblock, j, n int) bool {
	if j <= 0 || !exprBefore(c.toks[j-1]) {
		return false
	}
	z := j + n
	if z >= b.end {
		return true
	}
	if exprAfter(c.toks[z]) {
		return true
	}
	for _, s := range b.items {
		if s.alias == z && s.b == z {
			return true
		}
	}
	return false
}

func (c *intentCtx) exprExtract() (*exprPlan, bool) {
	a, z, ok := c.selToks()
	if !ok || z-a < 2 || c.owner[a] < 0 {
		return nil, false
	}
	b := c.blocks[c.owner[a]]
	if len(b.from) != 1 {
		return nil, false
	}
	it := b.from[0]
	if it.sub || it.fn || it.colList {
		return nil, false
	}
	if _, into := b.clause["into"]; into {
		return nil, false
	}
	d := c.toks[a].depth
	for j := a; j < z; j++ {
		t := c.toks[j]
		if c.owner[j] != b.idx || isIWord(t, "select", "over") || t.depth == d && isIPunct(t, ",") {
			return nil, false
		}
		if t.kind == fkWord && intentAggWords[strings.ToLower(t.text)] && j+1 < z && isIPunct(c.toks[j+1], "(") {
			return nil, false
		}
	}
	f := b.clause["from"]
	fe := c.nextClause(b, f)
	if a >= f && a < fe {
		return nil, false
	}
	for _, w := range []string{"limit", "offset", "fetch"} {
		if k, ok := b.clause[w]; ok && a > k && a < c.nextClause(b, k) {
			return nil, false
		}
	}
	if a >= b.selStart && a < b.selEnd {
		inItem := false
		for _, s := range b.items {
			inItem = inItem || a >= s.a && z <= s.b
		}
		if !inItem {
			return nil, false
		}
	}
	if !c.standsAlone(b, a, z-a) {
		return nil, false
	}
	// schema.table without an alias: other schema.table.x would break.
	multi := it.b-it.a > 1 && it.alias < 0
	if multi {
		for j := b.sel; j < b.end; j++ {
			if (j < it.a || j >= it.end) && c.sameToks(j, it.a, it.b) {
				return nil, false
			}
		}
	}
	q := c.toks[it.b-1].text
	if it.alias >= 0 {
		q = c.toks[it.alias].text
	}
	cols, known := c.colsOf(it)
	colset := map[string]bool{}
	for _, n := range cols {
		colset[strings.ToLower(n)] = true
	}
	for j := a; j < z; j++ {
		t := c.toks[j]
		if !c.isName(t) || t.kind == fkWord && intentKeyword(t.text) || j > a && isIPunct(c.toks[j-1], ".") {
			continue
		}
		next := itok{}
		if j+1 < len(c.toks) {
			next = c.toks[j+1]
		}
		switch {
		case isIPunct(next, "."):
			if it.alias >= 0 && !c.sameName(t, c.toks[it.alias]) || it.alias < 0 && !c.sameName(t, c.toks[it.b-1]) {
				return nil, false // a name of another (outer) table
			}
		case isIPunct(next, "("), next.kind == fkString && next.text[0] == '\'':
			// a function, or DATE '…'
		case t.kind == fkWord && fmtKeywords[strings.ToUpper(t.text)]:
			// a keyword such as CASE, NULL, TRUE
		case known:
			if !colset[strings.ToLower(unquoteIdent(t.text))] {
				return nil, false
			}
		case b.depth > 0:
			return nil, false // could be a column of an outer query
		}
	}
	name := "expr"
	for _, s := range b.items {
		if s.a == a && s.b == z && s.alias >= 0 {
			name = c.toks[s.alias].text
		}
	}
	taken := c.takenNames()
	for n := range colset {
		taken[n] = true
	}
	if name == "expr" || taken[strings.ToLower(unquoteIdent(name))] {
		name = freeName(unquoteIdent(name), taken)
	}
	p := &exprPlan{it: it, q: q, name: name}
	norm, n := c.norm(a, z), z-a
	for j := b.sel + 1; j+n <= b.end; j++ {
		if j >= f && j < fe || c.owner[j] != b.idx || c.norm(j, j+n) != norm {
			continue
		}
		same := true
		for k := j; k < j+n; k++ {
			same = same && c.owner[k] == b.idx
		}
		if !same || !c.standsAlone(b, j, n) {
			continue
		}
		start, end := c.toks[j].pos, c.toks[j+n-1].end
		for _, s := range b.items {
			if s.a == j && s.b == j+n && s.alias >= 0 && strings.EqualFold(unquoteIdent(c.toks[s.alias].text), unquoteIdent(name)) {
				end = c.toks[s.alias].end // "expr AS total" becomes "total"
			}
		}
		p.eds = append(p.eds, sqlEdit{start, end, name})
		j += n - 1
	}
	table := c.src[c.toks[it.a].pos:c.toks[it.end-1].end]
	p.inner = "SELECT " + q + ".*, " + c.text(a, z) + " AS " + name + " FROM " + table
	p.short = strings.Join(strings.Fields(c.text(a, z)), " ")
	if r := []rune(p.short); len(r) > 30 {
		p.short = string(r[:29]) + "…"
	}
	return p, true
}

func (c *intentCtx) extractExprSub() (string, []sqlEdit, bool) {
	p, ok := c.exprExtract()
	if !ok {
		return "", nil, false
	}
	eds := append(p.eds, sqlEdit{c.toks[p.it.a].pos, c.toks[p.it.end-1].end, "(" + p.inner + ") " + p.q})
	return "Extract " + p.short + " into subquery column " + p.name, eds, true
}

func (c *intentCtx) extractExprCTE() (string, []sqlEdit, bool) {
	if !c.cteOK() {
		return "", nil, false
	}
	p, ok := c.exprExtract()
	if !ok {
		return "", nil, false
	}
	taken := c.takenNames()
	base := unquoteIdent(c.toks[p.it.b-1].text) + "_" + unquoteIdent(p.name)
	name := freeName(c.quoteCol(base), taken)
	eds := append(p.eds,
		sqlEdit{c.toks[p.it.a].pos, c.toks[p.it.end-1].end, name + " " + p.q},
		c.cteDefEdit(p.it.a, name+" AS ("+p.inner+")"))
	return "Extract " + p.short + " into CTE " + name, eds, true
}

func (c *intentCtx) extractSelCTE() (string, []sqlEdit, bool) {
	a, z, ok := c.selToks()
	if !ok || !c.cteOK() {
		return "", nil, false
	}
	var open, cl int
	switch {
	case isIPunct(c.toks[a], "(") && c.match(a) == z-1 && a+1 < z-1 && isIWord(c.toks[a+1], "select", "with"):
		open, cl = a, z-1
	case isIWord(c.toks[a], "select", "with") && a > 0 && isIPunct(c.toks[a-1], "(") && c.match(a-1) == z:
		open, cl = a-1, z
	default:
		return "", nil, false
	}
	for _, d := range c.ctes {
		if d.open == open {
			return "", nil, false
		}
	}
	// The names the subquery defines; any other qualifier is an outer one.
	defined := map[string]bool{}
	inFrom := map[int]bool{}
	for _, b := range c.blocks {
		if b.sel <= open || b.sel >= cl {
			continue
		}
		for _, it := range b.from {
			if it.a == open {
				return "", nil, false
			}
			if !it.sub && !it.fn {
				defined[strings.ToLower(unquoteIdent(c.toks[it.b-1].text))] = true
			}
			if it.alias >= 0 {
				defined[strings.ToLower(unquoteIdent(c.toks[it.alias].text))] = true
			}
			for j := it.a; j < it.b; j++ {
				inFrom[j] = true
			}
		}
	}
	for _, b := range c.blocks { // a FROM ( … ) is Extract subquery as CTE
		for _, it := range b.from {
			if it.a == open {
				return "", nil, false
			}
		}
	}
	for j := open + 1; j < cl; j++ {
		t := c.toks[j]
		if inFrom[j] || !c.isName(t) || isIPunct(c.toks[j-1], ".") || !isIPunct(c.toks[j+1], ".") {
			continue
		}
		if !defined[strings.ToLower(unquoteIdent(t.text))] {
			return "", nil, false
		}
	}
	name := freeName("sub", c.takenNames())
	body := c.src[c.toks[open+1].pos:c.toks[cl-1].end]
	eds := []sqlEdit{
		{c.toks[open].pos, c.toks[cl].end, "(SELECT * FROM " + name + ")"},
		c.cteDefEdit(open, name+" AS ("+body+")"),
	}
	return "Extract the selected subquery as CTE " + name, eds, true
}
