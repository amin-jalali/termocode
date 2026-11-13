package dbclient

import "fmt"

// Dialects of the DBX-71 engines. Every catalog query binds its
// arguments in the order Introspect passes them: the schema for a list,
// the schema and the table for one object. Firebird has no schemas: its
// queries take the schema argument and ignore it.

// currentSchemaQuerier is a dialect with its own "current schema" query
// (Introspect asks it when only a table is named).
type currentSchemaQuerier interface{ CurrentSchemaQuery() string }

// fbIgnore consumes the schema argument in a Firebird query.
const fbIgnore = `CAST(? AS VARCHAR(63)) IS NOT NULL`

// fbType names a Firebird column type from RDB$FIELDS (alias f).
const fbType = `CASE f.RDB$FIELD_TYPE WHEN 7 THEN 'SMALLINT' WHEN 8 THEN 'INTEGER' WHEN 16 THEN 'BIGINT' WHEN 10 THEN 'FLOAT' WHEN 27 THEN 'DOUBLE PRECISION' WHEN 12 THEN 'DATE' WHEN 13 THEN 'TIME' WHEN 35 THEN 'TIMESTAMP' WHEN 23 THEN 'BOOLEAN' WHEN 14 THEN 'CHAR(' || f.RDB$CHARACTER_LENGTH || ')' WHEN 37 THEN 'VARCHAR(' || f.RDB$CHARACTER_LENGTH || ')' WHEN 261 THEN 'BLOB' ELSE 'OTHER' END`

// FirebirdDialect is Firebird SQL (3.0 and newer).
type FirebirdDialect struct{}

func (FirebirdDialect) QuoteIdent(s string) string { return PostgresDialect{}.QuoteIdent(s) }
func (FirebirdDialect) Placeholder(int) string     { return "?" }
func (FirebirdDialect) Limit(limit, offset int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", offset, limit)
}
func (FirebirdDialect) ReadOnlyBegin() string { return "SET TRANSACTION READ ONLY" }

