package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-43: c on a routine asks for each argument and makes the call; →
// in the Generate code picker inserts the text at the editor cursor.

func TestDBRoutineArgsParse(t *testing.T) {
	got := dbParseRoutineArgs("a integer, b numeric(10,2), OUT total bigint, character varying, VARIADIC xs text[]")
	want := []dbRoutineArg{
		{name: "a", typ: "integer"},
		{name: "b", typ: "numeric(10,2)"},
		{mode: "OUT", name: "total", typ: "bigint"},
		{typ: "character varying"},
		{mode: "VARIADIC", name: "xs", typ: "text[]"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d args %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d: got %+v want %+v", i, got[i], want[i])
		}
	}
	my := dbParseRoutineArgs("IN `id` int, INOUT `n` varchar(20)")
	if len(my) != 2 || my[0] != (dbRoutineArg{mode: "IN", name: "id", typ: "int"}) || my[1] != (dbRoutineArg{mode: "INOUT", name: "n", typ: "varchar(20)"}) {
		t.Errorf("mysql args: %+v", my)
	}
	if len(dbParseRoutineArgs("")) != 0 {
		t.Error("no args should be empty")
	}
}

func TestDBRoutineCallText(t *testing.T) {
	pg := dbRoutineTarget{driver: "postgres", schema: "public", name: "add_user"}
	fn := dbclient.CanonRoutine{Name: "add_user", Kind: "FUNCTION", Args: "name text, age integer, ok boolean"}
	got := dbRoutineCall(pg, fn, []string{"O'Neil", "42", ""})
	want := `SELECT * FROM "public"."add_user"('O''Neil' /* name */, 42 /* age */, NULL /* ok */);`
	if got != want {
		t.Errorf("pg function:\n got %s\nwant %s", got, want)
	}
	proc := dbclient.CanonRoutine{Name: "add_user", Kind: "PROCEDURE", Args: "a integer, OUT b integer"}
	if got := dbRoutineCall(pg, proc, []string{"1"}); got != `CALL "public"."add_user"(1 /* a */, NULL /* b */);` {
		t.Errorf("pg procedure: %s", got)
	}
	my := dbRoutineTarget{driver: "mysql", schema: "shop", name: "p"}
	mp := dbclient.CanonRoutine{Name: "p", Kind: "PROCEDURE", Args: "IN `id` int, OUT `total` int"}
	if got := dbRoutineCall(my, mp, []string{"7"}); got != "CALL `shop`.`p`(7 /* id */, @total);" {
		t.Errorf("mysql procedure: %s", got)
	}
	// a name that closes the comment cannot end it early
	odd := dbclient.CanonRoutine{Name: "f", Kind: "FUNCTION", Args: `"x*/y" text`}
	if got := dbRoutineCall(pg, odd, []string{"NULL"}); strings.Count(got, "*/") != 1 {
		t.Errorf("comment escape: %s", got)
	}
}

func TestDBRoutineAskedArgs(t *testing.T) {
	r := dbclient.CanonRoutine{Kind: "PROCEDURE", Args: "IN a int, OUT b int, INOUT c int"}
	got := dbRoutineInputs(dbParseRoutineArgs(r.Args))
	if len(got) != 2 || got[0].name != "a" || got[1].name != "c" {
		t.Errorf("asked: %+v", got)
	}
}

func TestDBRoutineDropText(t *testing.T) {
	pg := dbRoutineTarget{driver: "postgres", schema: "public", name: "f"}
	if got := dbRoutineDrop(pg, dbclient.CanonRoutine{Kind: "FUNCTION", Args: "a integer, b text"}); got != `DROP FUNCTION "public"."f"(a integer, b text);` {
		t.Errorf("pg drop: %s", got)
	}
	my := dbRoutineTarget{driver: "mysql", schema: "shop", name: "p"}
	if got := dbRoutineDrop(my, dbclient.CanonRoutine{Kind: "PROCEDURE", Args: "IN `id` int"}); got != "DROP PROCEDURE `shop`.`p`;" {
		t.Errorf("mysql drop: %s", got)
	}
}

func TestDBGenerateRoutineItems(t *testing.T) {
	items := dbRoutineGenerateItems()
	if len(items) != 2 || items[0].ID != "call" || items[1].ID != "rdrop" {
		t.Errorf("routine items: %+v", items)
	}
}
