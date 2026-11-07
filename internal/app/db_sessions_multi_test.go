package app

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// dbMultiSess records the SQL it runs and when it is closed.
type dbMultiSess struct {
	dbclient.Session
	name   string
	ran    *[]string
	closed *int
}

func (s dbMultiSess) Run(_ context.Context, sql string, _ dbclient.RunOptions) dbclient.Result {
	*s.ran = append(*s.ran, s.name+":"+sql)
	return dbclient.Result{}
}
func (s dbMultiSess) Close() error { *s.closed++; return nil }

func dbMultiModel(t *testing.T) (*Model, *[]string, *int) {
	t.Helper()
	m := dbTestModel(newDBState())
	m.w, m.h = 120, 40
	m.databases.loaded = true
	m.databases.conns = []dbclient.Conn{
		{ID: "pg", Name: "pg", Driver: "postgres"},
		{ID: "lite", Name: "lite", Driver: "sqlite", Path: "a.db"},
	}
	ran, closed := []string{}, 0
	m.databases.sessions["pg"] = dbMultiSess{name: "main", ran: &ran, closed: &closed}
	return m, &ran, &closed
}

// Databases: Console Session… is in the palette, the AI never runs it, and
// the web parity list knows it.
func TestDBConsoleSessionPaletteAndPolicy(t *testing.T) {
	found := false
	for _, it := range databasesPaletteItems() {
		found = found || it.ID == "db-console-session"
	}
	if !found {
		t.Fatal("db-console-session not in the palette")
	}
	if _, ok := aiActionPolicies["db-console-session"]; !ok {
		t.Fatal("db-console-session has no AI policy")
	}
}

// The picker lists the main session, the extra ones and "New session";
// "New session" binds the console to a fresh session key.
func TestDBConsoleSessionPickNew(t *testing.T) {
	m, _, _ := dbMultiModel(t)
	m.databaseConsoleSessionPickerFor("/p/q.sql", "pg")
	if !m.pickerOpen || m.pickerKind != pickerKindDBConsoleSession {
		t.Fatal("no console session picker")
	}
	view := ansi.Strip(m.picker.View())
	for _, want := range []string{"Main session", "New session"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker misses %q:\n%s", want, view)
		}
	}
	m.pickerOpen = false
	if _, ok := m.databaseConsolePicker("new"); !ok {
		t.Fatal("pick not routed")
	}
	key := m.databases.consoleSessionKey("/p/q.sql", "pg")
	if key != "pg/session/2" || !dbIsExtraSession(key) || dbTxConn(key) != "pg" {
		t.Fatalf("bound key = %q", key)
	}
	if got := m.databases.dbSessionTitle(key); got != "pg · session 2" {
		t.Fatalf("title = %q", got)
	}
	// A second new session gets the next number; Main unbinds.
	m.databaseConsoleSessionPickerFor("/p/r.sql", "pg")
	m.pickerOpen = false
	m.databaseConsolePicker("new")
	if got := m.databases.consoleSessionKey("/p/r.sql", "pg"); got != "pg/session/3" {
		t.Fatalf("second key = %q", got)
	}
	m.databaseConsoleSessionPickerFor("/p/q.sql", "pg")
	m.pickerOpen = false
	m.databaseConsolePicker("pg")
	if got := m.databases.consoleSessionKey("/p/q.sql", "pg"); got != "" {
		t.Fatalf("main pick kept %q", got)
	}
}

// A binding of another connection is ignored (the file moved on).
func TestDBConsoleSessionOtherConn(t *testing.T) {
	m, _, _ := dbMultiModel(t)
	m.databases.bindConsoleSession("/p/q.sql", "pg/session/2")
	if got := m.databases.consoleSessionKey("/p/q.sql", "other"); got != "" {
		t.Fatalf("key for another connection = %q", got)
	}
}

// A driver with one session per connection (sqlite) says so, no picker.
func TestDBConsoleSessionOneOnly(t *testing.T) {
	m, _, _ := dbMultiModel(t)
	m.databaseConsoleSessionPickerFor("/p/q.sql", "lite")
	if m.pickerOpen {
		t.Fatal("sqlite got a session picker")
	}
}

// A run on a bound console opens the extra session first, then runs on it,
// never on the main session.
func TestDBConsoleRunOnExtraSession(t *testing.T) {
	m, ran, _ := dbMultiModel(t)
	opened := ""
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(_ context.Context, c dbclient.Conn, _ string) (dbclient.Session, error) {
		opened = c.ID
		return dbMultiSess{name: "extra", ran: ran, closed: new(int)}, nil
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })
	key := "pg/session/2"
	m.databases.bindConsoleSession("/p/q.sql", key)
	r := &dbRun{conn: "pg", file: "/p/q.sql", sql: "SELECT 1", statements: []string{"SELECT 1"}, by: "user"}
	r.sessKey = m.databases.consoleSessionKey(r.file, r.conn)
	cmd := m.databaseStartRunRevealed(r)
	if cmd == nil || m.dbConsole().waiting != r {
		t.Fatal("the run does not wait for its session")
	}
	msg, ok := cmd().(dbConnectMsg)
	if !ok || msg.id != key || opened != "pg" {
		t.Fatalf("connect = %#v opened %q", msg, opened)
	}
	m.onDBConnect(msg)
	if m.databases.sessions[key] == nil {
		t.Fatal("the extra session is not stored")
	}
	if _, ok := m.databases.nodes[key]; ok {
		t.Fatal("the extra session made a tree node")
	}
	if m.dbConsole().running != r {
		t.Fatal("the run did not start after the session opened")
	}
}

// Disconnect closes the extra sessions too.
func TestDBDisconnectClosesExtraSessions(t *testing.T) {
	m, ran, closed := dbMultiModel(t)
	m.databases.sessions["pg/session/2"] = dbMultiSess{name: "extra", ran: ran, closed: closed}
	m.databaseDisconnectNow("pg")
	if *closed != 2 || m.databases.sessions["pg/session/2"] != nil {
		t.Fatalf("closed %d, left %v", *closed, m.databases.sessions)
	}
}

// A temp table made in auto-commit gets the one-session hint once.
func TestDBTempTableHint(t *testing.T) {
	m, _, _ := dbMultiModel(t)
	r := &dbRun{conn: "pg", sql: "CREATE TEMP TABLE scratch (id int)", by: "user"}
	msg := dbRunMsg{run: r, key: "pg"}
	if m.databaseTempTableHint(msg) == nil {
		t.Fatal("no hint for a temp table in auto-commit")
	}
	if m.databaseTempTableHint(msg) != nil {
		t.Fatal("the hint came twice for one session")
	}
	m2, _, _ := dbMultiModel(t)
	m2.databases.tx("pg").open = true
	if m2.databaseTempTableHint(msg) != nil {
		t.Fatal("hint inside a transaction (one session already)")
	}
	plain := dbRunMsg{run: &dbRun{conn: "pg", sql: "CREATE TABLE t (id int)"}, key: "pg"}
	if m2.databaseTempTableHint(plain) != nil {
		t.Fatal("hint for a plain table")
	}
}