// CurrentSchemaQuery: one schema, "default".
func (FirebirdDialect) CurrentSchemaQuery() string { return `SELECT 'default' FROM RDB$DATABASE` }
func (FirebirdDialect) CatalogQueries() map[string]string {
	return map[string]string{
		"databases":    `SELECT TRIM(MON$DATABASE_NAME) FROM MON$DATABASE`,
		"schemas":      `SELECT 'default' FROM RDB$DATABASE`,
		"objects":      `SELECT TRIM(RDB$RELATION_NAME), CASE WHEN RDB$VIEW_BLR IS NULL THEN 'BASE TABLE' ELSE 'VIEW' END FROM RDB$RELATIONS WHERE COALESCE(RDB$SYSTEM_FLAG, 0) = 0 AND ` + fbIgnore + ` ORDER BY 1`,
		"columns":      `SELECT TRIM(rf.RDB$FIELD_NAME), ` + fbType + ` FROM RDB$RELATION_FIELDS rf JOIN RDB$FIELDS f ON f.RDB$FIELD_NAME = rf.RDB$FIELD_SOURCE WHERE ` + fbIgnore + ` AND rf.RDB$RELATION_NAME = ? ORDER BY rf.RDB$FIELD_POSITION`,
		"keys":         `SELECT TRIM(s.RDB$FIELD_NAME) FROM RDB$RELATION_CONSTRAINTS rc JOIN RDB$INDEX_SEGMENTS s ON s.RDB$INDEX_NAME = rc.RDB$INDEX_NAME WHERE ` + fbIgnore + ` AND rc.RDB$CONSTRAINT_TYPE = 'PRIMARY KEY' AND rc.RDB$RELATION_NAME = ? ORDER BY s.RDB$FIELD_POSITION`,
		"indexes":      `SELECT TRIM(i.RDB$INDEX_NAME), LIST(TRIM(s.RDB$FIELD_NAME), ','), MAX(COALESCE(i.RDB$UNIQUE_FLAG, 0)) FROM RDB$INDICES i JOIN RDB$INDEX_SEGMENTS s ON s.RDB$INDEX_NAME = i.RDB$INDEX_NAME WHERE ` + fbIgnore + ` AND i.RDB$RELATION_NAME = ? GROUP BY i.RDB$INDEX_NAME ORDER BY 1`,
		"foreign_keys": `SELECT TRIM(rc.RDB$CONSTRAINT_NAME), TRIM(s.RDB$FIELD_NAME), TRIM(rc2.RDB$RELATION_NAME), TRIM(s2.RDB$FIELD_NAME) FROM RDB$RELATION_CONSTRAINTS rc JOIN RDB$REF_CONSTRAINTS ref ON ref.RDB$CONSTRAINT_NAME = rc.RDB$CONSTRAINT_NAME JOIN RDB$RELATION_CONSTRAINTS rc2 ON rc2.RDB$CONSTRAINT_NAME = ref.RDB$CONST_NAME_UQ JOIN RDB$INDEX_SEGMENTS s ON s.RDB$INDEX_NAME = rc.RDB$INDEX_NAME JOIN RDB$INDEX_SEGMENTS s2 ON s2.RDB$INDEX_NAME = rc2.RDB$INDEX_NAME AND s2.RDB$FIELD_POSITION = s.RDB$FIELD_POSITION WHERE ` + fbIgnore + ` AND rc.RDB$CONSTRAINT_TYPE = 'FOREIGN KEY' AND rc.RDB$RELATION_NAME = ? ORDER BY 1, s.RDB$FIELD_POSITION`,
		"routines":     `SELECT TRIM(RDB$PROCEDURE_NAME) FROM RDB$PROCEDURES WHERE COALESCE(RDB$SYSTEM_FLAG, 0) = 0 AND ` + fbIgnore + ` ORDER BY 1`,
		"sequences":    `SELECT TRIM(RDB$GENERATOR_NAME) FROM RDB$GENERATORS WHERE COALESCE(RDB$SYSTEM_FLAG, 0) = 0 AND ` + fbIgnore + ` ORDER BY 1`,
		"triggers":     `SELECT TRIM(RDB$TRIGGER_NAME) FROM RDB$TRIGGERS WHERE COALESCE(RDB$SYSTEM_FLAG, 0) = 0 AND ` + fbIgnore + ` ORDER BY 1`,
		"roles":        `SELECT TRIM(RDB$ROLE_NAME) FROM RDB$ROLES ORDER BY 1`,
	}
}

// DDL builds a CREATE TABLE from the column catalog (Firebird has no
// SQL function for it).
func (FirebirdDialect) DDL(table string) (string, []any) {
	return `SELECT 'CREATE TABLE "' || TRIM(MAX(c.rel)) || '" (' || LIST(c.col, ', ') || ')' FROM (SELECT rf.RDB$RELATION_NAME AS rel, '"' || TRIM(rf.RDB$FIELD_NAME) || '" ' || ` + fbType + ` || CASE WHEN rf.RDB$NULL_FLAG = 1 THEN ' NOT NULL' ELSE '' END AS col FROM RDB$RELATION_FIELDS rf JOIN RDB$FIELDS f ON f.RDB$FIELD_NAME = rf.RDB$FIELD_SOURCE WHERE rf.RDB$RELATION_NAME = ? ORDER BY rf.RDB$FIELD_POSITION) c`, []any{table}
}

// Explain is "": Firebird shows plans only through the client API, not
// through a SQL statement.
func (FirebirdDialect) Explain(string, bool) string { return "" }

// VerticaDialect is Vertica SQL (v_catalog).
type VerticaDialect struct{}

