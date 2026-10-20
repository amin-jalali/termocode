package dbclient

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/go-sql-driver/mysql"
)

func TestSQLSessionCapsRowsAndReportsAffectedOnlyForDML(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"n"}, Rows: [][]driver.Value{{int64(1)}}, RepeatRows: 3000000, ExecRows: 4}
	db := dbtest.Open("caps", stub)
	defer db.Close()
	s := newSQLSession(db, PostgresDialect{}, "postgres", Conn{}, false)
	s.newDB = func() (*sql.DB, error) { return dbtest.Open("caps-ro", stub), nil }
	defer s.Close()
	r := s.Run(context.Background(), "SELECT n FROM t", RunOptions{MaxRows: 2})
	if r.Err != nil || len(r.Rows) != 2 || !r.Truncated || r.Affected != 0 {
		t.Fatalf("read result = %+v", r)
	}
	if got := stub.ReadCount(); got != 3 {
		t.Fatalf("read %d rows before applying the cap, want 3", got)
	}
	r = s.Run(context.Background(), "UPDATE t SET n=1", RunOptions{})
	if r.Err != nil || r.Affected != 4 {
		t.Fatalf("write result = %+v", r)
	}
	r = s.Run(context.Background(), "CREATE TABLE t(n int)", RunOptions{})
	if r.Err != nil || r.Affected != 0 {
		t.Fatalf("DDL result = %+v", r)
	}
}

func TestSQLSessionRefusesReadOnlySideEffectsBeforeExecution(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"n"}, Rows: [][]driver.Value{{int64(1)}}}
	db := dbtest.Open("readonly-gate", stub)
	defer db.Close()
	s := newSQLSession(db, MySQLDialect{}, "mysql", Conn{}, false)
	s.newDB = func() (*sql.DB, error) { return dbtest.Open("readonly-gate-clone", stub), nil }
	roSession, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	ro := roSession.(*SQLSession)
	defer ro.Close()
	if ro.db == s.db || !ro.ownsDB {
		t.Fatal("read-only session did not get its own database pool")
	}
	for _, query := range []string{
		"CREATE TABLE dbint_forbidden (id INT)", "TRUNCATE TABLE t", "RENAME TABLE t TO t2",
		"SELECT 1 INTO OUTFILE '/tmp/x'", "COPY t TO '/tmp/x'", "COPY t TO PROGRAM 'x'",
		"TABLE ro_t INTO OUTFILE '/tmp/x'", "VALUES (1) INTO @x",
		"SELECT 1 /*!50000 INTO OUTFILE '/tmp/x' */", "SELECT 1 /*! INTO OUTFILE '/tmp/x' */",
		"SELECT 1 /*M! INTO OUTFILE '/tmp/x' */",
		"SELECT GET_LOCK('x', 1)", "SELECT RELEASE_LOCK('x')", "PREPARE p FROM 'SELECT 1'",
		"EXECUTE p", "DEALLOCATE p", "SET SESSION max_execution_time = 1", "LOCK TABLES t READ",
		"SELECT pg_advisory_unlock(1)", "SELECT pg_advisory_xact_lock(1)",
		"SELECT LOAD_FILE('/etc/passwd')", "SELECT pg_logical_emit_message(true, 'x', 'y')",
		"SELECT txid_current()", "SELECT pg_current_xact_id()", "SELECT BENCHMARK(1000000, SHA2('x', 256))",
	} {
		if r := ro.Run(context.Background(), query, RunOptions{}); r.Err == nil {
			t.Errorf("read-only session accepted %q", query)
		}
	}
	for _, query := range stub.Recorded() {
		for _, forbidden := range []string{"CREATE TABLE dbint_forbidden", "TRUNCATE TABLE", "RENAME TABLE", "INTO OUTFILE", "COPY t", "GET_LOCK", "RELEASE_LOCK", "PREPARE p", "EXECUTE p", "DEALLOCATE p", "LOCK TABLES"} {
			if strings.Contains(query, forbidden) {
				t.Fatalf("refused query reached the driver: %q", query)
			}
		}
	}
}

