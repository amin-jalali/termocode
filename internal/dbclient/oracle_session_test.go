package dbclient

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

// From a URL (DBX-63): oracle:// (and ora://) reads host, port, user and
// the service name; the password moves out of the saved URL.
func TestParseURLOracle(t *testing.T) {
	c, err := ParseURL("oracle://scott:tiger@db.local:1522/ORCLPDB1?TRACE+FILE=x")
	if err != nil {
		t.Fatal(err)
	}
	if c.Driver != "oracle" || c.Host != "db.local" || c.Port != 1522 || c.User != "scott" || c.Database != "ORCLPDB1" {
		t.Fatalf("conn = %+v", c)
	}
	if c.Password != "tiger" || strings.Contains(c.URL, "tiger") {
		t.Fatalf("password kept in the URL: %+v", c)
	}
	if c, err := ParseURL("ora://system@db/FREEPDB1"); err != nil || c.Driver != "oracle" || c.Port != 1521 || c.Database != "FREEPDB1" {
		t.Fatalf("ora:// = %+v, %v", c, err)
	}
	if dsn := OracleDSN(c, "tiger", nil); !strings.Contains(dsn, "scott:tiger@db.local:1522/ORCLPDB1") {
		t.Fatalf("dsn = %s", dsn)
	}
}

// A read-only Oracle session begins with SET TRANSACTION READ ONLY and no
// PostgreSQL statement_timeout (Oracle refuses it), and rolls back.
func TestOracleReadOnlyRunHasNoPostgresTimeout(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"X"}, Rows: [][]driver.Value{{int64(1)}}}
	db := dbtest.Open("oracle-ro", stub)
	defer db.Close()
	s := newSQLSession(db, OracleDialect{}, "oracle", Conn{Driver: "oracle"}, true)
	if r := s.Run(context.Background(), "SELECT 1 FROM dual", RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	joined := strings.Join(stub.Recorded(), "\n")
	if !strings.Contains(joined, "SET TRANSACTION READ ONLY") || strings.Contains(joined, "statement_timeout") || !strings.Contains(joined, "ROLLBACK") {
		t.Fatalf("read-only setup: %s", joined)
	}
}

// The plan (x) on Oracle: EXPLAIN PLAN with its own statement id and
// DBMS_XPLAN.DISPLAY on one connection, in a transaction that is rolled
// back (the plan table keeps nothing); it works on a read-only session
// because EXPLAIN PLAN never runs the statement.
func TestOraclePlanRunsExplainPlanAndRollsBack(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"PLAN_TABLE_OUTPUT"}, Rows: [][]driver.Value{{"Plan hash value: 1"}, {"|   0 | SELECT STATEMENT  |      |"}, {"|   1 |  TABLE ACCESS FULL| T    |"}}}
	db := dbtest.Open("oracle-plan", stub)
	defer db.Close()
	s := newSQLSession(db, OracleDialect{}, "oracle", Conn{Driver: "oracle"}, true)
	r := s.Run(context.Background(), OraclePlanForm("UPDATE t SET x = 1"), RunOptions{})
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	joined := strings.Join(stub.Recorded(), "\n")
	if !strings.Contains(joined, "EXPLAIN PLAN SET STATEMENT_ID = 'termocode' FOR UPDATE t SET x = 1") ||
		!strings.Contains(joined, "DBMS_XPLAN.DISPLAY('PLAN_TABLE', 'termocode'") || strings.Contains(joined, "SET TRANSACTION READ ONLY") {
		t.Fatalf("plan statements: %s", joined)
	}
	if b, c, rb := stub.TransactionCounts(); b != 1 || c != 0 || rb != 1 {
		t.Fatalf("begins %d commits %d rollbacks %d", b, c, rb)
	}
	if len(r.Rows) != 3 || r.Rows[2][0].Text != "|   1 |  TABLE ACCESS FULL| T    |" {
		t.Fatalf("rows = %+v", r.Rows)
	}
	if p := ParsePlan("oracle", r, false); !strings.Contains(p.Raw, "TABLE ACCESS FULL") {
		t.Fatalf("plan = %+v", p)
	}
	if r := s.Run(context.Background(), OraclePlanForm("SELECT 1 FROM dual; SELECT 2 FROM dual"), RunOptions{}); r.Err == nil {
		t.Fatal("two statements explained")
	}
}
