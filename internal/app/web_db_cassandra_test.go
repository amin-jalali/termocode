package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// webTestCassandra is a web hub on a workspace with one Cassandra
// connection ("wide", keyspace shop) talking to stub.
func webTestCassandra(t *testing.T, stub *cqlStub) (*Model, *webConn) {
	t.Helper()
	m := aiTestModel(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	writeCQLConns(t, root)
	m.ws = workspace.Workspace{Root: root}
	m.output = newOutputStore()
	m.databases = newDBState()
	m.databases.load(root)
	m.databases.passwords["wide"] = ""
	useCQLStub(t, stub)
	webTestHub(t, &m)
	return &m, webTestSub(t, &m)
}

// Spec (DBX-67): the web browses Cassandra: the connection can be opened,
// db.tables lists every keyspace's tables (the connection's own keyspace
// first, as keyspace.table with the keyspace as the group), db.rows reads
// a table a page at a time and hands the CQL page state back as a cursor,
// and the next call sends it (CQL has no OFFSET).
func TestWebDBCassandraBrowse(t *testing.T) {
	stub := shopStub()
	m, c := webTestCassandra(t, stub)
	var cs webproto.DBConns
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBConns, `{}`), &cs)
	if len(cs.Conns) != 1 || cs.Conns[0].Driver != "cassandra" || !cs.Conns[0].SQL || cs.Conns[0].Database != "shop" {
		t.Fatalf("conns %+v", cs)
	}
	if f := webTestCallBg(t, m, c, webproto.OpDBRows, `{"conn":"wide","table":"shop.orders"}`); webTestErr(f) != "not_found" {
		t.Fatalf("rows before the list: %v", f)
	}
	var tb webproto.DBTables
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBTables, `{"conn":"wide"}`), &tb)
	var got []string
	for _, x := range tb.Tables {
		got = append(got, x.Group+"/"+x.Name)
	}
	if strings.Join(got, " ") != "shop/shop.orders shop/shop.users system/system.local" {
		t.Fatalf("tables %v", got)
	}

	var p1 webproto.DBRows
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBRows, `{"conn":"wide","table":"shop.orders"}`), &p1)
	if len(p1.Rows) != 100 || p1.Rows[0][0].V != "1" || p1.Cursor == "" || p1.Next != 0 || len(p1.Key) != 0 {
		t.Fatalf("page 1: %d rows, cursor %q, next %d, key %v", len(p1.Rows), p1.Cursor, p1.Next, p1.Key)
	}
	var p2 webproto.DBRows
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBRows, `{"conn":"wide","table":"shop.orders","cursor":"`+p1.Cursor+`"}`), &p2)
	if len(p2.Rows) != 100 || p2.Rows[0][0].V != "101" || p2.Cursor == "" {
		t.Fatalf("page 2: %d rows first %v cursor %q", len(p2.Rows), p2.Rows[0], p2.Cursor)
	}
	var p3 webproto.DBRows
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBRows, `{"conn":"wide","table":"shop.orders","cursor":"`+p2.Cursor+`"}`), &p3)
	if len(p3.Rows) != 50 || p3.Rows[0][0].V != "201" || p3.Cursor != "" || p3.Truncated {
		t.Fatalf("page 3: %d rows cursor %q truncated %v", len(p3.Rows), p3.Cursor, p3.Truncated)
	}
	if want := `SELECT * FROM "shop"."orders"`; stub.calls[len(stub.calls)-1] != want {
		t.Fatalf("ran %q, want %q", stub.calls[len(stub.calls)-1], want)
	}
}

// Spec (DBX-67): db.query runs one CQL read (SELECT, DESCRIBE) in a
// read-only session and pages it with the cursor; any other CQL needs
// the laptop (forbidden here), two statements are refused.
func TestWebDBCassandraQuery(t *testing.T) {
	stub := shopStub()
	m, c := webTestCassandra(t, stub)
	var r1 webproto.DBRows
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBQuery, `{"conn":"wide","sql":"SELECT * FROM shop.orders"}`), &r1)
	if len(r1.Rows) != 250 || r1.Cursor != "" {
		t.Fatalf("query: %d rows cursor %q", len(r1.Rows), r1.Cursor)
	}
	for _, bad := range []string{`INSERT INTO shop.orders (id) VALUES (1)`, `DROP TABLE shop.orders`, `TRUNCATE shop.orders`} {
		if f := webTestCallBg(t, m, c, webproto.OpDBQuery, `{"conn":"wide","sql":"`+bad+`"}`); webTestErr(f) != "forbidden" {
			t.Errorf("%s: %v", bad, f)
		}
	}
	if f := webTestCallBg(t, m, c, webproto.OpDBQuery, `{"conn":"wide","sql":"SELECT 1; DROP TABLE shop.orders"}`); webTestErr(f) != "bad_request" {
		t.Errorf("two statements: %v", f)
	}
	for _, call := range stub.calls {
		if strings.Contains(strings.ToUpper(call), "DROP") || strings.Contains(strings.ToUpper(call), "INSERT") {
			t.Errorf("reached the server: %s", call)
		}
	}
}
