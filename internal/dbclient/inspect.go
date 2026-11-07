package dbclient

import (
	"fmt"
	"sort"
	"strings"
)

// SQL inspections (DBX-4): a small, forgiving scan of the console text
// that finds syntax slips (unclosed quotes, comments, parentheses) and
// names the loaded schema does not know. It never talks to the server;
// a name is only flagged when the tree has loaded the list it would be
// in, so a half-loaded tree gives no false warnings.

// IssueKind is what an Issue is about.
type IssueKind int

const (
	IssueSyntax IssueKind = iota
	IssueTable
	IssueColumn
)

// Issue is one finding: Start/End are byte offsets in the inspected text.
// Fix is the closest known name ("" when nothing is close).
type Issue struct {
	Start, End int
	Kind       IssueKind
	Msg        string
	Fix        string
}

// InspectSchema is what the tree knows. Tables maps a schema name (lower
// case) to its table and view names (lower case); only schemas whose
// table lists are loaded are present. Complete is true when every table
// list of the connection is loaded, so an unqualified unknown name is
// really unknown. Columns maps a table name (lower case) to its loaded
// columns.
type InspectSchema struct {
	Tables   map[string]map[string]bool
	Complete bool
	Columns  map[string][]string
}

type insTokKind int

const (
	insTokIdent insTokKind = iota
	insTokQIdent
	insTokString
	insTokNumber
	insTokParam
	insTokPunct
)

type insTok struct {
	kind       insTokKind
	text       string // ident: as written; qident: unquoted; punct: the char
	start, end int
}

func (t insTok) lower() string { return strings.ToLower(t.text) }

func (t insTok) isName() bool { return t.kind == insTokIdent || t.kind == insTokQIdent }

func (t insTok) is(p string) bool {
	if t.kind == insTokPunct {
		return t.text == p
	}
	return t.kind == insTokIdent && strings.EqualFold(t.text, p)
}

// Inspect returns the issues of sql, sorted by position.
func Inspect(sql string, sc InspectSchema) []Issue {
	toks, issues := insTokens(sql)
	var stmt []insTok
	flush := func() {
		issues = append(issues, inspectStatement(stmt, sc)...)
		stmt = stmt[:0]
	}
	for _, t := range toks {
		if t.kind == insTokPunct && t.text == ";" {
			flush()
			continue
		}
		stmt = append(stmt, t)
	}
	flush()
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].Start < issues[j].Start })
	return issues
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isIdentPart(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' || c == '$' }

// insTokens splits sql into tokens; an unclosed string, quoted name,
// comment or dollar quote is a syntax issue running to the end.
func insTokens(s string) ([]insTok, []Issue) {
	var toks []insTok
	var issues []Issue
	unclosed := func(at int, what string) {
		issues = append(issues, Issue{Start: at, End: len(s), Kind: IssueSyntax, Msg: "Unclosed " + what + "."})
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '-' && i+1 < len(s) && s[i+1] == '-':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				unclosed(i, "comment")
				return toks, issues
			}
			i += 2 + end + 2
		case c == '\'' || c == '"' || c == '`':
			j := i + 1
			var b strings.Builder
			closed := false
			for j < len(s) {
				if s[j] == c {
					if j+1 < len(s) && s[j+1] == c { // doubled quote
						b.WriteByte(c)
						j += 2
						continue
					}
					closed = true
					j++
					break
				}
				b.WriteByte(s[j])
				j++
			}
			if !closed {
				if c == '\'' {
					unclosed(i, "string")
				} else {
					unclosed(i, "quoted name")
				}
				return toks, issues
			}
			k := insTokQIdent
			if c == '\'' {
				k = insTokString
			}
			toks = append(toks, insTok{kind: k, text: b.String(), start: i, end: j})
			i = j
		case c == '$':
			j := i + 1
			if j < len(s) && s[j] >= '0' && s[j] <= '9' {
				for j < len(s) && s[j] >= '0' && s[j] <= '9' {
					j++
				}
				toks = append(toks, insTok{kind: insTokParam, text: s[i:j], start: i, end: j})
				i = j
				continue
			}
			for j < len(s) && isIdentPart(s[j]) && s[j] != '$' {
				j++
			}
			if j < len(s) && s[j] == '$' {
				tag := s[i : j+1]
				end := strings.Index(s[j+1:], tag)
				if end < 0 {
					unclosed(i, "dollar-quoted string")
					return toks, issues
				}
				stop := j + 1 + end + len(tag)
				toks = append(toks, insTok{kind: insTokString, text: s[j+1 : j+1+end], start: i, end: stop})
				i = stop
				continue
			}
			toks = append(toks, insTok{kind: insTokPunct, text: "$", start: i, end: i + 1})
			i++
		case c == ':' || c == '@' || c == '?':
			j := i + 1
			for j < len(s) && isIdentPart(s[j]) {
				j++
			}
			if j == i+1 {
				toks = append(toks, insTok{kind: insTokPunct, text: string(c), start: i, end: j})
			} else {
				toks = append(toks, insTok{kind: insTokParam, text: s[i:j], start: i, end: j})
			}
			i = j
		case isIdentStart(c):
			j := i + 1
			for j < len(s) && isIdentPart(s[j]) {
				j++
			}
			toks = append(toks, insTok{kind: insTokIdent, text: s[i:j], start: i, end: j})
			i = j
		case c >= '0' && c <= '9':
			j := i + 1
			for j < len(s) && (isIdentPart(s[j]) || s[j] == '.') {
				j++
			}
			toks = append(toks, insTok{kind: insTokNumber, text: s[i:j], start: i, end: j})
			i = j
		default:
			toks = append(toks, insTok{kind: insTokPunct, text: string(c), start: i, end: i + 1})
			i++
		}
	}
	return toks, issues
}

