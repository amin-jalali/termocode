package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/tests"
)

const cdbTAP = `# Subtest: public.test_users()
    ok 1 - users table exists
    not ok 2 - users has a primary key
    1..2
not ok 1 - public.test_users
# Subtest: public.test_orders()
    ok 1 - orders exist
    1..1
ok 2 - public.test_orders
1..2`

func cdbPgTAPModel(t *testing.T) (*Model, *cdbSession) {
	t.Helper()
	m := cdbRealModel(t,
		dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432},
		dbclient.Conn{ID: "my", Driver: "mysql", Host: "h", Port: 3306})
	sess := &cdbSession{run: func(string) dbclient.Result {
		var r dbclient.Result
		for _, l := range strings.Split(cdbTAP, "\n") {
			r.Rows = append(r.Rows, []dbclient.Cell{{Text: l}})
		}
		return r
	}}
	m.databases.sessions["pg"] = sess
	return m, sess
}

func TestCovDBPgTAPRunsAndFoldsResultsIntoTheTestingView(t *testing.T) {
	m, sess := cdbPgTAPModel(t)
	m.databases.databaseSelectPath("pg")
	m.databasePgTAPStart("")
	if !m.promptOpen || m.promptKind != promptKindDBPgTAP {
		t.Fatal("no pattern prompt")
	}
	// a bad pattern is refused
	m.promptOpen = false
	m.databasePgTAPPrompt(promptKindDBPgTAP, "(")
	if !strings.Contains(covToastText(m), "not valid") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// the prompt for a connection that is gone says nothing
	if cmd, ok := m.databasePgTAPPrompt(promptKindDBPgTAP, "x"); !ok || cmd != nil {
		t.Fatal("no connection")
	}
	if _, ok := m.databasePgTAPPrompt(0, "x"); ok {
		t.Fatal("foreign prompt")
	}
	m.databasePgTAPStart("pg")
	cdbSubmit(t, m, "")
	if !m.confirmOpen || m.confirmKind != confirmKindDBPgTAP {
		t.Fatal("no card")
	}
	m.databases.conns[0].Environment = "production"
	m.databasePgTAPStart("pg")
	cdbSubmit(t, m, "test_")
	m.databases.conns[0].Environment = ""
	// cancel
	cdbConfirm(t, m, "cancel")
	// run
	m.databasePgTAPStart("pg")
	cdbSubmit(t, m, "")
	m.confirmOpen = false
	cmd, ok := m.databasePgTAPConfirm("run")
	if !ok {
		t.Fatal("not routed")
	}
	msgs := cdbMsgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("msgs %v (toast %q)", msgs, covToastText(m))
	}
	done := msgs[0].(dbPgTAPDoneMsg)
	if done.err != nil || len(done.lines) == 0 || !strings.Contains(sess.ran(), "runtests") {
		t.Fatalf("%+v", done)
	}
	m.onDBPgTAPDone(done)
	if !strings.Contains(covToastText(m), "pgTAP: 1 failed, 1 passed") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// a late answer is dropped
	if cmd := m.onDBPgTAPDone(done); cmd != nil {
		t.Fatal("stale answer applied")
	}

	// the Testing view rows run it again / show output
	pkg := m.testView.tree.PgTAP()
	if pkg == nil || len(pkg.Files) != 1 || len(pkg.Files[0].Cases) != 2 {
		t.Fatalf("tree %+v", pkg)
	}
	file, c := pkg.Files[0], pkg.Files[0].Cases[0]
	if cmd, ok := m.testsPgTAPRow(testRow{pkg: pkg, file: file, c: c}, false); !ok || cmd != nil {
		t.Fatal("open a case")
	}
	if _, ok := m.testsPgTAPRow(testRow{pkg: pkg, file: file}, false); !ok {
		t.Fatal("open a file")
	}
	m.testsPgTAPRow(testRow{pkg: pkg, file: file, c: c}, true)
	if !m.confirmOpen || !strings.Contains(m.databases.console.pgtapRun.sql, "test_") {
		t.Fatalf("no card for one case: %+v", m.databases.console.pgtapRun)
	}
	m.testsPgTAPRow(testRow{pkg: pkg}, true) // the whole node: first file
	if !m.databases.console.pgtapRun.whole {
		t.Fatal("not a whole run")
	}
	if _, ok := m.testsPgTAPRow(testRow{pkg: &tests.Package{Name: "x"}}, true); ok {
		t.Fatal("not a pgTAP row")
	}
	if cmd, ok := m.testsPgTAPRow(testRow{pkg: &tests.Package{Name: tests.PgTAPPackage}}, true); !ok || cmd != nil {
		t.Fatal("empty node")
	}
	if _, ok := m.testsPgTAPRow(testRow{}, true); ok {
		t.Fatal("no package")
	}
}

