package app

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Result page conveniences (DBX-26).

// dbTabTitleMax is the most characters a tab name keeps.
const dbTabTitleMax = 24

// dbTabTitle is the tab name from the first "-- comment" line above the
// statement ("-- @conn" lines and empty comments skipped); empty: none.
func dbTabTitle(sql string) string {
	for _, l := range strings.Split(sql, "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "--") {
			return ""
		}
		t = strings.TrimSpace(strings.TrimPrefix(t, "--"))
		if t == "" || strings.HasPrefix(t, "@") {
			continue
		}
		t = dbOneLine(t)
		if r := []rune(t); len(r) > dbTabTitleMax {
			t = string(r[:dbTabTitleMax]) + "…"
		}
		return t
	}
	return ""
}

// dbColumnValues is column col of r, one value per line (each on one line).
func dbColumnValues(r dbclient.Result, col int) string {
	if col < 0 || col >= len(r.Columns) {
		return ""
	}
	vals := make([]string, 0, len(r.Rows))
	for _, row := range r.Rows {
		if col < len(row) {
			vals = append(vals, dbOneLine(row[col].String()))
		}
	}
	return strings.Join(vals, "\n")
}
