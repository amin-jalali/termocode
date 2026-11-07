package dbclient

import "strings"

// JoinTable is one table of a JOIN, as the FROM clause names it.
type JoinTable struct {
	Schema, Table, Alias string
}

// JoinColumn is a select item written as qualifier.column.
type JoinColumn struct {
	Qual, Name string
}

// JoinTarget is what a plain SELECT over joined tables reads (DBX-55).
type JoinTarget struct {
	Tables []JoinTable
	Quals  []JoinColumn // the qualified select items, in order
	Bare   []string     // the select items without a qualifier
}

// TableOf is the index in Tables that qual (an alias or a table name)
// names; -1 when none or more than one.
func (j JoinTarget) TableOf(qual string) int {
	found := -1
	for i, t := range j.Tables {
		if strings.EqualFold(t.Alias, qual) || t.Alias == "" && strings.EqualFold(t.Table, qual) {
			if found >= 0 {
				return -1
			}
			found = i
		}
	}
	return found
}

// joinStart are the words that start the next table of a FROM clause.
var joinStart = map[string]bool{"JOIN": true, "INNER": true, "LEFT": true, "RIGHT": true, "FULL": true, "CROSS": true, "OUTER": true}

// joinTail are the words that make the rows something other than joined
// table rows (JOIN itself is fine here).
var joinTail = map[string]bool{"UNION": true, "INTERSECT": true, "EXCEPT": true, "GROUP": true, "HAVING": true, "WINDOW": true, "INTO": true, "NATURAL": true, "LATERAL": true}

// JoinSelect reads a SELECT over two or more joined tables whose cells can
// be edited one table at a time: columns named as they are in their table
// (bare or qualified, or *), tables joined with JOIN … ON / USING or a
// comma, and after them only WHERE / ORDER BY / LIMIT / OFFSET / FETCH /
// FOR. No subquery in FROM, no NATURAL or LATERAL join, no DISTINCT or
// GROUP BY. Anything else returns a reason in plain words.
func JoinSelect(sql, dialect string) (JoinTarget, string) {
	const notJoin = "not a plain SELECT from joined tables"
	toks, ok := selectTokens(sql, normDriver(dialect))
	if !ok {
		return JoinTarget{}, notJoin
	}
	for len(toks) > 0 && toks[len(toks)-1].text == ";" && toks[len(toks)-1].kind == tokPunct {
		toks = toks[:len(toks)-1]
	}
	p := &selParser{toks: toks, pg: normDriver(dialect) == "postgres"}
	if !p.word("SELECT") {
		return JoinTarget{}, notJoin
	}
	p.word("ALL")
	if p.peekWord("DISTINCT") {
		return JoinTarget{}, "DISTINCT rows cannot be edited"
	}
	var out JoinTarget
	for {
		if !p.punct("*") {
			name, ok := p.ident()
			if !ok {
				return JoinTarget{}, "a column is an expression: name the table's columns to edit"
			}
			if p.punct(".") {
				if !p.punct("*") {
					col, ok := p.ident()
					if !ok {
						return JoinTarget{}, notJoin
					}
					out.Quals = append(out.Quals, JoinColumn{Qual: name, Name: col})
				}
			} else {
				out.Bare = append(out.Bare, name)
			}
		}
		if p.punct(",") {
			continue
		}
		if p.word("FROM") {
			break
		}
		return JoinTarget{}, "a column is renamed or computed: name the table's columns to edit"
	}
	t, ok := p.joinTable()
	if !ok {
		return JoinTarget{}, notJoin
	}
	out.Tables = append(out.Tables, t)
	for {
		if p.punct(",") {
			if t, ok = p.joinTable(); !ok {
				return JoinTarget{}, notJoin
			}
			out.Tables = append(out.Tables, t)
			continue
		}
		if !p.peekJoinStart() {
			break
		}
		// [INNER | CROSS | LEFT | RIGHT | FULL] [OUTER] JOIN
		for p.peekJoinStart() && !p.peekWord("JOIN") {
			p.i++
		}
		if !p.word("JOIN") {
			return JoinTarget{}, notJoin
		}
		if t, ok = p.joinTable(); !ok {
			return JoinTarget{}, notJoin
		}
		out.Tables = append(out.Tables, t)
		if !p.skipJoinCondition() {
			return JoinTarget{}, notJoin
		}
	}
	if len(out.Tables) < 2 {
		return JoinTarget{}, "the query reads one table"
	}
	if p.i < len(p.toks) {
		t := p.toks[p.i]
		if t.kind != tokWord || !selFollow[strings.ToUpper(t.text)] {
			return JoinTarget{}, notJoin
		}
	}
	depth := 0
	for _, t := range p.toks[p.i:] {
		switch {
		case t.kind == tokPunct && t.text == "(":
			depth++
		case t.kind == tokPunct && t.text == ")":
			depth--
		case t.kind == tokPunct && t.text == ";":
			return JoinTarget{}, notJoin
		case depth == 0 && t.kind == tokWord && (joinTail[strings.ToUpper(t.text)] || strings.EqualFold(t.text, "JOIN")):
			return JoinTarget{}, notJoin
		}
	}
	for _, q := range out.Quals {
		if out.TableOf(q.Qual) < 0 {
			return JoinTarget{}, "a column names a table that is not in FROM"
		}
	}
	return out, ""
}