func TestCovDBPgTAPRunGuardsAndErrors(t *testing.T) {
	m, sess := cdbPgTAPModel(t)
	// not a PostgreSQL connection
	m.databasePgTAPStart("my")
	if !strings.Contains(covToastText(m), "needs a PostgreSQL connection") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// no row selected: ask for one
	m.databases.cursor = 99
	m.databasePgTAPStart("")
	// card for an unknown connection
	if cmd := m.databasePgTAPAsk(&dbPgTAPRun{conn: "gone"}); cmd != nil || m.confirmOpen {
		t.Fatal("card for a gone connection")
	}
	// no session: connects first
	delete(m.databases.sessions, "pg")
	m.databasePgTAPRun(&dbPgTAPRun{conn: "pg", sql: "x"})
	if !strings.Contains(covToastText(m), "Connecting") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.databases.sessions["pg"] = sess
	// open transaction
	m.databases.txs = map[string]*dbTxState{"pg": {open: true}}
	m.databasePgTAPRun(&dbPgTAPRun{conn: "pg", sql: "x"})
	if !strings.Contains(covToastText(m), "transaction is open") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.databases.txs = nil
	// no Testing view state
	saved := m.testView
	m.testView = nil
	if cmd := m.databasePgTAPRun(&dbPgTAPRun{conn: "pg", sql: "x"}); cmd != nil {
		t.Fatal("no test view")
	}
	m.testView = saved
	if cmd := m.onDBPgTAPDone(dbPgTAPDoneMsg{}); cmd != nil {
		t.Fatal("stale")
	}
	// the card routes
	m.confirmKind = 0
	if _, ok := m.databasePgTAPConfirm("run"); ok {
		t.Fatal("foreign card")
	}
	m.confirmKind = confirmKindDBPgTAP
	if cmd, ok := m.databasePgTAPConfirm("run"); !ok || cmd != nil {
		t.Fatal("no pending run")
	}
	// errors: not installed, plain error, no tests
	for _, tc := range []struct {
		lines []string
		err   error
		want  string
	}{
		{nil, errors.New("function runtests() does not exist"), "pgTAP is not installed"},
		{nil, errors.New("connection reset"), "connection reset"},
		{nil, nil, "No pgTAP tests found"},
		{[]string{"1..1", "ok 1 - good"}, nil, "pgTAP: "},
	} {
		sess.run = func(string) dbclient.Result {
			r := dbclient.Result{Err: tc.err}
			for _, l := range tc.lines {
				r.Rows = append(r.Rows, []dbclient.Cell{{Text: l}})
			}
			r.Rows = append(r.Rows, []dbclient.Cell{})
			return r
		}
		cmd := m.databasePgTAPRun(&dbPgTAPRun{conn: "pg", sql: "x", whole: true})
		got := cdbMsgs(cmd)
		if len(got) != 1 {
			t.Fatalf("%v", got)
		}
		m.onDBPgTAPDone(got[0].(dbPgTAPDoneMsg))
		if !strings.Contains(covToastText(m), tc.want) {
			t.Fatalf("%q: toast %q", tc.want, covToastText(m))
		}
	}
}
