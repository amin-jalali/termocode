package dbclient

import (
	"strings"
	"testing"
)

func TestOracleDialectSyntax(t *testing.T) {
	var d Dialect = OracleDialect{}
	if got := d.QuoteIdent(`a"b`); got != `"a""b"` {
		t.Errorf("QuoteIdent = %s", got)
	}
	if got := d.Placeholder(2); got != ":2" {
		t.Errorf("Placeholder = %s", got)
	}
	if got := d.Limit(100, 200); got != " OFFSET 200 ROWS FETCH NEXT 100 ROWS ONLY" {
		t.Errorf("Limit = %q", got)
	}
	if got := d.Limit(0, 5); got != "" {
		t.Errorf("Limit(0, 5) = %q", got)
	}
	if got := d.ReadOnlyBegin(); got != "SET TRANSACTION READ ONLY" {
		t.Errorf("ReadOnlyBegin = %s", got)
	}
	if got := d.Explain("SELECT 1 FROM dual", false); got != "EXPLAIN PLAN FOR SELECT 1 FROM dual" {
		t.Errorf("Explain = %s", got)
	}
	if got := d.Explain("SELECT 1 FROM dual", true); got != "EXPLAIN PLAN FOR SELECT 1 FROM dual" {
		t.Errorf("Explain analyze = %s", got)
	}
	if !strings.Contains(OracleShowPlan, "DBMS_XPLAN.DISPLAY") {
		t.Errorf("OracleShowPlan = %s", OracleShowPlan)
	}
}

func TestOracleDialectCatalogBindsEveryName(t *testing.T) {
	q := OracleDialect{}.CatalogQueries()
	for _, k := range []string{"schemas", "objects", "columns", "keys", "indexes", "foreign_keys", "routines", "sequences", "packages"} {
		sql := q[k]
		if sql == "" {
			t.Errorf("no %s query", k)
			continue
		}
		if k != "schemas" && !strings.Contains(sql, ":1") {
			t.Errorf("%s does not bind the schema: %s", k, sql)
		}
		if strings.Contains(sql, "'%s'") || strings.Contains(sql, "$1") || strings.Contains(sql, "?") {
			t.Errorf("%s has a foreign placeholder: %s", k, sql)
		}
	}
	for _, k := range []string{"columns", "keys", "indexes", "foreign_keys"} {
		if !strings.Contains(q[k], ":2") {
			t.Errorf("%s does not bind the table: %s", k, q[k])
		}
	}
	sql, args := OracleDialect{}.DDL("USERS")
	if !strings.Contains(sql, "DBMS_METADATA.GET_DDL") || strings.Contains(sql, "USERS") || len(args) != 1 || args[0] != "USERS" {
		t.Errorf("DDL = %s %v", sql, args)
	}
}

func TestOracleIsWired(t *testing.T) {
	// DBX-63 brought the go-ora driver, so "oracle" now has its dialect.
	if _, ok := DialectFor("oracle").(OracleDialect); !ok {
		t.Fatalf("oracle dialect = %T", DialectFor("oracle"))
	}
}
