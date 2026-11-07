package dbclient

import (
	"errors"
	"strings"
)

// The SQL formatter (DBX-2) only moves whitespace and upper-cases
// keywords: every other byte of every token is kept, so it can never
// change what a statement does.

type fmtKind uint8

const (
	fkWord   fmtKind = iota // names, keywords, numbers, $1, :name, @var
	fkString                // '…', "…", `…`, [names], $tag$…$tag$
	fkLine                  // -- or # comment (to the end of the line)
	fkBlock                 // /* … */
	fkOp                    // operators
	fkPunct                 // ( ) [ ] , ; .
)

type fmtTok struct {
	kind fmtKind
	text string
	nl   bool // a newline stood between this token and the one before it
	sp   bool // white space stood before it
}

const fmtOpChars = "<>=!|&+-*/%^~#@?:"

// fmtOps are the operators read as one token (longest first).
var fmtOps = []string{"->>", "#>>", "::", "<=", ">=", "<>", "!=", "||", "->", "#>", "=>", "<<", ">>", "@>", "<@", "&&", ":="}

var errFormatDelimiter = errors.New("format SQL: scripts with DELIMITER are not formatted")

// sqlLex splits sql into tokens with the dialect's quote and comment
// rules. Open quotes or comments are an error.
func sqlLex(sql string, dialect SQLDialect) ([]fmtTok, error) {
	var out []fmtTok
	nl, sp := false, false
	lineStart := true
	for i := 0; i < len(sql); {
		c := sql[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == '\v' {
			if c == '\n' {
				nl = true
				lineStart = true
			}
			sp = true
			i++
			continue
		}
		if lineStart && len(sql)-i >= 10 && strings.EqualFold(sql[i:i+10], "DELIMITER ") {
			return nil, errFormatDelimiter
		}
		lineStart = false
		start := i
		kind := fkPunct
		switch {
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-' && (dialect != DialectMySQL || i+2 == len(sql) || sql[i+2] <= ' '),
			c == '#' && dialect == DialectMySQL:
			kind = fkLine
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			// The comment text ends before a CR of a CRLF line end.
			if i > start && sql[i-1] == '\r' {
				i--
			}
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			kind = fkBlock
			depth := 0
			for {
				if i+1 >= len(sql) {
					return nil, errors.New("format SQL: a /* comment is not closed")
				}
				if sql[i] == '/' && sql[i+1] == '*' {
					depth++
					i += 2
					continue
				}
				if sql[i] == '*' && sql[i+1] == '/' {
					i += 2
					if depth--; depth == 0 || dialect != DialectPostgres {
						break
					}
					continue
				}
				i++
			}
		case c == '\'' || c == '"' || c == '`' || c == '[' && dialect == DialectSQLite:
			kind = fkString
			end, err := fmtQuoteEnd(sql, i, dialect, false)
			if err != nil {
				return nil, err
			}
			i = end
		case c == '$' && (dialect == "" || dialect == DialectPostgres):
			if tag, end := dollarTag([]byte(sql), i, DialectPostgres); tag != "" {
				k := strings.Index(sql[end:], tag)
				if k < 0 {
					return nil, errors.New("format SQL: a " + tag + " quote is not closed")
				}
				kind = fkString
				i = end + k + len(tag)
				break
			}
			kind, i = fkWord, fmtWordEnd(sql, i+1)
		case fmtWordByte(c):
			kind = fkWord
			i = fmtWordEnd(sql, i)
			// 1e-5 is one number, not "1e" minus 5.
			if c >= '0' && c <= '9' && (sql[i-1] == 'e' || sql[i-1] == 'E') && i+1 < len(sql) && (sql[i] == '-' || sql[i] == '+') && sql[i+1] >= '0' && sql[i+1] <= '9' {
				i = fmtWordEnd(sql, i+1)
			}
			// N'…', E'…', X'…', B'…': the prefix belongs to the string.
			if i-start == 1 && i < len(sql) && sql[i] == '\'' && strings.ContainsRune("NnEeXxBb", rune(c)) {
				kind = fkString
				end, err := fmtQuoteEnd(sql, i, dialect, c == 'E' || c == 'e')
				if err != nil {
					return nil, err
				}
				i = end
			}
		case c == '@' && i+1 < len(sql) && (sql[i+1] == '@' || fmtWordByte(sql[i+1])):
			kind = fkWord
			j := i + 1
			if sql[j] == '@' {
				j++
			}
			i = fmtWordEnd(sql, j)
		case c == ':' && i+1 < len(sql) && fmtWordByte(sql[i+1]) && (i == 0 || sql[i-1] != ':'):
			kind, i = fkWord, fmtWordEnd(sql, i+1)
		case strings.IndexByte(fmtOpChars, c) >= 0:
			kind = fkOp
			i++
			for _, op := range fmtOps {
				if strings.HasPrefix(sql[start:], op) {
					i = start + len(op)
					break
				}
			}
		default:
			i++
		}
		out = append(out, fmtTok{kind: kind, text: sql[start:i], nl: nl, sp: sp})
		nl, sp = false, false
	}
	return out, nil
}

func fmtDigitStart(s string) bool { return s != "" && s[0] >= '0' && s[0] <= '9' }

func fmtWordByte(c byte) bool { return isWordByte(c) || c == '$' || c >= 0x80 }

func fmtWordEnd(s string, i int) int {
	for i < len(s) && fmtWordByte(s[i]) {
		i++
	}
	return i
}

// fmtQuoteEnd returns the index after the quote that opens at i.
func fmtQuoteEnd(s string, i int, dialect SQLDialect, eString bool) (int, error) {
	q := s[i]
	closer := q
	if q == '[' {
		closer = ']'
	}
	backslash := q != '`' && q != '[' && (dialect == "" && q == '\'' || dialect == DialectMySQL || eString)
	for j := i + 1; j < len(s); j++ {
		switch {
		case backslash && s[j] == '\\':
			j++
		case s[j] == closer:
			if closer != ']' && j+1 < len(s) && s[j+1] == closer {
				j++
				continue
			}
			return j + 1, nil
		}
	}
	return 0, errors.New("format SQL: a " + string(q) + " quote is not closed")
}

// fmtKeywords are upper-cased; fmtFuncs are keywords written like
// functions (no space before their "(").
var fmtKeywords = wordSet(`select from where group order by having limit offset union all intersect except
values set returning insert into update delete with recursive join inner left right full cross outer natural
on using as and or not in is null like ilike between exists distinct case when then else end asc desc any
primary key foreign references default unique create table alter drop index view add column if cascade
true false nulls first last over partition window filter begin commit rollback truncate explain analyze
conflict do nothing lateral escape some only constraint check temporary temp replace grant revoke to
count sum avg min max coalesce cast lower upper now nullif rank row_number dense_rank extract`)

var fmtFuncs = wordSet(`count sum avg min max coalesce cast lower upper now nullif rank row_number dense_rank extract replace`)

func wordSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[strings.ToUpper(w)] = true
	}
	return m
}

