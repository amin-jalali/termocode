package dbclient

import (
	"fmt"
	"sort"
	"strings"
)

// DBX-46: a schema folder (SchemaObjectFiles, or DDL written by hand) read
// back as a CanonSchema. Our own small parser: it reads CREATE TABLE,
// CREATE INDEX, CREATE VIEW and CREATE FUNCTION/PROCEDURE, which is what
// the folder holds. It feeds SQL completion (CanonCompletion) and the
// folder-to-database migration script (SchemaFolderMigration). Anything
// it cannot read becomes a warning; nothing here runs SQL.

// ParseSchemaFolder reads files (slash name → text, as ReadSchemaFolder
// returns them) as one schema of driver. Files under views/ and routines/
// hold one object each; any other file is a list of statements.
func ParseSchemaFolder(driver, schema string, files map[string]string) (CanonSchema, []string) {
	driver = normDriver(driver)
	if driver == "postgresql" {
		driver = "postgres"
	}
	p := &folderParser{driver: driver, out: CanonSchema{Driver: driver, Schema: schema}, tables: map[string]int{}}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		text := files[name]
		switch {
		case strings.HasPrefix(name, "views/"), strings.HasPrefix(name, "routines/"):
			if stmt := wholeStatement(text); stmt != "" {
				p.statement(name, stmt)
			}
		default:
			for _, stmt := range splitStatements(blankComments(text)) {
				p.statement(name, stmt)
			}
		}
	}
	for _, ix := range p.indexes {
		i, ok := p.tables[ix.ix.Table]
		if !ok {
			p.warn(ix.file, "index "+ix.ix.Name+" is on table "+ix.ix.Table+", which the folder does not have")
			continue
		}
		p.out.Tables[i].Indexes = append(p.out.Tables[i].Indexes, ix.ix)
	}
	return p.out, p.warns
}

// SchemaFolderMigration is the script that makes the database match the
// folder for diffs (CompareSchemaFolder rows): each row's folder text is
// the source and its live text the target. Lines the parser could not
// read come first, as REVIEW notes. Text only: nothing runs it.
func SchemaFolderMigration(driver, schema, folderLabel string, diffs []SchemaFileDiff) string {
	folder, live := map[string]string{}, map[string]string{}
	for _, d := range diffs {
		if d.Folder != "" {
			folder[d.Name] = d.Folder
		}
		if d.Live != "" {
			live[d.Name] = d.Live
		}
	}
	src, w1 := ParseSchemaFolder(driver, "", folder)
	src.Label = folderLabel
	tgt, w2 := ParseSchemaFolder(driver, schema, live)
	var b strings.Builder
	for _, w := range append(w1, w2...) {
		b.WriteString("-- REVIEW: " + schemaOneLine(w) + "\n")
	}
	b.WriteString(CompareSchemas(src, tgt).Script)
	return b.String()
}

func schemaOneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// CanonCompletion is s as a completion source: every table, view and
// routine, the columns with their types and the foreign keys.
func CanonCompletion(s CanonSchema) CompletionSchema {
	out := CompletionSchema{Cols: map[string][]Column{}}
	for _, t := range s.Tables {
		out.Tables = append(out.Tables, CompletionTable{Schema: s.Schema, Name: t.Name, Kind: "table"})
		key := strings.ToLower(t.Name)
		for _, c := range t.Columns {
			out.Cols[key] = append(out.Cols[key], Column{Name: c.Name, Type: c.Type, Table: t.Name})
		}
		for _, c := range t.Constraints {
			if c.Kind != "FOREIGN KEY" {
				continue
			}
			for i, col := range c.Columns {
				fk := CompletionFK{Table: key, Column: col, RefTable: strings.ToLower(c.RefTable)}
				if i < len(c.RefColumns) {
					fk.RefColumn = c.RefColumns[i]
				}
				out.FKs = append(out.FKs, fk)
			}
		}
	}
	for _, v := range s.Views {
		out.Tables = append(out.Tables, CompletionTable{Schema: s.Schema, Name: v.Name, Kind: "view"})
	}
	for _, r := range s.Routines {
		out.Tables = append(out.Tables, CompletionTable{Schema: s.Schema, Name: r.Name, Kind: "func"})
	}
	return out
}