func (VerticaDialect) QuoteIdent(s string) string { return PostgresDialect{}.QuoteIdent(s) }
func (VerticaDialect) Placeholder(int) string     { return "?" }
func (VerticaDialect) Limit(limit, offset int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
}
func (VerticaDialect) ReadOnlyBegin() string      { return "START TRANSACTION READ ONLY" }
func (VerticaDialect) CurrentSchemaQuery() string { return `SELECT CURRENT_SCHEMA()` }
func (VerticaDialect) CatalogQueries() map[string]string {
	return map[string]string{
		"databases":    `SELECT database_name FROM v_catalog.databases ORDER BY 1`,
		"schemas":      `SELECT schema_name FROM v_catalog.schemata WHERE NOT is_system_schema ORDER BY 1`,
		"objects":      `SELECT table_name, CASE table_type WHEN 'VIEW' THEN 'VIEW' ELSE 'BASE TABLE' END FROM v_catalog.all_tables WHERE schema_name = ? AND table_type IN ('TABLE', 'VIEW') ORDER BY 1`,
		"columns":      `SELECT column_name, data_type FROM v_catalog.columns WHERE table_schema = ? AND table_name = ? ORDER BY ordinal_position`,
		"keys":         `SELECT column_name FROM v_catalog.primary_keys WHERE table_schema = ? AND table_name = ? ORDER BY ordinal_position`,
		"foreign_keys": `SELECT constraint_name, column_name, reference_table_name, reference_column_name FROM v_catalog.foreign_keys WHERE table_schema = ? AND table_name = ? ORDER BY constraint_name, ordinal_position`,
		"routines":     `SELECT function_name FROM v_catalog.user_functions WHERE schema_name = ? ORDER BY 1`,
		"sequences":    `SELECT sequence_name FROM v_catalog.sequences WHERE sequence_schema = ? ORDER BY 1`,
		"roles":        `SELECT name FROM v_catalog.roles ORDER BY 1`,
	}
}

// DDL asks the server for the table's CREATE statements.
func (VerticaDialect) DDL(table string) (string, []any) {
	return `SELECT EXPORT_TABLES('', ?)`, []any{table}
}

// Explain: EXPLAIN shows the plan; PROFILE runs the statement and keeps
// its counters (the session rolls a write back).
func (VerticaDialect) Explain(sql string, analyze bool) string {
	if analyze {
		return "PROFILE " + sql
	}
	return "EXPLAIN " + sql
}

// ExasolDialect is Exasol SQL (SYS.EXA_ALL_* views).
type ExasolDialect struct{}

func (ExasolDialect) QuoteIdent(s string) string { return PostgresDialect{}.QuoteIdent(s) }
func (ExasolDialect) Placeholder(int) string     { return "?" }
func (ExasolDialect) Limit(limit, offset int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
}

