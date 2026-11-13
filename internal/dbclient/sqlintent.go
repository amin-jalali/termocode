package dbclient

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// SQL intentions (DBX-42): small rewrites of one statement at the cursor,
// done on the tokens of our own lexer (sqlLex). Every byte outside an
// edit is kept, so comments and layout stay as the user wrote them. An
// intention is only offered when the rewrite is safe to read: when the
// statement is not understood, nothing is offered.

// SQLIntention is one rewrite offered for the statement at the cursor.
type SQLIntention struct {
	ID    string
	Title string
}

// The intention ids.
const (
	IntentAlias         = "alias"
	IntentQualify       = "qualify"
	IntentUnqualify     = "unqualify"
	IntentExpandStar    = "expand-star"
	IntentSubquery      = "subquery"
	IntentExtractCTE    = "extract-cte"
	IntentInlineCTE     = "inline-cte"
	IntentParam         = "param"
	IntentDistinctGroup = "distinct-to-group"
	IntentGroupDistinct = "group-to-distinct"
)

// SQLIntentions lists the rewrites that apply to stmt with the cursor at
// byte offset cur. driver is a driver or dialect name ("" = generic).
// cols maps a lower-case table name to its column names; Expand * and
// Qualify Names are only offered when the columns of every table are
// known.
func SQLIntentions(stmt, driver string, cur int, cols map[string][]string) []SQLIntention {
	c, err := newIntentCtx(stmt, driver, cur, cols)
	if err != nil {
		return nil
	}
	var out []SQLIntention
	for _, p := range c.plans() {
		if title, _, ok := p.fn(); ok {
			out = append(out, SQLIntention{ID: p.id, Title: title})
		}
	}
	return out
}

// ApplySQLIntention returns stmt rewritten by the intention id.
func ApplySQLIntention(stmt, driver, id string, cur int, cols map[string][]string) (string, error) {
	c, err := newIntentCtx(stmt, driver, cur, cols)
	if err != nil {
		return "", err
	}
	for _, p := range c.plans() {
		if p.id != id {
			continue
		}
		_, eds, ok := p.fn()
		if !ok {
			return "", errors.New("this rewrite does not apply here")
		}
		return applySQLEdits(stmt, eds)
	}
	return "", errors.New("unknown SQL intention " + id)
}

type itok struct {
	kind            fmtKind
	text            string
	pos, end, depth int
}

type sqlEdit struct {
	start, end int
	text       string
}

// applySQLEdits applies edits that do not overlap, last one first.
func applySQLEdits(s string, eds []sqlEdit) (string, error) {
	sort.SliceStable(eds, func(i, j int) bool { return eds[i].start > eds[j].start })
	limit := len(s)
	for _, e := range eds {
		if e.start < 0 || e.end < e.start || e.end > limit {
			return "", errors.New("SQL rewrite: edits overlap")
		}
		s = s[:e.start] + e.text + s[e.end:]
		limit = e.start
	}
	return s, nil
}

type cteDef struct {
	name, open, close int // token of the name and the body's ( and )
	colList           bool
}

// fromItem is one table, subquery or table function after FROM / JOIN.
type fromItem struct {
	a, b    int // the name tokens, or the ( … ) of a subquery: [a, b)
	sub, fn bool
	alias   int // token of the alias, -1 = none
	colList bool
	end     int // the token after the item (alias included)
}

type selItem struct {
	a, b  int // expression tokens [a, b), alias excluded
	alias int
}

type qblock struct {
	idx, sel, end, depth int
	distinct             int // -1 = none
	distinctOn           bool
	selStart, selEnd     int
	clause               map[string]int // clause word → its token
	items                []selItem
	from                 []fromItem
}

type intentCtx struct {
	src       string
	driver    string
	dialect   SQLDialect
	toks      []itok
	blocks    []*qblock
	owner     []int // token → innermost block, -1 = none
	ctes      []cteDef
	main      int // first token after the WITH list (0 without WITH)
	recursive bool
	cur       int
	cols      map[string][]string
}

func newIntentCtx(src, driver string, cur int, cols map[string][]string) (*intentCtx, error) {
	d, _ := DialectByName(driver)
	toks, err := intentLex(src, d)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, errors.New("no SQL")
	}
	c := &intentCtx{src: src, driver: driver, dialect: d, toks: toks, cur: cur, cols: cols}
	c.parseWith()
	c.owner = make([]int, len(toks))
	for i := range c.owner {
		c.owner[i] = -1
	}
	for j, t := range toks {
		if isIWord(t, "select") {
			c.blocks = append(c.blocks, c.parseBlock(j))
		}
	}
	// Outer blocks first, so the innermost one owns its tokens.
	order := make([]*qblock, len(c.blocks))
	copy(order, c.blocks)
	sort.SliceStable(order, func(i, j int) bool { return order[i].depth < order[j].depth })
	for _, b := range order {
		for j := b.sel; j < b.end; j++ {
			c.owner[j] = b.idx
		}
	}
	return c, nil
}

