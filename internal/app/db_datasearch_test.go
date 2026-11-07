package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// searchSession answers the catalog with two tables and finds the value
// only in "users"; every statement it saw is kept.
type searchSession struct {
	dbclient.Session
	sqls []string
	args [][]any
}

func (f *searchSession) ReadOnlySession() (dbclient.Session, error) { return f, nil }
func (f *searchSession) Close() error                               { return nil }
func (f *searchSession) Introspect(_ context.Context, r dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	switch r.Object {
	case "":
		return dbclient.IntrospectionResult{Objects: []dbclient.SchemaObject{{Name: "users", Kind: "BASE TABLE"}, {Name: "orders", Kind: "BASE TABLE"}, {Name: "v_users", Kind: "VIEW"}}}, nil
	case "users":
		return dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "id"}, {Name: "email"}}}, nil
	case "orders":
		return dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "id"}, {Name: "note"}}}, nil
	}
	return dbclient.IntrospectionResult{}, nil
}
func (f *searchSession) Run(_ context.Context, sql string, o dbclient.RunOptions) dbclient.Result {
	f.sqls = append(f.sqls, sql)
	f.args = append(f.args, o.Args)
	if strings.Contains(sql, `"users"`) {
		return dbclient.Result{Columns: []dbclient.Column{{Name: "id"}}, Rows: [][]dbclient.Cell{{{Text: "1"}}, {{Text: "2"}}}}
	}
	return dbclient.Result{Columns: []dbclient.Column{{Name: "id"}}}
}

func TestDBSearchParseValue(t *testing.T) {
	if o := dbSearchParse("  alice "); o.Value != "alice" || o.Exact {
		t.Fatalf("contains: %+v", o)
	}
	if o := dbSearchParse("=42"); o.Value != "42" || !o.Exact {
		t.Fatalf("exact: %+v", o)
	}
}

func TestDBSearchTargetFromTree(t *testing.T) {
	s := newDBState()
	s.conns = []dbclient.Conn{{ID: "pg", Driver: "postgres"}, {ID: "my", Driver: "mysql", Database: "shop"}}
	cases := []struct {
		row          dbTreeRow
		schema, path string
	}{
		{dbTreeRow{kind: dbNodeSchema, path: "pg/db/app/schema/sales", name: "sales", conn: "pg"}, "sales", "pg/db/app/schema/sales"},
		{dbTreeRow{kind: dbNodeTable, path: "pg/db/app/schema/sales/Tables/users", name: "users", conn: "pg"}, "sales", "pg/db/app/schema/sales"},
		{dbTreeRow{kind: dbNodeConnection, path: "pg", name: "pg", conn: "pg"}, "public", "pg"},
		{dbTreeRow{kind: dbNodeDatabase, path: "my/db/shop", name: "shop", conn: "my"}, "shop", "my/db/shop"},
		{dbTreeRow{kind: dbNodeConnection, path: "my", name: "my", conn: "my"}, "shop", "my"},
	}
	for _, c := range cases {
		got, err := dbSearchTargetAt(s, c.row)
		if err != nil || got.schema != c.schema || got.path != c.path {
			t.Errorf("%s: got %+v, %v; want schema %q path %q", c.row.path, got, err, c.schema, c.path)
		}
	}
	if _, err := dbSearchTargetAt(s, dbTreeRow{kind: dbNodeSuggestion, path: "x"}); err == nil {
		t.Error("a suggestion row is no search target")
	}
}

func TestDBDataSearchFindsHitsReadOnlyAndReportsProgress(t *testing.T) {
	sess := &searchSession{}
	tgt := dbSearchTarget{conn: "pg", driver: "postgres", schema: "public", path: "pg"}
	var last [2]int
	res, err := dbDataSearch(context.Background(), sess, tgt, dbclient.DataSearchOptions{Value: "a@x"}, func(done, total int) { last = [2]int{done, total} })
	if err != nil {
		t.Fatal(err)
	}
	if len(res.report.Hits) != 1 || res.report.Hits[0].Table != "users" || res.report.Hits[0].Rows != 2 {
		t.Fatalf("hits: %+v", res.report.Hits)
	}
	if res.report.Searched != 2 || last != [2]int{2, 2} {
		t.Fatalf("searched %d, progress %v (views are not searched)", res.report.Searched, last)
	}
	for i, sql := range sess.sqls {
		if strings.Contains(sql, "a@x") {
			t.Fatalf("the value must be bound, not in the SQL: %s", sql)
		}
		if len(sess.args[i]) == 0 {
			t.Fatalf("no bound args for %s", sql)
		}
	}
	items := dbSearchHitItems(res)
	if len(items) != 1 || !strings.Contains(items[0].Title, "public.users") || !strings.Contains(items[0].Hint, "2 rows") {
		t.Fatalf("items: %+v", items)
	}
}

func TestDBSearchWhereIsAFilterTheDataViewAccepts(t *testing.T) {
	for _, driver := range []string{"postgres", "mysql", "sqlite"} {
		w := dbSearchWhere(driver, []string{"email", "note"}, dbclient.DataSearchOptions{Value: "o'neil"})
		if err := dbWhereCheck(driver, w); err != nil {
			t.Fatalf("%s: %q refused: %v", driver, w, err)
		}
		if !strings.Contains(w, "LIKE") || !strings.Contains(w, " OR ") {
			t.Fatalf("%s: %q", driver, w)
		}
	}
	w := dbSearchWhere("postgres", []string{"id"}, dbclient.DataSearchOptions{Value: "7", Exact: true})
	if strings.Contains(w, "LIKE") || !strings.Contains(w, "= '7'") {
		t.Fatalf("exact: %q", w)
	}
}
