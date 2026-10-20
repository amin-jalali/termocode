package dbclient

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Schema compare (DB-15): one schema read whole from the catalogs and
// written out as canonical DDL, one text per object kind. "Canonical"
// means: objects sorted by name, identifiers quoted for the dialect,
// types normalised, whitespace normalised, no schema qualifiers. Two
// schemas that are the same give the same text, so the diff of the two
// texts is the schema difference and is stable from run to run.
//
// The reads are plain catalog SELECTs: the caller runs them on a
// read-only session. Nothing here writes.

// Bounds of one schema read. A catalog query stops at compareMaxRows
// rows; a schema with more tables than CompareMaxTables keeps the first
// ones by name and says so (Truncated).
const (
	CompareMaxTables = 2000
	compareMaxRows   = 100000
)

// CanonSchema is one schema as the compare sees it.
type CanonSchema struct {
	Driver    string // postgres, mysql or sqlite
	Schema    string // the schema (PostgreSQL), database (MySQL) or attached name (SQLite)
	Label     string // how the script header names it ("conn · db · schema"); "" is Schema
	Tables    []CanonTable
	Views     []CanonView
	Routines  []CanonRoutine
	Truncated bool // over the bounds: only part of the schema is in
}

// CanonColumn is one column of a table.
type CanonColumn struct {
	Name, Type string
	NotNull    bool
	Default    string // SQL text; "" means none
	Extra      string // AUTO_INCREMENT, identity … (SQL text)
}

// CanonConstraint is a table constraint. Kind is PRIMARY KEY, UNIQUE,
// FOREIGN KEY or CHECK.
type CanonConstraint struct {
	Kind, Name         string
	Columns            []string
	RefTable           string
	RefColumns         []string
	OnUpdate, OnDelete string // "" or NO ACTION: the default
	Check              string // CHECK (…) text
}

// CanonIndex is an index that is not a constraint's own. Exprs are SQL
// text (quoted names or expressions).
type CanonIndex struct {
	Table, Name string
	Exprs       []string
	Unique      bool
	Method      string // "" is the default method
	Where       string // partial index predicate
	Raw         string // SQLite: the CREATE INDEX text, when it is an expression or partial index
}

// CanonTable is one table with its columns, constraints and indexes.
type CanonTable struct {
	Name        string
	Columns     []CanonColumn
	Constraints []CanonConstraint
	Indexes     []CanonIndex
}

// CanonView is a view. Full means Definition is the whole CREATE VIEW
// text (SQLite keeps it that way).
type CanonView struct {
	Name, Definition string
	Materialized     bool
	Full             bool
}

// CanonRoutine is a function or procedure.
type CanonRoutine struct {
	Name, Kind, Args, Returns, Language, Body string
}

// ReadCanonSchema reads schema through sess (which should be read-only)
// for the dialect of driver.
func ReadCanonSchema(ctx context.Context, sess Session, driver, schema string) (CanonSchema, error) {
	driver = normDriver(driver)
	if driver == "postgresql" {
		driver = "postgres"
	}
	out := CanonSchema{Driver: driver, Schema: schema}
	if sess == nil {
		return out, errors.New("not connected")
	}
	r := &canonReader{ctx: ctx, sess: sess, out: &out, tables: map[string]*CanonTable{}}
	var err error
	switch driver {
	case "postgres":
		err = r.readPostgres(schema)
	case "mysql":
		err = r.readMySQL(schema)
	case "sqlite":
		err = r.readSQLite(schema)
	default:
		return out, fmt.Errorf("schema compare does not support %s", driver)
	}
	if err != nil {
		return out, err
	}
	r.finish()
	return out, nil
}

type canonReader struct {
	ctx    context.Context
	sess   Session
	out    *CanonSchema
	tables map[string]*CanonTable
	order  []string
	cut    string // tables >= cut were dropped (a query stopped early)
}

func (r *canonReader) query(sql string, args ...any) ([][]Cell, error) {
	res := r.sess.Run(r.ctx, sql, RunOptions{MaxRows: compareMaxRows, Args: args})
	if res.Err != nil {
		return nil, res.Err
	}
	if res.Truncated {
		r.out.Truncated = true
		// The last table of a cut-off answer may be incomplete: drop it
		// and everything after it.
		if n := len(res.Rows); n > 0 && len(res.Rows[n-1]) > 0 {
			last := canonText(res.Rows[n-1][0])
			if r.cut == "" || last < r.cut {
				r.cut = last
			}
		}
	}
	return res.Rows, nil
}

