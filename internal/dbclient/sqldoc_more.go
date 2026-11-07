package dbclient

import "strings"

// SQLCall is the routine call around a cursor (DBX-30 parameter info):
// its name and the 0-based argument the cursor is in.
type SQLCall struct {
	Schema, Routine string
	Arg             int
}

// docNotCallBefore are the words before a name whose "(" opens a column
// list, not a call: INSERT INTO t (a, b), CREATE TABLE t (…), REFERENCES t (id).
// CREATE INDEX i ON t (…) is caught on its own: ON f(x) in a JOIN is a call.
var docNotCallBefore = []string{"INTO", "TABLE", "REFERENCES", "VIEW", "INDEX"}

// SQLCallAt finds the innermost routine call whose argument list holds
// byte offset off of sql. A plain paren inside the arguments is part of
// one argument; IN (…), VALUES (…) and column lists are not calls.
func SQLCallAt(sql string, off int, driver string) (SQLCall, bool) {
	driver = normDriver(driver)
	pg := driver == "postgres"
	toks := docTokens(sql, driver)
	n := 0
	for n < len(toks) && toks[n].start < off {
		n++
	}
	depth, arg := 0, 0
	for i := n - 1; i >= 0; i-- {
		t := toks[i]
		if t.kind != tokPunct {
			continue
		}
		switch t.text {
		case ";":
			return SQLCall{}, false
		case ")":
			depth++
		case ",":
			if depth == 0 {
				arg++
			}
		case "(":
			if depth > 0 {
				depth--
				continue
			}
			if call, ok := docCallBefore(toks, i, pg); ok {
				call.Arg = arg
				return call, true
			}
			// Not a call: this paren is part of one argument further out.
			arg = 0
		}
	}
	return SQLCall{}, false
}

// docCallBefore reads the routine name before the "(" at i.
func docCallBefore(toks []docTok, i int, pg bool) (SQLCall, bool) {
	if i == 0 {
		return SQLCall{}, false
	}
	name, ok := docName(toks[i-1], pg)
	if !ok {
		// REPLACE, LEFT and RIGHT are keywords too, but before "(" they
		// are the functions.
		_, builtin := docBuiltins[strings.ToLower(toks[i-1].text)]
		if toks[i-1].kind != tokWord || !builtin {
			return SQLCall{}, false
		}
		return SQLCall{Routine: strings.ToLower(toks[i-1].text)}, true
	}
	call := SQLCall{Routine: name}
	first := i - 1
	if docPunct(toks, i-2, ".") && i >= 3 {
		if s, ok := docName(toks[i-3], pg); ok {
			call.Schema = s
			first = i - 3
		}
	}
	for _, w := range docNotCallBefore {
		if docWord(toks, first-1, w) {
			return SQLCall{}, false
		}
	}
	if docWord(toks, first-1, "ON") && (docWord(toks, first-2, "INDEX") || docWord(toks, first-3, "INDEX")) {
		return SQLCall{}, false
	}
	return call, true
}

// JoinSide is one table of a join condition with the columns it compares.
type JoinSide struct {
	Schema, Table string
	Columns       []string
}

// JoinHint is the join of one JOIN clause (DBX-30 JOIN cardinality hint):
// Offset is the byte offset of the joined table's name, Joined that table
// and Other the one table its condition compares it with.
type JoinHint struct {
	Offset int
	Joined JoinSide
	Other  JoinSide
}

// docJoinEnd are the words that end an ON condition.
var docJoinEnd = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`JOIN INNER LEFT RIGHT FULL CROSS NATURAL WHERE GROUP ORDER HAVING LIMIT OFFSET FETCH
		UNION INTERSECT EXCEPT WINDOW RETURNING`) {
		docJoinEnd[w] = true
	}
}