func TestPostgresConnectRedactionKeepsFailureReason(t *testing.T) {
	for _, raw := range []string{
		"failed to connect to `user=termocode database=termocode`: 127.0.0.1:5432 (localhost): password authentication failed for user \"termocode\" (SQLSTATE 28P01)",
		"failed to connect to `user=termocode database=missing`: dial tcp 127.0.0.1:5432: connect: connection refused",
		"failed to connect to `user=termocode database=missing`: FATAL: database \"missing\" does not exist (SQLSTATE 3D000)",
	} {
		got := Redact(raw)
		for _, reason := range []string{"password authentication failed", "connection refused", `database "missing" does not exist`} {
			if strings.Contains(raw, reason) && !strings.Contains(got, reason) {
				t.Errorf("Redact(%q) lost %q: %q", raw, reason, got)
			}
		}
		if strings.Contains(got, "termocode database") == false {
			t.Errorf("Redact(%q) removed connection context: %q", raw, got)
		}
	}
	raw := "failed to connect to `user=termocode password=secret database=termocode`: 127.0.0.1:5432: password authentication failed for user \"termocode\" (SQLSTATE 28P01)"
	want := "failed to connect to `user=termocode password=[redacted] database=termocode`: 127.0.0.1:5432: password authentication failed for user \"termocode\" (SQLSTATE 28P01)"
	if got := Redact(raw); got != want {
		t.Errorf("Redact(pgconn error) = %q, want %q", got, want)
	}
}

func TestSQLCellDateOnly(t *testing.T) {
	got := sqlCell(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), "DATE")
	if got.Text != "2026-01-02" {
		t.Fatalf("DATE cell = %+v", got)
	}
}

func TestSQLSessionQueriesRowReturningWritesAndTextBytes(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"value"}, ColumnTypes: []string{"VARCHAR"}, Rows: [][]driver.Value{{[]byte("plain text")}}}
	db := dbtest.Open("returning", stub)
	defer db.Close()
	s := newSQLSession(db, PostgresDialect{}, "postgres", Conn{}, false)
	defer s.Close()
	r := s.Run(context.Background(), "INSERT INTO t(value) VALUES ('plain text') RETURNING value", RunOptions{})
	if r.Err != nil || len(r.Rows) != 1 || r.Affected != 1 || r.Rows[0][0].Text != "plain text" || r.Rows[0][0].Kind == "binary" {
		t.Fatalf("RETURNING result = %+v", r)
	}
	if got := strings.Join(stub.Recorded(), "\n"); !strings.Contains(got, "INSERT INTO") {
		t.Fatalf("query not executed: %s", got)
	}
}

func TestSQLSessionWritableCTEAffectedRows(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"id"}, Rows: [][]driver.Value{{int64(1)}, {int64(2)}}}
	db := dbtest.Open("cte-returning", stub)
	defer db.Close()
	s := newSQLSession(db, PostgresDialect{}, "postgres", Conn{}, false)
	defer s.Close()
	r := s.Run(context.Background(), "WITH changed AS (DELETE FROM t RETURNING id) SELECT id FROM changed", RunOptions{})
	if r.Err != nil || len(r.Rows) != 2 || r.Affected != 2 {
		t.Fatalf("writable CTE result = %+v", r)
	}
}

func TestMariaDBAnalyzeUsesRowsQuery(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"Table", "Op", "Msg_type", "Msg_text"}, Rows: [][]driver.Value{{"t", "analyze", "status", "OK"}}}
	db := dbtest.Open("mariadb-analyze-rows", stub)
	defer db.Close()
	s := newSQLSession(db, MySQLDialect{}, "mysql", Conn{}, false)
	s.mariadb = true
	defer s.Close()
	r := s.Run(context.Background(), "ANALYZE TABLE t", RunOptions{})
	if r.Err != nil || len(r.Rows) != 1 || !strings.Contains(strings.Join(stub.Recorded(), "\n"), "ANALYZE TABLE t") {
		t.Fatalf("MariaDB ANALYZE result = %+v; statements=%v", r, stub.Recorded())
	}
}