// intentLex is sqlLex with byte offsets and paren depth; comments are
// dropped (their bytes stay in the text, edits never touch them).
func intentLex(src string, d SQLDialect) ([]itok, error) {
	raw, err := sqlLex(src, d)
	if err != nil {
		return nil, err
	}
	out := make([]itok, 0, len(raw))
	at, depth := 0, 0
	for _, t := range raw {
		for at < len(src) && strings.IndexByte(" \t\r\n\f\v", src[at]) >= 0 {
			at++
		}
		if !strings.HasPrefix(src[at:], t.text) {
			return nil, errors.New("SQL: the text could not be read")
		}
		pos := at
		at += len(t.text)
		if t.kind == fkLine || t.kind == fkBlock {
			continue
		}
		if t.kind == fkPunct && t.text == ")" && depth > 0 {
			depth--
		}
		out = append(out, itok{kind: t.kind, text: t.text, pos: pos, end: at, depth: depth})
		if t.kind == fkPunct && t.text == "(" {
			depth++
		}
	}
	return out, nil
}

func isIWord(t itok, words ...string) bool {
	if t.kind != fkWord {
		return false
	}
	for _, w := range words {
		if strings.EqualFold(t.text, w) {
			return true
		}
	}
	return false
}

func isIPunct(t itok, s string) bool { return t.kind == fkPunct && t.text == s }

// intentStop are words that end a name or an expression; they are never
// read as an alias or a column.
var intentStop = wordSet(`on using join inner left right full cross outer natural where group order having limit offset
window fetch for union intersect except lateral as set returning values and or not with into from select straight_join
tablesample partition`)

func intentKeyword(w string) bool {
	w = strings.ToUpper(w) // wordSet keys are upper case
	return intentStop[w] || fmtKeywords[w]
}

// isName: a plain or quoted identifier.
func (c *intentCtx) isName(t itok) bool {
	switch t.kind {
	case fkWord:
		ch := t.text[0]
		return ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= 0x80
	case fkString:
		ch := t.text[0]
		return ch == '`' || ch == '[' || ch == '"' && c.dialect != DialectMySQL
	}
	return false
}

// isAlias: a name that is not a keyword.
func (c *intentCtx) isAlias(t itok) bool {
	return c.isName(t) && (t.kind != fkWord || !intentKeyword(t.text))
}

// unquoteIdent strips "…", `…` or […] (doubled quotes undone).
func unquoteIdent(s string) string {
	if len(s) >= 2 {
		switch s[0] {
		case '"':
			return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
		case '`':
			return strings.ReplaceAll(s[1:len(s)-1], "``", "`")
		case '[':
			return strings.ReplaceAll(s[1:len(s)-1], "]]", "]")
		}
	}
	return s
}

func (c *intentCtx) sameName(a, b itok) bool {
	return c.isName(a) && c.isName(b) && strings.EqualFold(unquoteIdent(a.text), unquoteIdent(b.text))
}

// match returns the ) that closes the ( at i, or -1.
func (c *intentCtx) match(i int) int {
	d := c.toks[i].depth
	for j := i + 1; j < len(c.toks); j++ {
		if c.toks[j].depth == d && isIPunct(c.toks[j], ")") {
			return j
		}
	}
	return -1
}

func (c *intentCtx) text(a, b int) string { return c.src[c.toks[a].pos:c.toks[b-1].end] }

// norm is an expression for comparing: tokens joined by one space, words
// lower-cased.
func (c *intentCtx) norm(a, b int) string {
	parts := make([]string, 0, b-a)
	for j := a; j < b; j++ {
		if c.toks[j].kind == fkWord {
			parts = append(parts, strings.ToLower(c.toks[j].text))
		} else {
			parts = append(parts, c.toks[j].text)
		}
	}
	return strings.Join(parts, " ")
}

// splitTop splits [a, z) at the commas of depth d.
func (c *intentCtx) splitTop(a, z, d int) [][2]int {
	var out [][2]int
	s := a
	for j := a; j <= z; j++ {
		if j == z || c.toks[j].depth == d && isIPunct(c.toks[j], ",") {
			if j > s {
				out = append(out, [2]int{s, j})
			}
			s = j + 1
		}
	}
	return out
}

// tokAt is the last token that starts at or before the cursor (-1: none).
func (c *intentCtx) tokAt() int {
	k := -1
	for j, t := range c.toks {
		if t.pos > c.cur {
			break
		}
		k = j
	}
	return k
}

// curBlock is the innermost SELECT at the cursor, else the main one.
func (c *intentCtx) curBlock() *qblock {
	if k := c.tokAt(); k >= 0 && c.owner[k] >= 0 {
		return c.blocks[c.owner[k]]
	}
	for _, b := range c.blocks {
		if b.depth == 0 {
			return b
		}
	}
	return nil
}

// lineIndent is the white space before p on its line; ok is false when
// other text stands before p on that line.
func (c *intentCtx) lineIndent(p int) (string, bool) {
	s := strings.LastIndexByte(c.src[:p], '\n') + 1
	ind := c.src[s:p]
	if strings.Trim(ind, " \t") != "" {
		return "", false
	}
	return ind, true
}

