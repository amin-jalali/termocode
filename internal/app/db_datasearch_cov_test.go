package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// dbeSearchCatalog makes the fake answer the schema's table list.
func dbeSearchCatalog(sess *dbeSession, objects ...dbclient.SchemaObject) {
	inner := sess.intro
	sess.intro = func(r dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
		if r.Object == "" && r.Schema != "" {
			return dbclient.IntrospectionResult{Objects: objects}, nil
		}
		return inner(r)
	}
}

// Search Data from the tree: the value prompt, the search, the hit list and
// the filtered data view of a hit.
func TestCovDBSearchFlow(t *testing.T) {
	m, sess := dbeModel(t)
	dbeSearchCatalog(sess, dbclient.SchemaObject{Name: "orders", Kind: "BASE TABLE"}, dbclient.SchemaObject{Name: "customers", Kind: "BASE TABLE"}, dbclient.SchemaObject{Name: "v", Kind: "VIEW"})
	s := m.databases
	s.databaseSelectPath("pg/db/app/schema/public")
	m.databaseSearchStart()
	if !m.promptOpen || m.promptKind != promptKindDBSearchValue {
		t.Fatalf("no value prompt (toast %q)", cdbToast(m))
	}
	m.promptOpen = false
	cmd, ok := m.handleHTTPPromptSubmit("first")
	if !ok {
		t.Fatal("not routed")
	}
	dbePump(m, cmd)
	if !m.pickerOpen || m.pickerKind != pickerKindDBSearchHits {
		t.Fatalf("no hit list (toast %q)", cdbToast(m))
	}
	hits := m.dbConsole().hits
	if hits == nil || len(hits.report.Hits) != 2 || hits.report.Searched != 2 {
		t.Fatalf("hits %+v", hits)
	}
	m.pickerOpen = false
	cmd, ok = m.databaseConsolePicker("orders")
	if !ok {
		t.Fatal("hit pick not routed")
	}
	dbePump(m, cmd)
	r := m.dbConsole().shown
	if r == nil || r.tv == nil || r.tv.name != "orders" || !strings.Contains(r.tv.where, `LOWER(CAST("note" AS TEXT)) LIKE`) {
		t.Fatalf("hit view %+v", r)
	}

	// Open a hit while a query runs, or with no session.
	m.dbConsole().running = &dbRun{}
	m.databaseSearchOpenHit("orders")
	dbeToastHas(t, m, "A query is running")
	m.dbConsole().running = nil
	delete(s.sessions, "pg")
	m.databaseSearchOpenHit("orders")
	dbeToastHas(t, m, "Not connected")
	m.dbConsole().hits = nil
	if m.databaseSearchOpenHit("orders") != nil {
		t.Fatal("a hit with no search")
	}
}

// The start and run guards say what is missing.
func TestCovDBSearchGuards(t *testing.T) {
	m, sess := dbeModel(t)
	s := m.databases
	s.cursor = -1
	m.databaseSearchStart()
	dbeToastHas(t, m, "Select a schema")
	s.databaseSelectPath("pg")
	delete(s.sessions, "pg")
	m.databaseSearchStart()
	dbeToastHas(t, m, "Not connected")
	s.sessions["pg"] = sess
	m.dbConsole().job = &dbJob{}
	m.databaseSearchStart()
	dbeToastHas(t, m, "Another job is running")
	m.dbConsole().job = nil

	c := m.dbConsole()
	if m.databaseSearchRun("x") != nil {
		t.Fatal("a run with no target")
	}
	c.search = &dbSearchTarget{conn: "pg", driver: "postgres", schema: "public", path: "pg", sessKey: "pg"}
	m.databaseSearchRun("   ")
	dbeToastHas(t, m, "Nothing to search")
	c.search = &dbSearchTarget{conn: "pg", sessKey: "gone"}
	m.databaseSearchRun("x")
	dbeToastHas(t, m, "Not connected")
	c.search = &dbSearchTarget{conn: "pg", sessKey: "pg"}
	c.job = &dbJob{}
	m.databaseSearchRun("x")
	dbeToastHas(t, m, "Another job is running")
	c.job = nil

	// A row that is no target: a Redis connection.
	m2 := cdbModel(t, dbclient.Conn{ID: "r", Driver: "redis", Host: "h"})
	m2.databases.databaseSelectPath("r")
	m2.databaseSearchStart()
	dbeToastHas(t, m2, "Cannot search here")
	if _, err := dbSearchTargetAt(m2.databases, dbTreeRow{}); err == nil {
		t.Fatal("an empty row")
	}
}