func TestSQLSessionPreservesBinaryBytesAndBlobNote(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"payload"}, ColumnTypes: []string{"BLOB"}, Rows: [][]driver.Value{{[]byte{0, 255}}}}
	db := dbtest.Open("binary-cell", stub)
	defer db.Close()
	s := newSQLSession(db, MySQLDialect{}, "mysql", Conn{}, false)
	defer s.Close()
	r := s.Run(context.Background(), "SELECT payload FROM t", RunOptions{})
	if r.Err != nil || len(r.Rows) != 1 || r.Rows[0][0].Text != "0x00ff" || r.Rows[0][0].Kind != "binary" || len(r.Notices) == 0 {
		t.Fatalf("binary cell = %+v", r)
	}
}

func TestMariaDBAnalyzeUsesAnalyzeSyntax(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"plan"}, Rows: [][]driver.Value{{"ok"}}}
	db := dbtest.Open("mariadb-analyze", stub)
	defer db.Close()
	s := newSQLSession(db, MySQLDialect{}, "mysql", Conn{}, false)
	s.mariadb = true
	defer s.Close()
	if r := s.Explain(context.Background(), "SELECT 1", true); r.Err != nil {
		t.Fatal(r.Err)
	}
	if got := strings.Join(stub.Recorded(), "\n"); !strings.Contains(got, "ANALYZE SELECT 1") {
		t.Fatalf("MariaDB explain syntax missing: %s", got)
	}
}

func TestMariaDBReadOnlyTimeoutSetting(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"value"}, Rows: [][]driver.Value{{int64(1)}}}
	db := dbtest.Open("mariadb-timeout", stub)
	defer db.Close()
	s := newSQLSession(db, MySQLDialect{}, "mysql", Conn{}, false)
	s.newDB = func() (*sql.DB, error) { return dbtest.Open("mariadb-timeout-ro", stub), nil }
	defer s.Close()
	s.mariadb = true
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if r := ro.Run(context.Background(), "SELECT 1", RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	joined := strings.Join(stub.Recorded(), "\n")
	if !strings.Contains(joined, "SET SESSION max_statement_time = 30") {
		t.Fatalf("MariaDB timeout setting missing: %s", joined)
	}
}

func TestRedactKeepsAuthenticationReasonAndMySQLPasswordMarker(t *testing.T) {
	pg := Redact("failed to connect to `user=termocode database=termocode`: 127.0.0.1: password authentication failed for user termocode")
	if !strings.Contains(pg, "password authentication failed") {
		t.Fatalf("authentication reason was lost: %q", pg)
	}
	for _, marker := range []string{"YES", "NO"} {
		got := Redact("Error 1045: Access denied (using password: " + marker + ")")
		if !strings.Contains(got, "using password: "+marker) {
			t.Errorf("MySQL marker was changed: %q", got)
		}
	}
}

func TestMySQLQueryTimeoutErrorIsReportedAsStopped(t *testing.T) {
	for _, number := range []uint16{3024, 1969} {
		err := queryError(context.Background(), &mysql.MySQLError{Number: number, Message: "query interrupted"})
		if err == nil || err.Error() != "stopped after 30 s (timeout)" {
			t.Errorf("timeout error %d = %v", number, err)
		}
	}
	if err := queryError(context.Background(), testSQLStateError("57014")); err == nil || err.Error() != "stopped after 30 s (timeout)" {
		t.Errorf("PostgreSQL cancellation error = %v", err)
	}
}

type testSQLStateError string

func (e testSQLStateError) Error() string    { return "canceling statement due to statement timeout" }
func (e testSQLStateError) SQLState() string { return string(e) }

