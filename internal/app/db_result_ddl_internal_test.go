package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-26: column → DDL quotes the name in the connection's dialect and
// keeps the reported type; an unknown type falls back to text.
func TestDBColumnDDL(t *testing.T) {
	cols := []dbclient.Column{{Name: "id", Type: "INT4"}, {Name: `we"ird`}}
	d := dbclient.DialectFor("postgres")
	if got := dbColumnDDL(d, cols[0]); got != `"id" INT4` {
		t.Fatalf("column ddl %q", got)
	}
	if got := dbColumnDDL(d, cols[1]); got != `"we""ird" text` {
		t.Fatalf("unknown type ddl %q", got)
	}
	all := dbResultDDL(d, "", cols)
	if !strings.HasPrefix(all, `CREATE TABLE "result" (`) || !strings.Contains(all, "\n  \"id\" INT4,\n") || !strings.HasSuffix(all, ");") {
		t.Fatalf("create table %q", all)
	}
	if got := dbResultDDL(nil, "", cols); !strings.Contains(got, "id INT4") {
		t.Fatalf("no dialect %q", got)
	}
}

// DBX-26: the inline result is a short grid with a "more rows" line.
func TestDBInlineLines(t *testing.T) {
	r := dbclient.Result{Columns: []dbclient.Column{{Name: "id"}, {Name: "name"}}}
	for i := 0; i < 8; i++ {
		r.Rows = append(r.Rows, []dbclient.Cell{{Text: "1"}, {Text: "x\x1b[31m"}})
	}
	lines := dbInlineLines(r, 80)
	if len(lines) != 2+dbInlineRows+1 {
		t.Fatalf("lines %q", lines)
	}
	if !strings.Contains(lines[len(lines)-1], "3 more rows") {
		t.Fatalf("last line %q", lines[len(lines)-1])
	}
	for _, l := range lines {
		if strings.Contains(l, "\x1b") {
			t.Fatalf("escape kept in %q", l)
		}
	}
	if got := dbInlineLines(dbclient.Result{}, 80); got != nil {
		t.Fatalf("no columns: %q", got)
	}
}
