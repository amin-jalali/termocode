package dbclient

import (
	"errors"
	"fmt"
	"strings"
)

// ObjectActionKind names one tree object action.
type ObjectActionKind int

const (
	ActDrop ObjectActionKind = iota + 1
	ActTruncate
	ActCreateSchema
	ActCreateDatabase
	ActCopyTable
	ActMakePK
)

// ObjectAction is one action on a database object. Names are raw; the
// builder quotes them for the dialect.
type ObjectAction struct {
	Kind    ObjectActionKind
	Object  string // ActDrop: table, view, materialized view, sequence, index, schema, function, procedure
	Schema  string
	Name    string
	NewName string   // ActCopyTable: the copy's name (same schema)
	Columns []string // ActMakePK
	Cascade bool     // ActDrop / ActTruncate: also drop / empty dependents (Postgres)
}

var dropObjects = map[string]string{
	"table": "TABLE", "view": "VIEW", "materialized view": "MATERIALIZED VIEW",
	"sequence": "SEQUENCE", "index": "INDEX", "schema": "SCHEMA",
	"function": "FUNCTION", "procedure": "PROCEDURE",
}

// ObjectActionSQL builds the statements for a, in run order. It refuses
// what the dialect cannot do rather than guessing.
func ObjectActionSQL(d Dialect, a ObjectAction) ([]string, error) {
	if d == nil {
		return nil, errors.New("this connection does not use SQL")
	}
	if strings.TrimSpace(a.Name) == "" {
		return nil, errors.New("no object name")
	}
	_, isPG := d.(PostgresDialect)
	_, isMy := d.(MySQLDialect)
	_, isLite := d.(SQLiteDialect)
	qual := func(name string) string {
		if a.Schema != "" {
			return d.QuoteIdent(a.Schema) + "." + d.QuoteIdent(name)
		}
		return d.QuoteIdent(name)
	}
	switch a.Kind {
	case ActDrop:
		kw, ok := dropObjects[strings.ToLower(strings.TrimSpace(a.Object))]
		if !ok {
			return nil, fmt.Errorf("cannot drop a %q", a.Object)
		}
		if kw == "MATERIALIZED VIEW" && !isPG {
			return nil, errors.New("this database has no materialized views")
		}
		name := qual(a.Name)
		if kw == "SCHEMA" {
			name = d.QuoteIdent(a.Name)
			if isLite {
				return nil, errors.New("SQLite has no schemas to drop")
			}
			if isMy {
				kw = "DATABASE"
			}
		}
		s := "DROP " + kw + " " + name
		if a.Cascade && isPG {
			s += " CASCADE"
		}
		return []string{s}, nil
	case ActTruncate:
		if isLite {
			return []string{"DELETE FROM " + qual(a.Name)}, nil
		}
		s := "TRUNCATE TABLE " + qual(a.Name)
		if a.Cascade && isPG {
			s += " CASCADE"
		}
		return []string{s}, nil
	case ActCreateSchema:
		switch {
		case isPG:
			return []string{"CREATE SCHEMA " + d.QuoteIdent(a.Name)}, nil
		case isMy:
			return []string{"CREATE DATABASE " + d.QuoteIdent(a.Name)}, nil
		}
		return nil, errors.New("SQLite has no schemas; attach a file instead")
	case ActCreateDatabase:
		if isPG || isMy {
			return []string{"CREATE DATABASE " + d.QuoteIdent(a.Name)}, nil
		}
		return nil, errors.New("SQLite has one database per file")
	case ActCopyTable:
		if strings.TrimSpace(a.NewName) == "" || a.NewName == a.Name {
			return nil, errors.New("the copy needs a new name")
		}
		src, dst := qual(a.Name), qual(a.NewName)
		switch {
		case isPG:
			return []string{"CREATE TABLE " + dst + " (LIKE " + src + " INCLUDING ALL)", "INSERT INTO " + dst + " SELECT * FROM " + src}, nil
		case isMy:
			return []string{"CREATE TABLE " + dst + " LIKE " + src, "INSERT INTO " + dst + " SELECT * FROM " + src}, nil
		}
		return []string{"CREATE TABLE " + dst + " AS SELECT * FROM " + src}, nil
	case ActMakePK:
		if len(a.Columns) == 0 {
			return nil, errors.New("pick at least one column")
		}
		if isLite {
			return nil, errors.New("SQLite cannot add a primary key to an existing table")
		}
		cols := make([]string, len(a.Columns))
		for i, c := range a.Columns {
			if strings.TrimSpace(c) == "" {
				return nil, errors.New("a column has no name")
			}
			cols[i] = d.QuoteIdent(c)
		}
		return []string{"ALTER TABLE " + qual(a.Name) + " ADD PRIMARY KEY (" + strings.Join(cols, ", ") + ")"}, nil
	}
	return nil, fmt.Errorf("unknown object action %d", a.Kind)
}

// DropDependentsQuery lists what a drop with cascade would also remove or
// break: rows of (kind, schema, name). Names go in as arguments.
func DropDependentsQuery(d Dialect, schema, table string) (string, []any, error) {
	switch d.(type) {
	case PostgresDialect:
		if schema == "" {
			schema = "public"
		}
		return `SELECT 'foreign key' AS kind, n.nspname, c.relname || '.' || con.conname
FROM pg_constraint con
JOIN pg_class c ON c.oid = con.conrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE con.contype = 'f' AND con.confrelid = (SELECT oid FROM pg_class WHERE relname = $2 AND relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = $1))
UNION
SELECT CASE v.relkind WHEN 'm' THEN 'materialized view' ELSE 'view' END, vn.nspname, v.relname
FROM pg_depend dep
JOIN pg_rewrite r ON r.oid = dep.objid
JOIN pg_class v ON v.oid = r.ev_class
JOIN pg_namespace vn ON vn.oid = v.relnamespace
WHERE dep.refobjid = (SELECT oid FROM pg_class WHERE relname = $2 AND relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = $1)) AND v.relname <> $2
ORDER BY 1, 2, 3`, []any{schema, table}, nil
	case MySQLDialect:
		return `SELECT 'foreign key' AS kind, TABLE_SCHEMA, CONCAT(TABLE_NAME, '.', CONSTRAINT_NAME)
FROM information_schema.KEY_COLUMN_USAGE
WHERE REFERENCED_TABLE_SCHEMA = COALESCE(NULLIF(?, ''), DATABASE()) AND REFERENCED_TABLE_NAME = ?
UNION
SELECT 'view', VIEW_SCHEMA, VIEW_NAME FROM information_schema.VIEW_TABLE_USAGE
WHERE TABLE_NAME = ? AND TABLE_SCHEMA = COALESCE(NULLIF(?, ''), DATABASE())
ORDER BY 1, 2, 3`, []any{schema, table, table, schema}, nil
	case SQLiteDialect:
		return `SELECT 'foreign key' AS kind, '' AS schema, m.name || '.' || f."from"
FROM sqlite_master m JOIN pragma_foreign_key_list(m.name) f
WHERE m.type = 'table' AND f."table" = ?
UNION
SELECT m.type, '', m.name FROM sqlite_master m
WHERE m.type IN ('view', 'trigger') AND m.tbl_name <> ? AND instr(lower(m.sql), lower(?)) > 0
ORDER BY 1, 2, 3`, []any{table, table, table}, nil
	}
	return "", nil, errors.New("this connection does not use SQL")
}

// BackupTableName is the default name for a quick table copy.
func BackupTableName(table, stamp string) string {
	return table + "_backup_" + stamp
}
