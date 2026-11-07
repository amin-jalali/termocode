package dbclient

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeCQL is a CQLExecutor that answers from a table of canned pages.
type fakeCQL struct {
	mu     sync.Mutex
	pages  map[string][]CQLPage // statement -> pages in order
	calls  []fakeCQLCall
	err    error
	closed bool
}

type fakeCQLCall struct {
	stmt  string
	args  []any
	size  int
	state string
}

func (f *fakeCQL) Page(_ context.Context, stmt string, args []any, size int, state []byte) (CQLPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCQLCall{stmt: stmt, args: args, size: size, state: string(state)})
	if f.err != nil {
		return CQLPage{}, f.err
	}
	ps := f.pages[stmt]
	i := 0
	if len(state) > 0 {
		i = int(state[0])
	}
	if i >= len(ps) {
		return CQLPage{}, nil
	}
	return ps[i], nil
}

func (f *fakeCQL) Close() {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
}

func textRow(vals ...string) []Cell {
	out := make([]Cell, len(vals))
	for i, v := range vals {
		out[i] = Cell{Text: v}
	}
	return out
}

func TestCassandraDriverNamesAndPort(t *testing.T) {
	for _, d := range []string{"cassandra", "scylla", "scylladb"} {
		if got := normDriver(d); got != "cassandra" {
			t.Fatalf("normDriver(%q) = %q, want cassandra", d, got)
		}
	}
	if p := defaultPort("cassandra"); p != 9042 {
		t.Fatalf("default port = %d, want 9042", p)
	}
	conns, err := ParseConfig([]byte(`{"connections":[{"id":"wide","driver":"scylla","host":"db.local"}]}`), SourceUser, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c := conns[0]; c.Problem != "" || c.Driver != "cassandra" || c.Port != 9042 {
		t.Fatalf("parsed %+v", c)
	}
	if err := Validate(Conn{ID: "wide", Driver: "cassandra", Host: "db.local", Port: 9042}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if DialectFor("cassandra") != nil {
		t.Fatal("cassandra must not get a SQL dialect")
	}
}

func TestCQLReadOnly(t *testing.T) {
	read := []string{
		"SELECT * FROM ks.t",
		"  select id from t where id = 1;",
		"-- note\nSELECT now() FROM system.local",
		"/* c */ DESCRIBE KEYSPACES",
		"desc table ks.t",
		"// line\nSELECT 1 FROM system.local ;  ",
	}
	for _, s := range read {
		if !CQLReadOnly(s) {
			t.Errorf("CQLReadOnly(%q) = false, want true", s)
		}
	}
	write := []string{
		"INSERT INTO t (id) VALUES (1)",
		"UPDATE t SET a = 1 WHERE id = 1",
		"DELETE FROM t WHERE id = 1",
		"TRUNCATE t",
		"DROP TABLE t",
		"CREATE TABLE t (id int PRIMARY KEY)",
		"ALTER TABLE t ADD b int",
		"BEGIN BATCH INSERT INTO t (id) VALUES (1); APPLY BATCH",
		"USE ks",
		"GRANT SELECT ON ks.t TO bob",
		"SELECT * FROM t; DROP TABLE t",
		"",
		"   ",
	}
	for _, s := range write {
		if CQLReadOnly(s) {
			t.Errorf("CQLReadOnly(%q) = true, want false", s)
		}
	}
}

func TestCassandraRunPages(t *testing.T) {
	cols := []Column{{Name: "id", Type: "int"}, {Name: "name", Type: "text"}}
	f := &fakeCQL{pages: map[string][]CQLPage{
		"SELECT id, name FROM ks.users": {
			{Columns: cols, Rows: [][]Cell{textRow("1", "ann"), textRow("2", "bo")}, Next: []byte{1}},
			{Columns: cols, Rows: [][]Cell{textRow("3", "cy")}, Warnings: []string{"tombstones"}},
		},
	}}
	s := NewCassandraSessionWith(Conn{ID: "wide", Driver: "cassandra"}, f)
	res, next := s.RunPage(context.Background(), "SELECT id, name FROM ks.users", RunOptions{MaxRows: 2}, nil)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if len(res.Rows) != 2 || !res.Truncated || len(next) == 0 || len(res.Columns) != 2 {
		t.Fatalf("first page: %+v next=%v", res, next)
	}
	if f.calls[0].size != 2 {
		t.Fatalf("page size sent = %d, want 2", f.calls[0].size)
	}
	res, next = s.RunPage(context.Background(), "SELECT id, name FROM ks.users", RunOptions{MaxRows: 2}, next)
	if res.Err != nil || len(res.Rows) != 1 || res.Truncated || next != nil {
		t.Fatalf("second page: %+v next=%v", res, next)
	}
	if res.Rows[0][1].Text != "cy" || len(res.Notices) != 1 || res.Notices[0] != "tombstones" {
		t.Fatalf("second page rows/notices: %+v", res)
	}
	if f.calls[1].state != string([]byte{1}) {
		t.Fatalf("page state not passed back: %q", f.calls[1].state)
	}

	// Run is the first page with the default size when MaxRows is 0.
	r := s.Run(context.Background(), "SELECT id, name FROM ks.users", RunOptions{})
	if r.Err != nil || !r.Truncated || f.calls[2].size != CassandraPageSize {
		t.Fatalf("Run: %+v size=%d", r, f.calls[2].size)
	}
}

func TestCassandraReadOnlySessionRefusesWrites(t *testing.T) {
	f := &fakeCQL{pages: map[string][]CQLPage{}}
	s := NewCassandraSessionWith(Conn{ID: "wide", Driver: "cassandra"}, f)
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	r := ro.Run(context.Background(), "INSERT INTO t (id) VALUES (1)", RunOptions{})
	if r.Err == nil || !strings.Contains(r.Err.Error(), "read-only") {
		t.Fatalf("read-only Run err = %v", r.Err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("a write reached the server: %+v", f.calls)
	}
	// A read-only connection config makes the main session read-only too.
	ros := NewCassandraSessionWith(Conn{ID: "wide", Driver: "cassandra", ReadOnly: true}, f)
	if r := ros.Run(context.Background(), "DROP TABLE t", RunOptions{}); r.Err == nil {
		t.Fatal("readOnly connection ran DROP")
	}
	// Closing the read-only view keeps the shared executor open.
	_ = ro.Close()
	if f.closed {
		t.Fatal("closing the read-only session closed the shared executor")
	}
	_ = s.Close()
	if !f.closed {
		t.Fatal("Close did not close the executor")
	}
	if r := s.Run(context.Background(), "SELECT 1 FROM system.local", RunOptions{}); r.Err == nil {
		t.Fatal("Run after Close must fail")
	}
}

func TestCassandraNoTransactions(t *testing.T) {
	s := NewCassandraSessionWith(Conn{ID: "wide", Driver: "cassandra"}, &fakeCQL{})
	for _, fn := range []func(context.Context) error{s.Begin, s.Commit, s.Rollback} {
		if err := fn(context.Background()); !errors.Is(err, ErrCQLNoTransactions) {
			t.Fatalf("err = %v, want ErrCQLNoTransactions", err)
		}
	}
}

func TestCassandraPing(t *testing.T) {
	f := &fakeCQL{pages: map[string][]CQLPage{
		cqlPing: {{Columns: []Column{{Name: "release_version"}}, Rows: [][]Cell{textRow("5.0.2")}}},
	}}
	s := NewCassandraSessionWith(Conn{ID: "wide", Driver: "cassandra"}, f)
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.err = errors.New("down")
	if err := s.Ping(context.Background()); err == nil {
		t.Fatal("Ping must report a server error")
	}
}

func TestCassandraIntrospect(t *testing.T) {
	f := &fakeCQL{pages: map[string][]CQLPage{
		cqlKeyspaces: {{Rows: [][]Cell{textRow("system"), textRow("shop")}}},
		cqlTables:    {{Rows: [][]Cell{textRow("orders"), textRow("users")}}},
		cqlViews:     {{Rows: [][]Cell{textRow("users_by_mail")}}},
		cqlColumns: {{Rows: [][]Cell{
			textRow("name", "text", "regular", "-1", "none"),
			textRow("id", "uuid", "partition_key", "0", "none"),
			textRow("tenant", "text", "partition_key", "1", "none"),
			textRow("at", "timestamp", "clustering", "0", "desc"),
			textRow("seq", "int", "clustering", "1", "asc"),
		}}},
		cqlIndexes: {{Rows: [][]Cell{textRow("orders_name_idx")}}},
	}}
	s := NewCassandraSessionWith(Conn{ID: "wide", Driver: "cassandra"}, f)
	ctx := context.Background()

	res, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || strings.Join(res.Databases, ",") != "shop,system" {
		t.Fatalf("keyspaces: %+v %v", res.Databases, err)
	}

	res, err = s.Introspect(ctx, IntrospectionRequest{Database: "shop"})
	if err != nil {
		t.Fatal(err)
	}
	want := []SchemaObject{{"orders", "table"}, {"users", "table"}, {"users_by_mail", "view"}}
	if len(res.Objects) != len(want) {
		t.Fatalf("objects: %+v", res.Objects)
	}
	for i := range want {
		if res.Objects[i] != want[i] {
			t.Fatalf("objects: %+v", res.Objects)
		}
	}
	if got := f.calls[len(f.calls)-1].args; len(got) != 1 || got[0] != "shop" {
		t.Fatalf("keyspace not bound: %+v", got)
	}

	res, err = s.Introspect(ctx, IntrospectionRequest{Schema: "shop", Object: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, c := range res.Columns {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "id,tenant,at,seq,name" {
		t.Fatalf("column order: %v", names)
	}
	if !res.Columns[0].PK || !res.Columns[2].PK || res.Columns[4].PK {
		t.Fatalf("PK flags: %+v", res.Columns)
	}
	if len(res.Indexes) != 1 || res.Indexes[0].Name != "orders_name_idx" {
		t.Fatalf("indexes: %+v", res.Indexes)
	}
	wantDDL := `CREATE TABLE "shop"."orders" (
  "id" uuid,
  "tenant" text,
  "at" timestamp,
  "seq" int,
  "name" text,
  PRIMARY KEY (("id", "tenant"), "at", "seq")
) WITH CLUSTERING ORDER BY ("at" DESC, "seq" ASC);`
	if res.DDL != wantDDL {
		t.Fatalf("DDL:\n%s\nwant:\n%s", res.DDL, wantDDL)
	}
}

func TestCassandraQuoteIdent(t *testing.T) {
	if got := cqlQuote(`we"ird`); got != `"we""ird"` {
		t.Fatalf("cqlQuote = %s", got)
	}
}
