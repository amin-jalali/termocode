package app

import (
	"slices"
	"strings"
	"testing"
)

func TestDBFormatRangeStatementAtCursor(t *testing.T) {
	lines := []string{"select 1;", "", "select a, b", "from t where x=1;", "select 3;"}
	start, end, repl, err := dbFormatRange(lines, 4, 0, 0, "postgres", false)
	if err != nil {
		t.Fatal(err)
	}
	if start != 2 || end != 4 {
		t.Fatalf("range %d..%d, want 2..4", start, end)
	}
	want := []string{"SELECT", "  a,", "  b", "FROM t", "WHERE x = 1;"}
	if !slices.Equal(repl, want) {
		t.Fatalf("got %q", repl)
	}
	// A blank line between statements formats the next one, as Run does.
	start, end, _, err = dbFormatRange(lines, 2, 0, 0, "postgres", false)
	if err != nil || start != 2 || end != 4 {
		t.Fatalf("blank line: %d..%d %v", start, end, err)
	}
}

func TestDBFormatRangeSelectionAndFile(t *testing.T) {
	lines := []string{"select a,b from t;", "select 1 from u"}
	start, end, repl, err := dbFormatRange(lines, 1, 2, 2, "sqlite", false)
	if err != nil || start != 1 || end != 2 || !slices.Equal(repl, []string{"SELECT 1", "FROM u"}) {
		t.Fatalf("selection: %d..%d %q %v", start, end, repl, err)
	}
	start, end, repl, err = dbFormatRange(lines, 1, 0, 0, "sqlite", true)
	if err != nil || start != 0 || end != 2 {
		t.Fatalf("file: %d..%d %v", start, end, err)
	}
	if got := strings.Join(repl, "\n"); got != "SELECT\n  a,\n  b\nFROM t;\n\nSELECT 1\nFROM u" {
		t.Fatalf("file: %q", got)
	}
}

func TestDBFormatRangeRefusesOpenQuote(t *testing.T) {
	if _, _, _, err := dbFormatRange([]string{"select 'open"}, 1, 0, 0, "postgres", true); err == nil {
		t.Fatal("want an error: the buffer must stay as it is")
	}
	if _, _, _, err := dbFormatRange([]string{"", "  "}, 1, 0, 0, "postgres", false); err == nil {
		t.Fatal("no SQL at the cursor: want an error")
	}
}
