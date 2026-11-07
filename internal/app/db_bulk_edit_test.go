package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// dbBulkFixture is a result of three rows: id (key), name, data
// (binary) and note.
func dbBulkFixture() (*dbTab, *dbEditTarget) {
	res := dbclient.Result{
		Columns: []dbclient.Column{{Name: "id"}, {Name: "name"}, {Name: "data"}, {Name: "note"}},
		Rows: [][]dbclient.Cell{
			{{Text: "1", Kind: "number"}, {Text: "ann"}, {Text: "0x00", Kind: "binary"}, {Text: "x"}},
			{{Text: "2", Kind: "number"}, {Text: "bob"}, {Text: "0x01", Kind: "binary"}, {Null: true}},
			{{Text: "3", Kind: "number"}, {Text: "cy"}, {Text: "0x02", Kind: "binary"}, {Text: "z"}},
		},
	}
	tg := &dbEditTarget{table: "people", key: []int{0}, editable: map[int]bool{0: true, 1: true, 2: true, 3: true}}
	return &dbTab{run: &dbRun{result: res}, target: tg}, tg
}

func dbOrig(rows ...int) []dbEditPos {
	var out []dbEditPos
	for _, r := range rows {
		out = append(out, dbEditPos{orig: r, added: -1})
	}
	return out
}

// - on a range marks every row; - again on the same rows keeps them.
func TestDBBulkDeleteToggles(t *testing.T) {
	tab, _ := dbBulkFixture()
	if del, n := dbToggleDeleteRows(tab, dbOrig(0, 2)); !del || n != 2 || !tab.edits.deleted[0] || !tab.edits.deleted[2] || tab.edits.deleted[1] {
		t.Fatalf("delete: %v %d %v", del, n, tab.edits.deleted)
	}
	// One row of the range is not marked yet: all get marked.
	if del, n := dbToggleDeleteRows(tab, dbOrig(1, 2)); !del || n != 2 || len(tab.edits.deleted) != 3 {
		t.Fatalf("mixed: %v %d %v", del, n, tab.edits.deleted)
	}
	if del, n := dbToggleDeleteRows(tab, dbOrig(0, 1, 2)); del || n != 3 || len(tab.edits.deleted) != 0 {
		t.Fatalf("keep: %v %d %v", del, n, tab.edits.deleted)
	}
	// New rows in the range are removed.
	tab.edits.added = []*dbNewRow{{set: map[int]dbclient.Cell{}}, {set: map[int]dbclient.Cell{1: {Text: "b"}}}, {set: map[int]dbclient.Cell{}}}
	if _, n := dbToggleDeleteRows(tab, []dbEditPos{{orig: -1, added: 0}, {orig: -1, added: 2}}); n != 2 || len(tab.edits.added) != 1 || tab.edits.added[0].set[1].Text != "b" {
		t.Fatalf("added: %d %+v", n, tab.edits.added)
	}
}

// + on a range adds a copy of each row: the key columns get their
// default and binary values are left out; a pending value is copied.
func TestDBBulkCloneRows(t *testing.T) {
	tab, tg := dbBulkFixture()
	tab.edits = newDBEdits()
	tab.edits.cells[dbCellRef{1, 1}] = dbclient.Cell{Text: "bobby"}
	if n := dbCloneRows(tab, tg, dbOrig(0, 1)); n != 2 || len(tab.edits.added) != 2 {
		t.Fatalf("cloned %d: %+v", n, tab.edits.added)
	}
	a, b := tab.edits.added[0].set, tab.edits.added[1].set
	if _, ok := a[0]; ok {
		t.Error("the key was copied")
	}
	if _, ok := a[2]; ok {
		t.Error("a binary value was copied")
	}
	if a[1].Text != "ann" || a[3].Text != "x" || b[1].Text != "bobby" || !b[3].Null {
		t.Errorf("copies = %+v %+v", a, b)
	}
	// A copy of a new row is another new row.
	if n := dbCloneRows(tab, tg, []dbEditPos{{orig: -1, added: 1}}); n != 1 || tab.edits.added[2].set[1].Text != "bobby" {
		t.Errorf("clone of a new row: %d %+v", n, tab.edits.added)
	}
	// A data page with hidden rowids has no key columns: all but binary is copied.
	tab2, tg2 := dbBulkFixture()
	tg2.key = nil
	dbCloneRows(tab2, tg2, dbOrig(2))
	if c := tab2.edits.added[0].set; c[0].Text != "3" || c[1].Text != "cy" {
		t.Errorf("rowid clone = %+v", c)
	}
}

