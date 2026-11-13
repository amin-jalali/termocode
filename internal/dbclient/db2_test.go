package dbclient

import (
	"context"
	"database/sql/driver"
	"io"
	"strings"
	"testing"
)

// DBX-71: IBM Db2 on the SQL session. The driver (cgo) is only built in
// with -tags db2; everything else here runs in the default build.

func TestDb2IsAnExtraEngine(t *testing.T) {
	e, ok := ExtraSQLEngineByID("db2")
	if !ok || e.Title != "IBM Db2" || e.Port != 50000 || e.User == "" || e.DBLabel == "" {
		t.Fatalf("engine = %+v ok=%v", e, ok)
	}
	for _, alias := range []string{"db2", "ibmdb2", "ibm_db"} {
		if !IsExtraSQL(alias) || normDriver(alias) != "db2" {
			t.Errorf("alias %s -> %s", alias, normDriver(alias))
		}
	}
	if defaultPort("db2") != 50000 || DialectFor("db2") == nil || ServerVersionQuery("db2") == "" {
		t.Error("db2 port, dialect or version query missing")
	}
	if got := (Conn{Driver: "db2"}).EngineLabel(); got != "IBM Db2" {
		t.Errorf("label = %q", got)
	}
	if err := Validate(Conn{ID: "x", Driver: "db2", Host: "db", Port: 50000, Database: "SAMPLE"}); err != nil {
		t.Errorf("db2 does not validate: %v", err)
	}
}

func TestDb2DSN(t *testing.T) {
	c := Conn{Driver: "db2", Host: "db", User: "db2inst1", Database: "SAMPLE", Params: map[string]string{"CurrentSchema": "APP"}}
	got, err := Db2DSN(c, "p;w")
	if err != nil {
		t.Fatal(err)
	}
	if got != `HOSTNAME=db;DATABASE=SAMPLE;PORT=50000;UID=db2inst1;PWD={p;w};CurrentSchema=APP` {
		t.Fatalf("dsn = %q", got)
	}
	c.SSL = &SSLConfig{Mode: "verify-full", CA: "/ca.pem"}
	got, err = Db2DSN(c, "")
	if err != nil || !strings.Contains(got, "SECURITY=SSL") || !strings.Contains(got, "SSLServerCertificate=/ca.pem") {
		t.Fatalf("ssl dsn = %q %v", got, err)
	}
	c.SSL = &SSLConfig{Mode: "disable"}
	if got, _ = Db2DSN(c, ""); strings.Contains(got, "SECURITY") {
		t.Fatalf("disabled ssl dsn = %q", got)
	}
}

func TestDb2DialectCatalogArguments(t *testing.T) {
	d := DialectFor("db2")
	want := map[string]int{"databases": 0, "schemas": 0, "roles": 0, "objects": 1, "routines": 1, "sequences": 1, "triggers": 1, "columns": 2, "keys": 2, "indexes": 2, "foreign_keys": 2}
	q := d.CatalogQueries()
	for _, key := range []string{"schemas", "objects", "columns"} {
		if q[key] == "" {
			t.Errorf("db2 lacks %s", key)
		}
	}
	for key, query := range q {
		n, ok := want[key]
		if !ok {
			t.Errorf("unexpected catalog key %s", key)
			continue
		}
		if got := strings.Count(query, "?"); got != n {
			t.Errorf("%s binds %d, Introspect passes %d", key, got, n)
		}
	}
	if _, ok := d.(currentSchemaQuerier); !ok {
		t.Error("no current schema query")
	}
	if got := d.Limit(10, 20); got != " OFFSET 20 ROWS FETCH NEXT 10 ROWS ONLY" {
		t.Errorf("limit = %q", got)
	}
	if d.Limit(0, 0) != "" || d.Placeholder(2) != "?" || d.QuoteIdent(`a"b`) != `"a""b"` {
		t.Error("basic syntax")
	}
	if d.ReadOnlyBegin() != "" {
		t.Errorf("Db2 has no read-only transaction, got %q", d.ReadOnlyBegin())
	}
	if ddl, args := d.DDL("T"); strings.Count(ddl, "?") != 1 || len(args) != 1 {
		t.Errorf("DDL = %q %v", ddl, args)
	}
	if d.Explain("SELECT 1", false) != "" {
		t.Error("Db2 EXPLAIN writes plan tables: no form")
	}
}