func TestSQLSessionKillsMySQLQueryOnSeparatePool(t *testing.T) {
	stub := &dbtest.Stub{}
	primary := dbtest.Open("mysql-kill-primary", stub)
	defer primary.Close()
	s := newSQLSession(primary, MySQLDialect{}, "mysql", Conn{}, false)
	s.newDB = func() (*sql.DB, error) { return dbtest.Open("mysql-kill-secondary", stub), nil }
	s.killMySQLQuery(42)
	joined := strings.Join(stub.Recorded(), "\n")
	if !strings.Contains(joined, "KILL QUERY 42") {
		t.Fatalf("kill query was not issued on the secondary pool: %s", joined)
	}
}

func TestSQLSessionRefusesMultipleStatements(t *testing.T) {
	stub := &dbtest.Stub{}
	db := dbtest.Open("multi-statement", stub)
	defer db.Close()
	s := newSQLSession(db, PostgresDialect{}, "postgres", Conn{}, false)
	defer s.Close()
	if r := s.Run(context.Background(), "SELECT 1; SELECT 2", RunOptions{}); r.Err == nil {
		t.Fatal("accepted multiple statements")
	}
	if got := stub.Recorded(); len(got) != 0 {
		t.Fatalf("executed statements: %v", got)
	}
}

func TestCatalogObjectQueriesBindTheObjectName(t *testing.T) {
	cases := map[string]struct {
		dialect Dialect
		name    string
		marker  string
	}{
		"postgres": {PostgresDialect{}, "postgres", "$1"},
		"mysql":    {MySQLDialect{}, "mysql", "?"},
	}
	for label, c := range cases {
		for _, key := range []string{"columns", "keys", "indexes", "foreign_keys"} {
			if q := c.dialect.CatalogQueries()[key]; label == "postgres" && (strings.Count(q, "$1") != 1 || strings.Count(q, "$2") != 1) || label == "mysql" && strings.Count(q, "?") != 2 {
				t.Errorf("%s %s: want schema and object placeholders: %s", label, key, q)
			}
		}
		ddl, args := c.dialect.DDL("orders")
		if label == "postgres" && (!strings.Contains(ddl, "$1") || len(args) != 1 || args[0] != "orders") {
			t.Errorf("postgres DDL does not bind the name: %s %v", ddl, args)
		}
	}
}

func TestIntrospectionNamesCannotChangeTheSQL(t *testing.T) {
	evil := []string{"orders", "o'rders", `x' ORDER BY 1; DROP TABLE t; --`, "a\"b ORDER BY GROUP BY"}
	for label, d := range map[string]struct {
		dialect Dialect
		name    string
	}{"postgres": {PostgresDialect{}, "postgres"}, "mysql": {MySQLDialect{}, "mysql"}} {
		var baseline []string
		for n, object := range evil {
			stub := &dbtest.Stub{Columns: []string{"a", "b"}, Rows: [][]driver.Value{{[]byte("x"), []byte("y")}}}
			db := dbtest.Open(fmt.Sprintf("evil-%s-%d", label, n), stub)
			s := newSQLSession(db, d.dialect, d.name, Conn{}, false)
			_, err := s.Introspect(context.Background(), IntrospectionRequest{Schema: "public", Object: object})
			s.Close()
			db.Close()
			if err != nil {
				t.Fatalf("%s %q: %v", label, object, err)
			}
			var stmts []string
			var args [][]any
			for i, st := range stub.Recorded() {
				if strings.HasPrefix(st, "SHOW CREATE TABLE") {
					continue // MySQL cannot bind SHOW; the name is escaped by QuoteIdent
				}
				stmts = append(stmts, st)
				args = append(args, stub.RecordedArgs()[i])
			}
			if baseline == nil {
				baseline = stmts
			}
			if !reflect.DeepEqual(stmts, baseline) {
				t.Errorf("%s %q changed the SQL: %v", label, object, stmts)
			}
			for i, st := range stmts {
				if strings.Contains(st, object) && object != "orders" {
					t.Errorf("%s: name spliced into SQL: %s", label, st)
				}
				if strings.Contains(st, "table_name='") || strings.Contains(st, "relname='") {
					t.Errorf("%s: text-spliced filter: %s", label, st)
				}
				if label == "postgres" && strings.Contains(st, "$1") && !strings.Contains(st, "$2") {
					if len(args[i]) != 1 || args[i][0] != object {
						t.Errorf("%s %q: DDL argument not passed: %v", label, object, args[i])
					}
				}
				if label == "postgres" && strings.Contains(st, "$2") || label == "mysql" && strings.Contains(st, "table_name = ?") {
					if len(args[i]) != 2 || args[i][0] != "public" || args[i][1] != object {
						t.Errorf("%s %q: argument not passed: %v", label, object, args[i])
					}
				}
			}
		}
	}
}