// fmtListClauses put one item per line when they have more than one.
var fmtListClauses = map[string]bool{"SELECT": true, "SET": true, "GROUP BY": true, "ORDER BY": true, "RETURNING": true}

// fmtCondClauses put each AND / OR on its own line.
var fmtCondClauses = map[string]bool{"WHERE": true, "HAVING": true}

type fmtFrame struct {
	base    int
	sub     bool // a subquery "(" (or the statement itself)
	plain   bool // a plain "(": no clause breaks inside
	clause  string
	list    bool
	between bool
	start   bool // no token written in this frame yet
}

type fmtWriter struct {
	b        strings.Builder
	pendNL   int
	pendInd  int
	fresh    bool // at the start of a line
	prev     *fmtTok
	prevText string
	unary    bool // prev is a unary sign
	star     bool // prev is a "*" that stands for "all columns"
}

func (w *fmtWriter) breakLine(indent, n int) {
	if w.b.Len() == 0 {
		return
	}
	w.pendNL = max(w.pendNL, n)
	w.pendInd = indent
}

func (w *fmtWriter) write(t *fmtTok, text string, space bool) {
	if w.pendNL > 0 && w.b.Len() > 0 {
		w.b.WriteString(strings.Repeat("\n", w.pendNL))
		w.b.WriteString(strings.Repeat(" ", w.pendInd))
		w.fresh = true
	}
	w.pendNL = 0
	if !w.fresh && w.b.Len() > 0 && (space || fmtWouldMerge(w.prev, w.prevText, t, text)) {
		w.b.WriteByte(' ')
	}
	w.b.WriteString(text)
	w.fresh = false
	w.prev, w.prevText = t, text
}

// fmtWouldMerge reports whether writing b right after a would read back
// as other tokens ("-" "-" is a comment, two words are one word).
func fmtWouldMerge(a *fmtTok, at string, b *fmtTok, bt string) bool {
	if a == nil || at == "" || bt == "" {
		return false
	}
	x, y := at[len(at)-1], bt[0]
	switch {
	case fmtWordByte(x) && fmtWordByte(y):
		return true
	case strings.IndexByte(fmtOpChars, x) >= 0 && strings.IndexByte(fmtOpChars, y) >= 0:
		return true
	case a.kind == fkString && b.kind == fkString:
		return true
	case x == '/' && y == '*', x == '*' && y == '/', x == '-' && y == '-':
		return true
	}
	return false
}

