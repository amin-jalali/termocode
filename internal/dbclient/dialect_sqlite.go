package dbclient

import (
	"context"
	"fmt"
	"strings"
)

// SQLiteDialect provides SQLite-specific syntax and introspection queries.
type SQLiteDialect struct{}

func (SQLiteDialect) QuoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func (SQLiteDialect) Placeholder(int) string     { return "?" }
func (SQLiteDialect) Limit(limit, offset int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
}
func (SQLiteDialect) ReadOnlyBegin() string { return "BEGIN" }
func (SQLiteDialect) CatalogQueries() map[string]string {
	return map[string]string{"objects": `SELECT name, type FROM sqlite_master WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%' ORDER BY name`}
}
func (d SQLiteDialect) DDL(table string) (string, []any) {
	name := strings.ReplaceAll(table, "'", "''")
	return "SELECT sql FROM sqlite_master WHERE (name = '" + name + "' OR tbl_name = '" + name + "') AND sql IS NOT NULL ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'view' THEN 0 ELSE 1 END, name", nil
}
func (SQLiteDialect) Explain(sql string, _ bool) string { return "EXPLAIN QUERY PLAN " + sql }

func (s *SQLiteSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	if req.Object == "" {
		dbs := s.Run(ctx, "PRAGMA database_list", RunOptions{})
		if dbs.Err != nil {
			return IntrospectionResult{}, dbs.Err
		}
		r := s.Run(ctx, (SQLiteDialect{}).CatalogQueries()["objects"], RunOptions{})
		if r.Err != nil {
			return IntrospectionResult{}, r.Err
		}
		out := IntrospectionResult{Objects: make([]SchemaObject, 0, len(r.Rows))}
		for _, row := range dbs.Rows {
			if len(row) > 1 {
				out.Databases = append(out.Databases, row[1].Text)
			}
		}
		for _, row := range r.Rows {
			if len(row) >= 2 {
				out.Objects = append(out.Objects, SchemaObject{Name: row[0].Text, Kind: row[1].Text})
			}
		}
		return out, nil
	}
	q := (SQLiteDialect{}).QuoteIdent(req.Object)
	cols := s.Run(ctx, "PRAGMA table_info("+q+")", RunOptions{})
	if cols.Err != nil {
		return IntrospectionResult{}, cols.Err
	}
	out := IntrospectionResult{}
	for _, row := range cols.Rows {
		if len(row) < 6 {
			continue
		}
		out.Columns = append(out.Columns, Column{Name: row[1].Text, Type: row[2].Text, Table: req.Object, PK: row[5].Text != "0"})
	}
	idx := s.Run(ctx, "PRAGMA index_list("+q+")", RunOptions{})
	if idx.Err != nil {
		return IntrospectionResult{}, idx.Err
	}
	for _, row := range idx.Rows {
		if len(row) < 3 {
			continue
		}
		name := row[1].Text
		cols := s.Run(ctx, "PRAGMA index_info("+(SQLiteDialect{}).QuoteIdent(name)+")", RunOptions{})
		if cols.Err != nil {
			return IntrospectionResult{}, cols.Err
		}
		entry := SchemaIndex{Name: name, Unique: row[2].Text == "1"}
		for _, c := range cols.Rows {
			if len(c) > 2 {
				entry.Columns = append(entry.Columns, c[2].Text)
			}
		}
		out.Indexes = append(out.Indexes, entry)
	}
	fks := s.Run(ctx, "PRAGMA foreign_key_list("+q+")", RunOptions{})
	if fks.Err != nil {
		return IntrospectionResult{}, fks.Err
	}
	for _, row := range fks.Rows {
		if len(row) >= 5 {
			out.ForeignKeys = append(out.ForeignKeys, SchemaForeignKey{Column: row[3].Text, RefTable: row[2].Text, RefColumn: row[4].Text})
		}
	}
	ddlSQL, _ := (SQLiteDialect{}).DDL(req.Object)
	ddl := s.Run(ctx, ddlSQL, RunOptions{})
	if ddl.Err != nil {
		return IntrospectionResult{}, ddl.Err
	}
	var definitions []string
	for _, row := range ddl.Rows {
		if len(row) > 0 && row[0].Text != "" {
			definitions = append(definitions, row[0].Text)
		}
	}
	out.DDL = strings.Join(definitions, ";\n")
	return out, nil
}
