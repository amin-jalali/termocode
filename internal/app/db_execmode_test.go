package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
	"github.com/charmbracelet/x/ansi"
)

// dbExecDriver is dbDriver with a config.json body.
func dbExecDriver(t *testing.T, sql, config string) (*testkit.Driver, *consoleSession) {
	t.Helper()
	s := &consoleSession{}
	t.Cleanup(app.SetDatabaseSessionFactoryForTest(func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return s, nil }))
	d := testkit.New(t, testkit.Options{W: 100, H: 30, Nvim: true, Config: map[string]string{"config.json": config},
		Files: map[string]string{"query.sql": sql, ".termocode/databases.json": dbTestConns}})
	d.Action("open:query.sql")
	d.Key("esc")
	return d, s
}

// DBX-25: db_exec_mode "smallest" runs the subquery under the caret.
func TestDBExecModeSmallestRunsTheSubquery(t *testing.T) {
	d, s := dbExecDriver(t, "-- @conn local\nSELECT * FROM (SELECT id FROM t) x;\n", `{"setup_done":true,"animations":"off","db_exec_mode":"smallest"}`)
	d.Key("j", "0", "2", "0", "l") // the caret inside "SELECT id"
	dbSend(d)
	if !d.WaitFor(func() bool { return s.count() == 1 }, 2*time.Second) {
		t.Fatal("nothing ran")
	}
	if got := s.ran()[0]; strings.Contains(got, "FROM (") || !strings.Contains(got, "SELECT id FROM t") {
		t.Fatalf("smallest mode ran %q", got)
	}
}

// DBX-25: Run From Caret runs the statement under the caret and every one
// after it, not the ones before.
func TestDBExecModeRunFromCaret(t *testing.T) {
	d, s := dbExecDriver(t, "-- @conn local\nSELECT 1;\nSELECT 2;\nSELECT 3;\n", `{"setup_done":true,"animations":"off"}`)
	d.Key("j", "j")
	d.Palette("db-run-from-caret")
	if !d.WaitFor(func() bool { return s.count() == 2 }, 2*time.Second) {
		t.Fatalf("from caret ran %v", s.ran())
	}
	if got := strings.Join(s.ran(), " "); strings.Contains(got, "SELECT 1") || !strings.Contains(got, "SELECT 3") {
		t.Fatalf("from caret ran %q", got)
	}
}

// DBX-25: Run… asks what to run (innermost first) and runs the choice.
func TestDBExecModeAskPicker(t *testing.T) {
	d, s := dbExecDriver(t, "-- @conn local\nSELECT * FROM (SELECT id FROM t) x;\n", `{"setup_done":true,"animations":"off"}`)
	d.Key("j", "0", "2", "0", "l")
	d.Palette("db-run-ask")
	if !d.WaitForText("Run what?", 2*time.Second) {
		t.Fatalf("no ask picker:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("enter") // the first item: the innermost query
	if !d.WaitFor(func() bool { return s.count() == 1 }, 2*time.Second) {
		t.Fatal("the choice did not run")
	}
	if got := s.ran()[0]; strings.Contains(got, "FROM (") {
		t.Fatalf("ask ran %q, want the subquery", got)
	}
}

// DBX-25: db_selection_split false sends the selection as one text.
func TestDBExecModeSelectionAsOne(t *testing.T) {
	d, s := dbExecDriver(t, "-- @conn local\nSELECT 1; SELECT 2;\n", `{"setup_done":true,"animations":"off","db_selection_split":false}`)
	d.Key("j", "V")
	dbSend(d)
	if !d.WaitFor(func() bool { return s.count() == 1 }, 2*time.Second) {
		t.Fatalf("selection as one ran %v", s.ran())
	}
	d.Settle(200 * time.Millisecond)
	if s.count() != 1 || !strings.Contains(s.ran()[0], "SELECT 2") {
		t.Fatalf("selection was split: %v", s.ran())
	}
}

// DBX-25: Choose Execution Mode writes db_exec_mode.
func TestDBExecModePickerSavesTheSetting(t *testing.T) {
	d, _ := dbExecDriver(t, "-- @conn local\nSELECT 1;\n", `{"setup_done":true,"animations":"off"}`)
	d.Palette("db-exec-mode")
	if !d.WaitForText("Run key runs", 2*time.Second) {
		t.Fatalf("no mode picker:\n%s", ansi.Strip(d.Frame()))
	}
	d.Type("from caret")
	d.Key("enter")
	if !d.WaitFor(func() bool { return strings.Contains(app.ReadConfigForTest(), `"db_exec_mode": "from-caret"`) }, 2*time.Second) {
		t.Fatalf("setting not saved: %s", app.ReadConfigForTest())
	}
}