func TestPostgresIntrospectionReadsObjectsAndDDL(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"name", "type"}, ColumnTypes: []string{"VARCHAR", "TEXT"}, Rows: [][]driver.Value{{[]byte("orders"), []byte("CREATE TABLE orders (id integer)")}}}
	db := dbtest.Open("pg-introspection", stub)
	defer db.Close()
	s := newSQLSession(db, PostgresDialect{}, "postgres", Conn{}, false)
	defer s.Close()
	result, err := s.Introspect(context.Background(), IntrospectionRequest{Object: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Columns) == 0 || result.DDL == "" {
		t.Fatalf("introspection result = %+v", result)
	}
	joined := strings.Join(stub.Recorded(), "\n")
	if !strings.Contains(joined, "c.table_name = $2") || !strings.Contains(joined, "t.relname = $2") {
		t.Fatalf("catalog query filters were not bound: %s", joined)
	}
}

func TestIntrospectObjectWithoutSchemaUsesCurrentSchema(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"name", "type"}, Rows: [][]driver.Value{{"public", "TEXT"}}}
	db := dbtest.Open("current-schema", stub)
	defer db.Close()
	s := newSQLSession(db, PostgresDialect{}, "postgres", Conn{}, false)
	defer s.Close()
	if _, err := s.Introspect(context.Background(), IntrospectionRequest{Object: "orders"}); err != nil {
		t.Fatal(err)
	}
	if got := stub.Recorded()[0]; got != "SELECT current_schema()" {
		t.Fatalf("first query = %q, want current schema lookup", got)
	}
	for i, query := range stub.Recorded() {
		if strings.Contains(query, "c.table_name = $2") || strings.Contains(query, "t.relname = $2") {
			args := stub.RecordedArgs()[i]
			if len(args) != 2 || args[0] != "public" || args[1] != "orders" {
				t.Fatalf("catalog args = %#v, want bound current schema and object", args)
			}
		}
	}
}

func TestMySQLDatabaseExpansionListsObjects(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"name", "type"}, Rows: [][]driver.Value{{"users", "BASE TABLE"}}}
	db := dbtest.Open("mysql-db-expansion", stub)
	defer db.Close()
	s := newSQLSession(db, MySQLDialect{}, "mysql", Conn{}, false)
	defer s.Close()
	result, err := s.Introspect(context.Background(), IntrospectionRequest{Database: "app", Schema: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Objects) != 1 || result.Objects[0].Name != "users" {
		t.Fatalf("database expansion = %#v", result.Objects)
	}
	if len(stub.RecordedArgs()) == 0 || len(stub.RecordedArgs()[0]) != 1 || stub.RecordedArgs()[0][0] != "app" {
		t.Fatalf("database name was not bound to objects query: %#v", stub.RecordedArgs())
	}
}

func TestMySQLObjectIntrospectionUsesCurrentDatabase(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"name", "type"}, Rows: [][]driver.Value{{"app", "TEXT"}}}
	db := dbtest.Open("mysql-current-database", stub)
	defer db.Close()
	s := newSQLSession(db, MySQLDialect{}, "mysql", Conn{}, false)
	defer s.Close()
	if _, err := s.Introspect(context.Background(), IntrospectionRequest{Object: "users"}); err != nil {
		t.Fatal(err)
	}
	if got := stub.Recorded()[0]; got != "SELECT DATABASE()" {
		t.Fatalf("first query = %q, want current database lookup", got)
	}
	for _, query := range stub.Recorded() {
		if strings.Contains(query, "current_schema()") {
			t.Fatalf("MySQL received unsupported current_schema query: %s", query)
		}
	}
}

