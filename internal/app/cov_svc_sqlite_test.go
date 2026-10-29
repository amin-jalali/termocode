package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
)

// covSvcSQLite is the whole app on a workspace with a real SQLite file
// (app.db: users with a key, 3 rows) and query.sql attached to it.
func covSvcSQLite(t *testing.T, sql string) *testkit.Driver {
	t.Helper()
	d := testkit.New(t, testkit.Options{W: 110, H: 34, Nvim: true, Files: map[string]string{
		"query.sql":                 sql,
		".termocode/databases.json": `{"connections":[{"id":"app","driver":"sqlite","path":"app.db"}]}`,
	}})
	sess, err := dbclient.NewSQLiteSession(context.Background(), dbclient.Conn{Driver: "sqlite", Path: filepath.Join(d.Dir, "app.db")})
	if err != nil {
		t.Skipf("sqlite3: %v", err)
	}
	for _, q := range []string{
		"CREATE TABLE users(id INTEGER PRIMARY KEY, email TEXT NOT NULL, plan TEXT)",
		"INSERT INTO users VALUES (1,'ann@example.com','pro'),(2,'bob@example.com','free'),(3,'cy@example.com',NULL)",
	} {
		if r := sess.Run(context.Background(), q, dbclient.RunOptions{}); r.Err != nil {
			t.Fatal(r.Err)
		}
	}
	_ = sess.Close()
	d.Action("open:query.sql")
	d.Key("esc")
	return d
}

func covSvcWaitFrame(t *testing.T, d *testkit.Driver, s string) {
	t.Helper()
	if !d.WaitForText(s, 4*time.Second) {
		t.Fatalf("frame lacks %q:\n%s", s, ansi.Strip(d.Frame()))
	}
}

// A SELECT from the console: the page views, sort, filter, copy, the
// history and an export of the console result.
func TestCovSvcSQLiteConsolePage(t *testing.T) {
	d := covSvcSQLite(t, "-- @conn app\nSELECT id, email, plan FROM users ORDER BY id;\n")
	d.Key("down")
	dbSend(d)
	dbWaitPage(t, d, "ann@example.com")
	page := dbText(t, d)
	if !strings.Contains(page, "cy@example.com") || !strings.Contains(page, "<null>") {
		t.Fatalf("page:\n%s", page)
	}
	d.Key("tab") // JSON
	dbWaitPage(t, d, `"email": "ann@example.com"`)
	d.Key("tab") // Messages
	d.Key("tab") // Grid
	dbWaitPage(t, d, "bob@example.com")
	cursorTo(t, d, "bob@example.com")
	d.Key("t") // one row as column | value
	dbWaitPage(t, d, "email")
	d.Key("t")
	d.Key("?")
	covSvcWaitFrame(t, d, "Result keys")
	d.Key("esc")
	d.Key("h")
	covSvcWaitFrame(t, d, "SELECT id, email")
	d.Key("esc")
	// Export the console result as JSON.
	d.Key("S")
	covSvcWaitFrame(t, d, "Markdown table")
	d.Key("down", "down", "enter")
	covSvcWaitFrame(t, d, "File (in the workspace")
	d.Key("ctrl+u")
	for i := 0; i < 60; i++ {
		d.Key("backspace")
	}
	d.Type("users.json")
	d.Key("enter")
	if !d.WaitFor(func() bool {
		b, err := os.ReadFile(filepath.Join(d.Dir, "users.json"))
		return err == nil && strings.Contains(string(b), "cy@example.com")
	}, 5*time.Second) {
		t.Fatalf("no export file:\n%s", ansi.Strip(d.Frame()))
	}
	// The same name again: the overwrite card; Cancel keeps the file.
	d.Key("S")
	covSvcWaitFrame(t, d, "Markdown table")
	d.Key("down", "down", "enter")
	covSvcWaitFrame(t, d, "File (in the workspace")
	d.Key("ctrl+u")
	for i := 0; i < 60; i++ {
		d.Key("backspace")
	}
	d.Type("users.json")
	d.Key("enter")
	covSvcWaitFrame(t, d, "exists")
	d.Key("esc")
	// x explains the statement (SQLite: EXPLAIN QUERY PLAN).
	d.Key("x")
	dbWaitPage(t, d, "SCAN")
}

// Edits on the data view of a keyed SQLite table: a cell, a new row and
// a deleted row go in one submit; the file really changes.
func TestCovSvcSQLiteEditSubmit(t *testing.T) {
	d := covSvcSQLite(t, "-- @conn app\nSELECT 1;\n")
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	covSvcWaitFrame(t, d, "DATABASES")
	d.Key("enter", "down", "enter")
	covSvcWaitFrame(t, d, "Tables")
	covSvcWaitFrame(t, d, "users")
	d.Key("down", "down")
	d.Key("o")
	dbWaitPage(t, d, "ann@example.com")
	cursorTo(t, d, "free")
	d.Key("e")
	covSvcWaitFrame(t, d, "Edit cell")
	d.Key("ctrl+u")
	for i := 0; i < 20; i++ {
		d.Key("backspace")
	}
	d.Type("team")
	d.Key("enter")
	dbWaitPage(t, d, "team")
	cursorTo(t, d, "cy@example.com")
	d.Key("-")
	d.Key("u") // undo the delete
	d.Key("-")
	d.Key("w")
	covSvcWaitFrame(t, d, "UPDATE")
	d.Key("right", "right", "enter")
	if !d.WaitFor(func() bool { return covSvcSQLiteCount(t, d, "plan = 'team'") == 1 && covSvcSQLiteCount(t, d, "id = 3") == 0 }, 5*time.Second) {
		t.Fatalf("submit did not reach the file:\n%s", ansi.Strip(d.Frame()))
	}
}

func covSvcSQLiteCount(t *testing.T, d *testkit.Driver, where string) int {
	t.Helper()
	sess, err := dbclient.NewSQLiteSession(context.Background(), dbclient.Conn{Driver: "sqlite", Path: filepath.Join(d.Dir, "app.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	r := sess.Run(context.Background(), "SELECT count(*) FROM users WHERE "+where, dbclient.RunOptions{})
	if r.Err != nil || len(r.Rows) != 1 {
		return -1
	}
	if r.Rows[0][0].Text == "1" {
		return 1
	}
	if r.Rows[0][0].Text == "0" {
		return 0
	}
	return -1
}