func (c *intentCtx) parseWith() {
	if !isIWord(c.toks[0], "with") {
		return
	}
	i := 1
	if i < len(c.toks) && isIWord(c.toks[i], "recursive") {
		c.recursive = true
		i++
	}
	var defs []cteDef
	for i < len(c.toks) {
		if !c.isName(c.toks[i]) {
			return
		}
		def := cteDef{name: i}
		j := i + 1
		if j < len(c.toks) && isIPunct(c.toks[j], "(") {
			cl := c.match(j)
			if cl < 0 {
				return
			}
			def.colList = true
			j = cl + 1
		}
		if j >= len(c.toks) || !isIWord(c.toks[j], "as") {
			return
		}
		j++
		for j < len(c.toks) && isIWord(c.toks[j], "not", "materialized") {
			j++
		}
		if j >= len(c.toks) || !isIPunct(c.toks[j], "(") {
			return
		}
		cl := c.match(j)
		if cl < 0 {
			return
		}
		def.open, def.close = j, cl
		defs = append(defs, def)
		j = cl + 1
		if j < len(c.toks) && isIPunct(c.toks[j], ",") {
			i = j + 1
			continue
		}
		if j >= len(c.toks) {
			return
		}
		c.ctes, c.main = defs, j
		return
	}
}

func (c *intentCtx) parseBlock(sel int) *qblock {
	d := c.toks[sel].depth
	b := &qblock{idx: len(c.blocks), sel: sel, depth: d, distinct: -1, clause: map[string]int{}}
	b.end = len(c.toks)
	for j := sel + 1; j < len(c.toks); j++ {
		t := c.toks[j]
		if t.depth < d || t.depth == d && (isIPunct(t, ";") || isIWord(t, "union", "intersect", "except")) {
			b.end = j
			break
		}
		if t.depth != d || t.kind != fkWord {
			continue
		}
		w := strings.ToLower(t.text)
		switch w {
		case "from":
			if isIWord(c.toks[j-1], "distinct") { // IS DISTINCT FROM
				continue
			}
		case "where", "having", "limit", "offset", "window", "fetch", "for", "into":
		case "group", "order":
			if j+1 >= len(c.toks) || !isIWord(c.toks[j+1], "by") {
				continue
			}
		default:
			continue
		}
		if _, seen := b.clause[w]; !seen {
			b.clause[w] = j
		}
	}
	start := sel + 1
	if start < b.end && isIWord(c.toks[start], "distinct") {
		b.distinct = start
		start++
		if start < b.end && isIWord(c.toks[start], "on") {
			b.distinctOn = true
			if start+1 < b.end && isIPunct(c.toks[start+1], "(") {
				if cl := c.match(start + 1); cl > 0 && cl < b.end {
					start = cl + 1
				}
			}
		}
	} else if start < b.end && isIWord(c.toks[start], "all") {
		start++
	}
	b.selStart, b.selEnd = start, c.nextClause(b, sel)
	for _, r := range c.splitTop(b.selStart, b.selEnd, d) {
		s := selItem{a: r[0], b: r[1], alias: -1}
		n := s.b - s.a
		switch {
		case n >= 3 && isIWord(c.toks[s.b-2], "as") && c.isName(c.toks[s.b-1]):
			s.alias = s.b - 1
			s.b -= 2
		case n >= 2 && c.isAlias(c.toks[s.b-1]) && c.endsExpr(c.toks[s.b-2]):
			s.alias = s.b - 1
			s.b--
		}
		b.items = append(b.items, s)
	}
	if f, ok := b.clause["from"]; ok {
		c.parseFrom(b, f+1, c.nextClause(b, f))
	}
	return b
}

// endsExpr: the token can be the last one of an expression.
func (c *intentCtx) endsExpr(t itok) bool {
	switch t.kind {
	case fkString:
		return true
	case fkWord:
		return !intentKeyword(t.text) || isIWord(t, "end", "null", "true", "false")
	}
	return isIPunct(t, ")")
}

// nextClause is the first clause after token j of b, or b.end.
func (c *intentCtx) nextClause(b *qblock, j int) int {
	end := b.end
	for _, k := range b.clause {
		if k > j && k < end {
			end = k
		}
	}
	return end
}

func (c *intentCtx) parseFrom(b *qblock, a, z int) {
	d := b.depth
	i := a
	for i < z {
		for i < z && isIWord(c.toks[i], "lateral", "only") {
			i++
		}
		if i >= z {
			return
		}
		it := fromItem{a: i, alias: -1}
		t := c.toks[i]
		switch {
		case isIPunct(t, "("):
			cl := c.match(i)
			if cl < 0 || cl >= z {
				return
			}
			it.sub = i+1 < cl && isIWord(c.toks[i+1], "select", "with")
			it.fn = !it.sub // a ( join ) or VALUES: left alone
			it.b = cl + 1
		case c.isName(t) && (t.kind != fkWord || !intentKeyword(t.text)):
			j := i + 1
			for j+1 < z && isIPunct(c.toks[j], ".") && c.isName(c.toks[j+1]) {
				j += 2
			}
			it.b = j
			if j < z && isIPunct(c.toks[j], "(") {
				cl := c.match(j)
				if cl < 0 || cl >= z {
					return
				}
				it.fn = true
				it.b = cl + 1
			}
		default:
			return
		}
		j := it.b
		if j < z && isIWord(c.toks[j], "as") {
			j++
		}
		if j < z && c.isAlias(c.toks[j]) {
			it.alias = j
			j++
			if j < z && isIPunct(c.toks[j], "(") {
				cl := c.match(j)
				if cl < 0 || cl >= z {
					return
				}
				it.colList = true
				j = cl + 1
			}
		}
		it.end = j
		b.from = append(b.from, it)
		for j < z {
			t := c.toks[j]
			j++
			if t.depth == d && (isIPunct(t, ",") || isIWord(t, "join")) {
				break
			}
		}
		i = j
	}
}

