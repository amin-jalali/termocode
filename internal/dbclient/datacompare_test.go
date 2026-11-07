package dbclient

import (
	"reflect"
	"strings"
	"testing"
)

func dcTxt(s string) Cell { return Cell{Text: s} }
func dcNum(s string) Cell { return Cell{Text: s, Kind: "number"} }

var dcNull = Cell{Null: true}

func dcCols(names ...string) []Column {
	out := make([]Column, len(names))
	for i, n := range names {
		out[i] = Column{Name: n}
	}
	return out
}

func TestCompareDataFindsAddedRemovedChanged(t *testing.T) {
	src := Result{Columns: dcCols("id", "name", "email", "src_only"), Rows: [][]Cell{
		{dcNum("1"), dcTxt("Ada"), dcTxt("ada@x"), dcTxt("s")},
		{dcNum("2"), dcTxt("Bob"), dcTxt("bob@new"), dcTxt("s")},
		{dcNum("4"), dcTxt("Dan"), dcNull, dcTxt("s")},
		{dcNum("5"), dcTxt("Eve"), dcNull, dcTxt("s")},
	}}
	tgt := Result{Columns: dcCols("ID", "email", "name", "tgt_only"), Rows: [][]Cell{
		{dcNum("1"), dcTxt("ada@x"), dcTxt("Ada"), dcTxt("t")},
		{dcNum("2"), dcTxt("bob@old"), dcTxt("Bob"), dcTxt("t")},
		{dcNum("3"), dcTxt("cy@x"), dcTxt("Cy"), dcTxt("t")},
		{dcNum("5"), dcTxt(""), dcTxt("Eve"), dcTxt("t")},
	}}
	c, err := CompareData(src, tgt, []string{"id"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Columns, []string{"id", "name", "email"}) || !reflect.DeepEqual(c.Key, []string{"id"}) {
		t.Fatalf("columns %v key %v", c.Columns, c.Key)
	}
	if !reflect.DeepEqual(c.OnlySource, []string{"src_only"}) || !reflect.DeepEqual(c.OnlyTarget, []string{"tgt_only"}) {
		t.Errorf("only: %v / %v", c.OnlySource, c.OnlyTarget)
	}
	if c.Same != 1 || c.Added != 1 || c.Removed != 1 || c.Changed != 2 {
		t.Fatalf("counts same %d added %d removed %d changed %d", c.Same, c.Added, c.Removed, c.Changed)
	}
	var got []string
	for _, d := range c.Diffs {
		got = append(got, d.Kind.String()+":"+d.Key[0].Text+":"+strings.Join(d.ChangedNames(c), ","))
	}
	want := []string{"changed:2:email", "added:4:", "changed:5:email", "removed:3:"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("diffs = %v, want %v", got, want)
	}
	// Target cells come back in the compared column order.
	if d := c.Diffs[0]; d.Target[2].Text != "bob@old" || d.Source[2].Text != "bob@new" {
		t.Errorf("changed row cells: %+v", d)
	}
}

func TestCompareDataKeyFromResultAndErrors(t *testing.T) {
	src := Result{Columns: []Column{{Name: "k", PK: true}, {Name: "v"}}, Rows: [][]Cell{{dcTxt("a"), dcTxt("1")}}}
	tgt := Result{Columns: dcCols("k", "v"), Rows: [][]Cell{{dcTxt("a"), dcTxt("1")}}}
	c, err := CompareData(src, tgt, nil)
	if err != nil || !reflect.DeepEqual(c.Key, []string{"k"}) || c.Same != 1 || len(c.Diffs) != 0 {
		t.Fatalf("pk key: %+v %v", c, err)
	}
	if _, err := CompareData(Result{Columns: dcCols("k", "v")}, tgt, nil); err == nil || !strings.Contains(err.Error(), "key") {
		t.Errorf("no key: %v", err)
	}
	if _, err := CompareData(src, tgt, []string{"nope"}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("unknown key: %v", err)
	}
	if _, err := CompareData(src, Result{Columns: dcCols("v")}, []string{"k"}); err == nil {
		t.Error("key missing in target: no error")
	}
	dup := Result{Columns: dcCols("k", "v"), Rows: [][]Cell{{dcTxt("a"), dcTxt("1")}, {dcTxt("a"), dcTxt("2")}}}
	c, err = CompareData(dup, tgt, []string{"k"})
	if err != nil || c.DupKeys != 1 {
		t.Errorf("dup keys %d %v", c.DupKeys, err)
	}
	// NULL keys never match each other (like SQL).
	nk := Result{Columns: dcCols("k", "v"), Rows: [][]Cell{{dcNull, dcTxt("1")}}}
	c, _ = CompareData(nk, nk, []string{"k"})
	if c.NullKeys != 2 || len(c.Diffs) != 0 {
		t.Errorf("null keys %d diffs %d", c.NullKeys, len(c.Diffs))
	}
}

func TestDataSyncScriptMakesTargetMatchSource(t *testing.T) {
	src := Result{Columns: dcCols("id", "name", "note"), Rows: [][]Cell{
		{dcNum("1"), dcTxt("O'Hara"), dcNull},
		{dcNum("2"), dcTxt("Bob"), dcTxt("new")},
	}}
	tgt := Result{Columns: dcCols("id", "name", "note"), Rows: [][]Cell{
		{dcNum("2"), dcTxt("Bob"), dcTxt("old")},
		{dcNum("3"), dcTxt("Cy"), dcNull},
	}}
	c, err := CompareData(src, tgt, []string{"id"})
	if err != nil {
		t.Fatal(err)
	}
	got := c.SyncScript(PostgresDialect{}, "public", "people")
	want := `-- Data sync: makes "public"."people" match the source.
-- 1 to delete, 1 to update, 1 to insert. Review before you run it.
BEGIN;
DELETE FROM "public"."people" WHERE "id" = 3;
UPDATE "public"."people" SET "note" = 'new' WHERE "id" = 2;
INSERT INTO "public"."people" ("id", "name", "note") VALUES (1, 'O''Hara', NULL);
COMMIT;
`
	if got != want {
		t.Errorf("script:\n%s\nwant:\n%s", got, want)
	}
	my := c.SyncScript(MySQLDialect{}, "", "people")
	for _, s := range []string{"START TRANSACTION;", "DELETE FROM `people` WHERE `id` = 3;", "COMMIT;"} {
		if !strings.Contains(my, s) {
			t.Errorf("mysql script lacks %q:\n%s", s, my)
		}
	}
	same, _ := CompareData(src, src, []string{"id"})
	if s := same.SyncScript(SQLiteDialect{}, "", "t"); !strings.Contains(s, "-- No differences.") || strings.Contains(s, "BEGIN") {
		t.Errorf("no-diff script:\n%s", s)
	}
}

func TestDataSyncScriptCompositeKey(t *testing.T) {
	src := Result{Columns: dcCols("a", "b", "v"), Rows: [][]Cell{{dcTxt("x"), dcTxt("y"), dcTxt("2")}}}
	tgt := Result{Columns: dcCols("a", "b", "v"), Rows: [][]Cell{{dcTxt("x"), dcTxt("y"), dcTxt("1")}}}
	c, _ := CompareData(src, tgt, []string{"a", "b"})
	s := c.SyncScript(SQLiteDialect{}, "", "t")
	if !strings.Contains(s, `UPDATE "t" SET "v" = '2' WHERE "a" = 'x' AND "b" = 'y';`) {
		t.Errorf("composite key script:\n%s", s)
	}
}
