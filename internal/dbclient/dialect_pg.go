package dbclient

import (
	"fmt"
	"strings"
)

// PostgresDialect contains PostgreSQL syntax and catalog queries.
type PostgresDialect struct{}

func (PostgresDialect) QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
func (PostgresDialect) Placeholder(n int) string { return fmt.Sprintf("$%d", n) }
func (PostgresDialect) Limit(limit, offset int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
}
func (PostgresDialect) ReadOnlyBegin() string { return "BEGIN READ ONLY" }
func (PostgresDialect) CatalogQueries() map[string]string {
	return map[string]string{
		"databases":    `SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname`,
		"schemas":      `SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT LIKE 'pg_%' AND schema_name <> 'information_schema' ORDER BY schema_name`,
		"objects":      `SELECT table_name, table_type FROM information_schema.tables WHERE table_schema = $1 ORDER BY table_name`,
		"columns":      `SELECT c.column_name, c.data_type FROM information_schema.columns c WHERE c.table_schema = $1 AND c.table_name = $2 ORDER BY c.ordinal_position`,
		"keys":         `SELECT kcu.column_name FROM information_schema.table_constraints tc JOIN information_schema.key_column_usage kcu ON tc.constraint_name=kcu.constraint_name AND tc.constraint_schema=kcu.constraint_schema WHERE tc.constraint_type='PRIMARY KEY' AND tc.table_schema=$1 AND tc.table_name = $2 ORDER BY kcu.ordinal_position`,
		"indexes":      `SELECT i.relname, string_agg(a.attname, ', ' ORDER BY k.ordinality), ix.indisunique, t.relname FROM pg_index ix JOIN pg_class t ON t.oid=ix.indrelid JOIN pg_class i ON i.oid=ix.indexrelid JOIN pg_namespace n ON n.oid=t.relnamespace CROSS JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ordinality) JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum=k.attnum WHERE n.nspname=$1 AND t.relname = $2 GROUP BY i.relname, ix.indisunique, t.relname ORDER BY i.relname`,
		"foreign_keys": `SELECT kcu.constraint_name, kcu.column_name, ccu.table_name, ccu.column_name FROM information_schema.key_column_usage kcu JOIN information_schema.constraint_column_usage ccu USING (constraint_catalog,constraint_schema,constraint_name) JOIN information_schema.table_constraints tc USING (constraint_catalog,constraint_schema,constraint_name) WHERE tc.constraint_type='FOREIGN KEY' AND tc.table_schema=$1 AND kcu.table_name = $2 ORDER BY kcu.constraint_name`,
		"routines":     `SELECT routine_name FROM information_schema.routines WHERE routine_schema=$1 ORDER BY routine_name`,
		"sequences":    `SELECT sequence_name FROM information_schema.sequences WHERE sequence_schema=$1 ORDER BY sequence_name`,
	}
}
func (d PostgresDialect) DDL(table string) (string, []any) {
	return `SELECT 'CREATE TABLE ' || quote_ident(n.nspname) || '.' || quote_ident(c.relname) || E' (\n' || string_agg('  ' || quote_ident(a.attname) || ' ' || format_type(a.atttypid,a.atttypmod), E',\n' ORDER BY a.attnum) || E'\n);' FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped WHERE c.relname = $1 AND n.nspname=current_schema() GROUP BY n.nspname,c.relname`, []any{table}
}
func (PostgresDialect) Explain(sql string, analyze bool) string {
	if analyze {
		return "EXPLAIN (ANALYZE, FORMAT TEXT) " + sql
	}
	return "EXPLAIN (FORMAT TEXT) " + sql
}
func (d PostgresDialect) CountSQL(table string) string {
	return "SELECT COUNT(*) FROM " + d.QuoteIdent(table)
}