// pickFrom is the item of b at the cursor that passes ok, else the only
// item that passes ok, else -1.
func (c *intentCtx) pickFrom(b *qblock, ok func(fromItem) bool) int {
	k := c.tokAt()
	only, n := -1, 0
	for i, it := range b.from {
		if !ok(it) {
			continue
		}
		if k >= it.a && k < it.end {
			return i
		}
		only, n = i, n+1
	}
	if n == 1 {
		return only
	}
	return -1
}

// qual is how a column of the item is qualified: its alias or its name.
func (c *intentCtx) qual(it fromItem) string {
	if it.alias >= 0 {
		return c.toks[it.alias].text
	}
	return c.text(it.a, it.b)
}

func (c *intentCtx) colsOf(it fromItem) ([]string, bool) {
	if it.sub || it.fn || c.cols == nil {
		return nil, false
	}
	cols, ok := c.cols[strings.ToLower(unquoteIdent(c.toks[it.b-1].text))]
	return cols, ok && len(cols) > 0
}

func (c *intentCtx) quoteCol(name string) string {
	simple := name != "" && !intentKeyword(name)
	for i := 0; i < len(name) && simple; i++ {
		ch := name[i]
		simple = ch == '_' || ch >= 'a' && ch <= 'z' || i > 0 && ch >= '0' && ch <= '9'
	}
	if simple {
		return name
	}
	if d := DialectFor(c.driver); d != nil {
		return d.QuoteIdent(name)
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

type intentPlan struct {
	id string
	fn func() (string, []sqlEdit, bool)
}

func (c *intentCtx) plans() []intentPlan {
	return []intentPlan{
		{IntentAlias, c.introduceAlias},
		{IntentQualify, c.qualify},
		{IntentUnqualify, c.unqualify},
		{IntentExpandStar, c.expandStar},
		{IntentSubquery, c.wrapSubquery},
		{IntentExtractCTE, c.extractCTE},
		{IntentInlineCTE, c.inlineCTE},
		{IntentParam, c.extractParam},
		{IntentDistinctGroup, c.distinctToGroup},
		{IntentGroupDistinct, c.groupToDistinct},
	}
}

// takenNames are the lower-case table names, aliases and CTE names.
func (c *intentCtx) takenNames() map[string]bool {
	taken := map[string]bool{}
	for _, b := range c.blocks {
		for _, it := range b.from {
			if !it.sub && !it.fn {
				taken[strings.ToLower(unquoteIdent(c.toks[it.b-1].text))] = true
			}
			if it.alias >= 0 {
				taken[strings.ToLower(unquoteIdent(c.toks[it.alias].text))] = true
			}
		}
	}
	for _, d := range c.ctes {
		taken[strings.ToLower(unquoteIdent(c.toks[d.name].text))] = true
	}
	return taken
}

func freeName(base string, taken map[string]bool) string {
	name := base
	for n := 2; taken[strings.ToLower(name)] || intentKeyword(name); n++ {
		name = base + strconv.Itoa(n)
	}
	return name
}

func (c *intentCtx) introduceAlias() (string, []sqlEdit, bool) {
	b := c.curBlock()
	if b == nil {
		return "", nil, false
	}
	k := c.pickFrom(b, func(it fromItem) bool { return !it.sub && !it.fn && it.alias < 0 })
	if k < 0 {
		return "", nil, false
	}
	it := b.from[k]
	name := unquoteIdent(c.toks[it.b-1].text)
	base := ""
	for _, p := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !(r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		for _, w := range strings.Split(p, "_") {
			if w != "" && w[0] >= 'a' && w[0] <= 'z' {
				base += w[:1]
			}
		}
	}
	if base == "" {
		base = "t"
	}
	alias := freeName(base, c.takenNames())
	last := c.toks[it.b-1]
	eds := []sqlEdit{{last.end, last.end, " " + alias}}
	n := it.b - it.a
	for j := b.sel; j < b.end; j++ {
		if j >= it.a && j < it.end || j > 0 && isIPunct(c.toks[j-1], ".") {
			continue
		}
		switch {
		case c.sameToks(j, it.a, it.b) && j+n < b.end && isIPunct(c.toks[j+n], "."):
			eds = append(eds, sqlEdit{c.toks[j].pos, c.toks[j+n-1].end, alias})
			j += n
		case n > 1 && c.sameName(c.toks[j], last) && j+1 < b.end && isIPunct(c.toks[j+1], "."):
			eds = append(eds, sqlEdit{c.toks[j].pos, c.toks[j].end, alias})
			j++
		}
	}
	return fmt.Sprintf("Introduce alias %s for %s", alias, name), eds, true
}

// sameToks: the tokens at j match the tokens [a, b).
func (c *intentCtx) sameToks(j, a, b int) bool {
	if j+(b-a) > len(c.toks) {
		return false
	}
	for k := 0; k < b-a; k++ {
		x, y := c.toks[j+k], c.toks[a+k]
		if c.isName(y) {
			if !c.sameName(x, y) {
				return false
			}
		} else if x.text != y.text {
			return false
		}
	}
	return true
}

func (c *intentCtx) qualify() (string, []sqlEdit, bool) {
	b := c.curBlock()
	if b == nil || len(b.from) == 0 {
		return "", nil, false
	}
	type source struct {
		q    string
		cols map[string]bool
	}
	var srcs []source
	skip := map[int]bool{}
	for _, it := range b.from {
		cols, ok := c.colsOf(it)
		if !ok {
			return "", nil, false
		}
		m := map[string]bool{}
		for _, n := range cols {
			m[strings.ToLower(n)] = true
		}
		srcs = append(srcs, source{c.qual(it), m})
		for j := it.a; j < it.end; j++ {
			skip[j] = true
		}
	}
	aliases := map[string]bool{}
	for _, s := range b.items {
		if s.alias >= 0 {
			skip[s.alias] = true
			aliases[strings.ToLower(unquoteIdent(c.toks[s.alias].text))] = true
		}
	}
	var eds []sqlEdit
	for j := b.sel + 1; j < b.end; j++ {
		t := c.toks[j]
		if c.owner[j] != b.idx || skip[j] {
			continue
		}
		if isIWord(t, "using") && j+1 < b.end && isIPunct(c.toks[j+1], "(") {
			if cl := c.match(j + 1); cl > 0 {
				j = cl
			}
			continue
		}
		if !c.isName(t) || t.kind == fkWord && intentKeyword(t.text) {
			continue
		}
		prev := c.toks[j-1]
		if isIPunct(prev, ".") || prev.text == "::" || isIWord(prev, "as") {
			continue
		}
		if j+1 < len(c.toks) {
			next := c.toks[j+1]
			// a.b, f(…), DATE '2024-01-01'
			if isIPunct(next, ".") || isIPunct(next, "(") || next.kind == fkString && next.text[0] == '\'' {
				continue
			}
		}
		w := strings.ToLower(unquoteIdent(t.text))
		if aliases[w] {
			continue
		}
		hit, n := "", 0
		for _, s := range srcs {
			if s.cols[w] {
				hit, n = s.q, n+1
			}
		}
		if n == 1 {
			eds = append(eds, sqlEdit{t.pos, t.pos, hit + "."})
		}
	}
	if len(eds) == 0 {
		return "", nil, false
	}
	if len(eds) == 1 {
		return "Qualify 1 column name", eds, true
	}
	return fmt.Sprintf("Qualify %d column names", len(eds)), eds, true
}

// unqualify drops "q." in front of the columns of the block at the
// cursor where only one of its tables can have the column: always with
// one table, else only when the columns of every table are known. A
// name that is also a select alias keeps its qualifier.
func (c *intentCtx) unqualify() (string, []sqlEdit, bool) {
	b := c.curBlock()
	if b == nil || len(b.from) == 0 {
		return "", nil, false
	}
	colSets := make([]map[string]bool, len(b.from))
	if len(b.from) > 1 {
		for k, it := range b.from {
			cols, ok := c.colsOf(it)
			if !ok {
				return "", nil, false
			}
			colSets[k] = map[string]bool{}
			for _, n := range cols {
				colSets[k][strings.ToLower(n)] = true
			}
		}
	}
	aliases := map[string]bool{}
	for _, s := range b.items {
		if s.alias >= 0 {
			aliases[strings.ToLower(unquoteIdent(c.toks[s.alias].text))] = true
		}
	}
	// qualifier is the from item the tokens at j name, and their count.
	qualifier := func(j int) (int, int) {
		for k, it := range b.from {
			if it.sub && it.alias < 0 || it.fn && it.alias < 0 {
				continue
			}
			if it.alias >= 0 {
				if c.sameName(c.toks[j], c.toks[it.alias]) {
					return k, 1
				}
				continue
			}
			if c.sameToks(j, it.a, it.b) {
				return k, it.b - it.a
			}
		}
		return -1, 0
	}
	skip := map[int]bool{}
	for _, it := range b.from {
		for j := it.a; j < it.end; j++ {
			skip[j] = true
		}
	}
	var eds []sqlEdit
	for j := b.sel + 1; j < b.end; j++ {
		t := c.toks[j]
		if c.owner[j] != b.idx || skip[j] || !c.isName(t) || isIPunct(c.toks[j-1], ".") {
			continue
		}
		k, n := qualifier(j)
		col := j + n + 1
		if k < 0 || col >= b.end || !isIPunct(c.toks[j+n], ".") || !c.isName(c.toks[col]) {
			continue
		}
		if col+1 < len(c.toks) && (isIPunct(c.toks[col+1], ".") || isIPunct(c.toks[col+1], "(")) {
			continue
		}
		w := strings.ToLower(unquoteIdent(c.toks[col].text))
		if aliases[w] {
			continue
		}
		if len(b.from) > 1 {
			hits := 0
			for _, cs := range colSets {
				if cs[w] {
					hits++
				}
			}
			if hits != 1 || !colSets[k][w] {
				continue
			}
		}
		eds = append(eds, sqlEdit{t.pos, c.toks[col].pos, ""})
		j = col
	}
	if len(eds) == 0 {
		return "", nil, false
	}
	if len(eds) == 1 {
		return "Unqualify 1 column name", eds, true
	}
	return fmt.Sprintf("Unqualify %d column names", len(eds)), eds, true
}

// starItem: the select item is * or q.*.
func (c *intentCtx) starItem(s selItem) bool {
	n := s.b - s.a
	return n == 1 && c.toks[s.a].text == "*" ||
		n == 3 && c.isName(c.toks[s.a]) && isIPunct(c.toks[s.a+1], ".") && c.toks[s.a+2].text == "*"
}

func (c *intentCtx) expandStar() (string, []sqlEdit, bool) {
	b := c.curBlock()
	if b == nil || len(b.from) == 0 {
		return "", nil, false
	}
	k := c.tokAt()
	pick := -1
	for i, s := range b.items {
		if !c.starItem(s) {
			continue
		}
		if pick < 0 || k >= s.a && k < s.b {
			pick = i
		}
	}
	if pick < 0 {
		return "", nil, false
	}
	s := b.items[pick]
	var names []string
	if s.b-s.a == 1 {
		for _, it := range b.from {
			cols, ok := c.colsOf(it)
			if !ok {
				return "", nil, false
			}
			for _, col := range cols {
				if len(b.from) == 1 {
					names = append(names, c.quoteCol(col))
				} else {
					names = append(names, c.qual(it)+"."+c.quoteCol(col))
				}
			}
		}
	} else {
		q := c.toks[s.a]
		found := false
		for _, it := range b.from {
			ref := c.toks[it.b-1]
			if it.alias >= 0 {
				ref = c.toks[it.alias]
			}
			if !c.sameName(q, ref) {
				continue
			}
			cols, ok := c.colsOf(it)
			if !ok {
				return "", nil, false
			}
			for _, col := range cols {
				names = append(names, q.text+"."+c.quoteCol(col))
			}
			found = true
			break
		}
		if !found {
			return "", nil, false
		}
	}
	title := fmt.Sprintf("Expand %s into %d columns", c.text(s.a, s.b), len(names))
	return title, []sqlEdit{{c.toks[s.a].pos, c.toks[s.b-1].end, strings.Join(names, ", ")}}, true
}

func (c *intentCtx) wrapSubquery() (string, []sqlEdit, bool) {
	b := c.curBlock()
	if b == nil {
		return "", nil, false
	}
	k := c.tokAt()
	for _, it := range b.from {
		if it.sub || it.fn || it.colList || k < it.a || k >= it.end {
			continue
		}
		name := c.text(it.a, it.b)
		alias := c.toks[it.b-1].text
		if it.alias >= 0 {
			alias = c.toks[it.alias].text
		}
		repl := "(SELECT * FROM " + name + ") " + alias
		return "Wrap " + name + " in a subquery", []sqlEdit{{c.toks[it.a].pos, c.toks[it.end-1].end, repl}}, true
	}
	return "", nil, false
}

func (c *intentCtx) extractCTE() (string, []sqlEdit, bool) {
	if isIWord(c.toks[0], "with") && len(c.ctes) == 0 || !isIWord(c.toks[0], "with", "select") {
		return "", nil, false
	}
	k := c.tokAt()
	var best *fromItem
	for _, b := range c.blocks {
		for i := range b.from {
			it := &b.from[i]
			if !it.sub || it.colList || k < it.a || k >= it.b {
				continue
			}
			if best == nil || it.b-it.a < best.b-best.a {
				best = it
			}
		}
	}
	if best == nil || best.b-best.a < 3 {
		return "", nil, false
	}
	cl := best.b - 1
	body := c.src[c.toks[best.a+1].pos:c.toks[cl-1].end]
	base := "cte"
	if best.alias >= 0 {
		base = c.toks[best.alias].text
	}
	taken := map[string]bool{}
	for _, d := range c.ctes {
		taken[strings.ToLower(unquoteIdent(c.toks[d.name].text))] = true
	}
	for _, b := range c.blocks {
		for _, it := range b.from {
			if !it.sub && !it.fn {
				taken[strings.ToLower(unquoteIdent(c.toks[it.b-1].text))] = true
			}
		}
	}
	name := base
	switch {
	case best.alias >= 0 && c.toks[best.alias].kind != fkWord:
		// A quoted alias is kept as it is, or nothing is offered.
		if taken[strings.ToLower(unquoteIdent(name))] {
			return "", nil, false
		}
	case best.alias >= 0 && taken[strings.ToLower(name)]:
		name = freeName(base+"_cte", taken)
	default:
		name = freeName(base, taken)
	}
	var eds []sqlEdit
	if best.alias >= 0 && name == c.toks[best.alias].text {
		eds = append(eds, sqlEdit{c.toks[best.a].pos, c.toks[best.alias].end, name})
	} else {
		eds = append(eds, sqlEdit{c.toks[best.a].pos, c.toks[cl].end, name})
	}
	def := name + " AS (" + body + ")"
	if len(c.ctes) == 0 {
		eds = append(eds, sqlEdit{c.toks[0].pos, c.toks[0].pos, "WITH " + def + "\n"})
	} else {
		at := -1
		for _, d := range c.ctes {
			if best.a > d.open && best.a < d.close {
				at = d.name
				break
			}
		}
		if at >= 0 {
			p := c.toks[at].pos
			ind, _ := c.lineIndent(p)
			eds = append(eds, sqlEdit{p, p, def + ",\n" + ind})
		} else {
			p := c.toks[c.ctes[len(c.ctes)-1].close].end
			eds = append(eds, sqlEdit{p, p, ",\n" + def})
		}
	}
	return "Extract subquery as CTE " + name, eds, true
}

func (c *intentCtx) inlineCTE() (string, []sqlEdit, bool) {
	if len(c.ctes) == 0 || c.recursive || c.main <= 0 || c.main >= len(c.toks) {
		return "", nil, false
	}
	k := c.tokAt()
	pick := -1
	for i, d := range c.ctes {
		if k >= d.name && k <= d.close {
			pick = i
		}
	}
	if pick < 0 && k >= 0 {
		for _, b := range c.blocks {
			for _, it := range b.from {
				if it.sub || it.fn || it.b-it.a != 1 || k < it.a || k >= it.end {
					continue
				}
				for i, d := range c.ctes {
					if c.sameName(c.toks[it.a], c.toks[d.name]) {
						pick = i
					}
				}
			}
		}
	}
	if pick < 0 {
		return "", nil, false
	}
	d := c.ctes[pick]
	if d.colList || d.close-d.open < 2 {
		return "", nil, false
	}
	nameTok := c.toks[d.name]
	var ref fromItem
	n := 0
	for _, b := range c.blocks {
		for _, it := range b.from {
			if !it.sub && !it.fn && it.b-it.a == 1 && c.sameName(c.toks[it.a], nameTok) {
				ref, n = it, n+1
			}
		}
	}
	if n != 1 {
		return "", nil, false
	}
	// No other use of the name (IN (TABLE x), x.col after an alias, …).
	for j := d.close + 1; j < len(c.toks); j++ {
		if j >= ref.a && j < ref.end || !c.sameName(c.toks[j], nameTok) {
			continue
		}
		if ref.alias < 0 && j+1 < len(c.toks) && isIPunct(c.toks[j+1], ".") {
			continue // x.col still works: the subquery keeps the name x
		}
		return "", nil, false
	}
	repl := "(" + c.src[c.toks[d.open+1].pos:c.toks[d.close-1].end] + ")"
	if ref.alias < 0 {
		repl += " " + nameTok.text
	}
	eds := []sqlEdit{{c.toks[ref.a].pos, c.toks[ref.a].end, repl}}
	switch {
	case len(c.ctes) == 1:
		eds = append(eds, sqlEdit{c.toks[0].pos, c.toks[c.main].pos, ""})
	case pick < len(c.ctes)-1:
		eds = append(eds, sqlEdit{nameTok.pos, c.toks[c.ctes[pick+1].name].pos, ""})
	default:
		eds = append(eds, sqlEdit{c.toks[c.ctes[pick-1].close].end, c.toks[d.close].end, ""})
	}
	return "Inline CTE " + nameTok.text, eds, true
}

func (c *intentCtx) isLiteral(j int) bool {
	t := c.toks[j]
	switch t.kind {
	case fkString:
		s := t.text
		return s[0] == '\'' || len(s) > 1 && s[1] == '\'' && strings.ContainsRune("NnEe", rune(s[0])) ||
			s[0] == '"' && c.dialect == DialectMySQL
	case fkWord:
		for i := 0; i < len(t.text); i++ {
			if t.text[i] < '0' || t.text[i] > '9' {
				return false
			}
		}
		// 1.5 is three tokens: leave decimals alone.
		if j+1 < len(c.toks) && isIPunct(c.toks[j+1], ".") && c.toks[j+1].pos == t.end {
			return false
		}
		return !(j > 0 && isIPunct(c.toks[j-1], ".") && c.toks[j-1].end == t.pos)
	}
	return false
}

func (c *intentCtx) extractParam() (string, []sqlEdit, bool) {
	k := c.tokAt()
	if k < 0 || c.cur > c.toks[k].end || !c.isLiteral(k) {
		return "", nil, false
	}
	t := c.toks[k]
	base := "value"
	if k >= 2 && (c.toks[k-1].kind == fkOp || isIWord(c.toks[k-1], "like", "ilike")) && c.isName(c.toks[k-2]) &&
		(c.toks[k-2].kind != fkWord || !intentKeyword(c.toks[k-2].text)) {
		base = paramName(unquoteIdent(c.toks[k-2].text))
	}
	taken := map[string]bool{}
	for _, r := range FindParams(c.src, c.driver) {
		taken[strings.ToLower(strings.TrimLeft(r.Name, ":@$?"))] = true
	}
	name := base
	for n := 2; taken[name]; n++ {
		name = base + strconv.Itoa(n)
	}
	// A string is replaced everywhere it stands; a number only here.
	eds := []sqlEdit{{t.pos, t.end, ":" + name}}
	for j, u := range c.toks {
		if t.kind == fkString && j != k && u.kind == fkString && u.text == t.text && c.isLiteral(j) {
			eds = append(eds, sqlEdit{u.pos, u.end, ":" + name})
		}
	}
	lit := t.text
	if len(lit) > 24 {
		lit = lit[:21] + "…"
	}
	title := "Replace " + lit + " with parameter :" + name
	if len(eds) > 1 {
		title += fmt.Sprintf(" (%d places)", len(eds))
	}
	return title, eds, true
}

// paramName makes a parameter name of a column name.
func paramName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "value"
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "p_" + out
	}
	return out
}