// The end of a search: cancelled, failed, no match, and the notes on the
// hit list.
func TestCovDBSearchDone(t *testing.T) {
	m, _ := dbeModel(t)
	c := m.dbConsole()
	tgt := dbSearchTarget{conn: "pg", driver: "postgres", schema: "public", path: "pg", sessKey: "pg"}
	if m.onDBSearchDone(dbSearchDoneMsg{job: &dbJob{}}) != nil {
		t.Fatal("a foreign job")
	}
	done := func(res dbSearchResult, err error) {
		j := &dbJob{}
		c.job = j
		m.onDBSearchDone(dbSearchDoneMsg{job: j, res: res, err: err})
		if c.job != nil {
			t.Fatal("the job stayed")
		}
	}
	done(dbSearchResult{tgt: tgt, report: dbclient.DataSearchReport{Searched: 3}}, context.Canceled)
	dbeToastHas(t, m, "Search cancelled")
	done(dbSearchResult{tgt: tgt}, errors.New("boom"))
	dbeToastHas(t, m, "Search failed")
	done(dbSearchResult{tgt: tgt, tables: 9, report: dbclient.DataSearchReport{Searched: 2, Errors: []dbclient.DataSearchError{{Table: "x", Err: errors.New("e")}}, Truncated: true}}, nil)
	dbeToastHas(t, m, "No match")
	for _, want := range []string{"(of 9)", "1 table could not be read", "stopped at 2000 rows"} {
		dbeToastHas(t, m, want)
	}
	done(dbSearchResult{tgt: tgt, opt: dbclient.DataSearchOptions{RowsPerTable: 5}, report: dbclient.DataSearchReport{Searched: 1, Hits: []dbclient.DataSearchHit{{Table: "t", Rows: 5}}}}, nil)
	if !m.pickerOpen || !strings.Contains(m.picker.View(), "5+ rows") {
		t.Fatalf("hit list %v", m.pickerOpen)
	}
	if dataSearchRows(dbclient.DataSearchOptions{RowsPerTable: 5000}) != 1000 {
		t.Fatal("the row cap")
	}
	if dbSearchWhere("nosuch", []string{"a"}, dbclient.DataSearchOptions{Value: "x"}) != "" {
		t.Fatal("a driver with no dialect")
	}
	if got := dbSearchWhere("mysql", []string{"a"}, dbclient.DataSearchOptions{Value: "x", Exact: true}); !strings.Contains(got, "AS CHAR") {
		t.Fatalf("%q", got)
	}
	if (dbSearchTarget{conn: "pg"}).label() != "pg" {
		t.Fatal("label")
	}
	// A hit of a SQLite table opens it in main.
	m.databases.conns = append(m.databases.conns, dbclient.Conn{ID: "lite", Driver: "sqlite", Path: "x.db"})
	m.databases.sessions["lite"] = &cdbSession{}
	c.hits = &dbSearchResult{tgt: dbSearchTarget{conn: "lite", driver: "sqlite", path: "lite", sessKey: "lite"}, cols: map[string][]string{"t": {"a"}}, opt: dbclient.DataSearchOptions{Value: "x"}}
	msgs := dbeMsgs(m.databaseSearchOpenHit("t"))
	var meta *dbTableMetaMsg
	for _, x := range msgs {
		if mm, ok := x.(dbTableMetaMsg); ok {
			meta = &mm
		}
	}
	if meta == nil || meta.tv.schema != "main" {
		t.Fatalf("%v", msgs)
	}
}

