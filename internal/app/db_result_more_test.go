package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
)

const dbResultCfg = `{"setup_done":true,"animations":"off"`

// DBX-26: Databases: Set Result Page Size… changes the page live.
func TestDBResultPageSizeLive(t *testing.T) {
	d, _ := dbExecDriver(t, "-- @conn local\nSELECT 1;\n", dbResultCfg+`}`)
	d.Key("j")
	dbSend(d)
	dbWaitPage(t, d, "page 1 of 3")
	d.Palette("db-result-page-size")
	d.Type("50")
	d.Key("enter")
	dbWaitPage(t, d, "page 1 of 5")
}

// DBX-26: Databases: Go to Result Row… opens the page of row N and puts
// the cursor on it.
func TestDBResultGotoRow(t *testing.T) {
	d, _ := dbExecDriver(t, "-- @conn local\nSELECT 1;\n", dbResultCfg+`}`)
	d.Key("j")
	dbSend(d)
	dbWaitPage(t, d, "row-000")
	d.Palette("db-result-goto-row")
	d.Type("150")
	d.Key("enter")
	dbWaitPage(t, d, "page 2 of 3")
	if !d.WaitFor(func() bool { return strings.Contains(dbLua(t, d, `return vim.api.nvim_get_current_line()`), "row-149") }, 2*time.Second) {
		t.Fatalf("cursor line %q", dbLua(t, d, `return vim.api.nvim_get_current_line()`))
	}
}

// DBX-26: Databases: Load All Rows runs again without db_max_rows.
func TestDBResultLoadAll(t *testing.T) {
	d, s := dbExecDriver(t, "-- @conn local\nSELECT 1;\n", dbResultCfg+`,"db_max_rows":50}`)
	d.Key("j")
	dbSend(d)
	dbWaitPage(t, d, "row-000")
	d.Palette("db-result-load-all")
	if !d.WaitFor(func() bool { s.mu.Lock(); defer s.mu.Unlock(); return len(s.limits) == 2 && s.limits[1] > 50 }, 2*time.Second) {
		s.mu.Lock()
		defer s.mu.Unlock()
		t.Fatalf("load all limits %v", s.limits)
	}
}

// DBX-26: db_result_new_tab keeps the old result in its own tab.
func TestDBResultNewTabPerRun(t *testing.T) {
	d, s := dbExecDriver(t, "-- @conn local\nSELECT 1;\n", dbResultCfg+`,"db_result_new_tab":true}`)
	d.Key("j")
	dbSend(d)
	dbWaitPage(t, d, "row-000")
	d.Key("gd")
	dbSend(d)
	if !d.WaitFor(func() bool { return s.count() == 2 }, 2*time.Second) {
		t.Fatal("second run did not start")
	}
	dbWaitPage(t, d, "[2]")
}

// oneRowSession answers every read with one row.
type oneRowSession struct{ consoleSession }

func (s *oneRowSession) Run(ctx context.Context, sql string, opt dbclient.RunOptions) dbclient.Result {
	s.consoleSession.Run(ctx, "-- counted", opt)
	return dbclient.Result{Columns: []dbclient.Column{{Name: "id"}, {Name: "name"}}, Rows: [][]dbclient.Cell{{{Text: "7", Kind: "number"}, {Text: "only-row"}}}}
}

// DBX-26: db_auto_transpose shows a one-row result as name/value lines.
func TestDBResultAutoTransposeOneRow(t *testing.T) {
	s := &oneRowSession{}
	t.Cleanup(app.SetDatabaseSessionFactoryForTest(func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return s, nil }))
	d := testkit.New(t, testkit.Options{W: 100, H: 30, Nvim: true, Config: map[string]string{"config.json": dbResultCfg + `,"db_auto_transpose":true}`},
		Files: map[string]string{"query.sql": "-- @conn local\nSELECT 1;\n", ".termocode/databases.json": dbTestConns}})
	d.Action("open:query.sql")
	d.Key("esc", "j")
	dbSend(d)
	dbWaitPage(t, d, "row 1 of 1")
}