var intentAggregates = wordSet(`count sum avg min max array_agg string_agg group_concat bool_and bool_or every
json_agg jsonb_agg json_object_agg jsonb_object_agg listagg stddev variance median mode percentile_cont
percentile_disc xmlagg bit_and bit_or`)

// groupable: the select list of b has items, no aggregate, no window and
// no *.
func (c *intentCtx) groupable(b *qblock) bool {
	if len(b.items) == 0 || b.distinctOn {
		return false
	}
	for _, s := range b.items {
		if c.toks[s.b-1].text == "*" {
			return false
		}
	}
	for j := b.selStart; j < b.selEnd; j++ {
		t := c.toks[j]
		if c.owner[j] != b.idx || t.kind != fkWord {
			continue
		}
		if isIWord(t, "over") || intentAggregates[strings.ToUpper(t.text)] && j+1 < len(c.toks) && isIPunct(c.toks[j+1], "(") {
			return false
		}
	}
	return true
}

func (c *intentCtx) distinctToGroup() (string, []sqlEdit, bool) {
	b := c.curBlock()
	if b == nil || b.distinct < 0 || !c.groupable(b) {
		return "", nil, false
	}
	if _, ok := b.clause["group"]; ok {
		return "", nil, false
	}
	if _, ok := b.clause["having"]; ok {
		return "", nil, false
	}
	var exprs []string
	for _, s := range b.items {
		exprs = append(exprs, c.text(s.a, s.b))
	}
	list := "GROUP BY " + strings.Join(exprs, ", ")
	eds := []sqlEdit{{c.toks[b.sel].end, c.toks[b.distinct].end, ""}}
	at := -1
	for _, w := range []string{"window", "order", "limit", "offset", "fetch", "for"} {
		if j, ok := b.clause[w]; ok && (at < 0 || j < at) {
			at = j
		}
	}
	if at >= 0 {
		p := c.toks[at].pos
		if ind, ok := c.lineIndent(p); ok {
			eds = append(eds, sqlEdit{p, p, list + "\n" + ind})
		} else {
			eds = append(eds, sqlEdit{p, p, list + " "})
		}
	} else {
		p := c.toks[b.end-1].end
		eds = append(eds, sqlEdit{p, p, " " + list})
	}
	return "Replace DISTINCT with GROUP BY", eds, true
}

