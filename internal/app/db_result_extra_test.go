package app_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
)

// typedSession answers every read with two typed columns and three rows.
type typedSession struct{ consoleSession }

func (s *typedSession) Run(ctx context.Context, sql string, opt dbclient.RunOptions) dbclient.Result {
	s.consoleSession.Run(ctx, "-- counted", opt)
	r := dbclient.Result{Columns: []dbclient.Column{{Name: "id", Type: "INT4"}, {Name: "name", Type: "TEXT"}}}
	for _, n := range []string{"ann", "bob", "cid"} {
		r.Rows = append(r.Rows, []dbclient.Cell{{Text: "1", Kind: "number"}, {Text: n}})
	}
	return r
}

func dbTypedDriver(t *testing.T, config string) *testkit.Driver {
	t.Helper()
	s := &typedSession{}
	t.Cleanup(app.SetDatabaseSessionFactoryForTest(func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return s, nil }))
	d := testkit.New(t, testkit.Options{W: 100, H: 30, Nvim: true, Config: map[string]string{"config.json": config},
		Files: map[string]string{"query.sql": "-- @conn local\nSELECT 1;\n", ".termocode/databases.json": dbTestConns}})
	d.Action("open:query.sql")
	d.Key("esc", "j")
	dbSend(d)
	dbWaitPage(t, d, "cid")
	return d
}

// DBX-26: db_column_types shows each column's type next to its name.
func TestDBResultColumnTypesInHeader(t *testing.T) {
	d := dbTypedDriver(t, dbResultCfg+`,"db_column_types":true}`)
	dbWaitPage(t, d, "id INT4")
	dbWaitPage(t, d, "name TEXT")
}

// DBX-26: T on the page turns the column types on and off.
func TestDBResultColumnTypesToggle(t *testing.T) {
	d := dbTypedDriver(t, dbResultCfg+`}`)
	if strings.Contains(dbText(t, d), "INT4") {
		t.Fatal("types shown before T")
	}
	d.Key("T")
	dbWaitPage(t, d, "id INT4")
}

// DBX-26: db_row_numbers puts the row number in front of each row.
func TestDBResultRowNumbers(t *testing.T) {
	d := dbTypedDriver(t, dbResultCfg+`,"db_row_numbers":true}`)
	text := dbText(t, d)
	for n, name := range []string{"ann", "bob", "cid"} {
		if !dbRowNumbered(text, name, n+1) {
			t.Fatalf("row %d (%s) has no number:\n%s", n+1, name, text)
		}
	}
}

// dbRowNumbered is true when the page line holding name starts with
// the mark column and the row number n.
func dbRowNumbered(text, name string, n int) bool {
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, name) {
			return strings.HasPrefix(l, " "+strconv.Itoa(n)+" ")
		}
	}
	return false
}

// DBX-26: I turns the row numbers on; the cursor still finds the column.
func TestDBResultRowNumbersToggleKeepsColumns(t *testing.T) {
	d := dbTypedDriver(t, dbResultCfg+`}`)
	d.Key("I")
	if !d.WaitFor(func() bool { return dbRowNumbered(dbText(t, d), "cid", 3) }, 2*time.Second) {
		t.Fatalf("no row numbers after I:\n%s", dbText(t, d))
	}
	// the bounds skip the number gutter: column 0 starts after it
	b := dbLua(t, d, `return tostring(_G._termocode_db.bounds[1])`)
	if b == "1" {
		t.Fatalf("first column bound %s ignores the gutter", b)
	}
}

// DBX-26: m opens the cell menu with the cell and column actions.
func TestDBResultCellMenu(t *testing.T) {
	d := dbTypedDriver(t, dbResultCfg+`}`)
	d.Key("m")
	frame := d.Frame()
	for _, want := range []string{"Open the cell", "Copy the cell", "Copy the column", "Copy column definition"} {
		if !d.WaitFor(func() bool { frame = d.Frame(); return strings.Contains(frame, want) }, 2*time.Second) {
			t.Fatalf("cell menu lacks %q:\n%s", want, frame)
		}
	}
}

// DBX-26: db_inline_result shows the first rows under the statement in
// the .sql buffer.
func TestDBResultInlineUnderQuery(t *testing.T) {
	d := dbTypedDriver(t, dbResultCfg+`,"db_inline_result":true}`)
	d.Key("ctrl+w", "h")
	got := ""
	if !d.WaitFor(func() bool {
		got = dbLua(t, d, `local out={};for _,b in ipairs(vim.api.nvim_list_bufs()) do if vim.api.nvim_buf_get_name(b):match('query%.sql$') then local ns=vim.api.nvim_create_namespace('termocode_sql_inline');for _,m in ipairs(vim.api.nvim_buf_get_extmarks(b,ns,0,-1,{details=true})) do for _,l in ipairs(m[4].virt_lines or {}) do for _,c in ipairs(l) do out[#out+1]=c[1] end end end end end;return table.concat(out,'\n')`)
		return strings.Contains(got, "bob")
	}, 3*time.Second) {
		t.Fatalf("no inline result under the query: %q", got)
	}
}
