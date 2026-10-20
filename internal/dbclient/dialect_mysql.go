package dbclient

import (
	"fmt"
	"strings"
)

// MySQLDialect contains MySQL and MariaDB syntax and catalog queries.
type MySQLDialect struct{}

func (MySQLDialect) QuoteIdent(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }
func (MySQLDialect) Placeholder(int) string     { return "?" }
func (MySQLDialect) Limit(limit, offset int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" LIMIT %d OFFSET %d", offset, limit)
}
func (MySQLDialect) ReadOnlyBegin() string { return "START TRANSACTION READ ONLY" }
func (MySQLDialect) CatalogQueries() map[string]string {
	return map[string]string{
		"databases":    `SELECT schema_name FROM information_schema.schemata ORDER BY schema_name`,
		"schemas":      `SELECT schema_name FROM information_schema.schemata ORDER BY schema_name`,
		"objects":      `SELECT table_name, table_type FROM information_schema.tables WHERE table_schema=? ORDER BY table_name`,
		"columns":      `SELECT column_name, column_type FROM information_schema.columns WHERE table_schema=? AND table_name = ? ORDER BY ordinal_position`,
		"keys":         `SELECT column_name FROM information_schema.key_column_usage WHERE table_schema=? AND constraint_name='PRIMARY' AND table_name = ? ORDER BY ordinal_position`,
		"indexes":      `SELECT index_name, GROUP_CONCAT(column_name ORDER BY seq_in_index), MIN(non_unique=0) FROM information_schema.statistics WHERE table_schema=? AND table_name = ? GROUP BY index_name ORDER BY index_name`,
		"foreign_keys": `SELECT constraint_name, column_name, referenced_table_name, referenced_column_name FROM information_schema.key_column_usage WHERE table_schema=? AND referenced_table_name IS NOT NULL AND table_name = ? ORDER BY constraint_name`,
		"routines":     `SELECT routine_name FROM information_schema.routines WHERE routine_schema=? ORDER BY routine_name`,
		"sequences":    `SELECT CAST(NULL AS CHAR) WHERE 1=0`,
	}
}

// DDL cannot bind the table name: SHOW does not accept placeholders, so the
// name goes through QuoteIdent, which escapes every backtick.
func (d MySQLDialect) DDL(table string) (string, []any) {
	return "SHOW CREATE TABLE " + d.QuoteIdent(table), nil
}
func (MySQLDialect) Explain(sql string, analyze bool) string {
	if analyze {
		return "EXPLAIN ANALYZE " + sql
	}
	return "EXPLAIN " + sql
}
func (d MySQLDialect) CountSQL(table string) string {
	return "SELECT COUNT(*) FROM " + d.QuoteIdent(table)
}