// table returns the table name (made on first use).
func (r *canonReader) table(name string) *CanonTable {
	if t, ok := r.tables[name]; ok {
		return t
	}
	t := &CanonTable{Name: name}
	r.tables[name] = t
	r.order = append(r.order, name)
	return t
}

// known is the table when the column query saw it (constraints and
// indexes of other relations are ignored).
func (r *canonReader) known(name string) *CanonTable { return r.tables[name] }

func (r *canonReader) finish() {
	names := make([]string, 0, len(r.tables))
	for n := range r.tables {
		if r.cut != "" && n >= r.cut {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > CompareMaxTables {
		names = names[:CompareMaxTables]
		r.out.Truncated = true
	}
	for _, n := range names {
		r.out.Tables = append(r.out.Tables, *r.tables[n])
	}
	if len(r.out.Views) > CompareMaxTables {
		r.out.Views = r.out.Views[:CompareMaxTables]
		r.out.Truncated = true
	}
	if len(r.out.Routines) > CompareMaxTables {
		r.out.Routines = r.out.Routines[:CompareMaxTables]
		r.out.Truncated = true
	}
	*r.out = sortCanon(*r.out)
}

func constraintRank(kind string) int {
	switch kind {
	case "PRIMARY KEY":
		return 0
	case "UNIQUE":
		return 1
	case "FOREIGN KEY":
		return 2
	}
	return 3
}

// canonText is a catalog cell as one clean line of server text: control
// characters (escape sequences) are dropped.
func canonText(c Cell) string {
	if c.Null {
		return ""
	}
	return canonClean(c.Text, false)
}

// canonBody is server text that may span lines (view and routine
// bodies): control characters other than newline and tab are dropped,
// line ends are normalised and trailing blanks trimmed.
func canonBody(c Cell) string {
	if c.Null {
		return ""
	}
	return canonClean(c.Text, true)
}

func canonClean(s string, multiline bool) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' && multiline:
			b.WriteRune(r)
		case r == '\t':
			b.WriteString("    ")
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			if r == '\n' || r == '\r' {
				b.WriteByte(' ')
			}
		default:
			b.WriteRune(r)
		}
	}
	if !multiline {
		return strings.TrimSpace(b.String())
	}
	lines := strings.Split(b.String(), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n ")
}

func canonCell(row []Cell, i int) Cell {
	if i < len(row) {
		return row[i]
	}
	return Cell{Null: true}
}

func cellTrue(c Cell) bool {
	switch strings.ToLower(strings.TrimSpace(c.Text)) {
	case "true", "t", "1", "yes", "y":
		return !c.Null
	}
	return false
}

// ── PostgreSQL ─────────────────────────────────────────────────────────

const pgCanonColumns = `SELECT c.relname, a.attname, format_type(a.atttypid, a.atttypmod), a.attnotnull, pg_get_expr(d.adbin, d.adrelid), a.attidentity::text
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = a.attnum
WHERE n.nspname = $1 AND c.relkind IN ('r', 'p')
ORDER BY c.relname, a.attnum`

const pgCanonConstraints = `SELECT c.relname, con.conname, con.contype::text,
 (SELECT string_agg(a.attname, E'\x1f' ORDER BY k.o) FROM unnest(con.conkey) WITH ORDINALITY AS k(n, o) JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.n),
 rc.relname,
 (SELECT string_agg(a.attname, E'\x1f' ORDER BY k.o) FROM unnest(con.confkey) WITH ORDINALITY AS k(n, o) JOIN pg_attribute a ON a.attrelid = con.confrelid AND a.attnum = k.n),
 con.confupdtype::text, con.confdeltype::text,
 CASE WHEN con.contype = 'c' THEN pg_get_constraintdef(con.oid, true) END
FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_class rc ON rc.oid = con.confrelid
WHERE n.nspname = $1 AND con.contype IN ('p', 'u', 'f', 'c') AND c.relkind IN ('r', 'p')
ORDER BY c.relname, con.conname`