type folderIndex struct {
	file string
	ix   CanonIndex
}

type folderParser struct {
	driver  string
	out     CanonSchema
	tables  map[string]int // table name -> index in out.Tables
	indexes []folderIndex
	warns   []string
}

func (p *folderParser) warn(file, msg string) {
	p.warns = append(p.warns, file+": "+msg)
}

// statement reads one statement (no trailing semicolon).
func (p *folderParser) statement(file, stmt string) {
	stmt = strings.TrimSpace(stmt)
	if stmt == "" {
		return
	}
	rest, ok := eatWords(stmt, "CREATE")
	if !ok {
		p.warn(file, "not a CREATE statement: "+clip(stmt))
		return
	}
	if r, ok := eatWords(rest, "OR", "REPLACE"); ok {
		rest = r
	}
	for _, w := range []string{"TEMPORARY", "TEMP"} {
		if r, ok := eatWords(rest, w); ok {
			rest = r
		}
	}
	var err error
	switch {
	case hasWords(rest, "TABLE"):
		r, _ := eatWords(rest, "TABLE")
		err = p.table(r)
	case hasWords(rest, "UNIQUE", "INDEX"):
		r, _ := eatWords(rest, "UNIQUE", "INDEX")
		err = p.index(file, stmt, r, true, "")
	case hasWords(rest, "FULLTEXT", "INDEX"), hasWords(rest, "SPATIAL", "INDEX"):
		method := asciiUpper(firstWord(rest))
		r, _ := eatWords(rest, method, "INDEX")
		err = p.index(file, stmt, r, false, method)
	case hasWords(rest, "INDEX"):
		r, _ := eatWords(rest, "INDEX")
		err = p.index(file, stmt, r, false, "")
	case hasWords(rest, "MATERIALIZED", "VIEW"):
		r, _ := eatWords(rest, "MATERIALIZED", "VIEW")
		err = p.view(stmt, r, true)
	case hasWords(rest, "VIEW"):
		r, _ := eatWords(rest, "VIEW")
		err = p.view(stmt, r, false)
	case hasWords(rest, "FUNCTION"):
		r, _ := eatWords(rest, "FUNCTION")
		err = p.routine("FUNCTION", r)
	case hasWords(rest, "PROCEDURE"):
		r, _ := eatWords(rest, "PROCEDURE")
		err = p.routine("PROCEDURE", r)
	default:
		err = fmt.Errorf("cannot read %s", clip(stmt))
	}
	if err != nil {
		p.warn(file, err.Error())
	}
}

func clip(s string) string {
	r := []rune(schemaOneLine(s))
	if len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return string(r)
}

func skipIfNotExists(s string) string {
	if r, ok := eatWords(s, "IF", "NOT", "EXISTS"); ok {
		return r
	}
	return s
}

func (p *folderParser) table(rest string) error {
	name, rest, ok := readIdent(skipIfNotExists(rest))
	if !ok {
		return fmt.Errorf("a CREATE TABLE without a name")
	}
	body, _, ok := parenBody(rest)
	if !ok {
		return fmt.Errorf("table %s has no column list", name)
	}
	t := CanonTable{Name: name}
	for _, part := range splitTop(body, ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if c, ok := tableConstraint(part); ok {
			t.Constraints = append(t.Constraints, c)
			continue
		}
		col, pk, ok := tableColumn(part)
		if !ok {
			return fmt.Errorf("table %s: cannot read %s", name, clip(part))
		}
		t.Columns = append(t.Columns, col)
		if pk {
			t.Constraints = append(t.Constraints, CanonConstraint{Kind: "PRIMARY KEY", Columns: []string{col.Name}})
		}
	}
	if i, dup := p.tables[name]; dup {
		p.out.Tables[i] = t
		return fmt.Errorf("table %s is in the folder twice; the last one counts", name)
	}
	p.tables[name] = len(p.out.Tables)
	p.out.Tables = append(p.out.Tables, t)
	return nil
}