func TestSQLSessionRedactsDriverErrors(t *testing.T) {
	stub := &dbtest.Stub{QueryError: errors.New("connect postgres://app:secret@host/db failed")}
	db := dbtest.Open("redact", stub)
	defer db.Close()
	s := newSQLSession(db, PostgresDialect{}, "postgres", Conn{}, false)
	defer s.Close()
	r := s.Run(context.Background(), "SELECT 1", RunOptions{})
	if r.Err == nil || strings.Contains(r.Err.Error(), "secret") || !strings.Contains(r.Err.Error(), "[redacted]") {
		t.Fatalf("error was not safely redacted: %v", r.Err)
	}
}

func TestSQLDialectReadOnlyAndManualConnection(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"x"}, Rows: [][]driver.Value{{int64(1)}}}
	db := dbtest.Open("transactions", stub)
	defer db.Close()
	s := newSQLSession(db, PostgresDialect{}, "postgres", Conn{}, false)
	s.newDB = func() (*sql.DB, error) { return dbtest.Open("transactions-ro", stub), nil }
	defer s.Close()
	if err := s.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := s.Run(context.Background(), "UPDATE t SET x=2", RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if err := s.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if err := ro.Begin(context.Background()); err == nil {
		t.Fatal("read-only session accepted a writable transaction")
	}
	if r := ro.Run(context.Background(), "SELECT 1", RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	joined := strings.Join(stub.Recorded(), "\n")
	if !strings.Contains(joined, "BEGIN READ ONLY") || !strings.Contains(joined, "statement_timeout") {
		t.Fatalf("read-only setup missing: %s", joined)
	}
	if strings.Count(joined, "ROLLBACK") == 0 {
		t.Fatalf("read-only transaction was not rolled back: %s", joined)
	}
}

func TestSQLDialectCatalogsAndExplain(t *testing.T) {
	for name, d := range map[string]Dialect{"postgres": PostgresDialect{}, "mysql": MySQLDialect{}} {
		q := d.CatalogQueries()
		for _, key := range []string{"databases", "schemas", "objects", "columns", "keys", "indexes", "foreign_keys", "routines", "sequences"} {
			if q[key] == "" {
				t.Errorf("%s missing catalog query %q", name, key)
			}
		}
		if ddl, _ := d.DDL("orders"); ddl == "" || d.Explain("UPDATE orders SET x=1", false) == "" || d.Explain("UPDATE orders SET x=1", true) == "" {
			t.Errorf("%s missing DDL or explain", name)
		}
	}
}

func TestMySQLReadOnlyTimeoutAndAnalyzeWriteRollback(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"plan"}, Rows: [][]driver.Value{{"ok"}}}
	db := dbtest.Open("mysql-read-only", stub)
	defer db.Close()
	s := newSQLSession(db, MySQLDialect{}, "mysql", Conn{}, false)
	s.newDB = func() (*sql.DB, error) { return dbtest.Open("mysql-read-only-ro", stub), nil }
	defer s.Close()
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	if r := ro.Run(context.Background(), "SELECT 1", RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	_ = ro.Close()
	r := s.Explain(context.Background(), "UPDATE t SET n=2", true)
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	joined := strings.Join(stub.Recorded(), "\n")
	if !strings.Contains(joined, "SET SESSION max_execution_time = 30000") || !strings.Contains(joined, "START TRANSACTION READ ONLY") || !strings.Contains(joined, "ROLLBACK") {
		t.Fatalf("MySQL read-only setup missing: %s", joined)
	}
	begins, _, rollbacks := stub.TransactionCounts()
	if begins != 1 || rollbacks != 1 {
		t.Fatalf("analyze transaction begin=%d rollback=%d", begins, rollbacks)
	}
}
