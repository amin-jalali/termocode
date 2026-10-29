package dbclient

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// A schema with more tables than the compare holds keeps the first ones
// by name and says it was cut.
func TestReadCanonSchemaKeepsAtMostMaxTables(t *testing.T) {
	var rows [][]Cell
	for i := 0; i < CompareMaxTables+5; i++ {
		rows = append(rows, cells(fmt.Sprintf("t%05d", i), "id", "int", "f", "<null>", ""))
	}
	s := &catalogSession{answers: map[string][][]Cell{"pg_attribute a ON a.attrelid = c.oid": rows}}
	got, err := ReadCanonSchema(context.Background(), s, "postgres", "public")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || len(got.Tables) != CompareMaxTables || got.Tables[0].Name != "t00000" {
		t.Fatalf("truncated=%v tables=%d", got.Truncated, len(got.Tables))
	}
}

// SQLite: no schema means main; temp reads sqlite_temp_master.
func TestReadCanonSchemaSQLiteSchemaNames(t *testing.T) {
	for schema, want := range map[string]string{"": `"main".sqlite_master`, "temp": "sqlite_temp_master"} {
		s := &catalogSession{}
		if _, err := ReadCanonSchema(context.Background(), s, "sqlite", schema); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(s.ran[0], want) {
			t.Errorf("schema %q read %q", schema, s.ran[0])
		}
	}
}

// Server text with tabs and line ends: one clean line for names, kept
// lines for bodies; a NULL body is empty.
func TestCanonCleanTabsAndNull(t *testing.T) {
	if got := canonText(Cell{Text: "a\tb\r\nc"}); got != "a    b c" {
		t.Errorf("text %q", got)
	}
	if got := canonBody(Cell{Null: true}); got != "" {
		t.Errorf("null body %q", got)
	}
	if cellTrue(Cell{Text: "no"}) || !cellTrue(Cell{Text: " YES "}) {
		t.Error("cellTrue")
	}
	if unquoteSQLite("plain") != "plain" || unquoteSQLite(`"a""b"`) != `a"b` {
		t.Error("unquoteSQLite")
	}
}