// tableConstraint reads [CONSTRAINT name] PRIMARY KEY | UNIQUE | FOREIGN
// KEY | CHECK; ok is false for a column.
func tableConstraint(part string) (CanonConstraint, bool) {
	var c CanonConstraint
	rest := part
	if r, ok := eatWords(rest, "CONSTRAINT"); ok {
		name, r2, ok := readIdent(r)
		if !ok {
			return c, false
		}
		c.Name, rest = name, r2
	}
	switch {
	case hasWords(rest, "PRIMARY", "KEY"):
		r, _ := eatWords(rest, "PRIMARY", "KEY")
		c.Kind = "PRIMARY KEY"
		c.Columns, _ = identList(r)
	case hasWords(rest, "UNIQUE"):
		r, _ := eatWords(rest, "UNIQUE")
		if r2, ok := eatWords(r, "KEY"); ok {
			r = r2
		}
		c.Kind = "UNIQUE"
		c.Columns, _ = identList(r)
	case hasWords(rest, "CHECK"):
		c.Kind = "CHECK"
		c.Check = strings.TrimSpace(rest)
	case hasWords(rest, "FOREIGN", "KEY"):
		r, _ := eatWords(rest, "FOREIGN", "KEY")
		c.Kind = "FOREIGN KEY"
		cols, r := identList(r)
		c.Columns = cols
		r, ok := eatWords(r, "REFERENCES")
		if !ok {
			return c, false
		}
		ref, r, ok := readIdent(r)
		if !ok {
			return c, false
		}
		c.RefTable = ref
		if strings.HasPrefix(strings.TrimSpace(r), "(") {
			c.RefColumns, r = identList(r)
		}
		c.OnUpdate, c.OnDelete = schemaFKActions(r)
	default:
		return c, false
	}
	return c, true
}

// schemaFKActions reads "ON UPDATE x ON DELETE y" in any order.
func schemaFKActions(s string) (onUpdate, onDelete string) {
	up := asciiUpper(s)
	u, d := strings.Index(up, "ON UPDATE "), strings.Index(up, "ON DELETE ")
	cut := func(from, other int) string {
		end := len(s)
		if other > from {
			end = other
		}
		return strings.TrimSpace(s[from:end])
	}
	if u >= 0 {
		onUpdate = cut(u+len("ON UPDATE "), d)
	}
	if d >= 0 {
		onDelete = cut(d+len("ON DELETE "), u)
	}
	return onUpdate, onDelete
}

// columnExtras start the Extra part of a column (canonWriter.column puts
// it last).
var columnExtras = []string{" AUTO_INCREMENT", " ON UPDATE ", " GENERATED "}

// tableColumn reads name type [NOT NULL] [DEFAULT x] [extra]; pk is an
// inline PRIMARY KEY (hand-written DDL).
func tableColumn(part string) (CanonColumn, bool, bool) {
	name, rest, ok := readIdent(part)
	if !ok {
		return CanonColumn{}, false, false
	}
	r := " " + strings.TrimSpace(rest)
	ex := len(r)
	for _, kw := range columnExtras {
		if i := indexTopFold(r, kw); i >= 0 && i < ex {
			ex = i
		}
	}
	head, extra := r[:ex], strings.TrimSpace(r[ex:])
	col := CanonColumn{Name: name, Extra: extra}
	if i := indexTopFold(head, " DEFAULT "); i >= 0 {
		col.Default = strings.TrimSpace(head[i+len(" DEFAULT "):])
		head = head[:i]
	}
	if i := indexTopFold(head, " NOT NULL"); i >= 0 {
		col.NotNull = true
		head = head[:i] + head[i+len(" NOT NULL"):]
	}
	pk := false
	if i := indexTopFold(head, " PRIMARY KEY"); i >= 0 {
		pk = true
		head = head[:i] + head[i+len(" PRIMARY KEY"):]
	}
	col.Type = strings.TrimSpace(head)
	return col, pk, true
}