// ReadOnlyBegin is "": Exasol has no read-only transaction, so a
// read-only session relies on the statement check alone.
func (ExasolDialect) ReadOnlyBegin() string      { return "" }
func (ExasolDialect) CurrentSchemaQuery() string { return `SELECT CURRENT_SCHEMA` }
func (ExasolDialect) CatalogQueries() map[string]string {
	return map[string]string{
		"databases":    `SELECT SCHEMA_NAME FROM SYS.EXA_SCHEMAS ORDER BY 1`,
		"schemas":      `SELECT SCHEMA_NAME FROM SYS.EXA_SCHEMAS ORDER BY 1`,
		"objects":      `SELECT OBJECT_NAME, CASE OBJECT_TYPE WHEN 'VIEW' THEN 'VIEW' ELSE 'BASE TABLE' END FROM SYS.EXA_ALL_OBJECTS WHERE ROOT_NAME = ? AND OBJECT_TYPE IN ('TABLE', 'VIEW') ORDER BY 1`,
		"columns":      `SELECT COLUMN_NAME, COLUMN_TYPE FROM SYS.EXA_ALL_COLUMNS WHERE COLUMN_SCHEMA = ? AND COLUMN_TABLE = ? ORDER BY COLUMN_ORDINAL_POSITION`,
		"keys":         `SELECT COLUMN_NAME FROM SYS.EXA_ALL_CONSTRAINT_COLUMNS WHERE CONSTRAINT_SCHEMA = ? AND CONSTRAINT_TABLE = ? AND CONSTRAINT_TYPE = 'PRIMARY KEY' ORDER BY ORDINAL_POSITION`,
		"foreign_keys": `SELECT CONSTRAINT_NAME, COLUMN_NAME, REFERENCED_TABLE, REFERENCED_COLUMN FROM SYS.EXA_ALL_CONSTRAINT_COLUMNS WHERE CONSTRAINT_SCHEMA = ? AND CONSTRAINT_TABLE = ? AND CONSTRAINT_TYPE = 'FOREIGN KEY' ORDER BY CONSTRAINT_NAME, ORDINAL_POSITION`,
		"routines":     `SELECT OBJECT_NAME FROM SYS.EXA_ALL_OBJECTS WHERE ROOT_NAME = ? AND OBJECT_TYPE IN ('FUNCTION', 'SCRIPT') ORDER BY 1`,
		"roles":        `SELECT ROLE_NAME FROM SYS.EXA_ALL_ROLES ORDER BY 1`,
	}
}

// DDL builds a CREATE TABLE from the column catalog of the open schema.
func (ExasolDialect) DDL(table string) (string, []any) {
	return `SELECT 'CREATE TABLE "' || COLUMN_SCHEMA || '"."' || COLUMN_TABLE || '" (' || GROUP_CONCAT('"' || COLUMN_NAME || '" ' || COLUMN_TYPE || CASE WHEN COLUMN_IS_NULLABLE THEN '' ELSE ' NOT NULL' END ORDER BY COLUMN_ORDINAL_POSITION SEPARATOR ', ') || ')' FROM SYS.EXA_ALL_COLUMNS WHERE COLUMN_SCHEMA = CURRENT_SCHEMA AND COLUMN_TABLE = ? GROUP BY COLUMN_SCHEMA, COLUMN_TABLE`, []any{table}
}

// Explain is "": Exasol profiles through session settings, not EXPLAIN.
func (ExasolDialect) Explain(string, bool) string { return "" }

// HANADialect is SAP HANA SQL (SYS catalog views).
type HANADialect struct{}

func (HANADialect) QuoteIdent(s string) string { return PostgresDialect{}.QuoteIdent(s) }
func (HANADialect) Placeholder(int) string     { return "?" }
func (HANADialect) Limit(limit, offset int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
}
func (HANADialect) ReadOnlyBegin() string      { return "SET TRANSACTION READ ONLY" }
func (HANADialect) CurrentSchemaQuery() string { return `SELECT CURRENT_SCHEMA FROM DUMMY` }
func (HANADialect) CatalogQueries() map[string]string {
	return map[string]string{
		"databases":    `SELECT DATABASE_NAME FROM SYS.M_DATABASE`,
		"schemas":      `SELECT SCHEMA_NAME FROM SYS.SCHEMAS WHERE HAS_PRIVILEGES = 'TRUE' ORDER BY 1`,
		"objects":      `SELECT OBJECT_NAME, CASE OBJECT_TYPE WHEN 'VIEW' THEN 'VIEW' ELSE 'BASE TABLE' END FROM SYS.OBJECTS WHERE SCHEMA_NAME = ? AND OBJECT_TYPE IN ('TABLE', 'VIEW') ORDER BY 1`,
		"columns":      `SELECT COLUMN_NAME, DATA_TYPE_NAME FROM SYS.TABLE_COLUMNS WHERE SCHEMA_NAME = ? AND TABLE_NAME = ? ORDER BY POSITION`,
		"keys":         `SELECT COLUMN_NAME FROM SYS.CONSTRAINTS WHERE SCHEMA_NAME = ? AND TABLE_NAME = ? AND IS_PRIMARY_KEY = 'TRUE' ORDER BY POSITION`,
		"indexes":      `SELECT INDEX_NAME, STRING_AGG(COLUMN_NAME, ',' ORDER BY POSITION), MAX(CASE WHEN CONSTRAINT IN ('UNIQUE', 'PRIMARY KEY') THEN 1 ELSE 0 END) FROM SYS.INDEX_COLUMNS WHERE SCHEMA_NAME = ? AND TABLE_NAME = ? GROUP BY INDEX_NAME ORDER BY 1`,
		"foreign_keys": `SELECT CONSTRAINT_NAME, COLUMN_NAME, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME FROM SYS.REFERENTIAL_CONSTRAINTS WHERE SCHEMA_NAME = ? AND TABLE_NAME = ? ORDER BY CONSTRAINT_NAME, POSITION`,
		"routines":     `SELECT PROCEDURE_NAME FROM SYS.PROCEDURES WHERE SCHEMA_NAME = ? ORDER BY 1`,
		"sequences":    `SELECT SEQUENCE_NAME FROM SYS.SEQUENCES WHERE SCHEMA_NAME = ? ORDER BY 1`,
		"triggers":     `SELECT TRIGGER_NAME FROM SYS.TRIGGERS WHERE SCHEMA_NAME = ? ORDER BY 1`,
		"roles":        `SELECT ROLE_NAME FROM SYS.ROLES ORDER BY 1`,
	}
}

