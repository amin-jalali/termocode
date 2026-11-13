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

// The last visual selection counts when the cursor stands at one of its
// ends: it becomes byte offsets inside the statement's lines.
func TestDBIntentSelection(t *testing.T) {
	lines := []string{"SELECT 1;", "SELECT name, price * qty", "FROM items;"}
	// statement lines [1, 3); selection "price * qty" on line 2 (cols 13..23)
	s, e, ok := dbIntentSelRange(lines, 1, 3, [2]int{2, 13}, [2]int{2, 23}, false)
	if !ok || strings.Join(lines[1:3], "\n")[s:e] != "price * qty" {
		t.Fatalf("selection = %d..%d ok=%v", s, e, ok)
	}
	// linewise: whole lines
	s, e, ok = dbIntentSelRange(lines, 1, 3, [2]int{2, 0}, [2]int{3, 2147483647}, true)
	if !ok || s != 0 || e != len("SELECT name, price * qty\nFROM items;") {
		t.Fatalf("linewise = %d..%d ok=%v", s, e, ok)
	}
	// a multi-byte last character is taken whole
	u := []string{"SELECT 'é'"}
	s, e, ok = dbIntentSelRange(u, 0, 1, [2]int{1, 7}, [2]int{1, 8}, false)
	if !ok || u[0][s:e] != "'é" {
		t.Fatalf("utf8 = %q ok=%v", u[0][s:e], ok)
	}
	// outside the statement: none
	if _, _, ok := dbIntentSelRange(lines, 1, 3, [2]int{1, 0}, [2]int{2, 3}, false); ok {
		t.Fatal("selection outside the statement taken")
	}
}

// The column types completion knows feed the table function intention.
func TestDBKnownColumnTypes(t *testing.T) {
	s := &dbState{}
	s.setFullSchema("pg", dbclient.CompletionSchema{Cols: map[string][]dbclient.Column{"orders": {{Name: "id", Type: "bigint"}, {Name: "Total", Type: "numeric"}, {Name: "x"}}}})
	got := s.knownColumnTypes("pg")
	if got["orders"]["id"] != "bigint" || got["orders"]["total"] != "numeric" {
		t.Fatalf("types = %v", got)
	}
	if _, ok := got["orders"]["x"]; ok {
		t.Fatal("a column without a type was given one")
	}
}
