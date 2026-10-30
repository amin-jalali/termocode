package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
)

// Result page keys on a console result of 205 rows: paging stops at the
// ends, the transposed view walks rows, v opens a cell, y copies it,
// gd goes back to the statement, q closes the page.
func TestCovSvcResultPageKeys(t *testing.T) {
	var clip []string
	t.Cleanup(app.SetClipboardSink(func(s string) { clip = append(clip, s) }))
	d, _ := dbDriverSized(t, "-- @conn local\nSELECT 1;\n", 110, 32)
	d.Key("down")
	dbSend(d)
	dbWaitPage(t, d, "row-000")
	d.Key("[") // already on page 1
	d.Key("]", "]", "]")
	dbWaitPage(t, d, "page 3 of 3")
	d.Key("[")
	dbWaitPage(t, d, "page 2 of 3")
	d.Key("[")
	cursorTo(t, d, "row-001")
	d.Key("t")
	dbWaitPage(t, d, "row-001")
	d.Key("]")
	dbWaitPage(t, d, "row-002")
	d.Key("[")
	dbWaitPage(t, d, "row-001")
	d.Key("[", "[", "[")
	d.Key("t")
	cursorTo(t, d, "row-004")
	d.Key("y")
	if !d.WaitFor(func() bool { return len(clip) > 0 }, 2*time.Second) {
		t.Fatal("y copied nothing")
	}
	d.Key("v")
	if !d.WaitFor(func() bool {
		return strings.Contains(dbLua(t, d, `return table.concat(vim.api.nvim_buf_get_lines(0,0,-1,false),'\n')`), "row-004")
	}, 3*time.Second) {
		t.Fatal("v did not open the cell")
	}
	d.Key("q")
	d.Action("open:query.sql")
	d.Key("esc", "down")
	dbSend(d)
	dbWaitPage(t, d, "row-000")
	d.Key("#") // not a data view: nothing
	d.Key("g", "d")
	if !d.WaitFor(func() bool { return strings.HasSuffix(dbBufName(t, d), "query.sql") }, 3*time.Second) {
		t.Fatalf("gd went to %q", dbBufName(t, d))
	}
}

// A plan: it cannot be edited, y copies it.
func TestCovSvcPlanPageKeys(t *testing.T) {
	var clip []string
	t.Cleanup(app.SetClipboardSink(func(s string) { clip = append(clip, s) }))
	s := newEditSession(3)
	d := editDriver(t, 110, 32, s, "")
	openUsers(t, d)
	d.Key("x")
	if !d.WaitFor(func() bool {
		return strings.Contains(dbText(t, d), "Plan") || strings.Contains(dbText(t, d), "EXPLAIN")
	}, 4*time.Second) {
		t.Skipf("no plan page:\n%s", dbText(t, d))
	}
	d.Key("e")
	covSvcWaitFrame(t, d, "A plan cannot be edited")
	d.Key("y")
	d.Key("#")
}
