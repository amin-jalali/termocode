package dbclient

import (
	"reflect"
	"testing"
)

func TestSQLCallAtFindsRoutineAndArgument(t *testing.T) {
	const q = "SELECT coalesce(a, substr(name, 2, 3), 'x'), sales.total_of(id) FROM t WHERE id IN (1, 2)"
	cases := []struct {
		name string
		off  int
		want SQLCall
		ok   bool
	}{
		{"first argument", at(t, q, "a,", 0), SQLCall{Routine: "coalesce", Arg: 0}, true},
		{"nested call", at(t, q, "2,", 0), SQLCall{Routine: "substr", Arg: 1}, true},
		{"after nested call", at(t, q, "'x'", 0), SQLCall{Routine: "coalesce", Arg: 2}, true},
		{"right after the paren", at(t, q, "(a", 0) + 1, SQLCall{Routine: "coalesce", Arg: 0}, true},
		{"schema routine", at(t, q, "id)", 0), SQLCall{Schema: "sales", Routine: "total_of", Arg: 0}, true},
		{"IN list is not a call", at(t, q, "2)", 0), SQLCall{}, false},
		{"outside any call", at(t, q, "FROM", 0), SQLCall{}, false},
	}
	for _, c := range cases {
		got, ok := SQLCallAt(q, c.off, "postgres")
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got %+v %v, want %+v %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestSQLCallAtSkipsTableColumnLists(t *testing.T) {
	for _, q := range []string{
		"INSERT INTO t (a, b) VALUES (1, 2)",
		"CREATE TABLE t (a int, b int)",
		"ALTER TABLE t ADD FOREIGN KEY (a) REFERENCES u (id)",
	} {
		if got, ok := SQLCallAt(q, len(q)-2, "postgres"); ok {
			t.Errorf("%q: a call %+v", q, got)
		}
	}
	// A plain paren inside the arguments counts as part of one argument.
	q := "SELECT round((a + b) * 2, 1)"
	if got, ok := SQLCallAt(q, at(t, q, "1)", 0), "postgres"); !ok || got.Routine != "round" || got.Arg != 1 {
		t.Fatalf("round: %+v %v", got, ok)
	}
}

func TestJoinHintsReadsOnAndUsing(t *testing.T) {
	const q = "SELECT * FROM orders o JOIN public.users u ON u.id = o.user_id LEFT JOIN items i USING (order_id) JOIN tags ON tags.a = 1"
	got := JoinHints(q, "postgres")
	want := []JoinHint{
		{Offset: at(t, q, "public", 0), Joined: JoinSide{Schema: "public", Table: "users", Columns: []string{"id"}}, Other: JoinSide{Table: "orders", Columns: []string{"user_id"}}},
		{Offset: at(t, q, "items", 0), Joined: JoinSide{Table: "items", Columns: []string{"order_id"}}, Other: JoinSide{Schema: "public", Table: "users", Columns: []string{"order_id"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestJoinHintsCompositeAndMixedTables(t *testing.T) {
	q := "SELECT * FROM a JOIN b ON b.x = a.x AND a.y = b.y"
	got := JoinHints(q, "postgres")
	if len(got) != 1 || !reflect.DeepEqual(got[0].Joined.Columns, []string{"x", "y"}) || !reflect.DeepEqual(got[0].Other.Columns, []string{"x", "y"}) {
		t.Fatalf("composite: %+v", got)
	}
	// Two other tables in one ON, or an OR: no hint.
	for _, q := range []string{
		"SELECT * FROM a JOIN c ON 1=1 JOIN b ON b.x = a.x AND b.y = c.y",
		"SELECT * FROM a JOIN b ON b.x = a.x OR b.y = a.y",
	} {
		for _, h := range JoinHints(q, "postgres") {
			if h.Joined.Table == "b" {
				t.Errorf("%q: hint %+v", q, h)
			}
		}
	}
}

func TestBuiltinParams(t *testing.T) {
	p, ok := BuiltinParams("postgres", "SUBSTR")
	if !ok || !reflect.DeepEqual(p, []string{"string", "start", "length"}) {
		t.Fatalf("substr: %v %v", p, ok)
	}
	if p, ok := BuiltinParams("mysql", "ifnull"); !ok || len(p) != 2 {
		t.Fatalf("ifnull: %v %v", p, ok)
	}
	if _, ok := BuiltinParams("postgres", "no_such_fn"); ok {
		t.Fatal("unknown function found")
	}
}