// failSearch is a read-only session whose catalog reads fail as asked.
type failSearch struct {
	cdbSession
	listErr, tableErr error
	n                 int
}

func (f *failSearch) ReadOnlySession() (dbclient.Session, error) {
	if f.roErr != nil {
		return nil, f.roErr
	}
	return f, nil
}
func (f *failSearch) Introspect(_ context.Context, r dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	if r.Object == "" {
		if f.listErr != nil {
			return dbclient.IntrospectionResult{}, f.listErr
		}
		var objs []dbclient.SchemaObject
		for i := 0; i < f.n; i++ {
			objs = append(objs, dbclient.SchemaObject{Name: fmt.Sprintf("t%03d", i), Kind: "BASE TABLE"})
		}
		return dbclient.IntrospectionResult{Objects: objs}, nil
	}
	if f.tableErr != nil && r.Object == "t000" {
		return dbclient.IntrospectionResult{}, f.tableErr
	}
	return dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "a"}}}, nil
}

func TestCovDBDataSearchFailures(t *testing.T) {
	tgt := dbSearchTarget{conn: "my", driver: "mysql", schema: "shop", path: "my/db/shop"}
	opt := dbclient.DataSearchOptions{Value: "x"}
	if _, err := dbDataSearch(context.Background(), &failSearch{cdbSession: cdbSession{roErr: errors.New("ro")}}, tgt, opt, nil); err == nil {
		t.Fatal("ro")
	}
	if _, err := dbDataSearch(context.Background(), &failSearch{listErr: errors.New("list")}, tgt, opt, nil); err == nil {
		t.Fatal("list")
	}
	// 205 tables: 200 are searched, one fails to describe.
	res, err := dbDataSearch(context.Background(), &failSearch{n: 205, tableErr: errors.New("desc")}, tgt, opt, nil)
	if err != nil || res.tables != 205 || len(res.report.Errors) != 1 || res.report.Searched != 200 {
		t.Fatalf("%v tables %d errors %d searched %d", err, res.tables, len(res.report.Errors), res.report.Searched)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dbDataSearch(ctx, &failSearch{n: 2}, tgt, opt, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
	if _, err := dbDataSearch(context.Background(), &failSearch{n: 1}, dbSearchTarget{driver: "nosuch"}, opt, nil); err == nil {
		t.Fatal("no dialect")
	}
	// Cancelled once the queries start.
	ctx, cancel = context.WithCancel(context.Background())
	if _, err := dbDataSearch(ctx, &failSearch{n: 2}, tgt, opt, func(done, total int) { cancel() }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled while searching: %v", err)
	}
	s := newDBState()
	s.conns = []dbclient.Conn{{ID: "pg", Driver: "postgres"}, {ID: "lite", Driver: "sqlite"}}
	for _, c := range []struct {
		row          dbTreeRow
		schema, path string
	}{
		{dbTreeRow{kind: dbNodeConnection, path: "lite", conn: "lite"}, "main", "lite"},
		{dbTreeRow{kind: dbNodeDatabase, path: "pg/db/app", conn: "pg"}, "public", "pg/db/app"},
		{dbTreeRow{kind: dbNodeTable, path: "lite/Tables/t", conn: "lite"}, "", "lite/Tables/t"},
	} {
		got, err := dbSearchTargetAt(s, c.row)
		if err != nil || got.schema != c.schema || got.path != c.path {
			t.Errorf("%s: %+v %v", c.row.path, got, err)
		}
	}
	if _, err := dbSearchTargetAt(s, dbTreeRow{kind: dbNodeNote, path: "pg/x", conn: "pg"}); err == nil {
		t.Fatal("a note row")
	}
}
