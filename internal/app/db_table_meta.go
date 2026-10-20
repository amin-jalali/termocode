package app

import (
	"context"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// dbColInfo is one column with what describe and the DDL view need.
type dbColInfo struct {
	name, typ string
	notNull   bool
	def       string
	pk        bool
}

// dbTableMeta is everything the server tells us about one table or view.
type dbTableMeta struct {
	cols     []dbColInfo
	ir       dbclient.IntrospectionResult
	triggers [][3]string // name, timing, events
	viewDef  string
}

func dbSQLString(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// dbLoadTableMeta reads the table's columns, keys, indexes, foreign keys,
// triggers and (views) definition. Names are bound as parameters; SQLite
// cannot bind, so its names are quoted as identifiers or escaped literals.
func dbLoadTableMeta(ctx context.Context, sess dbclient.Session, t dbTable, req dbclient.IntrospectionRequest) (dbTableMeta, error) {
	var meta dbTableMeta
	ir, err := sess.Introspect(ctx, req)
	if err != nil {
		return meta, err
	}
	meta.ir = ir
	keys := map[string]bool{}
	for _, k := range ir.Keys {
		keys[k] = true
	}
	for _, c := range ir.Columns {
		if c.PK {
			keys[c.Name] = true
		}
	}
	run := func(sql string, args ...any) (dbclient.Result, error) {
		r := sess.Run(ctx, sql, dbclient.RunOptions{MaxRows: 10000, Args: args})
		return r, r.Err
	}
	switch dbclient.DialectFor(t.driver).(type) {
	case dbclient.PostgresDialect:
		q := `SELECT column_name, data_type, is_nullable, column_default FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position`
		args := []any{t.schema, t.name}
		tq := `SELECT trigger_name, action_timing, event_manipulation FROM information_schema.triggers WHERE event_object_schema = $1 AND event_object_table = $2 ORDER BY trigger_name, event_manipulation`
		if t.schema == "" {
			q = strings.Replace(q, "table_schema = $1 AND table_name = $2", "table_schema = current_schema() AND table_name = $1", 1)
			tq = strings.Replace(tq, "event_object_schema = $1 AND event_object_table = $2", "event_object_schema = current_schema() AND event_object_table = $1", 1)
			args = []any{t.name}
		}
		r, err := run(q, args...)
		if err != nil {
			return meta, err
		}
		meta.cols = dbInfoColumns(r, 0, 1, 2, 3, keys)
		if r, err = run(tq, args...); err != nil {
			return meta, err
		}
		meta.triggers = dbInfoTriggers(r)
		if t.view {
			vq, vargs := `SELECT definition FROM pg_views WHERE schemaname = $1 AND viewname = $2`, args
			if t.schema == "" {
				vq = `SELECT definition FROM pg_views WHERE schemaname = current_schema() AND viewname = $1`
			}
			if r, err = run(vq, vargs...); err == nil && len(r.Rows) > 0 && len(r.Rows[0]) > 0 {
				meta.viewDef = r.Rows[0][0].Text
			}
		}
	case dbclient.MySQLDialect:
		q := `SELECT column_name, column_type, is_nullable, column_default FROM information_schema.columns WHERE table_schema = ? AND table_name = ? ORDER BY ordinal_position`
		tq := `SELECT trigger_name, action_timing, event_manipulation FROM information_schema.triggers WHERE event_object_schema = ? AND event_object_table = ? ORDER BY trigger_name, event_manipulation`
		args := []any{t.schema, t.name}
		if t.schema == "" {
			q = strings.Replace(q, "table_schema = ?", "table_schema = DATABASE()", 1)
			tq = strings.Replace(tq, "event_object_schema = ?", "event_object_schema = DATABASE()", 1)
			args = []any{t.name}
		}
		r, err := run(q, args...)
		if err != nil {
			return meta, err
		}
		meta.cols = dbInfoColumns(r, 0, 1, 2, 3, keys)
		if r, err = run(tq, args...); err != nil {
			return meta, err
		}
		meta.triggers = dbInfoTriggers(r)
	case dbclient.SQLiteDialect:
		d := dbclient.SQLiteDialect{}
		pragma := "PRAGMA "
		if t.schema != "" {
			pragma += d.QuoteIdent(t.schema) + "."
		}
		r, err := run(pragma + "table_info(" + d.QuoteIdent(t.name) + ")")
		if err != nil {
			return meta, err
		}
		for _, row := range r.Rows {
			if len(row) < 6 {
				continue
			}
			c := dbColInfo{name: sanitizeDBText(row[1].Text), typ: sanitizeDBText(row[2].Text), notNull: row[3].Text == "1", pk: row[5].Text != "0"}
			if !row[4].Null {
				c.def = sanitizeDBText(row[4].Text)
			}
			meta.cols = append(meta.cols, c)
		}
		master := "sqlite_master"
		if t.schema != "" && t.schema != "main" {
			master = d.QuoteIdent(t.schema) + ".sqlite_master"
		}
		tr, err := run("SELECT name FROM " + master + " WHERE type = 'trigger' AND tbl_name = " + dbSQLString(t.name) + " ORDER BY name")
		if err != nil {
			return meta, err
		}
		for _, row := range tr.Rows {
			if len(row) > 0 {
				meta.triggers = append(meta.triggers, [3]string{sanitizeDBText(row[0].Text), "", ""})
			}
		}
	}
	return meta, nil
}

func dbInfoColumns(r dbclient.Result, name, typ, nullable, def int, keys map[string]bool) []dbColInfo {
	var out []dbColInfo
	for _, row := range r.Rows {
		if len(row) <= max(name, typ, nullable, def) {
			continue
		}
		c := dbColInfo{name: sanitizeDBText(row[name].Text), typ: sanitizeDBText(row[typ].Text), notNull: strings.EqualFold(row[nullable].Text, "NO")}
		if !row[def].Null {
			c.def = sanitizeDBText(row[def].Text)
		}
		c.pk = keys[c.name]
		out = append(out, c)
	}
	return out
}

func dbInfoTriggers(r dbclient.Result) [][3]string {
	var out [][3]string
	for _, row := range r.Rows {
		if len(row) < 3 {
			continue
		}
		name, timing, event := sanitizeDBText(row[0].Text), sanitizeDBText(row[1].Text), sanitizeDBText(row[2].Text)
		if n := len(out); n > 0 && out[n-1][0] == name {
			out[n-1][2] += " OR " + event
			continue
		}
		out = append(out, [3]string{name, timing, event})
	}
	return out
}

// dbForeignKeys groups the rows of a composite key; an unnamed key (SQLite)
// stands alone.
func dbForeignKeys(fks []dbclient.SchemaForeignKey) [][]dbclient.SchemaForeignKey {
	var groups [][]dbclient.SchemaForeignKey
	at := map[string]int{}
	for _, fk := range fks {
		if fk.Name == "" {
			groups = append(groups, []dbclient.SchemaForeignKey{fk})
			continue
		}
		if i, ok := at[fk.Name]; ok {
			groups[i] = append(groups[i], fk)
			continue
		}
		at[fk.Name] = len(groups)
		groups = append(groups, []dbclient.SchemaForeignKey{fk})
	}
	return groups
}

func dbIsKeyIndex(idx dbclient.SchemaIndex, keys []string) bool {
	if !idx.Unique || len(keys) == 0 || len(idx.Columns) != len(keys) {
		return false
	}
	for i := range keys {
		if idx.Columns[i] != keys[i] {
			return false
		}
	}
	return true
}

func dbColumnsText(names []string) string {
	clean := make([]string, len(names))
	for i, n := range names {
		clean[i] = sanitizeDBText(n)
	}
	return "(" + strings.Join(clean, ", ") + ")"
}

// dbDescribeResult is the describe page: one row per column, index,
// foreign key and trigger.
func dbDescribeResult(t dbTable, meta dbTableMeta) dbclient.Result {
	res := dbclient.Result{Columns: []dbclient.Column{{Name: "Kind"}, {Name: "Name"}, {Name: "Type"}, {Name: "Details"}}}
	add := func(kind, name, typ, details string) {
		res.Rows = append(res.Rows, []dbclient.Cell{{Text: kind}, {Text: sanitizeDBText(name)}, {Text: sanitizeDBText(typ)}, {Text: details}})
	}
	for _, c := range meta.cols {
		var parts []string
		if c.notNull {
			parts = append(parts, "not null")
		} else {
			parts = append(parts, "can be null")
		}
		if c.def != "" {
			parts = append(parts, "default "+c.def)
		}
		if c.pk {
			parts = append(parts, "primary key")
		}
		add("Column", c.name, c.typ, strings.Join(parts, " · "))
	}
	for _, idx := range meta.ir.Indexes {
		if dbIsKeyIndex(idx, meta.ir.Keys) {
			continue
		}
		details := dbColumnsText(idx.Columns)
		if idx.Unique {
			details = "unique · " + details
		}
		add("Index", idx.Name, "", details)
	}
	for _, group := range dbForeignKeys(meta.ir.ForeignKeys) {
		var cols, refs []string
		for _, fk := range group {
			cols, refs = append(cols, fk.Column), append(refs, fk.RefColumn)
		}
		add("Foreign key", group[0].Name, "", dbColumnsText(cols)+" → "+sanitizeDBText(group[0].RefTable)+" "+dbColumnsText(refs))
	}
	for _, tr := range meta.triggers {
		add("Trigger", tr[0], "", strings.TrimSpace(tr[1]+" "+tr[2]))
	}
	return res
}

// dbBuildDDL is the text of the DDL view. SQLite and MySQL give a real
// CREATE statement. PostgreSQL has none, so it is rebuilt from the
// catalogs (and says so).
func dbBuildDDL(t dbTable, meta dbTableMeta) string {
	if _, pg := dbclient.DialectFor(t.driver).(dbclient.PostgresDialect); !pg {
		if strings.TrimSpace(meta.ir.DDL) == "" {
			return "-- The server returned no definition."
		}
		return dbSafeMultiline(strings.TrimSpace(meta.ir.DDL) + ";")
	}
	d := dbclient.PostgresDialect{}
	q := t.qualified()
	var b strings.Builder
	b.WriteString("-- Rebuilt from the catalogs. It is a reconstruction, not a dump.\n")
	if t.view && meta.viewDef != "" {
		b.WriteString("CREATE VIEW " + q + " AS\n" + strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(meta.viewDef), ";")) + ";\n")
		return dbSafeMultiline(strings.TrimRight(b.String(), "\n"))
	}
	b.WriteString("CREATE TABLE " + q + " (\n")
	var lines []string
	var pk []string
	for _, c := range meta.cols {
		line := "  " + d.QuoteIdent(c.name) + " " + c.typ
		if c.notNull {
			line += " NOT NULL"
		}
		if c.def != "" {
			line += " DEFAULT " + c.def
		}
		lines = append(lines, line)
		if c.pk {
			pk = append(pk, d.QuoteIdent(c.name))
		}
	}
	if len(pk) > 0 {
		lines = append(lines, "  PRIMARY KEY ("+strings.Join(pk, ", ")+")")
	}
	b.WriteString(strings.Join(lines, ",\n") + "\n);\n")
	quoteAll := func(names []string) string {
		out := make([]string, len(names))
		for i, n := range names {
			out[i] = d.QuoteIdent(n)
		}
		return "(" + strings.Join(out, ", ") + ")"
	}
	for _, idx := range meta.ir.Indexes {
		if dbIsKeyIndex(idx, meta.ir.Keys) {
			continue
		}
		unique := ""
		if idx.Unique {
			unique = "UNIQUE "
		}
		b.WriteString("CREATE " + unique + "INDEX " + d.QuoteIdent(idx.Name) + " ON " + q + " " + quoteAll(idx.Columns) + ";\n")
	}
	for _, group := range dbForeignKeys(meta.ir.ForeignKeys) {
		var cols, refs []string
		for _, fk := range group {
			cols, refs = append(cols, fk.Column), append(refs, fk.RefColumn)
		}
		ref := d.QuoteIdent(group[0].RefTable)
		if t.schema != "" {
			ref = d.QuoteIdent(t.schema) + "." + ref // the catalog gives the name only: same schema
		}
		b.WriteString("ALTER TABLE " + q + " ADD CONSTRAINT " + d.QuoteIdent(group[0].Name) + " FOREIGN KEY " + quoteAll(cols) + " REFERENCES " + ref + " " + quoteAll(refs) + ";\n")
	}
	return dbSafeMultiline(strings.TrimRight(b.String(), "\n"))
}

// dbMetaTimeout bounds the catalog reads of describe and the DDL view.
const dbMetaTimeout = 15 * time.Second