// docRefIndex is the index of the table ref name points to: an alias
// first, then a table name; -1 when none.
func docRefIndex(refs []docTableRef, name string) int {
	for i, r := range refs {
		if r.alias != "" && strings.EqualFold(r.alias, name) {
			return i
		}
	}
	for i, r := range refs {
		if strings.EqualFold(r.table, name) {
			return i
		}
	}
	return -1
}

// JoinHints lists the JOIN clauses of sql whose condition compares the
// joined table with exactly one other table by equality: ON a.x = b.y
// [AND …] or USING (cols) (the table before the JOIN). A condition with
// OR, a bare column or a third table gets no hint.
func JoinHints(sql, driver string) []JoinHint {
	driver = normDriver(driver)
	pg := driver == "postgres"
	toks := docTokens(sql, driver)
	refs := docTableRefs(toks, pg)
	var out []JoinHint
	for ri, r := range refs {
		if !docWord(toks, r.from-1, "JOIN") {
			continue
		}
		k := r.to
		if r.aliasTok >= 0 {
			k = r.aliasTok + 1
		}
		h := JoinHint{Offset: toks[r.from].start, Joined: JoinSide{Schema: r.schema, Table: r.table}}
		switch {
		case docWord(toks, k, "USING") && docPunct(toks, k+1, "(") && ri > 0:
			o := refs[ri-1]
			h.Other = JoinSide{Schema: o.schema, Table: o.table}
			for j := k + 2; j < len(toks) && !docPunct(toks, j, ")"); j++ {
				if c, ok := docName(toks[j], pg); ok {
					h.Joined.Columns = append(h.Joined.Columns, c)
					h.Other.Columns = append(h.Other.Columns, c)
				}
			}
		case docWord(toks, k, "ON"):
			if !docJoinPairs(toks, refs, ri, k+1, pg, &h) {
				continue
			}
		default:
			continue
		}
		if len(h.Joined.Columns) > 0 {
			out = append(out, h)
		}
	}
	return out
}

// docJoinPairs reads the equalities of the ON condition from i into h;
// false when the condition is not a plain AND of a.x = b.y between the
// joined ref ri and one other ref.
func docJoinPairs(toks []docTok, refs []docTableRef, ri, i int, pg bool, h *JoinHint) bool {
	other := -1
	side := func(j int) (ref int, col string, next int, ok bool) {
		names, k := docChain(toks, j, pg)
		if len(names) < 2 || len(names) > 3 {
			return -1, "", k, false
		}
		ref = docRefIndex(refs, names[len(names)-2])
		return ref, names[len(names)-1], k, ref >= 0
	}
	for depth := 0; i < len(toks); {
		t := toks[i]
		if t.kind == tokWord && depth == 0 && docJoinEnd[strings.ToUpper(t.text)] {
			break
		}
		if t.kind == tokPunct {
			switch t.text {
			case "(":
				depth++
			case ")":
				depth--
			case ";":
				depth = -1
			}
			if depth < 0 {
				break
			}
			i++
			continue
		}
		if docWord(toks, i, "OR") {
			return false
		}
		if docWord(toks, i, "AND") {
			i++
			continue
		}
		a, ac, j, ok := side(i)
		if !ok || !docPunct(toks, j, "=") {
			i = max(j, i+1)
			continue
		}
		b, bc, k, ok := side(j + 1)
		if !ok {
			i = max(k, j+1)
			continue
		}
		if a != ri {
			a, ac, b, bc = b, bc, a, ac
		}
		if a != ri || b == ri || other >= 0 && b != other {
			return false
		}
		other = b
		h.Joined.Columns = append(h.Joined.Columns, ac)
		h.Other.Columns = append(h.Other.Columns, bc)
		i = k
	}
	if other < 0 {
		return false
	}
	h.Other.Schema, h.Other.Table = refs[other].schema, refs[other].table
	return true
}