func TestParseURLDb2(t *testing.T) {
	c, err := ParseURL("db2://db2inst1:secret@h/SAMPLE")
	if err != nil || c.Driver != "db2" || c.Host != "h" || c.Port != 50000 || c.User != "db2inst1" || c.Database != "SAMPLE" {
		t.Fatalf("got %+v %v", c, err)
	}
	if c.Password != "secret" || strings.Contains(c.URL, "secret") {
		t.Errorf("password kept in the URL: %+v", c)
	}
}

// fakeDb2Driver answers like a tiny Db2: one column, one row.
type fakeDb2Conn struct{ queries *[]string }

func (c fakeDb2Conn) Prepare(q string) (driver.Stmt, error) {
	*c.queries = append(*c.queries, q)
	return fakeDb2Stmt{}, nil
}
func (fakeDb2Conn) Close() error              { return nil }
func (fakeDb2Conn) Begin() (driver.Tx, error) { return nil, io.EOF }

type fakeDb2Stmt struct{}

func (fakeDb2Stmt) Close() error  { return nil }
func (fakeDb2Stmt) NumInput() int { return -1 }
func (fakeDb2Stmt) Exec([]driver.Value) (driver.Result, error) {
	return driver.RowsAffected(0), nil
}
func (fakeDb2Stmt) Query([]driver.Value) (driver.Rows, error) { return &fakeDb2Rows{}, nil }

type fakeDb2Rows struct{ done bool }

func (*fakeDb2Rows) Columns() []string { return []string{"ONE"} }
func (*fakeDb2Rows) Close() error      { return nil }
func (r *fakeDb2Rows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = int64(1)
	return nil
}

type fakeDb2Connector struct{ queries *[]string }

func (c fakeDb2Connector) Connect(context.Context) (driver.Conn, error) {
	return fakeDb2Conn{c.queries}, nil
}
func (fakeDb2Connector) Driver() driver.Driver { return nil }

// The Db2 pool opens through the same SQL session as the other engines.
func TestDb2SessionRunsThroughTheSQLSession(t *testing.T) {
	var queries []string
	var gotDSN string
	old := db2OpenConnector
	db2OpenConnector = func(dsn string) (driver.Connector, error) {
		gotDSN = dsn
		return fakeDb2Connector{&queries}, nil
	}
	t.Cleanup(func() { db2OpenConnector = old })
	var s Session
	sess, err := NewSQLSession(context.Background(), Conn{ID: "d", Driver: "db2", Host: "db", User: "u", Database: "SAMPLE"}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	s = sess
	defer s.Close()
	if !strings.Contains(gotDSN, "HOSTNAME=db;DATABASE=SAMPLE;PORT=50000;UID=u;PWD={pw}") {
		t.Fatalf("dsn = %q", gotDSN)
	}
	res := s.Run(context.Background(), "SELECT 1 FROM SYSIBM.SYSDUMMY1", RunOptions{MaxRows: 10})
	if res.Err != nil || len(res.Rows) != 1 || len(res.Columns) != 1 {
		t.Fatalf("result = %+v", res)
	}
}

// A schema option becomes SET SCHEMA on every new Db2 connection.
func TestDb2StartSQLSetsTheSchema(t *testing.T) {
	got := (ConnOptions{Schema: "APP"}).StartSQL("db2")
	if len(got) != 1 || got[0] != `SET SCHEMA "APP"` {
		t.Fatalf("start sql = %q", got)
	}
}
