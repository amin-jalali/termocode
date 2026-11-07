package dbclient

import "strings"

// SQLRef is the database object a name in SQL text points to: what the
// quick documentation popup shows. Kind is "table", "column", "routine"
// or "schema". Aliases are resolved to their table; a bare column gets
// its table only when the statement reads exactly one table.
type SQLRef struct {
	Kind                           string
	Schema, Table, Column, Routine string
}

// InsertHint names the column a value of INSERT ... VALUES goes to; Offset
// is the byte offset of the value's first character.
type InsertHint struct {
	Offset int
	Column string
}

type docTok struct {
	kind       selTokKind // tokWord, tokQuoted, tokString, tokPunct
	text       string
	start, end int
}

// docTokens splits sql into tokens with their byte range, without comments.
// An unclosed quote or comment ends the list there.
func docTokens(sql, driver string) []docTok {
	var out []docTok
	mysql := driver == "mysql"
	add := func(kind selTokKind, text string, i, j int) { out = append(out, docTok{kind, text, i, j}) }
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
				return out
			}
			i += end + 4
		case c == '\'' || c == '"' && mysql:
			j, ok := selQuoteEnd(sql, i, c, mysql)
			if !ok {
				return out
			}
			add(tokString, sql[i:j], i, j)
			i = j
		case c == '"' || c == '`':
			j, ok := selQuoteEnd(sql, i, c, false)
			if !ok {
				return out
			}
			add(tokQuoted, strings.ReplaceAll(sql[i+1:j-1], string([]byte{c, c}), string(c)), i, j)
			i = j
		case c == '[' && driver == "sqlite":
			end := strings.IndexByte(sql[i:], ']')
			if end < 0 {
				return out
			}
			add(tokQuoted, sql[i+1:i+end], i, i+end+1)
			i += end + 1
		case c == '$' && !mysql && dollarTagAt(sql, i) != "":
			tag := dollarTagAt(sql, i)
			end := strings.Index(sql[i+len(tag):], tag)
			if end < 0 {
				return out
			}
			j := i + len(tag) + end + len(tag)
			add(tokString, sql[i:j], i, j)
			i = j
		case c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80:
			j := i
			for j < len(sql) {
				d := sql[j]
				if d == '_' || d == '$' || d >= 'a' && d <= 'z' || d >= 'A' && d <= 'Z' || d >= '0' && d <= '9' || d >= 0x80 {
					j++
					continue
				}
				break
			}
			add(tokWord, sql[i:j], i, j)
			i = j
		default:
			add(tokPunct, string(c), i, i+1)
			i++
		}
	}
	return out
}

// dollarTagAt is the $tag$ opening a dollar quote at i, or "".
func dollarTagAt(sql string, i int) string {
	for j := i + 1; j < len(sql); j++ {
		d := sql[j]
		if d == '$' {
			return sql[i : j+1]
		}
		if !(d == '_' || d >= 'a' && d <= 'z' || d >= 'A' && d <= 'Z' || j > i+1 && d >= '0' && d <= '9') {
			return ""
		}
	}
	return ""
}

var docKeywords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`SELECT FROM WHERE JOIN INNER LEFT RIGHT FULL OUTER CROSS NATURAL LATERAL ON USING AS AND OR NOT IN IS NULL
		LIKE ILIKE BETWEEN CASE WHEN THEN ELSE END GROUP BY ORDER HAVING LIMIT OFFSET FETCH FOR UNION INTERSECT EXCEPT ALL
		DISTINCT INSERT INTO VALUES UPDATE SET DELETE RETURNING WITH TRUE FALSE ASC DESC EXISTS TABLE CREATE ALTER DROP
		DEFAULT PRIMARY KEY REFERENCES IGNORE REPLACE CONFLICT DO NOTHING WINDOW OVER PARTITION ROWS ONLY`) {
		docKeywords[w] = true
	}
}

// docName reports whether t can be a name, and the name as the server
// folds it.
func docName(t docTok, pg bool) (string, bool) {
	switch t.kind {
	case tokQuoted:
		return t.text, true
	case tokWord:
		c := t.text[0]
		if c >= '0' && c <= '9' || c == '$' || docKeywords[strings.ToUpper(t.text)] {
			return "", false
		}
		if pg {
			return strings.ToLower(t.text), true
		}
		return t.text, true
	}
	return "", false
}

func docPunct(toks []docTok, i int, p string) bool {
	return i >= 0 && i < len(toks) && toks[i].kind == tokPunct && toks[i].text == p
}

func docWord(toks []docTok, i int, w string) bool {
	return i >= 0 && i < len(toks) && toks[i].kind == tokWord && strings.EqualFold(toks[i].text, w)
}

// docChain reads name(.name)* from i: the names and the index after.
func docChain(toks []docTok, i int, pg bool) ([]string, int) {
	var names []string
	for i < len(toks) {
		n, ok := docName(toks[i], pg)
		if !ok {
			break
		}
		names = append(names, n)
		i++
		if !docPunct(toks, i, ".") {
			break
		}
		i++
	}
	return names, i
}

type docTableRef struct {
	schema, table, alias string
	from, to             int // token range of the name chain
	aliasTok             int // -1 when there is no alias
}