func (p *folderParser) index(file, stmt, rest string, unique bool, method string) error {
	name, rest, ok := readIdent(skipIfNotExists(rest))
	if !ok {
		return fmt.Errorf("a CREATE INDEX without a name")
	}
	rest, ok = eatWords(rest, "ON")
	if !ok {
		return fmt.Errorf("index %s has no ON table", name)
	}
	table, rest, ok := readIdent(rest)
	if !ok {
		return fmt.Errorf("index %s has no table", name)
	}
	ix := CanonIndex{Table: table, Name: name, Unique: unique, Method: method}
	if r, ok := eatWords(rest, "USING"); ok {
		ix.Method = firstWord(r)
		rest = strings.TrimSpace(r)[len(ix.Method):]
	}
	body, rest, ok := parenBody(rest)
	if !ok {
		return fmt.Errorf("index %s has no column list", name)
	}
	for _, e := range splitTop(body, ',') {
		ix.Exprs = append(ix.Exprs, strings.TrimSpace(e))
	}
	if r, ok := eatWords(rest, "USING"); ok {
		ix.Method = firstWord(r)
		rest = strings.TrimSpace(r)[len(ix.Method):]
	}
	if r, ok := eatWords(rest, "WHERE"); ok {
		ix.Where = strings.TrimSpace(r)
	}
	if p.driver == "sqlite" {
		raw := ix.Where != ""
		for _, e := range ix.Exprs {
			if _, r, ok := readIdent(e); !ok || strings.TrimSpace(r) != "" || !strings.HasPrefix(e, `"`) {
				raw = true
			}
		}
		if raw {
			ix.Raw = strings.TrimSpace(stmt)
		}
	}
	p.indexes = append(p.indexes, folderIndex{file: file, ix: ix})
	return nil
}

func (p *folderParser) view(stmt, rest string, materialized bool) error {
	name, rest, ok := readIdent(skipIfNotExists(rest))
	if !ok {
		return fmt.Errorf("a CREATE VIEW without a name")
	}
	v := CanonView{Name: name, Materialized: materialized}
	if p.driver == "sqlite" {
		v.Definition, v.Full = strings.TrimRight(stmt, "; \n\t\r"), true
	} else {
		t := strings.TrimLeft(rest, " \t\r\n")
		if !hasWords(t, "AS") {
			return fmt.Errorf("view %s has no AS", name)
		}
		// The body stays as written: only the one line break after AS goes.
		body := t[len("AS"):]
		switch {
		case strings.HasPrefix(body, "\r\n"):
			body = body[2:]
		case strings.HasPrefix(body, "\n"), strings.HasPrefix(body, " "):
			body = body[1:]
		}
		v.Definition = body
	}
	p.out.Views = append(p.out.Views, v)
	return nil
}