// DDL builds a CREATE TABLE from the column catalog of the current
// schema.
func (HANADialect) DDL(table string) (string, []any) {
	return `SELECT 'CREATE TABLE "' || SCHEMA_NAME || '"."' || TABLE_NAME || '" (' || STRING_AGG('"' || COLUMN_NAME || '" ' || DATA_TYPE_NAME || CASE WHEN IS_NULLABLE = 'FALSE' THEN ' NOT NULL' ELSE '' END, ', ' ORDER BY POSITION) || ')' FROM SYS.TABLE_COLUMNS WHERE SCHEMA_NAME = CURRENT_SCHEMA AND TABLE_NAME = ? GROUP BY SCHEMA_NAME, TABLE_NAME`, []any{table}
}

// Explain is "": EXPLAIN PLAN writes into a table that a second
// statement has to read.
func (HANADialect) Explain(string, bool) string { return "" }

// errNoExplain is the answer of Explain on an engine without a SQL form.
func errNoExplain(engine string) error {
	return fmt.Errorf("%s has no EXPLAIN statement that termocode can run", engine)
}

// Db2Dialect is IBM Db2 for LUW (SYSCAT catalog views).
type Db2Dialect struct{}

func (Db2Dialect) QuoteIdent(s string) string { return PostgresDialect{}.QuoteIdent(s) }
func (Db2Dialect) Placeholder(int) string     { return "?" }
func (Db2Dialect) Limit(limit, offset int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", offset, limit)
}