// FormatQuery formats sql (one or more statements) for reading. dialect
// is a driver name ("postgres", "mysql", "sqlite"; "" = generic). It
// fails, and changes nothing, on open quotes or comments and on scripts
// with DELIMITER lines.
func FormatQuery(sql, dialect string) (string, error) {
	d := dialectArg([]string{dialect})
	toks, err := sqlLex(sql, d)
	if err != nil {
		return "", err
	}
	w := &fmtWriter{}
	stack := []*fmtFrame{{sub: true, start: true}}
	top := func() *fmtFrame { return stack[len(stack)-1] }
	var prevSig *fmtTok // the last token that is not a comment
	for i := 0; i < len(toks); i++ {
		t := &toks[i]
		f := top()
		if t.kind == fkLine || t.kind == fkBlock {
			cont := f.base
			if f.clause != "" {
				cont += 2
			}
			if t.nl {
				w.breakLine(cont, 1)
				w.write(t, t.text, true)
			} else {
				saved, savedInd := w.pendNL, w.pendInd
				w.pendNL = 0
				w.write(t, t.text, true)
				w.pendNL, w.pendInd = saved, savedInd
			}
			if t.kind == fkLine {
				if w.pendNL == 0 {
					w.pendInd = cont
				}
				w.pendNL = max(w.pendNL, 1)
			}
			continue
		}
		upper := strings.ToUpper(t.text)
		afterDot := prevSig != nil && prevSig.text == "."
		isKW := t.kind == fkWord && !afterDot && fmtKeywords[upper]

		// Clause keywords start a line.
		if isKW && !f.plain {
			if name, n := fmtClauseAt(toks, i, f, prevSig); name != "" {
				if !f.start {
					w.breakLine(f.base, 1)
				}
				for k := 0; k < n; k++ {
					tk := &toks[i+k]
					w.write(tk, strings.ToUpper(tk.text), true)
				}
				i += n - 1
				f.clause, f.between, f.start = name, false, false
				f.list = fmtListClauses[name] && fmtHasTopComma(toks, i+1, f)
				if f.list {
					w.breakLine(f.base+2, 1)
				}
				prevSig = &toks[i]
				w.unary, w.star = false, false
				continue
			}
		}
		text := t.text
		if isKW {
			text = upper
		}
		space := fmtSpace(w, prevSig, t, isKW)
		switch {
		case t.text == "(":
			sub := fmtNextIsQuery(toks, i+1)
			w.write(t, text, space)
			if sub {
				stack = append(stack, &fmtFrame{base: f.base + 2, sub: true, start: true})
				w.breakLine(f.base+2, 1)
			} else {
				stack = append(stack, &fmtFrame{base: f.base, plain: true})
			}
		case t.text == ")":
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
				if f.sub {
					w.breakLine(f.base-2, 1)
				}
			}
			w.write(t, text, false)
		case t.text == ";":
			w.write(t, text, false)
			stack = []*fmtFrame{{sub: true, start: true}}
			w.breakLine(0, 2)
		case t.text == ",":
			w.write(t, text, false)
			if f.list && !f.plain {
				w.breakLine(f.base+2, 1)
			}
		case isKW && (upper == "AND" || upper == "OR") && !f.plain && (fmtCondClauses[f.clause] || strings.HasSuffix(f.clause, "JOIN")):
			if f.between && upper == "AND" {
				f.between = false
				w.write(t, text, space)
				break
			}
			w.breakLine(f.base+2, 1)
			w.write(t, text, true)
		default:
			if isKW && upper == "BETWEEN" {
				f.between = true
			}
			w.write(t, text, space)
		}
		w.unary = t.kind == fkOp && (t.text == "-" || t.text == "+") && fmtValueExpected(prevSig)
		w.star = t.kind == fkOp && t.text == "*" && (fmtValueExpected(prevSig) || prevSig.text == ".")
		if t.text != "(" && t.text != ")" {
			f.start = false
		} else if t.text == ")" {
			top().start = false
		} else {
			f.start = false
		}
		prevSig = t
	}
	return w.b.String(), nil
}

// fmtValueExpected reports whether a value (not an operator) comes after
// prev: a sign there is unary and a "*" means all columns.
func fmtValueExpected(prev *fmtTok) bool {
	if prev == nil {
		return true
	}
	switch prev.kind {
	case fkOp:
		return true
	case fkPunct:
		return prev.text == "(" || prev.text == "," || prev.text == "[" || prev.text == ";"
	case fkWord:
		u := strings.ToUpper(prev.text)
		return fmtKeywords[u] && !fmtFuncs[u]
	}
	return false
}

