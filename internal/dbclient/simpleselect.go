package dbclient

import "strings"

// SelectTarget is the one table a plain SELECT reads.
type SelectTarget struct {
	Schema, Table string
}

// SimpleSelect reports the table of a plain SELECT whose rows can be
// edited: one table (optionally schema-qualified, with an alias), columns
// named as they are in the table (no expression, no renamed column, no
// DISTINCT), and after the table only WHERE / ORDER BY / LIMIT / OFFSET /
// FETCH / FOR. Anything else returns a reason in plain words. Unquoted
// names fold to lower case on PostgreSQL, as the server does.
func SimpleSelect(sql, dialect string) (SelectTarget, string) {
	const notPlain = "not a plain SELECT from one table"
	toks, ok := selectTokens(sql, normDriver(dialect))
	if !ok {
		return SelectTarget{}, notPlain
	}
	for len(toks) > 0 && toks[len(toks)-1].text == ";" && toks[len(toks)-1].kind == tokPunct {
		toks = toks[:len(toks)-1]
	}
	p := &selParser{toks: toks, pg: normDriver(dialect) == "postgres"}
	if !p.word("SELECT") {
		return SelectTarget{}, notPlain
	}
	p.word("ALL")
	if p.peekWord("DISTINCT") {
		return SelectTarget{}, "DISTINCT rows cannot be edited"
	}
	var quals []string
	for {
		switch {
		case p.punct("*"):
		default:
			name, ok := p.ident()
			if !ok {
				return SelectTarget{}, "a column is an expression: name the table's columns to edit"
			}
			if p.punct(".") {
				if !p.punct("*") {
					if _, ok := p.ident(); !ok {
						return SelectTarget{}, notPlain
					}
				}
				quals = append(quals, name)
			}
		}
		if p.punct(",") {
			continue
		}
		if p.word("FROM") {
			break
		}
		return SelectTarget{}, "a column is renamed or computed: name the table's columns to edit"
	}
	first, ok := p.ident()
	if !ok {
		return SelectTarget{}, notPlain
	}
	target := SelectTarget{Table: first}
	if p.punct(".") {
		second, ok := p.ident()
		if !ok {
			return SelectTarget{}, notPlain
		}
		target = SelectTarget{Schema: first, Table: second}
		if p.peekPunct(".") {
			return SelectTarget{}, notPlain
		}
	}
	alias := ""
	if p.word("AS") {
		a, ok := p.ident()
		if !ok {
			return SelectTarget{}, notPlain
		}
		alias = a
	} else if p.i < len(p.toks) && !selFollow[strings.ToUpper(p.toks[p.i].text)] && (p.toks[p.i].kind == tokWord || p.toks[p.i].kind == tokQuoted) {
		alias, _ = p.ident()
	}
	if p.i < len(p.toks) {
		t := p.toks[p.i]
		if t.kind != tokWord || !selFollow[strings.ToUpper(t.text)] {
			return SelectTarget{}, "the query reads more than one table"
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
			return SelectTarget{}, notPlain
		case depth == 0 && t.kind == tokWord && selRefuse[strings.ToUpper(t.text)]:
			return SelectTarget{}, notPlain
		}
	}
	for _, q := range quals {
		if !strings.EqualFold(q, target.Table) && !strings.EqualFold(q, alias) {
			return SelectTarget{}, "a column comes from another table"
		}
	}
	return target, ""
}

// selFollow are the words that may follow the table.
var selFollow = map[string]bool{"WHERE": true, "ORDER": true, "LIMIT": true, "OFFSET": true, "FETCH": true, "FOR": true}

// selRefuse are words that make the rows something other than table rows.
var selRefuse = map[string]bool{"JOIN": true, "UNION": true, "INTERSECT": true, "EXCEPT": true, "GROUP": true, "HAVING": true, "WINDOW": true, "INTO": true}

type selTokKind int

const (
	tokWord selTokKind = iota
	tokQuoted
	tokString
	tokPunct
)

