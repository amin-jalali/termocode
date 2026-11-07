package dbclient

import (
	"reflect"
	"strings"
	"testing"
)

// at is the byte offset of the n-th (0-based) occurrence of mark in s.
func at(t *testing.T, s, mark string, n int) int {
	t.Helper()
	off := -1
	for i := 0; i <= n; i++ {
		j := strings.Index(s[off+1:], mark)
		if j < 0 {
			t.Fatalf("mark %q #%d not in %q", mark, n, s)
		}
		off += j + 1
	}
	return off
}

func TestSQLRefAtResolvesTablesColumnsAndAliases(t *testing.T) {
	const q = "SELECT u.name, total FROM public.users u JOIN orders AS o ON o.user_id = u.id WHERE u.id = 1"
	cases := []struct {
		name string
		off  int
		want SQLRef
	}{
		{"alias column", at(t, q, "name", 0), SQLRef{Kind: "column", Table: "users", Schema: "public", Column: "name"}},
		{"alias itself", at(t, q, "u.", 0), SQLRef{Kind: "table", Schema: "public", Table: "users"}},
		{"table in FROM", at(t, q, "users", 0), SQLRef{Kind: "table", Schema: "public", Table: "users"}},
		{"schema part", at(t, q, "public", 0) + 2, SQLRef{Kind: "table", Schema: "public", Table: "users"}},
		{"table in JOIN", at(t, q, "orders", 0) + 3, SQLRef{Kind: "table", Table: "orders"}},
		{"second alias column", at(t, q, "user_id", 0), SQLRef{Kind: "column", Table: "orders", Column: "user_id"}},
		{"bare column, many tables", at(t, q, "total", 0), SQLRef{Kind: "column", Column: "total"}},
		{"end of word", at(t, q, "name", 0) + 4, SQLRef{Kind: "column", Table: "users", Schema: "public", Column: "name"}},
	}
	for _, c := range cases {
		got, ok := SQLRefAt(q, c.off, "postgres")
		if !ok || got != c.want {
			t.Errorf("%s: SQLRefAt(%d) = %+v, %v; want %+v", c.name, c.off, got, ok, c.want)
		}
	}
}

func TestSQLRefAtBareColumnWithOneTable(t *testing.T) {
	const q = "select id, email from accounts where email like 'a%'"
	got, ok := SQLRefAt(q, at(t, q, "email", 1)+1, "postgres")
	want := SQLRef{Kind: "column", Table: "accounts", Column: "email"}
	if !ok || got != want {
		t.Fatalf("got %+v, %v; want %+v", got, ok, want)
	}
}

func TestSQLRefAtRoutineCall(t *testing.T) {
	const q = "SELECT count(*), my_func(a) FROM t"
	got, ok := SQLRefAt(q, at(t, q, "my_func", 0)+2, "postgres")
	want := SQLRef{Kind: "routine", Routine: "my_func"}
	if !ok || got != want {
		t.Fatalf("got %+v, %v; want %+v", got, ok, want)
	}
}

func TestSQLRefAtQuotedAndMySQL(t *testing.T) {
	const q = "UPDATE `Shop`.`Items` i SET i.`Price` = 2"
	got, ok := SQLRefAt(q, at(t, q, "Price", 0), "mysql")
	want := SQLRef{Kind: "column", Schema: "Shop", Table: "Items", Column: "Price"}
	if !ok || got != want {
		t.Fatalf("got %+v, %v; want %+v", got, ok, want)
	}
}

func TestSQLRefAtNothingUseful(t *testing.T) {
	const q = "SELECT 'users' FROM t -- users\n WHERE x = 10"
	for _, off := range []int{
		at(t, q, "SELECT", 0) + 1, // keyword
		at(t, q, "users", 0),      // inside a string
		at(t, q, "users", 1),      // inside a comment
		at(t, q, "10", 0),         // a number
		at(t, q, "=", 0),          // punctuation
		-1, len(q) + 5,            // out of range
	} {
		if got, ok := SQLRefAt(q, off, "postgres"); ok {
			t.Errorf("SQLRefAt(%d) = %+v, want none", off, got)
		}
	}
}

func TestInsertValueHintsNameTheColumnOfEachValue(t *testing.T) {
	const q = "INSERT INTO users (id, name, email) VALUES (1, 'Ann', lower('A@X')), (2, 'Bo, Jr', NULL)"
	got := InsertValueHints(q, "postgres")
	want := []InsertHint{
		{Offset: at(t, q, "1,", 0), Column: "id"},
		{Offset: at(t, q, "'Ann'", 0), Column: "name"},
		{Offset: at(t, q, "lower", 0), Column: "email"},
		{Offset: at(t, q, "2,", 0), Column: "id"},
		{Offset: at(t, q, "'Bo", 0), Column: "name"},
		{Offset: at(t, q, "NULL", 0), Column: "email"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestInsertValueHintsExtraValuesAndNoColumnList(t *testing.T) {
	const q = "insert into t (a) values (1, 2)"
	got := InsertValueHints(q, "sqlite")
	want := []InsertHint{{Offset: at(t, q, "1", 0), Column: "a"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for _, s := range []string{"INSERT INTO t VALUES (1, 2)", "SELECT 1", "INSERT INTO t (a, b) SELECT 1, 2", ""} {
		if h := InsertValueHints(s, "postgres"); len(h) != 0 {
			t.Errorf("InsertValueHints(%q) = %+v, want none", s, h)
		}
	}
}
