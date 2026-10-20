package app

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DB-0 review: every caller of dbclient.Split / Classify passes the
// connection's dialect (with "" a Postgres $tag$ hides a second statement).
func TestDBEveryCallerPassesTheDialect(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	calls := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "dbclient" || (sel.Sel.Name != "Split" && sel.Sel.Name != "Classify") {
				return true
			}
			calls++
			if len(call.Args) != 2 {
				t.Errorf("%s: dbclient.%s without a dialect", fset.Position(call.Pos()), sel.Sel.Name)
				return true
			}
			if lit, ok := call.Args[1].(*ast.BasicLit); ok {
				t.Errorf("%s: dbclient.%s with the literal dialect %s", fset.Position(call.Pos()), sel.Sel.Name, lit.Value)
			}
			return true
		})
	}
	if calls == 0 {
		t.Fatal("no dbclient.Split / Classify call found (the scan is broken)")
	}
}

// Multi-line connect errors show the reason, not "failed to connect to …:".
func TestDBErrorTextShowsTheReasonLine(t *testing.T) {
	for _, tc := range []struct{ err, want string }{
		{"failed to connect to `user=app database=app`:\n\t127.0.0.1:5432 (localhost): server error: FATAL: password authentication failed for user \"app\" (SQLSTATE 28P01)",
			`FATAL: password authentication failed for user "app" (SQLSTATE 28P01)`},
		{"failed to connect to `user=app database=app`:\n\t[::1]:5432 (localhost): dial error: dial tcp [::1]:5432: connect: connection refused\n\t127.0.0.1:5432 (localhost): dial error: dial tcp 127.0.0.1:5432: connect: connection refused",
			"connection refused"},
		{"ERROR: relation \"userz\" does not exist (SQLSTATE 42P01)", `ERROR: relation "userz" does not exist (SQLSTATE 42P01)`},
		{"Error 1146 (42S02): Table 'app.userz' doesn't exist", "Error 1146 (42S02): Table 'app.userz' doesn't exist"},
		{"postgres://app:hunter2@db/app: dial error\x1b[31m", "postgres://app:[redacted]@db/app: dial error"},
	} {
		if got := dbErrorText(errors.New(tc.err)); got != tc.want {
			t.Errorf("dbErrorText(%q)\n got %q\nwant %q", tc.err, got, tc.want)
		}
	}
}

func TestDBStatementSpansSkipLeadingComments(t *testing.T) {
	text := "-- @conn local\nSELECT 1;\n\n-- second\nSELECT\n  2;\n"
	spans := dbStatementSpans(text, "postgres")
	if len(spans) != 2 || spans[0].line != 2 || spans[1].line != 5 {
		t.Fatalf("spans: %+v", spans)
	}
}

func TestDBGuardText(t *testing.T) {
	c := dbclient.Conn{ID: "local", Driver: "postgres"}
	for _, tc := range []struct {
		sql, title, note string
		conn             dbclient.Conn
	}{
		{"DELETE FROM orders", "Run DELETE without WHERE on local?", "Every row of orders will go.", c},
		{"UPDATE users SET plan = 'pro'", "Run UPDATE without WHERE on local?", "Every row of users will change.", c},
		{"DROP TABLE users", "Run DROP on local?", "This cannot be undone.", c},
		{"INSERT INTO t VALUES (1)", "Run this statement on local?", "", c},
		{"INSERT INTO t VALUES (1)", "Write to production (local)?", "", dbclient.Conn{ID: "local", Driver: "postgres", Environment: "production"}},
		{"DELETE FROM t WHERE id = 1", "local is read-only. Run anyway?", "", dbclient.Conn{ID: "local", Driver: "postgres", ReadOnly: true}},
	} {
		kind, stmts := dbclient.Classify(tc.sql, tc.conn.Driver)
		title, note := dbGuardText(tc.conn, kind, stmts)
		if title != tc.title || note != tc.note {
			t.Errorf("%s: got %q / %q", tc.sql, title, note)
		}
	}
}

// Results page over the loaded rows; the grid holds one page only and
// marks NULL; ASCII and Unicode rules both fit the column starts.
func TestDBPageGridOnePageOnly(t *testing.T) {
	m := &Model{w: 120, h: 40}
	s := m.dbConsole()
	res := dbclient.Result{Columns: []dbclient.Column{{Name: "id"}, {Name: "email"}}}
	for i := 0; i < 250; i++ {
		res.Rows = append(res.Rows, []dbclient.Cell{{Text: strings.Repeat("9", i%4+1), Kind: "number"}, {Null: i == 3, Text: "x"}})
	}
	s.shown, s.size, s.sortCol = &dbRun{conn: "c", sql: "SELECT 1", result: res}, 100, -1
	p := m.databasePage()
	if p.first == 0 || len(p.lines)-p.first+1 > 101 {
		t.Fatalf("grid wrote %d lines (first data row %d)", len(p.lines), p.first)
	}
	if !strings.Contains(strings.Join(p.lines, "\n"), "<null>") {
		t.Fatal("NULL not shown")
	}
	if !strings.Contains(p.lines[1], "page 1 of 3") {
		t.Fatalf("summary: %q", p.lines[1])
	}
	if len(p.bounds) != 2 || p.bounds[0] != 1 {
		t.Fatalf("bounds %v", p.bounds)
	}
}

// Idle rule: hiding the view while a console query runs cancels it, and
// nothing is left running or scheduled afterwards.
func TestPerfGuardDatabasesHiddenConsoleCancelsRun(t *testing.T) {
	sess := &blockingSession{started: make(chan struct{}), done: make(chan struct{})}
	s := newDBState()
	s.visible = true
	s.loaded = true
	s.conns = []dbclient.Conn{{ID: "local", Driver: "postgres"}}
	s.sessions["local"] = sess
	m := dbTestModel(s)
	before := runtime.NumGoroutine()
	cmd := m.databaseStartRun(&dbRun{conn: "local", sql: "SELECT pg_sleep(60)", by: "user", statements: []string{"SELECT pg_sleep(60)"}})
	if cmd == nil || m.dbConsole().running == nil {
		t.Fatal("run did not start")
	}
	go func() { _ = cmd() }()
	<-sess.started
	updated, _ := m.Update(activity.SwitchMsg{View: activity.ViewFiles})
	got := updated.(Model)
	select {
	case <-sess.done:
	case <-time.After(time.Second):
		t.Fatal("hiding the view did not cancel the running query")
	}
	if c := got.databases.console; c.running != nil || c.waiting != nil || c.cancel != nil {
		t.Fatal("console kept a run after the view hid")
	}
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if n := runtime.NumGoroutine(); n <= before {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("goroutines left after hiding: %d -> %d", before, runtime.NumGoroutine())
}

type blockingSession struct {
	fakeDBSession
	started, done chan struct{}
}

func (b *blockingSession) Run(ctx context.Context, _ string, _ dbclient.RunOptions) dbclient.Result {
	close(b.started)
	<-ctx.Done()
	close(b.done)
	return dbclient.Result{Err: ctx.Err()}
}