type selTok struct {
	kind selTokKind
	text string
}

type selParser struct {
	toks []selTok
	i    int
	pg   bool
}

func (p *selParser) word(w string) bool {
	if p.peekWord(w) {
		p.i++
		return true
	}
	return false
}

func (p *selParser) peekWord(w string) bool {
	return p.i < len(p.toks) && p.toks[p.i].kind == tokWord && strings.EqualFold(p.toks[p.i].text, w)
}

func (p *selParser) punct(s string) bool {
	if p.peekPunct(s) {
		p.i++
		return true
	}
	return false
}

func (p *selParser) peekPunct(s string) bool {
	return p.i < len(p.toks) && p.toks[p.i].kind == tokPunct && p.toks[p.i].text == s
}

// ident takes a name: a quoted one as written, a bare one (not a keyword
// that ends the list) folded like the server folds it.
func (p *selParser) ident() (string, bool) {
	if p.i >= len(p.toks) {
		return "", false
	}
	t := p.toks[p.i]
	switch t.kind {
	case tokQuoted:
		p.i++
		return t.text, true
	case tokWord:
		up := strings.ToUpper(t.text)
		if up == "FROM" || up == "SELECT" || up == "AS" || selFollow[up] || selRefuse[up] || t.text[0] >= '0' && t.text[0] <= '9' {
			return "", false
		}
		if p.i+1 < len(p.toks) && p.toks[p.i+1].kind == tokPunct && p.toks[p.i+1].text == "(" {
			return "", false // a function call
		}
		p.i++
		if p.pg {
			return strings.ToLower(t.text), true
		}
		return t.text, true
	}
	return "", false
}

// selectTokens splits sql into words, quoted names, strings and
// punctuation, without comments. false: something this reader does not
// know (a dollar quote, an open quote).
func selectTokens(sql, driver string) ([]selTok, bool) {
	var out []selTok
	mysql := driver == "mysql"
	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-', mysql && c == '#':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				return nil, false
			}
			i += end + 4
		case c == '\'' || c == '"' && mysql:
			j, ok := selQuoteEnd(sql, i, c, mysql)
			if !ok {
				return nil, false
			}
			out = append(out, selTok{kind: tokString, text: sql[i:j]})
			i = j
		case c == '"' || c == '`':
			j, ok := selQuoteEnd(sql, i, c, false)
			if !ok {
				return nil, false
			}
			name := sql[i+1 : j-1]
			out = append(out, selTok{kind: tokQuoted, text: strings.ReplaceAll(name, string([]byte{c, c}), string(c))})
			i = j
		case c == '[' && driver == "sqlite":
			end := strings.IndexByte(sql[i:], ']')
			if end < 0 {
				return nil, false
			}
			out = append(out, selTok{kind: tokQuoted, text: sql[i+1 : i+end]})
			i += end + 1
		case c == '$' && !mysql:
			return nil, false // dollar quotes and parameters: not a plain select
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80 || c == '$':
			j := i
			for j < len(sql) {
				d := sql[j]
				if d == '_' || d == '$' || d >= 'a' && d <= 'z' || d >= 'A' && d <= 'Z' || d >= '0' && d <= '9' || d >= 0x80 {
					j++
					continue
				}
				break
			}
			out = append(out, selTok{kind: tokWord, text: sql[i:j]})
			i = j
		default:
			out = append(out, selTok{kind: tokPunct, text: string(c)})
			i++
		}
	}
	return out, true
}

// selQuoteEnd is the index after the quote that closes the one at i.
func selQuoteEnd(sql string, i int, q byte, backslash bool) (int, bool) {
	for j := i + 1; j < len(sql); j++ {
		switch sql[j] {
		case '\\':
			if backslash {
				j++
			}
		case q:
			if j+1 < len(sql) && sql[j+1] == q {
				j++
				continue
			}
			return j + 1, true
		}
	}
	return 0, false
}