const pgCanonIndexes = `SELECT t.relname, i.relname, ix.indisunique, am.amname,
 (SELECT string_agg(pg_get_indexdef(ix.indexrelid, k.o, true), E'\x1f' ORDER BY k.o) FROM generate_series(1, ix.indnkeyatts) AS k(o)),
 pg_get_expr(ix.indpred, ix.indrelid, true)
FROM pg_index ix JOIN pg_class t ON t.oid = ix.indrelid JOIN pg_class i ON i.oid = ix.indexrelid
JOIN pg_namespace n ON n.oid = t.relnamespace JOIN pg_am am ON am.oid = i.relam
WHERE n.nspname = $1 AND t.relkind IN ('r', 'p')
 AND NOT EXISTS (SELECT 1 FROM pg_constraint con WHERE con.conindid = ix.indexrelid AND con.conrelid = ix.indrelid AND con.contype IN ('p', 'u', 'x'))
ORDER BY t.relname, i.relname`

const pgCanonViews = `SELECT c.relname, c.relkind::text, pg_get_viewdef(c.oid, true)
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relkind IN ('v', 'm')
ORDER BY c.relname`

const pgCanonRoutines = `SELECT p.proname, p.prokind::text, pg_get_function_identity_arguments(p.oid), COALESCE(pg_get_function_result(p.oid), ''), l.lanname, p.prosrc
FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace JOIN pg_language l ON l.oid = p.prolang
WHERE n.nspname = $1 AND p.prokind IN ('f', 'p')
 AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid = 'pg_proc'::regclass AND d.objid = p.oid AND d.deptype = 'e')
ORDER BY p.proname, 3`

var pgFKAction = map[string]string{"r": "RESTRICT", "c": "CASCADE", "n": "SET NULL", "d": "SET DEFAULT"}

func (r *canonReader) readPostgres(schema string) error {
	rows, err := r.query(pgCanonColumns, schema)
	if err != nil {
		return err
	}
	for _, row := range rows {
		t := r.table(canonText(canonCell(row, 0)))
		col := CanonColumn{Name: canonText(canonCell(row, 1)), Type: canonText(canonCell(row, 2)), NotNull: cellTrue(canonCell(row, 3)), Default: canonText(canonCell(row, 4))}
		switch canonText(canonCell(row, 5)) {
		case "a":
			col.Extra = "GENERATED ALWAYS AS IDENTITY"
		case "d":
			col.Extra = "GENERATED BY DEFAULT AS IDENTITY"
		}
		t.Columns = append(t.Columns, col)
	}
	if rows, err = r.query(pgCanonConstraints, schema); err != nil {
		return err
	}
	for _, row := range rows {
		t := r.known(canonText(canonCell(row, 0)))
		if t == nil {
			continue
		}
		c := CanonConstraint{Name: canonText(canonCell(row, 1)), Columns: splitUnit(canonText(canonCell(row, 3)))}
		switch canonText(canonCell(row, 2)) {
		case "p":
			c.Kind = "PRIMARY KEY"
		case "u":
			c.Kind = "UNIQUE"
		case "f":
			c.Kind = "FOREIGN KEY"
			c.RefTable = canonText(canonCell(row, 4))
			c.RefColumns = splitUnit(canonText(canonCell(row, 5)))
			c.OnUpdate = pgFKAction[canonText(canonCell(row, 6))]
			c.OnDelete = pgFKAction[canonText(canonCell(row, 7))]
		case "c":
			c.Kind = "CHECK"
			c.Columns = nil
			c.Check = canonText(canonCell(row, 8))
		default:
			continue
		}
		t.Constraints = append(t.Constraints, c)
	}
	if rows, err = r.query(pgCanonIndexes, schema); err != nil {
		return err
	}
	for _, row := range rows {
		t := r.known(canonText(canonCell(row, 0)))
		if t == nil {
			continue
		}
		ix := CanonIndex{Table: t.Name, Name: canonText(canonCell(row, 1)), Unique: cellTrue(canonCell(row, 2)), Exprs: splitUnit(canonText(canonCell(row, 4))), Where: canonText(canonCell(row, 5))}
		if m := canonText(canonCell(row, 3)); m != "btree" {
			ix.Method = m
		}
		t.Indexes = append(t.Indexes, ix)
	}
	if rows, err = r.query(pgCanonViews, schema); err != nil {
		return err
	}
	for _, row := range rows {
		def := stripSchemaPrefix(canonBody(canonCell(row, 2)), schema, PostgresDialect{}.QuoteIdent)
		r.out.Views = append(r.out.Views, CanonView{Name: canonText(canonCell(row, 0)), Materialized: canonText(canonCell(row, 1)) == "m", Definition: strings.TrimRight(def, "; \n")})
	}
	if rows, err = r.query(pgCanonRoutines, schema); err != nil {
		return err
	}
	for _, row := range rows {
		kind := "FUNCTION"
		if canonText(canonCell(row, 1)) == "p" {
			kind = "PROCEDURE"
		}
		r.out.Routines = append(r.out.Routines, CanonRoutine{Name: canonText(canonCell(row, 0)), Kind: kind, Args: canonText(canonCell(row, 2)), Returns: canonText(canonCell(row, 3)), Language: canonText(canonCell(row, 4)), Body: canonBody(canonCell(row, 5))})
	}
	return nil
}