// ReadOnlyBegin is "": Db2 has no read-only transaction, so a read-only
// session relies on the statement check alone (the AI tools refuse Db2).
func (Db2Dialect) ReadOnlyBegin() string { return "" }
func (Db2Dialect) CurrentSchemaQuery() string {
	return `SELECT RTRIM(CURRENT SCHEMA) FROM SYSIBM.SYSDUMMY1`
}
func (Db2Dialect) CatalogQueries() map[string]string {
	return map[string]string{
		"databases":    `SELECT RTRIM(CURRENT SERVER) FROM SYSIBM.SYSDUMMY1`,
		"schemas":      `SELECT RTRIM(SCHEMANAME) FROM SYSCAT.SCHEMATA WHERE SCHEMANAME NOT LIKE 'SYS%' AND SCHEMANAME NOT IN ('NULLID', 'SQLJ') ORDER BY 1`,
		"objects":      `SELECT TABNAME, CASE TYPE WHEN 'V' THEN 'VIEW' ELSE 'BASE TABLE' END FROM SYSCAT.TABLES WHERE TABSCHEMA = ? AND TYPE IN ('T', 'V') ORDER BY 1`,
		"columns":      `SELECT COLNAME, TYPENAME || CASE WHEN TYPENAME IN ('VARCHAR', 'CHARACTER') THEN '(' || RTRIM(CHAR(LENGTH)) || ')' ELSE '' END FROM SYSCAT.COLUMNS WHERE TABSCHEMA = ? AND TABNAME = ? ORDER BY COLNO`,
		"keys":         `SELECT k.COLNAME FROM SYSCAT.KEYCOLUSE k JOIN SYSCAT.TABCONST t ON t.CONSTNAME = k.CONSTNAME AND t.TABSCHEMA = k.TABSCHEMA AND t.TABNAME = k.TABNAME WHERE t.TYPE = 'P' AND k.TABSCHEMA = ? AND k.TABNAME = ? ORDER BY k.COLSEQ`,
		"indexes":      `SELECT i.INDNAME, LISTAGG(c.COLNAME, ',') WITHIN GROUP (ORDER BY c.COLSEQ), CASE WHEN i.UNIQUERULE IN ('U', 'P') THEN 1 ELSE 0 END FROM SYSCAT.INDEXES i JOIN SYSCAT.INDEXCOLUSE c ON c.INDSCHEMA = i.INDSCHEMA AND c.INDNAME = i.INDNAME WHERE i.TABSCHEMA = ? AND i.TABNAME = ? GROUP BY i.INDNAME, i.UNIQUERULE ORDER BY 1`,
		"foreign_keys": `SELECT r.CONSTNAME, k.COLNAME, r.REFTABNAME, p.COLNAME FROM SYSCAT.REFERENCES r JOIN SYSCAT.KEYCOLUSE k ON k.CONSTNAME = r.CONSTNAME AND k.TABSCHEMA = r.TABSCHEMA AND k.TABNAME = r.TABNAME JOIN SYSCAT.KEYCOLUSE p ON p.CONSTNAME = r.REFKEYNAME AND p.TABSCHEMA = r.REFTABSCHEMA AND p.TABNAME = r.REFTABNAME AND p.COLSEQ = k.COLSEQ WHERE r.TABSCHEMA = ? AND r.TABNAME = ? ORDER BY r.CONSTNAME, k.COLSEQ`,
		"routines":     `SELECT ROUTINENAME FROM SYSCAT.ROUTINES WHERE ROUTINESCHEMA = ? ORDER BY 1`,
		"sequences":    `SELECT SEQNAME FROM SYSCAT.SEQUENCES WHERE SEQSCHEMA = ? AND SEQTYPE = 'S' ORDER BY 1`,
		"triggers":     `SELECT TRIGNAME FROM SYSCAT.TRIGGERS WHERE TRIGSCHEMA = ? ORDER BY 1`,
		"roles":        `SELECT ROLENAME FROM SYSCAT.ROLES ORDER BY 1`,
	}
}

// DDL builds a CREATE TABLE from the column catalog of the current schema.
func (Db2Dialect) DDL(table string) (string, []any) {
	return `SELECT 'CREATE TABLE "' || RTRIM(TABSCHEMA) || '"."' || TABNAME || '" (' || LISTAGG('"' || COLNAME || '" ' || TYPENAME || CASE WHEN NULLS = 'N' THEN ' NOT NULL' ELSE '' END, ', ') WITHIN GROUP (ORDER BY COLNO) || ')' FROM SYSCAT.COLUMNS WHERE TABSCHEMA = CURRENT SCHEMA AND TABNAME = ? GROUP BY TABSCHEMA, TABNAME`, []any{table}
}

// Explain is "": EXPLAIN fills plan tables that need setup, not a result.
func (Db2Dialect) Explain(string, bool) string { return "" }