func (p *folderParser) routine(kind, rest string) error {
	name, rest, ok := readIdent(skipIfNotExists(rest))
	if !ok {
		return fmt.Errorf("a CREATE %s without a name", kind)
	}
	args, after, ok := parenBody(rest)
	if !ok {
		return fmt.Errorf("%s %s has no argument list", strings.ToLower(kind), name)
	}
	r := CanonRoutine{Name: name, Kind: kind, Args: args}
	if p.driver == "postgres" {
		lang := indexTopFold(after, " LANGUAGE ")
		if lang < 0 {
			return fmt.Errorf("function %s has no LANGUAGE", name)
		}
		if ret, ok := eatWords(after[:lang], "RETURNS"); ok {
			r.Returns = strings.TrimSpace(ret)
		}
		tail := strings.TrimSpace(after[lang+len(" LANGUAGE "):])
		r.Language = firstWord(tail)
		tail, ok := eatWords(tail[len(r.Language):], "AS")
		if !ok {
			return fmt.Errorf("function %s has no AS body", name)
		}
		if tag, ok := schemaDollarTag(tail, 0); ok && strings.HasSuffix(tail, tag) && len(tail) >= 2*len(tag) {
			body := tail[len(tag) : len(tail)-len(tag)]
			body = strings.TrimPrefix(body, "\n")
			r.Body = strings.TrimSuffix(body, "\n")
		} else {
			r.Body = tail
		}
	} else {
		header, body := after, ""
		if i := strings.IndexByte(after, '\n'); i >= 0 {
			header, body = after[:i], after[i+1:]
		}
		if ret, ok := eatWords(header, "RETURNS"); ok {
			r.Returns = strings.TrimSpace(ret)
		}
		r.Language, r.Body = "SQL", body
	}
	p.out.Routines = append(p.out.Routines, r)
	return nil
}

// --- lexing ---

// sqlMask marks each byte of s: its paren depth (>= 0) outside quotes
// and comments, maskQuote inside a quoted string or name (delimiters
// too) or a dollar-quoted body, maskComment inside a comment.
const (
	maskQuote   = -1
	maskComment = -2
)

func sqlMask(s string) []int {
	d := make([]int, len(s))
	depth := 0
	fill := func(from, to, v int) int {
		for k := from; k < to; k++ {
			d[k] = v
		}
		return to
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '-' && i+1 < len(s) && s[i+1] == '-':
			end := len(s)
			if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
				end = i + j
			}
			i = fill(i, end, maskComment)
			continue
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			end := len(s)
			if j := strings.Index(s[i+2:], "*/"); j >= 0 {
				end = i + 2 + j + 2
			}
			i = fill(i, end, maskComment)
			continue
		case c == '\'' || c == '"' || c == '`':
			i = fill(i, quoteEnd(s, i), maskQuote)
			continue
		case c == '$':
			if tag, ok := schemaDollarTag(s, i); ok {
				end := len(s)
				if j := strings.Index(s[i+len(tag):], tag); j >= 0 {
					end = i + len(tag) + j + len(tag)
				}
				i = fill(i, end, maskQuote)
				continue
			}
		case c == '(':
			d[i] = depth
			depth++
			i++
			continue
		case c == ')':
			if depth > 0 {
				depth--
			}
			d[i] = depth
			i++
			continue
		}
		d[i] = depth
		i++
	}
	return d
}

// quoteEnd is the index after the quote that closes the one at s[i]; a
// doubled quote is part of the text.
func quoteEnd(s string, i int) int {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		if s[j] == q {
			if j+1 < len(s) && s[j+1] == q {
				j++
				continue
			}
			return j + 1
		}
	}
	return len(s)
}

// schemaDollarTag is the $tag$ (or $$) that starts at s[i].
func schemaDollarTag(s string, i int) (string, bool) {
	if i >= len(s) || s[i] != '$' {
		return "", false
	}
	for j := i + 1; j < len(s); j++ {
		c := s[j]
		switch {
		case c == '$':
			return s[i : j+1], true
		case c >= '0' && c <= '9':
			if j == i+1 {
				return "", false // $1 is a parameter
			}
		case identByte(c):
		default:
			return "", false
		}
	}
	return "", false
}

// blankComments replaces every comment byte with a space (a newline stays).
func blankComments(s string) string {
	m := sqlMask(s)
	b := []byte(s)
	for i := range b {
		if m[i] == maskComment && b[i] != '\n' {
			b[i] = ' '
		}
	}
	return string(b)
}