func splitUnit(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x1f")
}

// stripSchemaPrefix drops "schema." qualifiers from server text (a view
// body), so two schemas with different names compare equal.
func stripSchemaPrefix(text, schema string, quote func(string) string) string {
	if schema == "" {
		return text
	}
	text = strings.ReplaceAll(text, quote(schema)+".", "")
	if !plainIdent.MatchString(schema) {
		return text
	}
	re := regexp.MustCompile(`(^|[^A-Za-z0-9_$"` + "`" + `])` + regexp.QuoteMeta(schema) + `\.`)
	return re.ReplaceAllString(text, "$1")
}

var plainIdent = regexp.MustCompile(`^[a-z_][a-z0-9_$]*$`)

// ── MySQL / MariaDB ────────────────────────────────────────────────────

const myCanonColumns = `SELECT c.table_name, c.column_name, c.column_type, c.is_nullable, c.column_default, c.extra, c.data_type
FROM information_schema.columns c JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
WHERE c.table_schema = ? AND t.table_type = 'BASE TABLE'
ORDER BY c.table_name, c.ordinal_position`

const myCanonConstraints = `SELECT kcu.table_name, kcu.constraint_name, tc.constraint_type, kcu.column_name, kcu.referenced_table_name, kcu.referenced_column_name, rc.update_rule, rc.delete_rule
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu ON kcu.constraint_schema = tc.constraint_schema AND kcu.constraint_name = tc.constraint_name AND kcu.table_name = tc.table_name
LEFT JOIN information_schema.referential_constraints rc ON rc.constraint_schema = tc.constraint_schema AND rc.constraint_name = tc.constraint_name AND rc.table_name = tc.table_name
WHERE tc.table_schema = ? AND tc.constraint_type IN ('PRIMARY KEY', 'UNIQUE', 'FOREIGN KEY')
ORDER BY kcu.table_name, kcu.constraint_name, kcu.ordinal_position`

// myCanonChecks reads CHECK constraints (MySQL 8.0.16+, MariaDB 10.2+).
// The columns are named so that MariaDB, whose table has a table_name column
// of its own, and MySQL, which does not, both work through the join.
const myCanonChecks = `SELECT tc.table_name, tc.constraint_name, cc.check_clause
FROM information_schema.check_constraints cc
JOIN information_schema.table_constraints tc ON tc.constraint_schema = cc.constraint_schema AND tc.constraint_name = cc.constraint_name
WHERE tc.table_schema = ? AND tc.constraint_type = 'CHECK'
ORDER BY tc.table_name, tc.constraint_name`

const myCanonIndexes = `SELECT table_name, index_name, non_unique, column_name, index_type, sub_part
FROM information_schema.statistics
WHERE table_schema = ?
ORDER BY table_name, index_name, seq_in_index`

const myCanonViews = `SELECT table_name, view_definition FROM information_schema.views WHERE table_schema = ? ORDER BY table_name`