// fmtSpace decides the space before t from the tokens around it.
func fmtSpace(w *fmtWriter, prev, t *fmtTok, isKW bool) bool {
	if prev == nil {
		return false
	}
	// Around a "." next to a number the source spacing stays ("1 .5" is
	// not "1.5").
	if (t.text == "." || prev.text == ".") && (fmtDigitStart(prev.text) || fmtDigitStart(t.text)) {
		return t.sp
	}
	switch t.text {
	case ",", ";", ")", "]", ".", "::":
		return false
	}
	switch prev.text {
	case "(", "[", ".", "::":
		return false
	}
	if w.unary {
		return false
	}
	if w.star && t.kind == fkOp {
		return true
	}
	if t.kind == fkOp && t.text == "*" && prev.text == "." {
		return false
	}
	if t.text == "(" {
		u := strings.ToUpper(prev.text)
		if prev.kind == fkWord && (!fmtKeywords[u] || fmtFuncs[u]) || prev.text == ")" || prev.text == "]" || prev.kind == fkString {
			return t.sp
		}
		return true
	}
	if t.text == "[" && (prev.kind == fkWord || prev.text == ")" || prev.text == "]") {
		return t.sp
	}
	return true
}

// fmtNextIsQuery reports whether the next real token is SELECT or WITH.
func fmtNextIsQuery(toks []fmtTok, i int) bool {
	for ; i < len(toks); i++ {
		if toks[i].kind == fkLine || toks[i].kind == fkBlock {
			continue
		}
		u := strings.ToUpper(toks[i].text)
		return toks[i].kind == fkWord && (u == "SELECT" || u == "WITH" || u == "VALUES")
	}
	return false
}

func fmtWordAt(toks []fmtTok, i int, words ...string) bool {
	for k, w := range words {
		if i+k >= len(toks) || toks[i+k].kind != fkWord || !strings.EqualFold(toks[i+k].text, w) {
			return false
		}
	}
	return true
}

// fmtClauseAt returns the clause that starts at toks[i] and how many
// words it has, or "".
func fmtClauseAt(toks []fmtTok, i int, f *fmtFrame, prev *fmtTok) (string, int) {
	prevU := ""
	if prev != nil {
		prevU = strings.ToUpper(prev.text)
	}
	u := strings.ToUpper(toks[i].text)
	switch u {
	case "SELECT", "WHERE", "HAVING", "LIMIT", "OFFSET", "VALUES", "RETURNING", "INTERSECT", "EXCEPT":
		return u, 1
	case "FROM":
		if prevU == "DISTINCT" {
			return "", 0
		}
		return u, 1
	case "GROUP", "ORDER":
		if fmtWordAt(toks, i+1, "BY") {
			return u + " BY", 2
		}
	case "UNION":
		if fmtWordAt(toks, i+1, "ALL") {
			return "UNION ALL", 2
		}
		return u, 1
	case "INSERT":
		if fmtWordAt(toks, i+1, "INTO") {
			return "INSERT INTO", 2
		}
	case "DELETE":
		if f.start && fmtWordAt(toks, i+1, "FROM") {
			return "DELETE FROM", 2
		}
	case "UPDATE", "WITH":
		if f.start {
			return u, 1
		}
	case "SET":
		if f.clause == "UPDATE" {
			return u, 1
		}
	case "JOIN":
		return u, 1
	case "NATURAL", "LEFT", "RIGHT", "FULL", "INNER", "CROSS":
		n := 1
		if u == "NATURAL" && i+1 < len(toks) {
			if v := strings.ToUpper(toks[i+1].text); v == "LEFT" || v == "RIGHT" || v == "FULL" || v == "INNER" {
				n++
			}
		}
		if fmtWordAt(toks, i+n, "OUTER") {
			n++
		}
		if fmtWordAt(toks, i+n, "JOIN") {
			var words []string
			for k := 0; k <= n; k++ {
				words = append(words, strings.ToUpper(toks[i+k].text))
			}
			return strings.Join(words, " "), n + 1
		}
	}
	return "", 0
}

// fmtHasTopComma reports whether the clause that starts at toks[i] has
// a comma at its own depth before the next clause.
func fmtHasTopComma(toks []fmtTok, i int, f *fmtFrame) bool {
	depth := 0
	var prev *fmtTok
	for ; i < len(toks); i++ {
		t := &toks[i]
		switch {
		case t.kind == fkLine || t.kind == fkBlock:
			continue
		case t.text == "(" || t.text == "[":
			depth++
		case t.text == ")" || t.text == "]":
			if depth == 0 {
				return false
			}
			depth--
		case t.text == ";":
			return false
		case t.text == "," && depth == 0:
			return true
		case depth == 0 && t.kind == fkWord && (prev == nil || prev.text != "."):
			if name, _ := fmtClauseAt(toks, i, &fmtFrame{clause: f.clause}, prev); name != "" && name != f.clause {
				return false
			}
		}
		prev = t
	}
	return false
}