// sqlClauseWords end a FROM list and are never an alias.
var sqlClauseWords = map[string]bool{
	"where": true, "join": true, "inner": true, "left": true, "right": true, "full": true, "cross": true,
	"natural": true, "outer": true, "on": true, "using": true, "group": true, "order": true, "limit": true,
	"offset": true, "having": true, "union": true, "intersect": true, "except": true, "set": true,
	"values": true, "returning": true, "window": true, "as": true, "lateral": true, "only": true,
	"select": true, "from": true, "for": true, "fetch": true, "with": true, "default": true,
	"straight_join": true, "use": true, "force": true, "ignore": true, "partition": true,
	"tablesample": true, "into": true, "and": true, "or": true, "not": true, "in": true, "exists": true,
	"when": true, "then": true, "else": true, "end": true, "case": true, "is": true, "null": true,
	"like": true, "between": true, "all": true, "any": true, "some": true, "over": true, "filter": true,
	"within": true, "by": true, "do": true, "conflict": true, "update": true, "delete": true, "insert": true,
}

type sqlTableRef struct {
	schema string
	name   insTok
}

// inspectStatement checks one statement (no ";").
func inspectStatement(toks []insTok, sc InspectSchema) []Issue {
	if len(toks) == 0 {
		return nil
	}
	var issues []Issue
	// Parentheses; a "(" right after a non-keyword name is a call, and a
	// FROM inside it (extract(year from x)) is not a table list.
	type paren struct {
		at   int
		call bool
	}
	var stack []paren
	inCall := make([]bool, len(toks))
	for i, t := range toks {
		if len(stack) > 0 {
			inCall[i] = stack[len(stack)-1].call
		}
		switch {
		case t.is("("):
			call := i > 0 && toks[i-1].isName() && !(toks[i-1].kind == insTokIdent && sqlClauseWords[toks[i-1].lower()])
			stack = append(stack, paren{at: i, call: call})
		case t.is(")"):
			if len(stack) == 0 {
				issues = append(issues, Issue{Start: t.start, End: t.end, Kind: IssueSyntax, Msg: "This ) has no matching (."})
				continue
			}
			stack = stack[:len(stack)-1]
		}
	}
	for _, p := range stack {
		t := toks[p.at]
		issues = append(issues, Issue{Start: t.start, End: t.end, Kind: IssueSyntax, Msg: "This ( is never closed."})
	}

	// CTE names: name [ (cols) ] AS (
	ctes := map[string]bool{}
	for i, t := range toks {
		if !t.isName() {
			continue
		}
		j := i + 1
		if j < len(toks) && toks[j].is("(") && i > 0 && (toks[i-1].is("with") || toks[i-1].is(",") || toks[i-1].is("recursive")) {
			j = skipParens(toks, j)
		}
		if j+1 < len(toks) && toks[j].is("as") && toks[j+1].is("(") {
			ctes[t.lower()] = true
		}
	}
	insert := toks[0].is("insert") || toks[0].is("replace")

	// Table references and aliases.
	var refs []sqlTableRef
	aliases := map[string]string{} // alias or table name → table name (lower)
	isRef := map[int]bool{}
	fromDepth := -1
	depth := 0
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case t.is("("):
			depth++
			continue
		case t.is(")"):
			depth--
			if depth < fromDepth {
				fromDepth = -1
			}
			continue
		}
		start := false
		switch {
		case t.is("from") && !inCall[i]:
			start, fromDepth = true, depth
		case t.is("join"):
			start, fromDepth = true, depth
		case t.is("update") && i == 0:
			start = true
		case t.is("into") && insert:
			start = true
		case t.is(",") && fromDepth == depth:
			start = true
		case t.kind == insTokIdent && sqlClauseWords[t.lower()] && depth == fromDepth && !t.is("as") && !t.is("lateral") && !t.is("only"):
			fromDepth = -1
		}
		if !start {
			continue
		}
		j := i + 1
		for j < len(toks) && (toks[j].is("lateral") || toks[j].is("only")) {
			j++
		}
		if j >= len(toks) || !toks[j].isName() || toks[j].kind == insTokIdent && sqlClauseWords[toks[j].lower()] {
			continue
		}
		parts := []int{j}
		for j+2 < len(toks) && toks[j+1].is(".") && toks[j+2].isName() {
			j += 2
			parts = append(parts, j)
		}
		if j+1 < len(toks) && toks[j+1].is("(") && !insert {
			continue // a function in FROM
		}
		name := toks[parts[len(parts)-1]]
		ref := sqlTableRef{name: name}
		if len(parts) > 1 {
			ref.schema = toks[parts[len(parts)-2]].lower()
		}
		for _, p := range parts {
			isRef[p] = true
		}
		refs = append(refs, ref)
		aliases[name.lower()] = name.lower()
		k := j + 1
		if k < len(toks) && toks[k].is("as") {
			k++
		}
		if k < len(toks) && toks[k].isName() && !(toks[k].kind == insTokIdent && sqlClauseWords[toks[k].lower()]) {
			aliases[toks[k].lower()] = name.lower()
			i = k
		} else {
			i = j
		}
	}

	for _, r := range refs {
		n := r.name.lower()
		if ctes[n] && r.schema == "" {
			continue
		}
		var known map[string]bool
		if r.schema != "" {
			known = sc.Tables[r.schema]
			if known == nil {
				continue
			}
		} else {
			if !sc.Complete || len(sc.Tables) == 0 {
				continue
			}
			known = map[string]bool{}
			for _, set := range sc.Tables {
				for t := range set {
					known[t] = true
				}
			}
		}
		if len(known) == 0 || known[n] {
			continue
		}
		names := make([]string, 0, len(known))
		for t := range known {
			names = append(names, t)
		}
		fix := closestName(n, names)
		issues = append(issues, Issue{Start: r.name.start, End: r.name.end, Kind: IssueTable, Fix: fix, Msg: unknownMsg("table", r.name.text, fix)})
	}

	// alias.column
	for i := 0; i+2 < len(toks); i++ {
		a, dot, c := toks[i], toks[i+1], toks[i+2]
		if !a.isName() || !dot.is(".") || !c.isName() || isRef[i] || isRef[i+2] {
			continue
		}
		if i > 0 && toks[i-1].is(".") || i+3 < len(toks) && (toks[i+3].is(".") || toks[i+3].is("(")) {
			continue
		}
		table, ok := aliases[a.lower()]
		if !ok {
			continue
		}
		cols := sc.Columns[table]
		if len(cols) == 0 {
			continue
		}
		found := false
		for _, col := range cols {
			if strings.EqualFold(col, c.text) {
				found = true
				break
			}
		}
		if found {
			continue
		}
		fix := closestName(c.lower(), cols)
		issues = append(issues, Issue{Start: c.start, End: c.end, Kind: IssueColumn, Fix: fix, Msg: unknownMsg("column", a.text+"."+c.text, fix)})
	}
	return issues
}

func unknownMsg(what, name, fix string) string {
	msg := fmt.Sprintf("Unknown %s %s.", what, name)
	if fix != "" {
		msg += " Did you mean " + fix + "?"
	}
	return msg
}

// skipParens returns the index after the ")" matching toks[i] == "(".
func skipParens(toks []insTok, i int) int {
	depth := 0
	for ; i < len(toks); i++ {
		switch {
		case toks[i].is("("):
			depth++
		case toks[i].is(")"):
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return i
}

// closestName is the known name nearest to n (edit distance at most a
// third of its length, at least 1), "" when none is that close.
func closestName(n string, names []string) string {
	sort.Strings(names)
	limit := max(1, len(n)/3)
	best, bestD := "", limit+1
	for _, name := range names {
		if d := editDistance(n, strings.ToLower(name)); d < bestD {
			best, bestD = name, d
		}
	}
	return best
}

// editDistance is the optimal-string-alignment distance (a swap of two
// neighbours counts as one edit: "ordres" → "orders").
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	d := make([][]int, len(ra)+1)
	for i := range d {
		d[i] = make([]int, len(rb)+1)
		d[i][0] = i
	}
	for j := range rb {
		d[0][j+1] = j + 1
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(ra)][len(rb)]
}
