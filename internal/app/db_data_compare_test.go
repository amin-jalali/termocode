package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func dbCmpResult(rows ...[2]string) dbclient.Result {
	r := dbclient.Result{Columns: []dbclient.Column{{Name: "id", PK: true}, {Name: "name"}}}
	for _, row := range rows {
		r.Rows = append(r.Rows, []dbclient.Cell{{Text: row[0], Kind: "number"}, {Text: row[1]}})
	}
	return r
}

func TestDBDropColumnKeepsTheKey(t *testing.T) {
	r := dbclient.Result{Columns: []dbclient.Column{{Name: "a"}, {Name: dbRowidAlias}, {Name: "b"}}, Key: []int{0, 1, 2},
		Rows: [][]dbclient.Cell{{{Text: "1"}, {Text: "9"}, {Text: "x"}}}}
	got := dbDropColumn(r, dbRowidAlias)
	if len(got.Columns) != 2 || got.Columns[1].Name != "b" || len(got.Rows[0]) != 2 || got.Rows[0][1].Text != "x" {
		t.Fatalf("got %+v", got)
	}
	if len(got.Key) != 2 || got.Key[0] != 0 || got.Key[1] != 1 {
		t.Fatalf("key %v", got.Key)
	}
	if len(r.Rows[0]) != 3 {
		t.Fatal("the input changed")
	}
}

func TestDBDataDiffText(t *testing.T) {
	cmp, err := dbclient.CompareData(dbCmpResult([2]string{"1", "a"}, [2]string{"2", "b"}, [2]string{"3", "c"}),
		dbCmpResult([2]string{"1", "a"}, [2]string{"2", "B"}, [2]string{"4", "d"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	old, nw := dbDataDiffText(cmp)
	if old != "id=2 | name=B\nid=4 | name=d\n" {
		t.Fatalf("old %q", old)
	}
	if nw != "id=2 | name=b\nid=3 | name=c\n" {
		t.Fatalf("new %q", nw)
	}
}

func TestDBDataCompareMarkThenCompare(t *testing.T) {
	withConfigDir(t)
	m := dbGridModel(dbCmpResult([2]string{"1", "a"}))
	s := m.dbConsole()
	src := s.shown
	m.databasePageKey("gc", 0, 0)
	if s.dataCmp.mark != src {
		t.Fatal("gc did not mark the source")
	}
	m.databasePageKey("gc", 0, 0)
	if s.dataCmp.mark != nil {
		t.Fatal("gc again did not unmark")
	}
	m.databasePageKey("gc", 0, 0)
	tgt := &dbRun{conn: "other", sql: "SELECT 2", by: "user", result: dbCmpResult([2]string{"1", "z"})}
	s.placeRun(tgt)
	if s.shown != tgt {
		t.Fatal("the target is not on the page")
	}
	if cmd := m.databasePageKey("gc", 0, 0); cmd == nil {
		t.Fatal("no compare started")
	}
	if s.dataCmp.mark != nil {
		t.Fatal("the mark stayed")
	}
	// The read finished: a console target has no sync script.
	gen := s.dataCmp.gen
	m.onDBDataCompare(gen, src, tgt, nil, dbDataSide{res: src.result}, dbDataSide{res: tgt.result})
	if m.aiS().review == nil {
		t.Fatal("no diff card")
	}
	if s.dataCmp.script != "" {
		t.Fatalf("a console result got a script: %s", s.dataCmp.script)
	}
}

func TestDBDataCompareTableTargetHasScript(t *testing.T) {
	withConfigDir(t)
	m := dbGridModel(dbCmpResult())
	s := m.dbConsole()
	src := &dbRun{conn: "a", result: dbCmpResult([2]string{"1", "a"}, [2]string{"2", "b"})}
	tgt := &dbRun{conn: "b", result: dbCmpResult([2]string{"1", "x"}), tv: &dbTableView{dbTable: dbTable{conn: "b", driver: "sqlite", name: "users"}, total: -1}}
	s.dataCmp.gen = 7
	m.onDBDataCompare(7, src, tgt, nil, dbDataSide{res: src.result}, dbDataSide{res: tgt.result})
	sc := s.dataCmp.script
	if !strings.Contains(sc, `UPDATE "users" SET "name" = 'a' WHERE "id" = 1;`) || !strings.Contains(sc, `INSERT INTO "users"`) {
		t.Fatalf("script:\n%s", sc)
	}
	// An old answer is dropped.
	s.dataCmp.script = ""
	m.onDBDataCompare(6, src, tgt, nil, dbDataSide{res: src.result}, dbDataSide{res: tgt.result})
	if s.dataCmp.script != "" {
		t.Fatal("a stale compare was shown")
	}
}

func TestDBDataCompareAsksForAKey(t *testing.T) {
	withConfigDir(t)
	m := dbGridModel(dbCmpResult())
	s := m.dbConsole()
	nokey := dbclient.Result{Columns: []dbclient.Column{{Name: "id"}}, Rows: [][]dbclient.Cell{{{Text: "1"}}}}
	src, tgt := &dbRun{conn: "a", result: nokey}, &dbRun{conn: "b", result: nokey}
	s.dataCmp.gen = 1
	m.onDBDataCompare(1, src, tgt, nil, dbDataSide{res: nokey}, dbDataSide{res: nokey})
	if !m.promptOpen || m.promptKind != promptKindDBDataCompareKey || s.dataCmp.pendSrc != src {
		t.Fatal("no key prompt")
	}
	if cmd := m.databaseDataCompareKeyTyped(" id "); cmd == nil {
		t.Fatal("the typed key did not start a compare")
	}
}
