package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func besideResult(col string, vals ...string) dbclient.Result {
	r := dbclient.Result{Columns: []dbclient.Column{{Name: col}}}
	for _, v := range vals {
		r.Rows = append(r.Rows, []dbclient.Cell{{Text: v}})
	}
	return r
}

// besideModel has two pinned result tabs: users (left) and orders.
func besideModel(t *testing.T) (*Model, *dbConsoleState) {
	t.Helper()
	withConfigDir(t)
	m := dbGridModel(besideResult("user_name", "ann", "bob"))
	s := m.dbConsole()
	s.curTab().pinned = true
	s.placeRun(&dbRun{conn: "local", sql: "SELECT * FROM orders", by: "user", result: besideResult("order_no", "A-1", "A-2", "A-3")})
	s.useTab(0)
	return m, s
}

// DBX-26: two results side by side: the page tab and another tab share
// every line, each in half of the width, split by a bar.
func TestDBResultBesideDrawsTwoGrids(t *testing.T) {
	m, s := besideModel(t)
	s.beside = s.tabs[1]
	p := m.databasePage()
	var head string
	for _, l := range p.lines {
		if strings.Contains(l, "user_name") {
			head = l
		}
	}
	if !strings.Contains(head, "order_no") || !strings.Contains(head, dbGlyphVBar.String()) {
		t.Fatalf("header line lacks both grids: %q", head)
	}
	text := strings.Join(p.lines, "\n")
	for _, v := range []string{"ann", "bob", "A-1", "A-3"} {
		if !strings.Contains(text, v) {
			t.Fatalf("page lacks %q:\n%s", v, text)
		}
	}
	for _, l := range p.lines {
		if strings.Contains(l, "ann") && !strings.Contains(l, "A-1") {
			t.Fatalf("row 1 of both grids are not on one line: %q", l)
		}
	}
}

// DBX-26: Databases: Show Result Beside… offers the other tabs with
// rows, a pick shows it beside, Close puts the one grid back.
func TestDBResultBesidePickAndClose(t *testing.T) {
	m, s := besideModel(t)
	if _, ok := m.dispatchDatabasesPalette("db-result-beside"); !ok || !m.pickerOpen || m.pickerKind != pickerKindDBBeside {
		t.Fatal("Databases: Show Result Beside… did not open the picker")
	}
	m.pickerOpen = false
	items := m.databaseBesideItems()
	if len(items) != 1 || items[0].ID != "1" {
		t.Fatalf("items %+v, want only tab 2", items)
	}
	m.databaseSetBeside("1")
	if s.beside != s.tabs[1] {
		t.Fatal("pick did not set the tab beside")
	}
	if items := m.databaseBesideItems(); len(items) != 2 || items[len(items)-1].ID != "close" {
		t.Fatalf("with one beside the picker offers Close last: %+v", items)
	}
	m.databaseSetBeside("close")
	if s.beside != nil {
		t.Fatal("close left the tab beside")
	}
	if text := strings.Join(m.databasePage().lines, "\n"); strings.Contains(text, "order_no") {
		t.Fatalf("closed, yet the other grid is drawn:\n%s", text)
	}
}

// DBX-26: a closed tab beside is no longer drawn; the page falls back to its own grid.
func TestDBResultBesideGoneTabFallsBack(t *testing.T) {
	m, s := besideModel(t)
	gone := &dbTab{run: &dbRun{result: besideResult("order_no", "A-1")}}
	s.beside = gone
	if text := strings.Join(m.databasePage().lines, "\n"); strings.Contains(text, "order_no") || !strings.Contains(text, "user_name") {
		t.Fatalf("stale beside tab drawn:\n%s", text)
	}
}
