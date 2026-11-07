package app

import (
	"reflect"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-55: an array cell opens as one element per line and goes back as an
// array literal; NULL, empty text and spaces survive the round trip.
func TestDBArrayLinesRoundTrip(t *testing.T) {
	cur := dbclient.Cell{Text: `{a,"b c",NULL,"","NULL","x\"y"}`}
	lines, ok := dbArrayLines("text[]", cur, true)
	if !ok {
		t.Fatal("an array was not opened as lines")
	}
	want := []string{"a", "b c", `\N`, `""`, `"NULL"`, `x"y`}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines %q, want %q", lines, want)
	}
	if got := dbArrayLiteral(append(lines, "", "  ")); got != `{a,"b c",NULL,"","NULL","x\"y"}` {
		t.Fatalf("literal %s", got)
	}
	if _, ok := dbArrayLines("_int4", dbclient.Cell{Text: "{{1,2},{3,4}}"}, true); ok {
		t.Fatal("an array in an array opened as lines")
	}
	if _, ok := dbArrayLines("text", cur, true); ok {
		t.Fatal("a text column opened as an array")
	}
	if _, ok := dbArrayLines("int[]", dbclient.Cell{Null: true}, true); ok {
		t.Fatal("a NULL array opened as lines")
	}
	if lines, ok := dbArrayLines("int[]", dbclient.Cell{Text: "{}"}, true); !ok || dbArrayLiteral(lines) != "{}" {
		t.Fatalf("empty array: %q %v", lines, ok)
	}
}

// DBX-55: each element of an array is checked against the element type.
func TestDBCheckCellTypeArrayElements(t *testing.T) {
	for typ, v := range map[string]string{"integer[]": "{1,NULL,3}", "_bool": "{t,f}", "uuid[]": "{}"} {
		if err := dbCheckCellType(typ, v); err != nil {
			t.Errorf("%s %q refused: %v", typ, v, err)
		}
	}
	for typ, v := range map[string]string{"integer[]": "{1,two}", "_int4": "1,2", "date[]": `{"07/11/2026"}`} {
		if err := dbCheckCellType(typ, v); err == nil {
			t.Errorf("%s %q accepted", typ, v)
		}
	}
}

// DBX-55: Tab completes from the column's loaded values and cycles.
func TestDBCellCompletionCycles(t *testing.T) {
	tab := &dbTab{run: &dbRun{result: dbclient.Result{
		Columns: []dbclient.Column{{Name: "status", Type: "text"}, {Name: "ok", Type: "boolean"}},
		Rows: [][]dbclient.Cell{
			{{Text: "shipped"}, {Text: "t"}},
			{{Text: "shipping"}, {Null: true}},
			{{Text: "open"}, {Text: "f"}},
			{{Text: "shipped"}, {Text: "t"}},
		},
	}}}
	cands := dbCellCompletions(tab, 0)
	if !reflect.DeepEqual(cands, []string{"open", "shipped", "shipping"}) {
		t.Fatalf("cands %q", cands)
	}
	if got := dbCellCompletions(tab, 1); len(got) < 2 || got[0] != "true" || got[1] != "false" {
		t.Fatalf("bool cands %q", got)
	}
	c := dbCycleCompleter(cands)
	if got := c("o"); got != "open" {
		t.Fatalf("o -> %q", got)
	}
	if got := c("sh"); got != "shipp" {
		t.Fatalf("sh -> %q (the common start)", got)
	}
	first := c("shipp")
	second := c(first)
	if first != "shipped" || second != "shipping" || c(second) != "shipped" {
		t.Fatalf("cycle %q %q", first, second)
	}
	if got := c("zzz"); got != "zzz" {
		t.Fatalf("no match changed the value: %q", got)
	}
}

// DBX-55: in a JOIN result the first table whose key is clearly its own
// is the one whose cells change; columns of the other tables stay
// read-only, and a column both tables have needs its table name.
func TestDBJoinEditTarget(t *testing.T) {
	jt, why := dbclient.JoinSelect(`SELECT u.id, u.name, o.status FROM users u JOIN orders o ON o.user_id = u.id`, "postgres")
	if why != "" {
		t.Fatal(why)
	}
	irs := []dbclient.IntrospectionResult{
		{Columns: []dbclient.Column{{Name: "id", PK: true}, {Name: "name"}}},
		{Columns: []dbclient.Column{{Name: "id", PK: true}, {Name: "user_id"}, {Name: "status"}}},
	}
	cols := []dbclient.Column{{Name: "id"}, {Name: "name"}, {Name: "status"}}
	tg := dbJoinEditTarget(cols, jt, irs, "c", "postgres", "c")
	if tg.why != "" || tg.table != "users" || !tg.join || !reflect.DeepEqual(tg.key, []int{0}) {
		t.Fatalf("target %+v", tg)
	}
	if !tg.editable[0] || !tg.editable[1] || tg.editable[2] {
		t.Fatalf("editable %v: status belongs to orders", tg.editable)
	}
	// SELECT *: id is in both tables, so neither key is clear.
	star, _ := dbclient.JoinSelect(`SELECT * FROM users u JOIN orders o ON o.user_id = u.id`, "postgres")
	cols = []dbclient.Column{{Name: "id"}, {Name: "name"}, {Name: "user_id"}, {Name: "status"}}
	if tg := dbJoinEditTarget(cols, star, irs, "c", "postgres", "c"); tg.why == "" {
		t.Fatalf("an ambiguous key was accepted: %+v", tg)
	}
}

// DBX-55: a loaded file becomes bytes in a binary column and text in the
// others; bytes that are not text do not go into a text column.
func TestDBLOBCell(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nrest")
	c, ok := dbLOBCell("bytea", dbclient.Cell{Null: true}, png)
	if b, isBin := dbclient.CellBytes(c); !ok || !isBin || string(b) != string(png) {
		t.Fatalf("bytea cell %+v", c)
	}
	if c, ok := dbLOBCell("text", dbclient.Cell{}, []byte("hello\nworld")); !ok || c.Text != "hello\nworld" || c.Kind == "binary" {
		t.Fatalf("text cell %+v", c)
	}
	if _, ok := dbLOBCell("text", dbclient.Cell{}, []byte{0xff, 0xfe}); ok {
		t.Fatal("bytes that are not text went into a text column")
	}
	if got := dbLOBFileName("users", "avatar", dbclient.CellOfBytes(png)); got != "users-avatar.png" {
		t.Fatalf("file name %q", got)
	}
	if got := dbLOBFileName("", "note", dbclient.Cell{Text: "x"}); got != "note.txt" {
		t.Fatalf("file name %q", got)
	}
}
