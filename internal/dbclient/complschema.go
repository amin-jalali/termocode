package dbclient

import (
	"context"
	"sort"
	"strings"
)

// Introspector is the part of a session the completion loader needs.
type Introspector interface {
	Introspect(context.Context, IntrospectionRequest) (IntrospectionResult, error)
}

// CompletionTable is one object the SQL completion offers.
type CompletionTable struct {
	Schema, Name, Kind string // Kind: table, view, func, seq
}

// CompletionFK is one foreign key: Table.Column references
// RefTable.RefColumn (table names lower case, without schema).
type CompletionFK struct {
	Table, Column, RefTable, RefColumn string
}

// CompletionSchema is the whole schema of one connection, loaded in the
// background for the SQL console (DBX-3): every object, the columns with
// their types, and the foreign keys for JOIN … ON suggestions.
type CompletionSchema struct {
	Tables []CompletionTable
	// Cols maps a lower-case table name to its columns (Name and Type).
	Cols map[string][]Column
	FKs  []CompletionFK
	// Partial is true when the table limit stopped the column load.
	Partial bool
}

// systemSchema reports a catalog schema the completion skips.
func systemSchema(s string) bool {
	l := strings.ToLower(s)
	return l == "information_schema" || l == "pg_catalog" || l == "pg_toast" || strings.HasPrefix(l, "pg_temp") || strings.HasPrefix(l, "pg_toast_temp") ||
		l == "mysql" || l == "performance_schema" || l == "sys"
}

// LoadCompletionSchema reads every object of the connection's database
// and the columns and foreign keys of up to maxTables tables and views.
// database is the connection's database (MySQL/ClickHouse need it). An
// object that cannot be read is skipped; only a failed object list is an
// error.
func LoadCompletionSchema(ctx context.Context, s Introspector, driver, database string, maxTables int) (CompletionSchema, error) {
	out := CompletionSchema{Cols: map[string][]Column{}}
	type scope struct{ db, schema string }
	var scopes []scope
	switch d := normDriver(driver); d {
	case "postgres", "postgresql":
		r, err := s.Introspect(ctx, IntrospectionRequest{Database: "current"})
		if err != nil {
			return out, err
		}
		for _, sc := range r.Schemas {
			if !systemSchema(sc) {
				scopes = append(scopes, scope{schema: sc})
			}
		}
	case "mysql", "clickhouse":
		if database == "" {
			return out, nil
		}
		scopes = append(scopes, scope{db: database, schema: database})
	default:
		scopes = append(scopes, scope{})
	}
	type obj struct {
		sc   scope
		name string
		kind string
	}
	var objs []obj
	for _, sc := range scopes {
		r, err := s.Introspect(ctx, IntrospectionRequest{Database: sc.db, Schema: sc.schema})
		if err != nil {
			if len(scopes) == 1 {
				return out, err
			}
			continue
		}
		for _, o := range r.Objects {
			kind := strings.ToLower(o.Kind)
			switch {
			case strings.Contains(kind, "view"):
				kind = "view"
			case kind == "" || strings.Contains(kind, "table"):
				kind = "table"
			default:
				continue
			}
			out.Tables = append(out.Tables, CompletionTable{Schema: sc.schema, Name: o.Name, Kind: kind})
			objs = append(objs, obj{sc: sc, name: o.Name, kind: kind})
		}
		for _, n := range r.Routines {
			out.Tables = append(out.Tables, CompletionTable{Schema: sc.schema, Name: n, Kind: "func"})
		}
		for _, n := range r.Sequences {
			out.Tables = append(out.Tables, CompletionTable{Schema: sc.schema, Name: n, Kind: "seq"})
		}
	}
	for i, o := range objs {
		if ctx.Err() != nil {
			out.Partial = true
			break
		}
		if maxTables > 0 && i >= maxTables {
			out.Partial = true
			break
		}
		r, err := s.Introspect(ctx, IntrospectionRequest{Database: o.sc.db, Schema: o.sc.schema, Object: o.name, Kind: o.kind})
		if err != nil {
			continue
		}
		key := strings.ToLower(o.name)
		if _, dup := out.Cols[key]; !dup {
			cols := make([]Column, 0, len(r.Columns))
			for _, c := range r.Columns {
				cols = append(cols, Column{Name: c.Name, Type: c.Type, Table: o.name, PK: c.PK})
			}
			out.Cols[key] = cols
		}
		for _, f := range r.ForeignKeys {
			ref := f.RefTable
			if j := strings.LastIndexByte(ref, '.'); j >= 0 {
				ref = ref[j+1:]
			}
			out.FKs = append(out.FKs, CompletionFK{Table: key, Column: f.Column, RefTable: strings.ToLower(strings.Trim(ref, "\"`")), RefColumn: f.RefColumn})
		}
	}
	sort.SliceStable(out.FKs, func(i, j int) bool {
		a, b := out.FKs[i], out.FKs[j]
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		return a.Column < b.Column
	})
	return out, nil
}

// TypeFamily groups a column type for type-aware completion: "num",
// "text", "time", "bool", "json", "uuid", "bin" or "" when unknown.
func TypeFamily(t string) string {
	l := strings.ToLower(strings.TrimSpace(t))
	if i := strings.IndexAny(l, "( "); i > 0 && !strings.HasPrefix(l, "double") && !strings.HasPrefix(l, "character") && !strings.HasPrefix(l, "timestamp") && !strings.HasPrefix(l, "time ") {
		l = l[:i]
	}
	switch {
	case l == "":
		return ""
	case strings.HasPrefix(l, "bool"):
		return "bool"
	case strings.Contains(l, "int") || strings.HasPrefix(l, "numeric") || strings.HasPrefix(l, "decimal") || strings.HasPrefix(l, "real") ||
		strings.HasPrefix(l, "double") || strings.HasPrefix(l, "float") || strings.Contains(l, "serial") || l == "money" || l == "number":
		return "num"
	case strings.HasPrefix(l, "timestamp") || strings.HasPrefix(l, "date") || strings.HasPrefix(l, "time") || l == "interval" || l == "year":
		return "time"
	case strings.HasPrefix(l, "json"):
		return "json"
	case l == "uuid" || l == "uniqueidentifier":
		return "uuid"
	case strings.Contains(l, "blob") || l == "bytea" || strings.Contains(l, "binary"):
		return "bin"
	case strings.Contains(l, "char") || strings.Contains(l, "text") || l == "string" || l == "citext" || l == "enum" || l == "clob":
		return "text"
	}
	return ""
}
