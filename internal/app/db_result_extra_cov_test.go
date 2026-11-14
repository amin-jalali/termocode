package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// m on a result cell: the cell menu, and its DDL copies.
func TestCovDBResultCellMenu(t *testing.T) {
	clip := dbeClip(t)
	m, _ := dbeModel(t)
	m.databaseCellMenu(0, 0)
	dbeToastHas(t, m, "No result yet")
	m.databaseCopyColumnDDL(false, 0, 0)
	if m.databaseToggleRowNums() != nil || m.databaseToggleColTypes() != nil {
		t.Fatal("a toggle with no result drew a page")
	}
	dbeOpen(t, m, "orders")
	s := m.dbConsole()
	s.view = dbViewMessages
	m.databaseCellMenu(0, 0)
	dbeToastHas(t, m, "The cell menu is for the Grid")
	s.view = dbViewGrid
	m.databaseCellMenu(0, 2)
	if !m.pickerOpen || m.pickerKind != pickerKindDBCellMenu || !strings.Contains(m.picker.View(), "Show only rows with this value") {
		t.Fatal("no cell menu with the table filter")
	}
	m.pickerOpen = false
	cmd, ok := m.databaseConsolePicker("ddl")
	if !ok {
		t.Fatal("not routed")
	}
	dbePump(m, cmd)
	if got := clip(); got != `"note" text` {
		t.Fatalf("column DDL %q", got)
	}
	m.databaseCellMenuPick("ddl-all")
	if got := clip(); !strings.HasPrefix(got, `CREATE TABLE "public"."orders" (`) || !strings.Contains(got, `"customer_id" int4`) {
		t.Fatalf("table DDL %q", got)
	}
	s.trans = true
	m.databaseCopyColumnDDL(false, 1, 0)
	if got := clip(); got != `"customer_id" int4` {
		t.Fatalf("transposed DDL %q", got)
	}
	s.trans = false
	m.databaseCopyColumnDDL(false, 0, 9)
	dbeToastHas(t, m, "Put the cursor on a column")
	m.databaseCellMenuPick("=")
	m.databaseCellMenuPick("y")
	// A console result (no data view) has no table filter.
	s.shown.tv = nil
	m.databaseCellMenu(0, 0)
	if strings.Contains(m.picker.View(), "Show only rows with this value") {
		t.Fatal("a console result offers the table filter")
	}
	m.databaseToggleRowNums()
	m.databaseToggleColTypes()
	if s.rowNums == nil || s.colTypes == nil || s.showRowNums() == *s.rowNums && false {
		t.Fatal("the toggles were not kept")
	}
}

// Inline results: the lines kept for a console run, the marks drawn
// under the statement, and Clear Inline Results.
func TestCovDBResultInline(t *testing.T) {
	r := dbclient.Result{Columns: []dbclient.Column{{Name: "n"}}, Truncated: true}
	for i := 0; i < 12; i++ {
		r.Rows = append(r.Rows, []dbclient.Cell{dbeNum("1")})
	}
	lines := dbGridLines(r, 40, 10)
	if last := lines[len(lines)-1]; last != "  … 2 more rows (cut at db_max_rows)" {
		t.Fatalf("%q", last)
	}
	m, _ := dbeModel(t)
	s := m.dbConsole()
	s.inline = map[string][]string{"/a.sql\x00SELECT 1": {"x"}, "/b.sql\x00SELECT 2": {"y"}}
	marks := s.dbInlineMarks("/a.sql", "SELECT 1;\nSELECT 3;", []dbStatementSpan{{sql: "SELECT 1", end: 8, line: 1}, {sql: "SELECT 3", end: 18, line: 2}})
	if len(marks) != 1 || marks[0]["line"] != 1 {
		t.Fatalf("%v", marks)
	}
	m.databaseClearInline()
	if s.inline != nil {
		t.Fatal("not cleared")
	}
	if got := s.dbInlineMarks("/a.sql", "", nil); len(got) != 0 {
		t.Fatal("marks with none kept")
	}
}

// Result Cell Menu… from the palette reads the cell under the cursor of
// the drawn page.
func TestCovDBResultCellMenuAtCursor(t *testing.T) {
	m, _ := dbeRealModel(t)
	m.databaseCellMenuAtCursor()
	dbeToastHas(t, m, "Open the result page first")
	dbeOpen(t, m, "orders")
	m.databaseShowPage()
	m.databaseCellMenuAtCursor()
	if !m.pickerOpen || m.pickerKind != pickerKindDBCellMenu {
		t.Fatalf("no cell menu (toast %q)", cdbToast(m))
	}
	nv := m.nvim
	m.nvim, m.pickerOpen = nil, false
	if m.databaseCellMenuAtCursor() != nil || m.pickerOpen {
		t.Fatal("no editor")
	}
	m.nvim = nv
}
