package dbclient

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

func stubSession(t *testing.T, name string, stub *dbtest.Stub, dialect Dialect, driverName string) *SQLSession {
	t.Helper()
	db := dbtest.Open(name, stub)
	s := newSQLSession(db, dialect, driverName, Conn{}, false)
	s.ownsDB = true
	s.newDB = func() (*sql.DB, error) { return dbtest.Open(name+"-clone", stub), nil }
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// The Stop key cancels a running query: Run returns at once with an
// error instead of waiting for the server.
func TestSQLSessionCancelStopsRunningQuery(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	started := make(chan string, 1)
	stub := &dbtest.Stub{Columns: []string{"n"}, Block: block, Started: started}
	s := stubSession(t, "cancel-run", stub, PostgresDialect{}, "postgres")
	done := make(chan Result, 1)
	go func() { done <- s.Run(context.Background(), "SELECT pg_sleep(60)", RunOptions{}) }()
	<-started
	// The cancel func is set before the query starts; Cancel finds it.
	s.Cancel()
	select {
	case r := <-done:
		if r.Err == nil || !strings.Contains(r.Err.Error(), "canceled") {
			t.Fatalf("cancelled query result = %+v", r)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Cancel did not stop the running query")
	}
	s.Cancel() // nothing running: no panic, no effect
}

// A query that runs into its deadline says it stopped on the timeout.
func TestQueryErrorAfterDeadlineSaysTimeout(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := queryError(ctx, errors.New("driver: bad connection")); err == nil || err.Error() != "stopped after 30 s (timeout)" {
		t.Fatalf("queryError = %v", err)
	}
}

// The connection drops after some rows: the user sees the error, not a
// short result that looks complete.
func TestSQLSessionConnectionDropMidResult(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"n"}, Rows: [][]driver.Value{{int64(1)}, {int64(2)}, {int64(3)}},
		RowsError: errors.New("unexpected EOF: connection reset by peer"), RowsBefore: 2}
	s := stubSession(t, "drop-mid", stub, PostgresDialect{}, "postgres")
	r := s.Run(context.Background(), "SELECT n FROM t", RunOptions{})
	if r.Err == nil || !strings.Contains(r.Err.Error(), "connection reset") {
		t.Fatalf("dropped connection result = %+v", r)
	}
}

// A closed session refuses work with a clear message, and closing twice
// is harmless.
func TestSQLSessionClosedRefusesWork(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"n"}}
	s := stubSession(t, "closed", stub, PostgresDialect{}, "postgres")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	if r := s.Run(context.Background(), "SELECT 1", RunOptions{}); r.Err == nil || r.Err.Error() != "database session is closed" {
		t.Errorf("Run after Close = %v", r.Err)
	}
	if err := s.Begin(context.Background()); err == nil || err.Error() != "database session is closed" {
		t.Errorf("Begin after Close = %v", err)
	}
	if _, err := s.ReadOnlySession(); err == nil || err.Error() != "database session is closed" {
		t.Errorf("ReadOnlySession after Close = %v", err)
	}
	if len(stub.Recorded()) != 0 {
		t.Errorf("a closed session sent %q", stub.Recorded())
	}
}

// Manual transactions: one at a time, Commit/Rollback need one, and
// server errors on BEGIN/COMMIT/ROLLBACK reach the user.
func TestSQLSessionTransactionErrors(t *testing.T) {
	ctx := context.Background()
	stub := &dbtest.Stub{}
	s := stubSession(t, "tx-errors", stub, PostgresDialect{}, "postgres")
	if err := s.Commit(ctx); err == nil || err.Error() != "no transaction is active" {
		t.Errorf("Commit without BEGIN = %v", err)
	}
	if err := s.Rollback(ctx); err == nil || err.Error() != "no transaction is active" {
		t.Errorf("Rollback without BEGIN = %v", err)
	}
	if err := s.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Begin(ctx); err == nil || err.Error() != "transaction already active" {
		t.Errorf("second Begin = %v", err)
	}
	stub.CommitError = errors.New("could not serialize access due to concurrent update")
	if err := s.Commit(ctx); err == nil || !strings.Contains(err.Error(), "could not serialize") {
		t.Errorf("Commit error = %v", err)
	}
	stub.CommitError = nil
	stub.RollbackError = errors.New("server closed the connection unexpectedly")
	if err := s.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Rollback(ctx); err == nil || !strings.Contains(err.Error(), "server closed") {
		t.Errorf("Rollback error = %v", err)
	}
	stub.BeginError = errors.New("cannot begin: too many connections")
	if err := s.Begin(ctx); err == nil || !strings.Contains(err.Error(), "too many connections") {
		t.Errorf("Begin error = %v", err)
	}
}