// docBuiltins are the parameters of common built-in functions; "…" marks
// a list that goes on. The keys are lower case; drivers lists the dialects
// a function is limited to ("" = all).
var docBuiltins = map[string]struct {
	params  []string
	drivers string
}{
	"coalesce":           {[]string{"value", "…"}, ""},
	"nullif":             {[]string{"value1", "value2"}, ""},
	"greatest":           {[]string{"value", "…"}, ""},
	"least":              {[]string{"value", "…"}, ""},
	"substr":             {[]string{"string", "start", "length"}, ""},
	"substring":          {[]string{"string", "start", "length"}, ""},
	"replace":            {[]string{"string", "from", "to"}, ""},
	"round":              {[]string{"number", "digits"}, ""},
	"trunc":              {[]string{"number", "digits"}, "postgres"},
	"truncate":           {[]string{"number", "digits"}, "mysql"},
	"abs":                {[]string{"number"}, ""},
	"length":             {[]string{"string"}, ""},
	"char_length":        {[]string{"string"}, ""},
	"lower":              {[]string{"string"}, ""},
	"upper":              {[]string{"string"}, ""},
	"trim":               {[]string{"string"}, ""},
	"ltrim":              {[]string{"string"}, ""},
	"rtrim":              {[]string{"string"}, ""},
	"left":               {[]string{"string", "count"}, ""},
	"right":              {[]string{"string", "count"}, ""},
	"lpad":               {[]string{"string", "length", "fill"}, ""},
	"rpad":               {[]string{"string", "length", "fill"}, ""},
	"concat":             {[]string{"value", "…"}, ""},
	"concat_ws":          {[]string{"separator", "value", "…"}, ""},
	"count":              {[]string{"expression"}, ""},
	"sum":                {[]string{"expression"}, ""},
	"avg":                {[]string{"expression"}, ""},
	"min":                {[]string{"expression"}, ""},
	"max":                {[]string{"expression"}, ""},
	"now":                {nil, ""},
	"date_trunc":         {[]string{"field", "source"}, "postgres"},
	"to_char":            {[]string{"value", "format"}, "postgres"},
	"to_date":            {[]string{"text", "format"}, "postgres"},
	"to_timestamp":       {[]string{"text", "format"}, "postgres"},
	"string_agg":         {[]string{"value", "delimiter"}, "postgres"},
	"array_agg":          {[]string{"value"}, "postgres"},
	"generate_series":    {[]string{"start", "stop", "step"}, "postgres"},
	"jsonb_build_object": {[]string{"key", "value", "…"}, "postgres"},
	"json_build_object":  {[]string{"key", "value", "…"}, "postgres"},
	"ifnull":             {[]string{"value", "fallback"}, "mysql sqlite"},
	"if":                 {[]string{"condition", "then", "else"}, "mysql"},
	"date_format":        {[]string{"date", "format"}, "mysql"},
	"date_add":           {[]string{"date", "interval"}, "mysql"},
	"date_sub":           {[]string{"date", "interval"}, "mysql"},
	"datediff":           {[]string{"end", "start"}, "mysql"},
	"group_concat":       {[]string{"expression"}, "mysql sqlite"},
	"strftime":           {[]string{"format", "time", "modifier", "…"}, "sqlite"},
	"json_extract":       {[]string{"json", "path", "…"}, "mysql sqlite"},
	"instr":              {[]string{"string", "substring"}, "mysql sqlite"},
}

// BuiltinParams names the parameters of the built-in function name for
// driver; false when it is not one the list knows.
func BuiltinParams(driver, name string) ([]string, bool) {
	b, ok := docBuiltins[strings.ToLower(name)]
	if !ok {
		return nil, false
	}
	if b.drivers != "" {
		d := normDriver(driver)
		if d == "postgresql" {
			d = "postgres"
		}
		found := false
		for _, w := range strings.Fields(b.drivers) {
			if w == d {
				found = true
			}
		}
		if !found {
			return nil, false
		}
	}
	return append([]string(nil), b.params...), true
}