const myCanonRoutines = `SELECT routine_name, routine_type, dtd_identifier, routine_definition, specific_name FROM information_schema.routines WHERE routine_schema = ? ORDER BY routine_name, routine_type`

const myCanonParams = `SELECT specific_name, parameter_mode, parameter_name, dtd_identifier FROM information_schema.parameters WHERE specific_schema = ? AND ordinal_position > 0 ORDER BY specific_name, ordinal_position`

var myIntWidth = regexp.MustCompile(`^(bigint|int|integer|mediumint|smallint|tinyint)\((\d+)\)`)

// mysqlType lower-cases the type and drops the integer display width
// (MySQL 8 no longer reports it, MariaDB does), keeping tinyint(1).
func mysqlType(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	if m := myIntWidth.FindStringSubmatch(t); m != nil && (m[1] != "tinyint" || m[2] != "1") {
		t = m[1] + t[len(m[0]):]
	}
	return t
}

var myStringTypes = map[string]bool{"char": true, "varchar": true, "tinytext": true, "text": true, "mediumtext": true, "longtext": true, "enum": true, "set": true, "binary": true, "varbinary": true}

// mysqlDefault writes a column default as SQL: MySQL 8 reports a string
// default without quotes, MariaDB with them; expressions keep theirs.
func mysqlDefault(def Cell, dataType, extra string) string {
	if def.Null {
		return ""
	}
	v := mysqlNow(canonText(def))
	switch {
	case strings.EqualFold(v, "NULL"):
		return ""
	case strings.Contains(strings.ToUpper(extra), "DEFAULT_GENERATED"):
		if strings.HasPrefix(strings.ToUpper(v), "CURRENT_TIMESTAMP") || strings.HasPrefix(v, "(") {
			return v
		}
		return "(" + v + ")"
	case strings.HasPrefix(v, "'"):
		return v
	case myStringTypes[strings.ToLower(dataType)]:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	}
	return v
}

func mysqlExtra(extra string) string {
	var parts []string
	up := strings.ToUpper(extra)
	if strings.Contains(up, "AUTO_INCREMENT") {
		parts = append(parts, "AUTO_INCREMENT")
	}
	if i := strings.Index(up, "ON UPDATE "); i >= 0 {
		parts = append(parts, "ON UPDATE "+mysqlNow(strings.TrimSpace(extra[i+len("ON UPDATE "):])))
	}
	return strings.Join(parts, " ")
}