// peekJoinStart: the next word starts a JOIN.
func (p *selParser) peekJoinStart() bool {
	return p.i < len(p.toks) && p.toks[p.i].kind == tokWord && joinStart[strings.ToUpper(p.toks[p.i].text)]
}

// joinTable reads "[schema.]table [[AS] alias]".
func (p *selParser) joinTable() (JoinTable, bool) {
	if p.peekPunct("(") {
		return JoinTable{}, false // a subquery
	}
	first, ok := p.ident()
	if !ok || joinStart[strings.ToUpper(first)] || joinTail[strings.ToUpper(first)] {
		return JoinTable{}, false
	}
	t := JoinTable{Table: first}
	if p.punct(".") {
		second, ok := p.ident()
		if !ok || p.peekPunct(".") {
			return JoinTable{}, false
		}
		t = JoinTable{Schema: first, Table: second}
	}
	if p.word("AS") {
		a, ok := p.ident()
		if !ok {
			return JoinTable{}, false
		}
		t.Alias = a
		return t, true
	}
	if p.i < len(p.toks) && (p.toks[p.i].kind == tokWord || p.toks[p.i].kind == tokQuoted) {
		up := strings.ToUpper(p.toks[p.i].text)
		if p.toks[p.i].kind == tokQuoted || !selFollow[up] && !joinStart[up] && !joinTail[up] && up != "ON" && up != "USING" {
			a, ok := p.ident()
			if !ok {
				return JoinTable{}, false
			}
			t.Alias = a
		}
	}
	return t, true
}

// skipJoinCondition steps over "ON <condition>" or "USING (…)" up to the
// next join, comma or the words after FROM. false: something else.
func (p *selParser) skipJoinCondition() bool {
	switch {
	case p.word("USING"):
		if !p.punct("(") {
			return false
		}
		for p.i < len(p.toks) && !p.peekPunct(")") {
			p.i++
		}
		return p.punct(")")
	case p.word("ON"):
		depth := 0
		start := p.i
		for p.i < len(p.toks) {
			t := p.toks[p.i]
			switch {
			case t.kind == tokPunct && t.text == "(":
				depth++
			case t.kind == tokPunct && t.text == ")":
				depth--
				if depth < 0 {
					return false
				}
			case depth == 0 && t.kind == tokPunct && (t.text == "," || t.text == ";"):
				return p.i > start
			case depth == 0 && t.kind == tokWord:
				up := strings.ToUpper(t.text)
				fn := p.i+1 < len(p.toks) && p.toks[p.i+1].kind == tokPunct && p.toks[p.i+1].text == "("
				if !fn && (joinStart[up] || selFollow[up]) {
					return p.i > start
				}
				if joinTail[up] {
					return false
				}
			}
			p.i++
		}
		return p.i > start && depth == 0
	}
	// a CROSS JOIN has no condition
	return true
}
