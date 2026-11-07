package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// SQL Intentions… works on the whole lines of the statement at the
// cursor, with the cursor offset inside them.
func TestDBIntentStatement(t *testing.T) {
	lines := []string{"SELECT 1;", "", "SELECT *", "FROM orders;"}
	start, end, cur, ok := dbIntentStatement(lines, 4, 6, "postgres")
	if !ok || start != 2 || end != 4 {
		t.Fatalf("statement = %d..%d ok=%v", start, end, ok)
	}
	stmt := strings.Join(lines[start:end], "\n")
	if !strings.HasPrefix(stmt[cur:], "rders") {
		t.Fatalf("cursor %d sits on %q", cur, stmt[cur:])
	}
	if _, _, _, ok := dbIntentStatement(nil, 1, 0, ""); ok {
		t.Fatal("empty buffer gave a statement")
	}
}

// The picker lists the intentions of the statement; Expand * appears
// once completion knows the columns.
func TestDBSQLIntentItems(t *testing.T) {
	stmt := "SELECT * FROM orders"
	cur := strings.Index(stmt, "*")
	has := func(cols map[string][]string, id string) bool {
		for _, it := range dbSQLIntentItems(stmt, "postgres", cur, cols) {
			if it.ID == id {
				return true
			}
		}
		return false
	}
	if !has(nil, dbclient.IntentAlias) {
		t.Fatal("no alias intention")
	}
	if has(nil, dbclient.IntentExpandStar) {
		t.Fatal("expand * offered without columns")
	}
	s := &dbState{}
	s.setColumns("pg", "Orders", []string{"id", "total"})
	s.setFullSchema("pg", dbclient.CompletionSchema{Cols: map[string][]dbclient.Column{"users": {{Name: "id"}, {Name: "name"}}}})
	cols := s.knownColumns("pg")
	if strings.Join(cols["orders"], ",") != "id,total" || strings.Join(cols["users"], ",") != "id,name" {
		t.Fatalf("known columns = %v", cols)
	}
	if !has(cols, dbclient.IntentExpandStar) {
		t.Fatal("expand * not offered with columns")
	}
	found := false
	for _, it := range databasesPaletteItems() {
		found = found || it.ID == "db-sql-intentions"
	}
	if !found {
		t.Fatal("db-sql-intentions not in the palette")
	}
	if _, ok := aiActionPolicies["db-sql-intentions"]; !ok {
		t.Fatal("db-sql-intentions has no AI policy")
	}
}
