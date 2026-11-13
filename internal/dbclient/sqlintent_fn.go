package dbclient

import (
	"strconv"
	"strings"
)

// Extract subquery as table function (PostgreSQL, DBX-42): the FROM
// subquery at the cursor becomes
//
//	CREATE FUNCTION get_x(p_total integer)
//	RETURNS SETOF orders            -- or RETURNS TABLE (id bigint, …)
//	LANGUAGE sql STABLE
//	AS $$ … $$;
//
// and the FROM reads get_x(100) x. Whole numbers become parameters;
// strings stay, since their column may be an enum or a date. The row
// type needs SELECT * over one table, or the type of every column.

func (c *intentCtx) tableFunction() (string, []sqlEdit, bool) {
	if c.dialect != DialectPostgres {
		return "", nil, false
	}
	k := c.tokAt()
	var best *fromItem
	for _, b := range c.blocks {
		for i := range b.from {
			it := &b.from[i]
			if !it.sub || k < it.a || k >= it.b {
				continue
			}
			if best == nil || it.b-it.a < best.b-best.a {
				best = it
			}
		}
	}
	if best == nil || best.alias < 0 || best.colList || !isIWord(c.toks[best.a+1], "select") {
		return "", nil, false
	}
	open, cl := best.a, best.b-1
	var ib *qblock
	for _, b := range c.blocks {
		if b.sel == open+1 {
			ib = b
		}
	}
	if ib == nil || ib.end != cl || c.outerRefs(open, cl) {
		return "", nil, false
	}
	// A CTE of the statement is not seen inside a function.
	for _, b := range c.blocks {
		if b.sel <= open || b.sel >= cl {
			continue
		}
		for _, it := range b.from {
			for _, d := range c.ctes {
				if !it.sub && !it.fn && it.b-it.a == 1 && c.sameName(c.toks[it.a], c.toks[d.name]) {
					return "", nil, false
				}
			}
		}
	}
	rows, ok := c.fnRowType(ib)
	if !ok {
		return "", nil, false
	}
	taken := map[string]bool{}
	for _, cols := range c.cols {
		for _, n := range cols {
			taken[strings.ToLower(n)] = true
		}
	}
	for _, cols := range c.opts.Types {
		for n := range cols {
			taken[n] = true
		}
	}
	type param struct{ name, typ, lit string }
	var params []param
	byLit := map[string]string{}
	var bodyEds []sqlEdit
	base := c.toks[open+1].pos
	for j := open + 1; j < cl; j++ {
		t := c.toks[j]
		if t.kind != fkWord || !c.isLiteral(j) {
			continue
		}
		name, seen := byLit[t.text]
		if !seen {
			b := "value"
			prev := c.toks[j-1]
			switch {
			case isIWord(prev, "limit", "offset"):
				b = strings.ToLower(prev.text)
			case prev.kind == fkOp && j >= 2 && c.isName(c.toks[j-2]) && (c.toks[j-2].kind != fkWord || !intentKeyword(c.toks[j-2].text)):
				b = paramName(unquoteIdent(c.toks[j-2].text))
			}
			name = "p_" + strings.TrimPrefix(b, "p_")
			for n := 2; taken[name]; n++ {
				name = "p_" + strings.TrimPrefix(b, "p_") + strconv.Itoa(n)
			}
			taken[name] = true
			byLit[t.text] = name
			typ := "integer"
			if v, err := strconv.ParseInt(t.text, 10, 64); err != nil {
				typ = "numeric"
			} else if v > 2147483647 {
				typ = "bigint"
			}
			params = append(params, param{name, typ, t.text})
		}
		bodyEds = append(bodyEds, sqlEdit{t.pos - base, t.end - base, name})
	}
	body, err := applySQLEdits(c.src[base:c.toks[cl-1].end], bodyEds)
	if err != nil {
		return "", nil, false
	}
	tag := "$$"
	if strings.Contains(body, tag) {
		tag = "$fn$"
		if strings.Contains(body, tag) {
			return "", nil, false
		}
	}
	fn := freeName("get_"+paramName(unquoteIdent(c.toks[best.alias].text)), c.takenNames())
	var decl, args []string
	for _, p := range params {
		decl = append(decl, p.name+" "+p.typ)
		args = append(args, p.lit)
	}
	create := "CREATE FUNCTION " + fn + "(" + strings.Join(decl, ", ") + ")\n" + rows +
		"\nLANGUAGE sql STABLE\nAS " + tag + "\n" + body + "\n" + tag + ";\n\n"
	eds := []sqlEdit{
		{c.toks[open].pos, c.toks[cl].end, fn + "(" + strings.Join(args, ", ") + ")"},
		{c.toks[0].pos, c.toks[0].pos, create},
	}
	return "Extract subquery as table function " + fn, eds, true
}

// fnRowType is the RETURNS clause of the subquery block b.
func (c *intentCtx) fnRowType(b *qblock) (string, bool) {
	if len(b.items) == 1 && c.starItem(b.items[0]) {
		if len(b.from) != 1 || b.from[0].sub || b.from[0].fn {
			return "", false
		}
		it := b.from[0]
		return "RETURNS SETOF " + c.text(it.a, it.b), true
	}
	var cols []string
	for _, s := range b.items {
		var tbl, col itok
		switch n := s.b - s.a; {
		case n == 1 && c.isName(c.toks[s.a]) && len(b.from) == 1 && !b.from[0].sub && !b.from[0].fn:
			tbl, col = c.toks[b.from[0].b-1], c.toks[s.a]
		case n == 3 && c.isName(c.toks[s.a]) && isIPunct(c.toks[s.a+1], ".") && c.isName(c.toks[s.a+2]):
			found := false
			for _, it := range b.from {
				if it.sub || it.fn {
					continue
				}
				if it.alias >= 0 && c.sameName(c.toks[it.alias], c.toks[s.a]) || it.alias < 0 && c.sameName(c.toks[it.b-1], c.toks[s.a]) {
					tbl, found = c.toks[it.b-1], true
				}
			}
			if !found {
				return "", false
			}
			col = c.toks[s.a+2]
		default:
			return "", false
		}
		if col.kind == fkWord && intentKeyword(col.text) {
			return "", false
		}
		typ := c.opts.Types[strings.ToLower(unquoteIdent(tbl.text))][strings.ToLower(unquoteIdent(col.text))]
		if typ == "" {
			return "", false
		}
		out := col.text
		if s.alias >= 0 {
			out = c.toks[s.alias].text
		}
		cols = append(cols, out+" "+typ)
	}
	if len(cols) == 0 {
		return "", false
	}
	return "RETURNS TABLE (" + strings.Join(cols, ", ") + ")", true
}
