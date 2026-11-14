package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// gf follows a foreign key to the row it points to, gb comes back, and
// the cells that cannot be followed say why.
func TestCovDBFKFollowAndBack(t *testing.T) {
	m, _ := dbeModel(t)
	dbeOpen(t, m, "orders")
	s := m.dbConsole()
	orders := s.shown.tv

	dbePump(m, m.databaseFKKey("gf", 0, 0))
	dbeToastHas(t, m, "Not a foreign key")
	dbePump(m, m.databaseFKKey("gf", 1, 1))
	dbeToastHas(t, m, "Nothing to follow")
	dbePump(m, m.databaseFKKey("gf", 0, 99))
	dbeToastHas(t, m, "Move to a cell first")
	if cmd := m.databaseFKKey("zz", 0, 0); cmd != nil {
		t.Fatal("an unknown key did something")
	}
	dbePump(m, m.databaseFKKey("gb", 0, 0))
	dbeToastHas(t, m, "No jump to go back from")

	dbePump(m, m.databaseFKKey("gf", 0, 1))
	if s.shown.tv.name != "customers" || s.shown.tv.where != `"id" = 5` {
		t.Fatalf("gf opened %q where %q", s.shown.tv.name, s.shown.tv.where)
	}
	if len(s.fk.back) != 1 || s.fk.back[0].tv != orders {
		t.Fatalf("trail %+v", s.fk.back)
	}
	dbePump(m, m.databaseFKKey("gb", 0, 0))
	if s.shown.tv != orders || len(s.fk.back) != 0 {
		t.Fatalf("gb showed %q", s.shown.tv.name)
	}

	// gb when the connection closed in the meantime.
	s.fk.pushBack(dbFKBack{tv: orders})
	sess := m.databases.sessions["pg"]
	delete(m.databases.sessions, "pg")
	dbePump(m, m.databaseFKKey("gb", 0, 0))
	dbeToastHas(t, m, "Not connected")
	// gf with no session: the jump is refused.
	dbePump(m, m.databaseFKOpen(orders, 0, dbSiblingTable(orders.dbTable, "customers"), `"id" = 5`))
	dbeToastHas(t, m, "Not connected")
	m.databases.sessions["pg"] = sess
	dbePump(m, m.databaseFKOpen(orders, 0, dbSiblingTable(orders.dbTable, "customers"), `1; DROP TABLE x`))
	dbeToastHas(t, m, "Filter refused")
}

// gf on a key whose column is hidden from the result, or on a new row.
func TestCovDBFKFollowNeedsTheWholeKey(t *testing.T) {
	m, sess := dbeModel(t)
	sess.tables["orders"].fks = append(sess.tables["orders"].fks, dbclient.SchemaForeignKey{Name: "orders_customer_fk", Column: "missing", RefTable: "customers", RefColumn: "org"})
	dbeOpen(t, m, "orders")
	dbePump(m, m.databaseFKKey("gf", 0, 1))
	dbeToastHas(t, m, "The key is not all here")

	tab := m.dbConsole().curTab()
	tab.edits = newDBEdits()
	tab.edits.added = append(tab.edits.added, &dbNewRow{set: map[int]dbclient.Cell{}})
	dbePump(m, m.databaseFKKey("gf", 2, 1))
	dbeToastHas(t, m, "Submit the new row first")
	dbePump(m, m.databaseFKKey("gr", 2, 1))
	dbeToastHas(t, m, "Submit the new row first")
}

// Outside a data view the keys say where they work; a run in flight
// takes no key.
func TestCovDBFKOutsideADataView(t *testing.T) {
	m, _ := dbeModel(t)
	dbePump(m, m.databaseFKKey("gf", 0, 0))
	dbeToastHas(t, m, "Foreign keys work in table data")
	dbeOpen(t, m, "orders")
	s := m.dbConsole()
	s.running = &dbRun{}
	if cmd := m.databaseFKKey("gf", 0, 1); cmd != nil {
		t.Fatal("a key ran while a query runs")
	}
	s.running = nil
	// The page shows another tab's run: nothing to act on.
	shown := s.shown
	s.shown = &dbRun{tv: shown.tv, result: shown.result}
	if _, _, _, cmd, ok := m.databaseFKCell(0, 1); ok || cmd != nil {
		t.Fatal("a cell of a run not in the tab")
	}
	s.shown = shown
}