func (r *canonReader) readMySQL(schema string) error {
	rows, err := r.query(myCanonColumns, schema)
	if err != nil {
		return err
	}
	for _, row := range rows {
		t := r.table(canonText(canonCell(row, 0)))
		extra := canonText(canonCell(row, 5))
		t.Columns = append(t.Columns, CanonColumn{
			Name: canonText(canonCell(row, 1)), Type: mysqlType(canonText(canonCell(row, 2))),
			NotNull: strings.EqualFold(canonText(canonCell(row, 3)), "NO"),
			Default: mysqlDefault(canonCell(row, 4), canonText(canonCell(row, 6)), extra), Extra: mysqlExtra(extra),
		})
	}
	if rows, err = r.query(myCanonConstraints, schema); err != nil {
		return err
	}
	names := map[string]map[string]bool{}
	for _, row := range rows {
		t := r.known(canonText(canonCell(row, 0)))
		if t == nil {
			continue
		}
		name, kind := canonText(canonCell(row, 1)), strings.ToUpper(canonText(canonCell(row, 2)))
		if names[t.Name] == nil {
			names[t.Name] = map[string]bool{}
		}
		names[t.Name][name] = true
		var c *CanonConstraint
		for i := range t.Constraints {
			if t.Constraints[i].Name == name && t.Constraints[i].Kind == kind {
				c = &t.Constraints[i]
			}
		}
		if c == nil {
			t.Constraints = append(t.Constraints, CanonConstraint{Kind: kind, Name: name})
			c = &t.Constraints[len(t.Constraints)-1]
			if kind == "PRIMARY KEY" {
				c.Name = "" // always PRIMARY
			}
			if kind == "FOREIGN KEY" {
				c.RefTable = canonText(canonCell(row, 4))
				c.OnUpdate, c.OnDelete = mysqlRule(canonText(canonCell(row, 6))), mysqlRule(canonText(canonCell(row, 7)))
			}
		}
		c.Columns = append(c.Columns, canonText(canonCell(row, 3)))
		if kind == "FOREIGN KEY" {
			c.RefColumns = append(c.RefColumns, canonText(canonCell(row, 5)))
		}
	}
	// Older servers have no CHECK_CONSTRAINTS table: skip quietly.
	if rows, err = r.query(myCanonChecks, schema); err == nil {
		for _, row := range rows {
			t := r.known(canonText(canonCell(row, 0)))
			if t == nil {
				continue
			}
			t.Constraints = append(t.Constraints, CanonConstraint{Kind: "CHECK", Name: canonText(canonCell(row, 1)), Check: mysqlCheck(canonText(canonCell(row, 2)))})
		}
	}
	if rows, err = r.query(myCanonIndexes, schema); err != nil {
		return err
	}
	q := MySQLDialect{}.QuoteIdent
	idx := map[string]*CanonIndex{}
	var order []string
	for _, row := range rows {
		t := r.known(canonText(canonCell(row, 0)))
		name := canonText(canonCell(row, 1))
		if t == nil || name == "PRIMARY" || names[t.Name][name] {
			continue
		}
		key := t.Name + "\x00" + name
		ix := idx[key]
		if ix == nil {
			ix = &CanonIndex{Table: t.Name, Name: name, Unique: canonText(canonCell(row, 2)) == "0"}
			switch m := strings.ToUpper(canonText(canonCell(row, 4))); m {
			case "", "BTREE":
			case "FULLTEXT", "SPATIAL":
				ix.Method = m
			default:
				ix.Method = m
			}
			idx[key] = ix
			order = append(order, key)
		}
		col := canonCell(row, 3)
		expr := "(expression)"
		if !col.Null {
			expr = q(canonText(col))
			if sub := canonText(canonCell(row, 5)); sub != "" {
				expr += "(" + sub + ")"
			}
		}
		ix.Exprs = append(ix.Exprs, expr)
	}
	for _, key := range order {
		ix := idx[key]
		t := r.tables[ix.Table]
		t.Indexes = append(t.Indexes, *ix)
	}
	if rows, err = r.query(myCanonViews, schema); err != nil {
		return err
	}
	for _, row := range rows {
		def := stripSchemaPrefix(canonBody(canonCell(row, 1)), schema, q)
		r.out.Views = append(r.out.Views, CanonView{Name: canonText(canonCell(row, 0)), Definition: strings.TrimRight(def, "; \n")})
	}
	if rows, err = r.query(myCanonRoutines, schema); err != nil {
		return err
	}
	params, err := r.query(myCanonParams, schema)
	if err != nil {
		return err
	}
	args := map[string][]string{}
	for _, p := range params {
		arg := strings.TrimSpace(canonText(canonCell(p, 1)) + " " + q(canonText(canonCell(p, 2))) + " " + strings.ToLower(canonText(canonCell(p, 3))))
		args[canonText(canonCell(p, 0))] = append(args[canonText(canonCell(p, 0))], arg)
	}
	for _, row := range rows {
		r.out.Routines = append(r.out.Routines, CanonRoutine{
			Name: canonText(canonCell(row, 0)), Kind: strings.ToUpper(canonText(canonCell(row, 1))),
			Returns: strings.ToLower(canonText(canonCell(row, 2))), Body: canonBody(canonCell(row, 3)),
			Args: strings.Join(args[canonText(canonCell(row, 4))], ", "), Language: "SQL",
		})
	}
	return nil
}

// mysqlCheck writes a CHECK clause as CHECK (…): MySQL reports the clause
// with its own parentheses, MariaDB without.
func mysqlCheck(clause string) string {
	clause = strings.TrimSpace(clause)
	if strings.HasPrefix(clause, "(") && strings.HasSuffix(clause, ")") && balancedOuter(clause) {
		clause = clause[1 : len(clause)-1]
	}
	return "CHECK (" + clause + ")"
}

// balancedOuter reports whether the first "(" closes at the last character
// (quotes are honoured).
func balancedOuter(s string) bool {
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' && quote != '`' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"' || c == '`':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 && i != len(s)-1 {
				return false
			}
		}
	}
	return depth == 0
}

