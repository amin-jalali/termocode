package dbclient

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

// When the read-only guard (BEGIN READ ONLY) cannot start, the AI's
// statement is not sent at all.
func TestReadOnlySessionGuardFailureSendsNothing(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"n"}, Rows: [][]driver.Value{{int64(1)}}}
	s := stubSession(t, "ro-guard", stub, PostgresDialect{}, "postgres")
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	stub.ExecError = errors.New("cannot set transaction read-only mode")
	r := ro.Run(context.Background(), "SELECT secret FROM vault", RunOptions{})
	if r.Err == nil || !strings.Contains(r.Err.Error(), "read-only mode") {
		t.Fatalf("result = %+v", r)
	}
	for _, st := range stub.Recorded() {
		if strings.Contains(st, "vault") {
			t.Fatalf("the statement ran without its guard: %q", stub.Recorded())
		}
	}
}

// EXPLAIN ANALYZE of a write must roll back; when the rollback fails the
// user is told, even though the plan came back.
func TestExplainAnalyzeRollbackFailureIsReported(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"QUERY PLAN"}, Rows: [][]driver.Value{{"Delete on t"}}}
	s := stubSession(t, "explain-rb", stub, PostgresDialect{}, "postgres")
	stub.RollbackError = errors.New("connection lost during rollback")
	r := s.Explain(context.Background(), "DELETE FROM t", true)
	if r.Err == nil || !strings.Contains(r.Err.Error(), "connection lost during rollback") {
		t.Fatalf("result = %+v", r)
	}
}
