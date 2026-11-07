package dbclient

import "fmt"

// OracleDialect contains Oracle Database syntax and catalog queries.
// DialectFor returns it; go-ora opens the session (DBX-63).
type OracleDialect struct{}

// OracleShowPlan reads the plan that EXPLAIN PLAN FOR wrote.
const OracleShowPlan = `SELECT plan_table_output FROM TABLE(DBMS_XPLAN.DISPLAY())`

func (OracleDialect) QuoteIdent(s string) string { return PostgresDialect{}.QuoteIdent(s) }
func (OracleDialect) Placeholder(n int) string   { return fmt.Sprintf(":%d", n) }
func (OracleDialect) Limit(limit, offset int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", offset, limit)
}
func (OracleDialect) ReadOnlyBegin() string { return "SET TRANSACTION READ ONLY" }
func (OracleDialect) CatalogQueries() map[string]string {
	return map[string]string{
		"databases":    `SELECT SYS_CONTEXT('USERENV', 'DB_NAME') FROM dual`,
		"schemas":      `SELECT username FROM all_users WHERE oracle_maintained = 'N' ORDER BY username`,
		"objects":      `SELECT object_name, CASE object_type WHEN 'TABLE' THEN 'BASE TABLE' ELSE 'VIEW' END FROM all_objects WHERE owner = :1 AND object_type IN ('TABLE', 'VIEW') ORDER BY object_name`,
		"columns":      `SELECT column_name, data_type FROM all_tab_columns WHERE owner = :1 AND table_name = :2 ORDER BY column_id`,
		"keys":         `SELECT cc.column_name FROM all_constraints c JOIN all_cons_columns cc ON cc.owner = c.owner AND cc.constraint_name = c.constraint_name WHERE c.constraint_type = 'P' AND c.owner = :1 AND c.table_name = :2 ORDER BY cc.position`,
		"indexes":      `SELECT i.index_name, LISTAGG(ic.column_name, CHR(31)) WITHIN GROUP (ORDER BY ic.column_position), CASE i.uniqueness WHEN 'UNIQUE' THEN 1 ELSE 0 END, i.table_name FROM all_indexes i JOIN all_ind_columns ic ON ic.index_owner = i.owner AND ic.index_name = i.index_name WHERE i.table_owner = :1 AND i.table_name = :2 GROUP BY i.index_name, i.uniqueness, i.table_name ORDER BY i.index_name`,
		"foreign_keys": `SELECT c.constraint_name, cc.column_name, rc.table_name, rcc.column_name FROM all_constraints c JOIN all_cons_columns cc ON cc.owner = c.owner AND cc.constraint_name = c.constraint_name JOIN all_constraints rc ON rc.owner = c.r_owner AND rc.constraint_name = c.r_constraint_name JOIN all_cons_columns rcc ON rcc.owner = rc.owner AND rcc.constraint_name = rc.constraint_name AND rcc.position = cc.position WHERE c.constraint_type = 'R' AND c.owner = :1 AND c.table_name = :2 ORDER BY c.constraint_name, cc.position`,
		"routines":     `SELECT object_name FROM all_objects WHERE owner = :1 AND object_type IN ('PROCEDURE', 'FUNCTION') ORDER BY object_name`,
		"sequences":    `SELECT sequence_name FROM all_sequences WHERE sequence_owner = :1 ORDER BY sequence_name`,
		"packages":     `SELECT object_name FROM all_objects WHERE owner = :1 AND object_type = 'PACKAGE' ORDER BY object_name`,
		"triggers":     `SELECT trigger_name FROM all_triggers WHERE owner = :1 ORDER BY trigger_name`,
	}
}

// DDL reads the CREATE statement of a table in the current schema.
func (OracleDialect) DDL(table string) (string, []any) {
	return `SELECT DBMS_METADATA.GET_DDL('TABLE', :1) FROM dual`, []any{table}
}

// Explain writes the plan into the plan table; OracleShowPlan reads it.
// Oracle has no EXPLAIN ANALYZE, so analyze asks for the same plan.
func (OracleDialect) Explain(sql string, _ bool) string { return "EXPLAIN PLAN FOR " + sql }