// mysqlRule is a foreign key rule; NO ACTION and RESTRICT are the same
// in InnoDB (MySQL reports the default as one, MariaDB as the other).
func mysqlRule(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "NO ACTION" || s == "RESTRICT" {
		return ""
	}
	return s
}

// sqliteRule is a foreign key rule; NO ACTION is the default.
func sqliteRule(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "NO ACTION" {
		return ""
	}
	return s
}

var myCurrentTS = regexp.MustCompile(`(?i)\bcurrent_timestamp(\(\))?`)

// mysqlNow writes current_timestamp() (MariaDB) as CURRENT_TIMESTAMP
// (MySQL).
func mysqlNow(s string) string { return myCurrentTS.ReplaceAllString(s, "CURRENT_TIMESTAMP") }

// ── SQLite ─────────────────────────────────────────────────────────────

func (r *canonReader) readSQLite(schema string) error {
	if schema == "" {
		schema = "main"
	}
	d := SQLiteDialect{}
	lit := "'" + strings.ReplaceAll(schema, "'", "''") + "'"
	master := d.QuoteIdent(schema) + ".sqlite_master"
	if schema == "temp" {
		master = "sqlite_temp_master"
	}
	userTables := ` FROM ` + master + ` m JOIN `
	where := ` WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%' `
	rows, err := r.query(`SELECT m.name, p.name, p.type, p."notnull", p.dflt_value, p.pk` + userTables + `pragma_table_info(m.name, ` + lit + `) p` + where + `ORDER BY m.name, p.cid`)
	if err != nil {
		return err
	}
	pks := map[string][][2]string{}
	for _, row := range rows {
		t := r.table(canonText(canonCell(row, 0)))
		col := CanonColumn{Name: canonText(canonCell(row, 1)), Type: strings.ToUpper(canonText(canonCell(row, 2))), NotNull: canonText(canonCell(row, 3)) == "1", Default: canonText(canonCell(row, 4))}
		t.Columns = append(t.Columns, col)
		if pk := canonText(canonCell(row, 5)); pk != "" && pk != "0" {
			pks[t.Name] = append(pks[t.Name], [2]string{pk, col.Name})
		}
	}
	for name, cols := range pks {
		sort.SliceStable(cols, func(i, j int) bool {
			return len(cols[i][0]) < len(cols[j][0]) || len(cols[i][0]) == len(cols[j][0]) && cols[i][0] < cols[j][0]
		})
		c := CanonConstraint{Kind: "PRIMARY KEY"}
		for _, p := range cols {
			c.Columns = append(c.Columns, p[1])
		}
		r.tables[name].Constraints = append(r.tables[name].Constraints, c)
	}
	rows, err = r.query(`SELECT m.name, il.name, il."unique", il.origin, il.partial, ii.name, ii.seqno` + userTables + `pragma_index_list(m.name, ` + lit + `) il JOIN pragma_index_info(il.name, ` + lit + `) ii` + where + `ORDER BY m.name, il.name, ii.seqno`)
	if err != nil {
		return err
	}
	raw, err := r.query(`SELECT name, sql FROM ` + master + ` WHERE type = 'index' AND sql IS NOT NULL ORDER BY name`)
	if err != nil {
		return err
	}
	rawSQL := map[string]string{}
	for _, row := range raw {
		rawSQL[canonText(canonCell(row, 0))] = canonBody(canonCell(row, 1))
	}
	type sqIdx struct {
		ix     CanonIndex
		origin string
		expr   bool
	}
	idx := map[string]*sqIdx{}
	var order []string
	for _, row := range rows {
		t := r.known(canonText(canonCell(row, 0)))
		if t == nil {
			continue
		}
		name := canonText(canonCell(row, 1))
		key := t.Name + "\x00" + name
		e := idx[key]
		if e == nil {
			e = &sqIdx{ix: CanonIndex{Table: t.Name, Name: name, Unique: canonText(canonCell(row, 2)) == "1"}, origin: canonText(canonCell(row, 3))}
			if canonText(canonCell(row, 4)) == "1" {
				e.expr = true
			}
			idx[key] = e
			order = append(order, key)
		}
		col := canonCell(row, 5)
		if col.Null {
			e.expr = true
			e.ix.Exprs = append(e.ix.Exprs, "(expression)")
			continue
		}
		e.ix.Exprs = append(e.ix.Exprs, d.QuoteIdent(canonText(col)))
	}
	for _, key := range order {
		e := idx[key]
		t := r.tables[e.ix.Table]
		switch e.origin {
		case "pk":
		case "u":
			cols := make([]string, len(e.ix.Exprs))
			for i, x := range e.ix.Exprs {
				cols[i] = unquoteSQLite(x)
			}
			t.Constraints = append(t.Constraints, CanonConstraint{Kind: "UNIQUE", Columns: cols})
		default:
			if e.expr {
				e.ix.Raw = collapseSpace(rawSQL[e.ix.Name])
			}
			t.Indexes = append(t.Indexes, e.ix)
		}
	}
	rows, err = r.query(`SELECT m.name, f.id, f."table", f."from", f."to", f.on_update, f.on_delete` + userTables + `pragma_foreign_key_list(m.name, ` + lit + `) f` + where + `ORDER BY m.name, f.id, f.seq`)
	if err != nil {
		return err
	}
	fk := map[string]*CanonConstraint{}
	var fkOrder []string
	for _, row := range rows {
		t := r.known(canonText(canonCell(row, 0)))
		if t == nil {
			continue
		}
		key := t.Name + "\x00" + canonText(canonCell(row, 1))
		c := fk[key]
		if c == nil {
			c = &CanonConstraint{Kind: "FOREIGN KEY", RefTable: canonText(canonCell(row, 2)), OnUpdate: sqliteRule(canonText(canonCell(row, 5))), OnDelete: sqliteRule(canonText(canonCell(row, 6)))}
			fk[key] = c
			fkOrder = append(fkOrder, key)
		}
		c.Columns = append(c.Columns, canonText(canonCell(row, 3)))
		if to := canonText(canonCell(row, 4)); to != "" {
			c.RefColumns = append(c.RefColumns, to)
		}
	}
	for _, key := range fkOrder {
		name, _, _ := strings.Cut(key, "\x00")
		r.tables[name].Constraints = append(r.tables[name].Constraints, *fk[key])
	}
	rows, err = r.query(`SELECT name, sql FROM ` + master + ` WHERE type = 'view' ORDER BY name`)
	if err != nil {
		return err
	}
	for _, row := range rows {
		r.out.Views = append(r.out.Views, CanonView{Name: canonText(canonCell(row, 0)), Definition: strings.TrimRight(canonBody(canonCell(row, 1)), "; \n"), Full: true})
	}
	return nil
}