// gr lists the tables whose rows point at this row and opens one.
func TestCovDBFKReferencing(t *testing.T) {
	m, sess := dbeModel(t)
	dbeOpen(t, m, "customers")
	s := m.dbConsole()
	customers := s.shown.tv

	// The schema the fake answers has no keys: no table points here.
	dbePump(m, m.databaseFKKey("gr", 0, 0))
	dbeToastHas(t, m, "No table points here")
	if !strings.Contains(sess.ran(), "pg_") && !strings.Contains(sess.ran(), "information_schema") {
		t.Fatalf("the catalogs were not read:\n%s", sess.ran())
	}
	sess.roErr = errors.New("no read-only session")
	dbePump(m, m.databaseFKKey("gr", 0, 0))
	dbeToastHas(t, m, "Could not read the foreign keys")
	sess.roErr = nil

	cells := dbRowCells(s.shown.result.Columns, s.shown.result.Rows[0])
	cs := dbclient.CanonSchema{Tables: []dbclient.CanonTable{
		{Name: "orders", Constraints: []dbclient.CanonConstraint{{Kind: "FOREIGN KEY", Name: "fk", Columns: []string{"customer_id"}, RefTable: "customers", RefColumns: []string{"id"}}}},
		{Name: "audit", Constraints: []dbclient.CanonConstraint{{Kind: "FOREIGN KEY", Name: "v", Columns: []string{"who"}, RefTable: "customers", RefColumns: []string{"id"}, Virtual: true}}},
	}}
	if cmd := m.onDBFKReferencing(s.tableGen+1, customers, 0, cells, cs, nil); cmd != nil || m.pickerOpen {
		t.Fatal("a stale answer opened the picker")
	}
	m.onDBFKReferencing(s.tableGen, customers, 0, cells, cs, nil)
	if !m.pickerOpen || m.pickerKind != pickerKindDBFKRefs || len(s.fk.refs) != 2 {
		t.Fatalf("picker %v refs %+v", m.pickerOpen, s.fk.refs)
	}
	if !strings.HasSuffix(s.fk.refs[0].label, "· virtual") {
		t.Fatalf("the virtual key is not marked: %q", s.fk.refs[0].label)
	}
	// A pick out of range, a bad id, a foreign picker.
	m.pickerOpen = false
	if cmd, ok := m.databaseFKPicker("x"); !ok || cmd != nil {
		t.Fatal("bad id")
	}
	if cmd, ok := m.databaseFKPicker("9"); !ok || cmd != nil {
		t.Fatal("out of range")
	}
	m.onDBFKReferencing(s.tableGen, customers, 0, cells, cs, nil)
	m.pickerOpen = false
	cmd, ok := m.databaseFKPicker("1")
	if !ok {
		t.Fatal("not routed")
	}
	dbePump(m, cmd)
	if s.shown.tv.name != "orders" || s.shown.tv.where != `"customer_id" = 5` {
		t.Fatalf("gr opened %q where %q", s.shown.tv.name, s.shown.tv.where)
	}
	m.pickerKind = 0
	if _, ok := m.databaseFKPicker("0"); ok {
		t.Fatal("a foreign picker was taken")
	}

	// gr with no session.
	dbeOpen(t, m, "customers")
	delete(m.databases.sessions, "pg")
	dbePump(m, m.databaseFKKey("gr", 0, 0))
	dbeToastHas(t, m, "Not connected")
}