// docTableRefs lists the tables a statement reads or writes, with aliases.
func docTableRefs(toks []docTok, pg bool) []docTableRef {
	var refs []docTableRef
	for i := 0; i < len(toks); i++ {
		if !(docWord(toks, i, "FROM") || docWord(toks, i, "JOIN") || docWord(toks, i, "UPDATE") || docWord(toks, i, "INTO") || docWord(toks, i, "TABLE")) {
			continue
		}
		into := docWord(toks, i, "INTO") || docWord(toks, i, "TABLE")
		j := i + 1
		for {
			names, k := docChain(toks, j, pg)
			if len(names) == 0 || !into && docPunct(toks, k, "(") || len(names) > 3 {
				break
			}
			r := docTableRef{table: names[len(names)-1], from: j, to: k, aliasTok: -1}
			if len(names) >= 2 {
				r.schema = names[len(names)-2]
			}
			if docWord(toks, k, "AS") {
				k++
			}
			if k < len(toks) {
				if a, ok := docName(toks[k], pg); ok {
					r.alias, r.aliasTok = a, k
					k++
				}
			}
			refs = append(refs, r)
			if !docPunct(toks, k, ",") {
				break
			}
			j = k + 1
		}
	}
	return refs
}

// SQLRefAt resolves the name at byte offset off of sql (the cursor may sit
// just after the name). false: a keyword, number, string, comment or
// nothing there.
func SQLRefAt(sql string, off int, driver string) (SQLRef, bool) {
	driver = normDriver(driver)
	pg := driver == "postgres"
	toks := docTokens(sql, driver)
	cur := -1
	for i, t := range toks {
		if t.start <= off && off < t.end {
			// A caret right after a word belongs to that word, not to
			// the punctuation that starts there.
			if cur < 0 || t.kind == tokWord || t.kind == tokQuoted {
				cur = i
			}
			break
		}
		if t.end == off && (t.kind == tokWord || t.kind == tokQuoted) {
			cur = i
		}
	}
	if cur < 0 {
		return SQLRef{}, false
	}
	if _, ok := docName(toks[cur], pg); !ok {
		return SQLRef{}, false
	}
	refs := docTableRefs(toks, pg)
	asTable := func(r docTableRef) SQLRef { return SQLRef{Kind: "table", Schema: r.schema, Table: r.table} }
	for _, r := range refs {
		if cur == r.aliasTok || cur >= r.from && cur < r.to {
			return asTable(r), true
		}
	}
	// The whole dotted chain around the cursor.
	start := cur
	for start >= 2 && docPunct(toks, start-1, ".") {
		if _, ok := docName(toks[start-2], pg); !ok {
			break
		}
		start -= 2
	}
	names, end := docChain(toks, start, pg)
	k := (cur - start) / 2
	if k >= len(names) {
		return SQLRef{}, false
	}
	lookup := func(name string) (docTableRef, bool) {
		for _, r := range refs {
			if r.alias != "" && strings.EqualFold(r.alias, name) {
				return r, true
			}
		}
		for _, r := range refs {
			if strings.EqualFold(r.table, name) {
				return r, true
			}
		}
		return docTableRef{}, false
	}
	last := len(names) - 1
	if k == last && docPunct(toks, end, "(") {
		ref := SQLRef{Kind: "routine", Routine: names[last]}
		if last >= 1 {
			ref.Schema = names[last-1]
		}
		return ref, true
	}
	switch {
	case len(names) == 1:
		if r, ok := lookup(names[0]); ok {
			return asTable(r), true
		}
		ref := SQLRef{Kind: "column", Column: names[0]}
		if len(refs) == 1 {
			ref.Schema, ref.Table = refs[0].schema, refs[0].table
		}
		return ref, true
	case k == last:
		ref := SQLRef{Kind: "column", Column: names[last], Table: names[last-1]}
		if r, ok := lookup(names[last-1]); ok && last == 1 {
			ref.Schema, ref.Table = r.schema, r.table
		} else if last >= 2 {
			ref.Schema = names[last-2]
		}
		return ref, true
	case k == last-1:
		if r, ok := lookup(names[k]); ok && k == 0 {
			return asTable(r), true
		}
		ref := SQLRef{Kind: "table", Table: names[k]}
		if k >= 1 {
			ref.Schema = names[k-1]
		}
		return ref, true
	default:
		return SQLRef{Kind: "schema", Schema: names[k]}, true
	}
}

// InsertValueHints names the column of each value in the VALUES rows of
// an INSERT with a column list. Values past the end of the list get no
// hint; an INSERT without a column list, or from a SELECT, gets none.
func InsertValueHints(sql, driver string) []InsertHint {
	driver = normDriver(driver)
	pg := driver == "postgres"
	toks := docTokens(sql, driver)
	i := 0
	for i < len(toks) && !docWord(toks, i, "INSERT") {
		i++
	}
	if i == len(toks) {
		return nil
	}
	for stop := i + 4; i < len(toks) && i < stop && !docWord(toks, i, "INTO"); i++ {
	}
	if !docWord(toks, i, "INTO") {
		return nil
	}
	names, i := docChain(toks, i+1, pg)
	if len(names) == 0 || !docPunct(toks, i, "(") {
		return nil
	}
	var cols []string
	for i++; ; i++ {
		if i >= len(toks) {
			return nil
		}
		n, ok := docName(toks[i], pg)
		if !ok {
			return nil
		}
		cols = append(cols, n)
		i++
		if docPunct(toks, i, ")") {
			break
		}
		if !docPunct(toks, i, ",") {
			return nil
		}
	}
	i++
	if !docWord(toks, i, "VALUES") {
		return nil
	}
	var out []InsertHint
	for i++; docPunct(toks, i, "("); {
		col, depth, fresh := 0, 0, true
		for i++; i < len(toks); i++ {
			t := toks[i]
			if t.kind == tokPunct {
				switch t.text {
				case "(":
					depth++
				case ")":
					depth--
				case ",":
					if depth == 0 {
						col++
						fresh = true
						continue
					}
				}
				if depth < 0 {
					break
				}
			}
			if fresh && col < len(cols) {
				out = append(out, InsertHint{Offset: t.start, Column: cols[col]})
			}
			fresh = false
		}
		i++
		if !docPunct(toks, i, ",") {
			break
		}
		i++
	}
	return out
}
