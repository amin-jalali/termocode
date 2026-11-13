package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/tests"
)

// Rerunning one tSQLt test runs that class and test.
func TestDBTSQLtCaseSQL(t *testing.T) {
	got := dbTSQLtCaseSQL("UserTests.test login")
	if !strings.Contains(got, "EXEC tSQLt.Run N'[UserTests].[test login]';") || !strings.Contains(got, "tSQLt.TestResult") {
		t.Fatalf("sql %q", got)
	}
}

func TestDBTSQLtPaletteIDHasAPolicy(t *testing.T) {
	if p, ok := aiActionPolicies["db-tsqlt-run"]; !ok || p.Gate != aiGateNever {
		t.Fatalf("db-tsqlt-run policy: %+v %v", p, ok)
	}
}

func TestDBTSQLtOnlyForSQLServer(t *testing.T) {
	m := sqlRunsTestModel(t, "")
	m.databaseTSQLtStart("local")
	if got := toastText(*m); !strings.Contains(got, "tSQLt needs a SQL Server connection") {
		t.Fatalf("toast %q", got)
	}
}

// The results of a run land under the tSQLt node and the toast counts them.
func TestDBTSQLtDoneFoldsResultsIntoTheTestingView(t *testing.T) {
	m := &Model{testView: newTestsState()}
	st := m.testView
	st.tree = &tests.Tree{Root: "/w"}
	st.running, st.started = true, time.Now()
	st.runID = 3
	run := &dbTSQLtRun{conn: "sql", whole: true}
	m.onDBTSQLtDone(dbTSQLtDoneMsg{
		runID: 3, run: run,
		cols: []string{"Class", "TestCase", "Result", "Msg", "TestStartTime", "TestEndTime"},
		rows: [][]string{{"A", "test one", "Success", "", "", ""}, {"A", "test two", "Failure", "expected 1", "", ""}},
	})
	if st.running || !st.hasRun || st.results != 2 {
		t.Fatalf("state running=%v hasRun=%v results=%d", st.running, st.hasRun, st.results)
	}
	p := st.tree.TSQLt()
	if p == nil || len(p.Files) != 1 || len(p.Files[0].Cases) != 2 || p.Files[0].Cases[1].Status != tests.StatusFailed {
		t.Fatalf("tree %+v", p)
	}
	if got := toastText(*m); !strings.Contains(got, "tSQLt: 1 failed, 1 passed") {
		t.Fatalf("toast %q", got)
	}
	// a stale run is ignored
	st.running = true
	if cmd := m.onDBTSQLtDone(dbTSQLtDoneMsg{runID: 2, run: run}); cmd != nil || !st.running {
		t.Fatal("an old run changed the view")
	}
}

func TestDBTSQLtNotInstalledSaysHowToInstall(t *testing.T) {
	m := &Model{testView: newTestsState()}
	st := m.testView
	st.tree = &tests.Tree{Root: "/w"}
	st.running, st.runID = true, 1
	m.onDBTSQLtDone(dbTSQLtDoneMsg{runID: 1, run: &dbTSQLtRun{conn: "sql"}, err: errString("Could not find stored procedure 'tSQLt.RunAll'.")})
	if got := toastText(*m); !strings.Contains(got, "tSQLt is not installed") {
		t.Fatalf("toast %q", got)
	}
}