func (c *intentCtx) groupToDistinct() (string, []sqlEdit, bool) {
	b := c.curBlock()
	if b == nil || b.distinct >= 0 || !c.groupable(b) {
		return "", nil, false
	}
	g, ok := b.clause["group"]
	if !ok {
		return "", nil, false
	}
	if _, ok := b.clause["having"]; ok {
		return "", nil, false
	}
	gEnd := c.nextClause(b, g)
	sel := map[string]bool{}
	alias := map[string]string{}
	var selNorm []string
	for _, s := range b.items {
		n := c.norm(s.a, s.b)
		selNorm = append(selNorm, n)
		sel[n] = true
		if s.alias >= 0 {
			alias[strings.ToLower(unquoteIdent(c.toks[s.alias].text))] = n
		}
	}
	got := map[string]bool{}
	for _, r := range c.splitTop(g+2, gEnd, b.depth) {
		if isIWord(c.toks[r[0]], "rollup", "cube", "grouping") {
			return "", nil, false
		}
		n := c.norm(r[0], r[1])
		if r[1]-r[0] == 1 {
			t := c.toks[r[0]]
			if i, err := strconv.Atoi(t.text); err == nil {
				if i < 1 || i > len(selNorm) {
					return "", nil, false
				}
				n = selNorm[i-1]
			} else if a, ok := alias[strings.ToLower(unquoteIdent(t.text))]; ok && !sel[n] {
				n = a
			}
		}
		got[n] = true
	}
	if len(got) != len(sel) {
		return "", nil, false
	}
	for n := range sel {
		if !got[n] {
			return "", nil, false
		}
	}
	eds := []sqlEdit{
		{c.toks[g-1].end, c.toks[gEnd-1].end, ""},
		{c.toks[b.sel].end, c.toks[b.sel].end, " DISTINCT"},
	}
	return "Replace GROUP BY with DISTINCT", eds, true
}
