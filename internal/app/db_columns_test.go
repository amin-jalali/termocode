package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// dbColumnsModel is a grid of the table users with the columns a b c d.
func dbColumnsModel() *Model {
	res := dbclient.Result{Columns: []dbclient.Column{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}}}
	for i := 0; i < 3; i++ {
		res.Rows = append(res.Rows, []dbclient.Cell{{Text: "a1"}, {Text: "b1"}, {Text: "c1"}, {Text: "d1"}})
	}
	m := dbGridModel(res)
	s := m.dbConsole()
	s.shown.tv = &dbTableView{dbTable: dbTable{conn: "local", driver: "sqlite", name: "users"}, total: -1}
	return m
}

// dbHeader is the column names in the grid header, in display order.
func dbHeader(t *testing.T, m *Model) []string {
	t.Helper()
	p := m.databasePage()
	if p.first < 3 {
		t.Fatalf("no grid: %q", p.lines)
	}
	return strings.Fields(strings.ReplaceAll(p.lines[p.first-3], dbGlyphVBar.String(), " "))
}

func TestDBColumnsHideMoveFreezeScroll(t *testing.T) {
	withConfigDir(t)
	m := dbColumnsModel()
	s := m.dbConsole()
	if got := strings.Join(dbHeader(t, m), " "); got != "a b c d" {
		t.Fatalf("start: %q", got)
	}
	m.databasePageKey("H", 0, 1) // hide b
	if got := strings.Join(dbHeader(t, m), " "); got != "a c d" {
		t.Fatalf("hide: %q", got)
	}
	m.databasePageKey(">", 0, 0) // a one to the right
	if got := strings.Join(dbHeader(t, m), " "); got != "c a d" {
		t.Fatalf("move right: %q", got)
	}
	// Keys after a change name the column under the cursor in display
	// order: display column 1 is now a (result column 0).
	if got := s.gridColumn(1); got != 0 {
		t.Fatalf("display 1 = result %d, want 0", got)
	}
	if got := s.gridColumn(2); got != 3 {
		t.Fatalf("display 2 = result %d, want 3", got)
	}
	if got := s.gridColumn(9); got != -1 {
		t.Fatalf("out of range = %d", got)
	}
	m.databasePageKey("<", 0, 2) // d one to the left
	if got := strings.Join(dbHeader(t, m), " "); got != "c d a" {
		t.Fatalf("move left: %q", got)
	}
	m.databasePageKey("F", 0, 0) // freeze c
	m.databasePageKey("}", 0, 1) // scroll the rest by one column
	if got := strings.Join(dbHeader(t, m), " "); got != "c a" {
		t.Fatalf("frozen + scrolled: %q", got)
	}
	if got := s.gridColumn(1); got != 0 {
		t.Fatalf("scrolled display 1 = result %d, want 0 (a)", got)
	}
	m.databasePageKey("}", 0, 1) // the last column stays on the page
	if got := strings.Join(dbHeader(t, m), " "); got != "c a" {
		t.Fatalf("scroll past the end: %q", got)
	}
	m.databasePageKey("{", 0, 1)
	if got := strings.Join(dbHeader(t, m), " "); got != "c d a" {
		t.Fatalf("scroll back: %q", got)
	}
	m.databasePageKey("F", 0, 0) // again on the last frozen column: unfreeze
	m.databasePageKey("}", 0, 1)
	if got := strings.Join(dbHeader(t, m), " "); got != "d a" {
		t.Fatalf("unfrozen scroll: %q", got)
	}
	m.databasePageKey("R", 0, 0) // reset
	if got := strings.Join(dbHeader(t, m), " "); got != "a b c d" {
		t.Fatalf("reset: %q", got)
	}
}

func TestDBColumnsWidth(t *testing.T) {
	withConfigDir(t)
	m := dbColumnsModel()
	w0 := func() int { p := m.databasePage(); return p.bounds[1] - p.bounds[0] }
	before := w0()
	m.databasePageKey(")", 0, 0)
	if got := w0(); got != before+4 {
		t.Fatalf("wider: %d, want %d", got, before+4)
	}
	for i := 0; i < 10; i++ {
		m.databasePageKey("(", 0, 0)
	}
	// a is 2 cells wide ("a1"); 3 cells is the narrowest column.
	if got := w0(); got != before+1 {
		t.Fatalf("narrowest: %d, want %d", got, before+1)
	}
}

func TestDBColumnsLastColumnStays(t *testing.T) {
	withConfigDir(t)
	m := dbColumnsModel()
	for i := 0; i < 5; i++ {
		m.databasePageKey("H", 0, 0)
	}
	if got := strings.Join(dbHeader(t, m), " "); got != "d" {
		t.Fatalf("the last column was hidden: %q", got)
	}
}

func TestDBColumnsSavedPerTable(t *testing.T) {
	withConfigDir(t)
	m := dbColumnsModel()
	m.databasePageKey("H", 0, 1)
	m.databasePageKey(">", 0, 0)
	m.databasePageKey(")", 0, 0)
	// A new session of the same table reads the layout back.
	m2 := dbColumnsModel()
	if got := strings.Join(dbHeader(t, m2), " "); got != "c a d" {
		t.Fatalf("restored: %q", got)
	}
	// Another table keeps its own layout.
	m3 := dbColumnsModel()
	m3.dbConsole().shown.tv.name = "orders"
	if got := strings.Join(dbHeader(t, m3), " "); got != "a b c d" {
		t.Fatalf("other table: %q", got)
	}
	// A column the table gained shows at the end; a dropped one is skipped.
	m4 := dbColumnsModel()
	s4 := m4.dbConsole()
	s4.shown.result.Columns[3].Name = "e"
	s4.cache = nil
	if got := strings.Join(dbHeader(t, m4), " "); got != "c a e" {
		t.Fatalf("changed table: %q", got)
	}
}

func TestDBColumnsPickerTogglesHidden(t *testing.T) {
	withConfigDir(t)
	m := dbColumnsModel()
	m.databasePageKey("H", 0, 2) // hide c
	m.databasePageKey("O", 0, 0)
	if !m.pickerOpen || m.pickerKind != pickerKindDBColumns {
		t.Fatalf("no columns picker (open %v kind %v)", m.pickerOpen, m.pickerKind)
	}
	if cmd, ok := m.databaseConsolePicker("2"); !ok {
		t.Fatalf("picker not handled (%v)", cmd)
	}
	if got := strings.Join(dbHeader(t, m), " "); got != "a b c d" {
		t.Fatalf("shown again: %q", got)
	}
	if _, ok := m.databaseConsolePicker("0"); !ok {
		t.Fatal("picker not handled")
	}
	if got := strings.Join(dbHeader(t, m), " "); got != "b c d" {
		t.Fatalf("hidden from the picker: %q", got)
	}
}

// The new keys reach Go from the page and are on the key card.
func TestDBColumnsKeysWired(t *testing.T) {
	for _, k := range []string{"'H'", "'O'", "'<'", "'>'", "'F'", "'('", "')'", "'{'", "'}'", "'='"} {
		if !strings.Contains(dbPageLua, k) {
			t.Errorf("Lua does not map %s", k)
		}
	}
	var text []string
	for _, r := range dbPageKeyRows() {
		text = append(text, r.Key+" "+r.What)
	}
	all := strings.Join(text, "\n")
	for _, want := range []string{"hide the column", "move the column", "freeze", "narrower / wider", "column layout"} {
		if !strings.Contains(all, want) {
			t.Errorf("key card misses %q", want)
		}
	}
}
