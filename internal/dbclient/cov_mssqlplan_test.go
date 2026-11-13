package dbclient

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
)

const fakePlanXML = `<ShowPlanXML xmlns="http://schemas.microsoft.com/sqlserver/2004/07/showplan"><BatchSequence/></ShowPlanXML>`

func mssqlSession(t *testing.T, f *fakeSQL, readonly bool) *SQLSession {
	t.Helper()
	return newSQLSession(f.open(t), SQLServerDialect{}, "sqlserver", Conn{}, readonly)
}

func TestSQLServerPlanReturnsOnlyTheShowplanSet(t *testing.T) {
	f := (&fakeSQL{}).on("select 1", fakeSet{cols: []string{"n"}, rows: [][]driver.Value{{int64(1)}}},
		fakeSet{cols: []string{"Microsoft SQL Server 2005 XML Showplan"}, rows: [][]driver.Value{{[]byte(fakePlanXML)}}})
	s := mssqlSession(t, f, false)
	res := s.Run(context.Background(), sqlServerStatisticsMark+"SELECT 1", RunOptions{})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if len(res.Rows) != 1 || !strings.Contains(res.Rows[0][0].Text, "<ShowPlanXML") {
		t.Fatalf("rows = %+v", res.Rows)
	}
	ran := strings.Join(f.ran(), " | ")
	if !strings.Contains(ran, "SET STATISTICS XML ON") || !strings.Contains(ran, "SET STATISTICS XML OFF") {
		t.Errorf("the plan switch must be turned on and off again: %s", ran)
	}
}

func TestSQLServerPlanRefusalsAndFailures(t *testing.T) {
	ctx := context.Background()
	f := &fakeSQL{}
	s := mssqlSession(t, f, true)
	if res := s.runSQLServerPlan(ctx, "SELECT 1; SELECT 2", false); res.Err == nil || !strings.Contains(res.Err.Error(), "one statement") {
		t.Errorf("two statements = %v", res.Err)
	}
	if res := s.runSQLServerPlan(ctx, "DELETE FROM t", true); res.Err == nil || !strings.Contains(res.Err.Error(), "read-only") {
		t.Errorf("analyze of a write in a read-only session = %v", res.Err)
	}
	// The estimated plan of a write never runs it: allowed read-only.
	f.on("delete", fakeSet{cols: []string{"showplan_xml"}, rows: [][]driver.Value{{fakePlanXML}}})
	if res := s.runSQLServerPlan(ctx, "DELETE FROM t", false); res.Err != nil || len(res.Rows) != 1 {
		t.Errorf("estimated plan = %+v", res)
	}
	// The switch cannot be turned on.
	bad := (&fakeSQL{}).fail("set showplan_xml on", errFake)
	if res := mssqlSession(t, bad, false).runSQLServerPlan(ctx, "SELECT 1", false); res.Err == nil {
		t.Error("a failing SET: want error")
	}
	// The statement itself fails.
	bad = (&fakeSQL{}).fail("select", errFake)
	if res := mssqlSession(t, bad, false).runSQLServerPlan(ctx, "SELECT 1", false); res.Err == nil {
		t.Error("a failing statement: want error")
	}
	// The switch cannot be turned off: the connection is dropped, the answer stands.
	off := (&fakeSQL{}).on("select", fakeSet{cols: []string{"showplan_xml"}, rows: [][]driver.Value{{fakePlanXML}}}).fail("set showplan_xml off", errFake)
	if res := mssqlSession(t, off, false).runSQLServerPlan(ctx, "SELECT 1", false); res.Err != nil || len(res.Rows) != 1 {
		t.Errorf("SET OFF failing = %+v", res)
	}
	// A closed session.
	closed := mssqlSession(t, &fakeSQL{}, false)
	closed.closed = true
	if res := closed.runSQLServerPlan(ctx, "SELECT 1", false); res.Err == nil || !strings.Contains(res.Err.Error(), "closed") {
		t.Errorf("closed = %v", res.Err)
	}
}

func TestSQLServerSavepointStatementsBecomeTSQL(t *testing.T) {
	f := &fakeSQL{}
	s := mssqlSession(t, f, false)
	for stmt, want := range map[string]string{
		"SAVEPOINT a":             "SAVE TRANSACTION a",
		"ROLLBACK TO SAVEPOINT a": "ROLLBACK TRANSACTION a",
	} {
		if res := s.Run(context.Background(), stmt, RunOptions{}); res.Err != nil {
			t.Errorf("%s: %v", stmt, res.Err)
		}
		if ran := strings.Join(f.ran(), "|"); !strings.Contains(ran, want) {
			t.Errorf("%s ran %q, want %q", stmt, ran, want)
		}
	}
	before := len(f.ran())
	if res := s.Run(context.Background(), "RELEASE SAVEPOINT a", RunOptions{}); res.Err != nil || len(f.ran()) != before {
		t.Errorf("RELEASE runs nothing: %+v, ran %v", res, f.ran()[before:])
	}
}