// splitStatements cuts s at every top-level semicolon.
func splitStatements(s string) []string {
	var out []string
	for _, part := range splitTop(s, ';') {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return out
}

// wholeStatement is a one-object file: leading comments and the last
// semicolon go, the rest stays as written.
func wholeStatement(s string) string {
	m := sqlMask(s)
	start := 0
	for start < len(s) && (m[start] == maskComment || s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n') {
		start++
	}
	s = strings.TrimRight(s[start:], " \t\r\n")
	return strings.TrimSuffix(s, ";")
}

// splitTop cuts s at sep where it is outside parens, quotes and comments.
func splitTop(s string, sep byte) []string {
	m := sqlMask(s)
	var out []string
	from := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep && m[i] == 0 {
			out = append(out, s[from:i])
			from = i + 1
		}
	}
	return append(out, s[from:])
}

func asciiUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// indexTopFold finds kw (upper case) in s at paren depth 0, any case,
// not inside quotes or comments, and not followed by a name character.
func indexTopFold(s, kw string) int {
	up, m := asciiUpper(s), sqlMask(s)
	for from := 0; from <= len(s)-len(kw); {
		k := strings.Index(up[from:], kw)
		if k < 0 {
			return -1
		}
		k += from
		good := true
		for j := k; j < k+len(kw); j++ {
			if m[j] != 0 {
				good = false
				break
			}
		}
		if end := k + len(kw); good && kw[len(kw)-1] != ' ' && end < len(s) && identByte(s[end]) {
			good = false
		}
		if good {
			return k
		}
		from = k + 1
	}
	return -1
}

// firstWord is the leading run of name characters of s (after spaces).
func firstWord(s string) string {
	s = strings.TrimLeft(s, " \t\r\n")
	j := 0
	for j < len(s) && identByte(s[j]) {
		j++
	}
	return s[:j]
}

// hasWords reports whether s starts with words (any case).
func hasWords(s string, words ...string) bool {
	_, ok := eatWords(s, words...)
	return ok
}

// eatWords takes words (any case) off the start of s.
func eatWords(s string, words ...string) (string, bool) {
	for _, w := range words {
		s = strings.TrimLeft(s, " \t\r\n")
		if len(s) < len(w) || asciiUpper(s[:len(w)]) != w || (len(s) > len(w) && identByte(s[len(w)])) {
			return s, false
		}
		s = s[len(w):]
	}
	return strings.TrimLeft(s, " \t\r\n"), true
}

// readIdent reads a name ("quoted", `quoted` or bare) off the start of s;
// a qualified name gives its last part.
func readIdent(s string) (name, rest string, ok bool) {
	s = strings.TrimLeft(s, " \t\r\n")
	for {
		if s == "" {
			return name, s, ok
		}
		switch q := s[0]; {
		case q == '"' || q == '`':
			end := quoteEnd(s, 0)
			if end < 2 || s[end-1] != q {
				return name, s, false
			}
			name = strings.ReplaceAll(s[1:end-1], string(q)+string(q), string(q))
			s = s[end:]
		default:
			j := 0
			for j < len(s) && identByte(s[j]) {
				j++
			}
			if j == 0 {
				return name, s, ok
			}
			name = s[:j]
			s = s[j:]
		}
		ok = true
		if !strings.HasPrefix(s, ".") {
			return name, s, true
		}
		s = s[1:]
	}
}

// parenBody is the text inside the parens that open s (after spaces) and
// what follows the closing one.
func parenBody(s string) (body, rest string, ok bool) {
	s = strings.TrimLeft(s, " \t\r\n")
	if !strings.HasPrefix(s, "(") {
		return "", s, false
	}
	m := sqlMask(s)
	for i := 1; i < len(s); i++ {
		if s[i] == ')' && m[i] == 0 {
			return s[1:i], s[i+1:], true
		}
	}
	return "", s, false
}

// identList reads "(a, b)" as names.
func identList(s string) ([]string, string) {
	body, rest, ok := parenBody(s)
	if !ok {
		return nil, s
	}
	var out []string
	for _, part := range splitTop(body, ',') {
		if name, _, ok := readIdent(part); ok {
			out = append(out, name)
		}
	}
	return out, rest
}