// Closing a session with an open transaction rolls it back.
func TestSQLSessionCloseRollsBackOpenTransaction(t *testing.T) {
	stub := &dbtest.Stub{}
	s := stubSession(t, "close-tx", stub, PostgresDialect{}, "postgres")
	if err := s.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if b, c, r := stub.TransactionCounts(); b != 1 || c != 0 || r != 1 {
		t.Fatalf("begins/commits/rollbacks = %d/%d/%d", b, c, r)
	}
}

// EXPLAIN without ANALYZE never starts a transaction; EXPLAIN ANALYZE of
// a write that cannot start its guard transaction does not run at all.
func TestSQLSessionExplainPlainAndGuardFailure(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"QUERY PLAN"}, Rows: [][]driver.Value{{"Seq Scan on t"}}}
	s := stubSession(t, "explain", stub, PostgresDialect{}, "postgres")
	r := s.Explain(context.Background(), "DELETE FROM t", false)
	if r.Err != nil || len(r.Rows) != 1 {
		t.Fatalf("plain explain = %+v", r)
	}
	if b, _, _ := stub.TransactionCounts(); b != 0 {
		t.Errorf("plain EXPLAIN started a transaction")
	}
	stub.BeginError = errors.New("begin refused")
	n := len(stub.Recorded())
	r = s.Explain(context.Background(), "DELETE FROM t", true)
	if r.Err == nil || !strings.Contains(r.Err.Error(), "begin refused") {
		t.Fatalf("analyze without guard = %+v", r)
	}
	if got := stub.Recorded()[n:]; len(got) != 0 {
		t.Fatalf("EXPLAIN ANALYZE of a write ran without its rollback guard: %q", got)
	}
}

func TestSQLSessionRunEmptyStatement(t *testing.T) {
	s := stubSession(t, "empty", &dbtest.Stub{}, PostgresDialect{}, "postgres")
	if r := s.Run(context.Background(), "  -- only a comment\n", RunOptions{}); r.Err == nil || r.Err.Error() != "no SQL statement to run" {
		t.Fatalf("empty statement = %v", r.Err)
	}
}

// A read-only clone needs a way to open a second pool.
func TestSQLSessionReadOnlyCloneWithoutPool(t *testing.T) {
	db := dbtest.Open("no-pool", &dbtest.Stub{})
	defer db.Close()
	s := newSQLSession(db, PostgresDialect{}, "postgres", Conn{}, false)
	if _, err := s.ReadOnlySession(); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("clone without a pool = %v", err)
	}
	opened := 0
	s.openDB = func() (*sql.DB, func(), error) {
		opened++
		return dbtest.Open("no-pool-ro", &dbtest.Stub{}), func() {}, nil
	}
	ro, err := s.ReadOnlySession()
	if err != nil || opened != 1 {
		t.Fatalf("clone through openDB: %v (opened %d)", err, opened)
	}
	_ = ro.Close()
}

// A server that refuses the login during Ping: the session is not
// returned and the password is not in the message.
func TestSQLSessionPingFailureIsRedacted(t *testing.T) {
	stub := &dbtest.Stub{PingError: errors.New("password=hunter2 authentication failed")}
	s := stubSession(t, "ping-fail", stub, PostgresDialect{}, "postgres")
	err := s.Ping(context.Background())
	if err == nil || strings.Contains(err.Error(), "hunter2") || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("Ping error = %v", err)
	}
}

// Cells: NULL stays NULL, JSON is shown as text, booleans get their kind.
func TestSQLCellNullJSONBool(t *testing.T) {
	if c := sqlCell(nil, "JSONB"); !c.Null {
		t.Errorf("NULL json = %+v", c)
	}
	if c := sqlCell([]byte(`{"a":"ü"}`), "JSON"); c.Kind != "json" || c.Text != `{"a":"ü"}` {
		t.Errorf("json = %+v", c)
	}
	if c := sqlCell(true, "BOOL"); c.Kind != "bool" || c.Text != "true" {
		t.Errorf("bool = %+v", c)
	}
}

// The tree lists databases with no request and schemas for a database.
func TestSQLSessionIntrospectDatabasesAndSchemas(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"name"}, Rows: [][]driver.Value{{"app"}, {nil}, {"logs"}}}
	s := stubSession(t, "introspect-list", stub, PostgresDialect{}, "postgres")
	r, err := s.Introspect(context.Background(), IntrospectionRequest{})
	if err != nil || strings.Join(r.Databases, ",") != "app,logs" {
		t.Fatalf("databases = %v %v", r.Databases, err)
	}
	r, err = s.Introspect(context.Background(), IntrospectionRequest{Database: "app"})
	if err != nil || strings.Join(r.Schemas, ",") != "app,logs" {
		t.Fatalf("schemas = %v %v", r.Schemas, err)
	}
	stub.QueryError = errors.New("permission denied for database app")
	for _, req := range []IntrospectionRequest{{}, {Database: "app"}, {Schema: "public"}, {Schema: "public", Object: "t"}, {Object: "t"}} {
		if _, err := s.Introspect(context.Background(), req); err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("Introspect(%+v) error = %v", req, err)
		}
	}
}