// e on a range sets one value in the column of every row.
func TestDBBulkSetValue(t *testing.T) {
	tab, _ := dbBulkFixture()
	tab.edits = newDBEdits()
	tab.edits.added = []*dbNewRow{{set: map[int]dbclient.Cell{}}}
	poses := []dbEditPos{{orig: 0, added: -1, col: 3}, {orig: 1, added: -1, col: 3}, {orig: 2, added: -1, col: 3}, {orig: -1, added: 0, col: 3}}
	for _, p := range poses {
		if !dbSetCellValue(tab, p, "z", false) {
			t.Fatalf("set %+v failed", p)
		}
	}
	// Row 2 already held z: no change there.
	if len(tab.edits.cells) != 2 || tab.edits.cells[dbCellRef{0, 3}].Text != "z" || tab.edits.cells[dbCellRef{1, 3}].Text != "z" {
		t.Errorf("cells = %+v", tab.edits.cells)
	}
	if tab.edits.added[0].set[3].Text != "z" {
		t.Errorf("new row = %+v", tab.edits.added[0].set)
	}
	if !dbSetCellValue(tab, dbEditPos{orig: 0, added: -1, col: 1}, `\N`, false) || !tab.edits.cells[dbCellRef{0, 1}].Null {
		t.Error(`\N is not NULL`)
	}
	if dbSetCellValue(tab, dbEditPos{orig: 9, added: -1, col: 1}, "x", false) || dbSetCellValue(tab, dbEditPos{orig: -1, added: 5, col: 1}, "x", false) {
		t.Error("a row out of range was set")
	}
}

func TestDBParseTSV(t *testing.T) {
	got, err := dbParseTSV("a\tb\r\nc\t\\N\n\n")
	if err != nil || len(got) != 2 || strings.Join(got[0], "|") != "a|b" || strings.Join(got[1], "|") != `c|\N` {
		t.Fatalf("parse = %q %v", got, err)
	}
	if _, err := dbParseTSV(""); err == nil {
		t.Error("empty text parsed")
	}
	if _, err := dbParseTSV("\n\n"); err == nil {
		t.Error("blank lines parsed")
	}
	if _, err := dbParseTSV(strings.Repeat("x\n", dbPasteMaxRows+1)); err == nil {
		t.Error("too many rows parsed")
	}
}

// P pastes from the cursor cell to the right and down: the rows under
// the cursor get the values, extra lines become new rows, cells past
// the last column or in a binary column and rows marked for deletion
// are skipped.
func TestDBPasteTSV(t *testing.T) {
	tab, tg := dbBulkFixture()
	tab.edits = newDBEdits()
	tab.edits.deleted[1] = true
	lines := [][]string{{"A", "0xff", "N1", "extra"}, {"B", "0xee", "N2"}, {"C", "", `\N`}, {"D"}}
	cells, added, skipped := dbPasteTSV(tab, tg, dbOrig(0, 1), 1, lines)
	// Row 0: name + note (binary skipped, "extra" past the end); row 1
	// deleted; 2 new rows.
	if cells != 2 || added != 2 || skipped != 6 {
		t.Fatalf("cells %d added %d skipped %d", cells, added, skipped)
	}
	if tab.edits.cells[dbCellRef{0, 1}].Text != "A" || tab.edits.cells[dbCellRef{0, 3}].Text != "N1" {
		t.Errorf("row 0 = %+v", tab.edits.cells)
	}
	if _, ok := tab.edits.cells[dbCellRef{1, 1}]; ok {
		t.Error("a deleted row was changed")
	}
	if len(tab.edits.added) != 2 {
		t.Fatalf("added = %+v", tab.edits.added)
	}
	c, d := tab.edits.added[0].set, tab.edits.added[1].set
	if c[1].Text != "C" || !c[3].Null || d[1].Text != "D" {
		t.Errorf("new rows = %+v %+v", c, d)
	}
	if _, ok := c[2]; ok {
		t.Error("an empty value on a new row is not the default")
	}
}

// The page binds the range keys and P, and the key card lists them.
func TestDBBulkKeysAreBound(t *testing.T) {
	for _, want := range []string{"termocode_db_range", "'P'", "vim.keymap.set('x'"} {
		if !strings.Contains(dbPageLua, want) {
			t.Errorf("dbPageLua has no %s", want)
		}
	}
	var keys []string
	for _, r := range dbPageKeyRows() {
		keys = append(keys, r.Key+" "+r.What)
	}
	all := strings.Join(keys, "\n")
	for _, want := range []string{"P paste", "V then e", "V then + / -"} {
		if !strings.Contains(all, want) {
			t.Errorf("key card has no %q:\n%s", want, all)
		}
	}
}
