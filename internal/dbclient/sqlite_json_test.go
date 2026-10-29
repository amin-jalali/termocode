package dbclient

import (
	"strings"
	"testing"
)

// sqlite3 output that stops before the closing "]" (the process died
// mid-result) is an error, not a shorter result that looks complete.
func TestParseSQLiteJSONTruncatedOutputIsAnError(t *testing.T) {
	for _, lines := range [][]string{
		{`[{"a":1},`, `{"a":2}`},
		{`[{"a":1}`},
		{`[`, `}`},
	} {
		rows, _, err := parseSQLiteJSON(lines)
		if err == nil || !strings.Contains(err.Error(), "ended early") {
			t.Errorf("%q: rows %v, err %v", lines, rows, err)
		}
	}
	rows, _, err := parseSQLiteJSON([]string{`[{"a":1},`, `{"a":2}]`})
	if err != nil || len(rows) != 2 {
		t.Fatalf("complete output: %v %v", rows, err)
	}
}
