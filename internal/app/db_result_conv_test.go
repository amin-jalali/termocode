package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-26: a tab is named by the "-- comment" line above its statement.
func TestDBTabStripNamesTabsFromComment(t *testing.T) {
	s := &dbConsoleState{sortCol: -1}
	s.placeRun(&dbRun{sql: "-- @conn pg\n-- Open orders\nSELECT * FROM orders"})
	s.curTab().pinned = true
	s.placeRun(&dbRun{sql: "SELECT 1"})
	if got, want := s.tabStrip(), "1 Open orders pinned [2]"; got != want {
		t.Fatalf("tabs = %q, want %q", got, want)
	}
	for sql, want := range map[string]string{
		"SELECT 1":                   "",
		"-- @conn pg\nSELECT 1":      "",
		"--   Top users  \nSELECT 1": "Top users",
		"--\n-- Second\nSELECT 1":    "Second",
		"-- a very long title that goes on and on\nSELECT 1": "a very long title that g…",
	} {
		if got := dbTabTitle(sql); got != want {
			t.Errorf("dbTabTitle(%q) = %q, want %q", sql, got, want)
		}
	}
}

// DBX-26: Y copies the column under the cursor, one value per line.
func TestDBColumnValuesOnePerLine(t *testing.T) {
	r := dbclient.Result{
		Columns: []dbclient.Column{{Name: "id"}, {Name: "name"}},
		Rows: [][]dbclient.Cell{
			{{Text: "1"}, {Text: "ann"}},
			{{Text: "2"}, {Null: true}},
			{{Text: "3"}, {Text: "a\nb"}},
		},
	}
	if got, want := dbColumnValues(r, 1), "ann\n<null>\na b"; got != want {
		t.Fatalf("column 1 = %q, want %q", got, want)
	}
	if got := dbColumnValues(r, 5); got != "" {
		t.Fatalf("no column = %q", got)
	}
}