func unquoteSQLite(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
	}
	return s
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// ListSchemas lists the schemas a compare can pick on sess: PostgreSQL's
// schemas of the connected database (database is its name), MySQL's
// databases (system ones left out), SQLite's attached files.
func ListSchemas(ctx context.Context, sess Session, driver string) (database string, schemas []string, err error) {
	one := func(sql string) ([][]Cell, error) {
		r := sess.Run(ctx, sql, RunOptions{MaxRows: 10000})
		return r.Rows, r.Err
	}
	var rows [][]Cell
	col := 0
	switch normDriver(driver) {
	case "postgres", "postgresql":
		if rows, err = one(`SELECT current_database()`); err != nil {
			return "", nil, err
		}
		if len(rows) > 0 {
			database = canonText(canonCell(rows[0], 0))
		}
		rows, err = one(`SELECT nspname FROM pg_namespace WHERE nspname NOT LIKE 'pg\_%' AND nspname <> 'information_schema' ORDER BY nspname`)
	case "mysql":
		rows, err = one(`SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('information_schema', 'mysql', 'performance_schema', 'sys') ORDER BY schema_name`)
	case "sqlite":
		rows, err = one(`PRAGMA database_list`)
		col = 1
	default:
		return "", nil, fmt.Errorf("schema compare does not support %s", driver)
	}
	if err != nil {
		return database, nil, err
	}
	for _, row := range rows {
		name := canonText(canonCell(row, col))
		if name == "" || name == "temp" && normDriver(driver) == "sqlite" {
			continue
		}
		schemas = append(schemas, name)
	}
	return database, schemas, nil
}