// gl lists the values of the referenced column and sets the picked one as
// a pending change.
func TestCovDBFKLookup(t *testing.T) {
	m, sess := dbeModel(t)
	dbeOpen(t, m, "orders")
	s := m.dbConsole()
	tab := s.curTab()

	dbePump(m, m.databaseFKKey("gl", 0, 2))
	dbeToastHas(t, m, "Not a foreign key")

	dbePump(m, m.databaseFKKey("gl", 0, 1))
	if !m.pickerOpen || m.pickerKind != pickerKindDBFKLookup || s.fk.lookup == nil || len(s.fk.lookup.values) != 2 {
		t.Fatalf("no value picker: %v %+v (toast %q)", m.pickerOpen, s.fk.lookup, cdbToast(m))
	}
	if !strings.Contains(sess.ran(), `SELECT DISTINCT`) && !strings.Contains(sess.ran(), `"customers"`) {
		t.Fatalf("lookup query:\n%s", sess.ran())
	}
	m.pickerOpen = false
	cmd, ok := m.databaseFKPicker("1")
	if !ok {
		t.Fatal("not routed")
	}
	dbePump(m, cmd)
	dbeToastHas(t, m, "Value set")
	if c, ok := dbCellNow(tab, dbEditPos{orig: 0, added: -1, col: 1}); !ok || c.Text != "6" {
		t.Fatalf("cell %+v %v", c, ok)
	}
	// A stale pick (no lookup open) does nothing.
	m.pickerKind = pickerKindDBFKLookup
	if cmd, ok := m.databaseFKPicker("0"); !ok || cmd != nil {
		t.Fatal("a pick with no lookup")
	}

	// The referenced table is empty / the read fails / a stale answer.
	lk := &dbFKLookup{tab: tab, pos: dbEditPos{orig: 0, added: -1, col: 1}}
	if m.onDBFKLookup(s.tableGen+1, lk, dbclient.Result{}) != nil {
		t.Fatal("stale")
	}
	m.onDBFKLookup(s.tableGen, lk, dbclient.Result{Rows: [][]dbclient.Cell{{{Null: true}}}})
	dbeToastHas(t, m, "The referenced table has no rows")
	m.onDBFKLookup(s.tableGen, lk, dbclient.Result{Err: errors.New("denied")})
	dbeToastHas(t, m, "Could not list the values")
	// A full list says it is cut.
	var rows [][]dbclient.Cell
	for i := 0; i < dbFKLookupLimit; i++ {
		rows = append(rows, []dbclient.Cell{dbeNum(strings.Repeat("1", 1+i%3))})
	}
	m.onDBFKLookup(s.tableGen, lk, dbclient.Result{Rows: rows})
	if !strings.Contains(m.picker.View(), "first 500") && !m.pickerOpen {
		t.Fatal("the cut list is not marked")
	}

	// With no session.
	delete(m.databases.sessions, "pg")
	tab.target = nil
	dbePump(m, m.databaseFKKey("gl", 0, 1))
	dbeToastHas(t, m, "Not connected")
}

// gl on a view (read-only) and in an open transaction.
func TestCovDBFKLookupReadOnlyAndInTx(t *testing.T) {
	m, sess := dbeModel(t)
	dbeOpen(t, m, "orders")
	s := m.dbConsole()
	s.shown.tv.view = true
	s.curTab().target = nil
	dbePump(m, m.databaseFKKey("gl", 0, 1))
	dbeToastHas(t, m, "This result cannot be edited")
	s.shown.tv.view = false
	s.curTab().target = nil
	// a key that cannot give a lookup query
	s.shown.tv.fks = []dbclient.SchemaForeignKey{{Name: "k", Column: "customer_id", RefTable: "", RefColumn: ""}}
	dbePump(m, m.databaseFKKey("gl", 0, 1))
	dbeToastHas(t, m, "No values to pick")
	s.shown.tv.fks = sess.tables["orders"].fks
	m.databases.txs = map[string]*dbTxState{"pg": {open: true}}
	dbePump(m, m.databaseFKKey("gl", 0, 1))
	if !m.pickerOpen {
		t.Fatalf("no picker in a transaction: %q", cdbToast(m))
	}
}

// The page's key notification reaches gf; gr and gl outside a data view
// say where they work; gl waits for a console result's key lookup.
func TestCovDBFKNotifyAndGuards(t *testing.T) {
	m, _ := dbeModel(t)
	notifyHandlers["termocode_db_fk"](m, []any{"gr", 0, 0})
	dbeToastHas(t, m, "Foreign keys work in table data")
	dbePump(m, m.databaseFKKey("gl", 0, 0))
	dbeToastHas(t, m, "Foreign keys work in table data")
	dbeOpen(t, m, "orders")
	s := m.dbConsole()
	s.curTab().target = &dbEditTarget{loading: true}
	dbePump(m, m.databaseFKKey("gl", 0, 1))
	dbeToastHas(t, m, "Looking up the table's key")
	s.curTab().target = nil
	// A value the cell cannot take (the column is gone) sets nothing.
	tab := s.curTab()
	s.fk.lookup = &dbFKLookup{tab: tab, pos: dbEditPos{orig: 0, added: 3, col: 1}, values: []dbclient.Cell{dbeNum("6")}}
	m.pickerKind = pickerKindDBFKLookup
	if cmd, ok := m.databaseFKPicker("0"); !ok || cmd != nil {
		t.Fatal("a value went to a cell that is not there")
	}
}
