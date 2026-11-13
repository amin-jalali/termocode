package dbclient

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
)

// DBX-65: Spark SQL / Hive (HiveServer2, Thrift) on the SQL session. The
// driver (gohive; Kerberos goes through cgo) is only linked in with
// -tags hive; everything here runs in the default build on a fake backend.

func TestHiveIsAnExtraEngine(t *testing.T) {
	e, ok := ExtraSQLEngineByID("hive")
	if !ok || e.Title != "Spark SQL / Hive" || e.Port != 10000 || e.User == "" || !e.DBOptional {
		t.Fatalf("engine = %+v ok=%v", e, ok)
	}
	for _, alias := range []string{"hive", "spark", "sparksql", "hiveserver2"} {
		if !IsExtraSQL(alias) || normDriver(alias) != "hive" {
			t.Errorf("alias %s -> %s", alias, normDriver(alias))
		}
	}
	if defaultPort("spark") != 10000 || DialectFor("hive") == nil || ServerVersionQuery("hive") == "" {
		t.Error("hive port, dialect or version query missing")
	}
	if got := (Conn{Driver: "spark"}).EngineLabel(); got != "Spark SQL / Hive" {
		t.Errorf("label = %q", got)
	}
	if err := Validate(Conn{ID: "x", Driver: "hive", Host: "h", Port: 10000}); err != nil {
		t.Errorf("hive does not validate: %v", err)
	}
}

func TestParseURLHive(t *testing.T) {
	for _, raw := range []string{"hive://hive@h/sales", "spark://hive@h:10000/sales", "hiveserver2://hive@h/sales"} {
		c, err := ParseURL(raw)
		if err != nil || c.Driver != "hive" || c.Host != "h" || c.Port != 10000 || c.User != "hive" || c.Database != "sales" {
			t.Errorf("%s = %+v %v", raw, c, err)
		}
	}
}

func TestHiveSettings(t *testing.T) {
	c := Conn{Driver: "hive", Host: "h", User: "me", Database: "sales"}
	s, err := HiveSettings(c, "")
	if err != nil || s.Host != "h" || s.Port != 10000 || s.Auth != "NONE" || s.Username != "me" || s.Database != "sales" || s.TLS != nil || s.Transport != "binary" {
		t.Fatalf("plain = %+v %v", s, err)
	}
	if s, _ = HiveSettings(c, "pw"); s.Auth != "LDAP" || s.Password != "pw" {
		t.Errorf("a password means LDAP: %+v", s)
	}
	c.Params = map[string]string{"auth": "kerberos", "service": "hive", "transportMode": "http", "httpPath": "cliservice"}
	if s, _ = HiveSettings(c, ""); s.Auth != "KERBEROS" || s.Service != "hive" || s.Transport != "http" || s.HTTPPath != "cliservice" {
		t.Errorf("params = %+v", s)
	}
	c.Params = map[string]string{"auth": "magic"}
	if _, err = HiveSettings(c, ""); err == nil || !strings.Contains(err.Error(), "magic") {
		t.Errorf("unknown auth taken: %v", err)
	}
	c.Params = map[string]string{"bogus": "1"}
	if _, err = HiveSettings(c, ""); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("unknown parameter taken: %v", err)
	}
	c.Params = nil
	c.SSL = &SSLConfig{Mode: "verify-full"}
	dialed := ""
	c.Dial = func(_ context.Context, _, addr string) (net.Conn, error) { dialed = addr; return nil, io.EOF }
	s, err = HiveSettings(c, "")
	if err != nil || s.TLS == nil || s.TLS.ServerName != "h" || s.Dial == nil {
		t.Fatalf("tls = %+v %v", s, err)
	}
	_, _ = s.Dial(context.Background(), "tcp", "h:10000")
	if dialed != "h:10000" {
		t.Errorf("tunnel dialed %q", dialed)
	}
}

func TestHiveDialect(t *testing.T) {
	d := DialectFor("hive")
	if d.QuoteIdent("a`b") != "`a``b`" || d.Placeholder(1) != "?" || d.ReadOnlyBegin() != "" {
		t.Error("basic syntax")
	}
	if d.Limit(0, 0) != "" || d.Limit(10, 0) != " LIMIT 10" || d.Limit(10, 20) != " LIMIT 10 OFFSET 20" {
		t.Errorf("limit = %q %q", d.Limit(10, 0), d.Limit(10, 20))
	}
	if d.Explain("SELECT 1", false) != "EXPLAIN SELECT 1" {
		t.Errorf("explain = %q", d.Explain("SELECT 1", false))
	}
	if ddl, _ := d.DDL("t"); ddl != "SHOW CREATE TABLE `t`" {
		t.Errorf("ddl = %q", ddl)
	}
	if got := (ConnOptions{Schema: "sales"}).StartSQL("hive"); len(got) != 1 || got[0] != "USE `sales`" {
		t.Errorf("start sql = %q", got)
	}
}

func TestHiveLiteralEscapesLikeHive(t *testing.T) {
	got, err := hiveInlineArgs("SELECT ? , '?' , ? -- ?\n, ?", []any{"it's a \\", int64(3), nil})
	if err != nil {
		t.Fatal(err)
	}
	if got != "SELECT 'it\\'s a \\\\' , '?' , 3 -- ?\n, NULL" {
		t.Fatalf("got %q", got)
	}
	if _, err := hiveInlineArgs("SELECT ?", nil); err == nil {
		t.Error("a missing argument was accepted")
	}
}

// fakeHive answers the statements of one test from a table.
type fakeHive struct {
	mu      sync.Mutex
	queries []string
	answers map[string]fakeHiveAnswer // by query prefix
	closed  bool
}

type fakeHiveAnswer struct {
	cols, types []string
	rows        [][]driver.Value
	err         error
}

func (f *fakeHive) Query(_ context.Context, q string) (hiveResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	for prefix, a := range f.answers {
		if strings.HasPrefix(q, prefix) {
			if a.err != nil {
				return nil, a.err
			}
			return &fakeHiveRows{a: a}, nil
		}
	}
	return &fakeHiveRows{}, nil
}
func (f *fakeHive) Close() error { f.closed = true; return nil }

type fakeHiveRows struct {
	a fakeHiveAnswer
	i int
}

func (r *fakeHiveRows) Columns() []string { return r.a.cols }
func (r *fakeHiveRows) Types() []string   { return r.a.types }
func (r *fakeHiveRows) Close() error      { return nil }
func (r *fakeHiveRows) Next() ([]driver.Value, error) {
	if r.i >= len(r.a.rows) {
		return nil, io.EOF
	}
	r.i++
	return r.a.rows[r.i-1], nil
}

func openFakeHive(t *testing.T, f *fakeHive, c Conn) *SQLSession {
	t.Helper()
	old := hiveOpenBackend
	hiveOpenBackend = func(context.Context, HiveConfig) (hiveBackend, error) { return f, nil }
	t.Cleanup(func() { hiveOpenBackend = old })
	if c.Driver == "" {
		c.Driver = "hive"
	}
	c.ID, c.Host = "h", "hs2"
	s, err := NewSQLSession(context.Background(), c, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestHiveSessionRunsRows(t *testing.T) {
	f := &fakeHive{answers: map[string]fakeHiveAnswer{
		"SELECT id": {cols: []string{"id", "name"}, types: []string{"BIGINT", "STRING"}, rows: [][]driver.Value{{int64(1), "ann"}, {int64(2), nil}, {int64(3), []byte("x")}}},
	}}
	s := openFakeHive(t, f, Conn{User: "me", Database: "sales"})
	var _ Session = s
	res := s.Run(context.Background(), "SELECT id, name FROM t", RunOptions{MaxRows: 2})
	if res.Err != nil || len(res.Columns) != 2 || res.Columns[0].Name != "id" || res.Columns[1].Type != "STRING" {
		t.Fatalf("result = %+v", res)
	}
	if len(res.Rows) != 2 || !res.Truncated || res.Rows[0][1].Text != "ann" || !res.Rows[1][1].Null {
		t.Fatalf("rows = %+v truncated=%v", res.Rows, res.Truncated)
	}
	res = s.Run(context.Background(), "SELECT id FROM t WHERE n = ?", RunOptions{Args: []any{"o'k"}})
	if res.Err != nil || !strings.Contains(strings.Join(f.queries, "\n"), `WHERE n = 'o\'k'`) {
		t.Fatalf("args: err=%v queries=%q", res.Err, f.queries)
	}
	f.answers["BOOM"] = fakeHiveAnswer{err: errors.New("Error while compiling statement: FAILED: ParseException password=hunter2")}
	if res = s.Run(context.Background(), "BOOM", RunOptions{}); res.Err == nil || strings.Contains(res.Err.Error(), "hunter2") {
		t.Fatalf("error = %v", res.Err)
	}
	if err := s.Begin(context.Background()); err == nil || !strings.Contains(err.Error(), "transaction") {
		t.Errorf("Hive has no transactions: %v", err)
	}
}

func TestHiveSessionRunsStartSQL(t *testing.T) {
	f := &fakeHive{}
	s := openFakeHive(t, f, Conn{Options: ConnOptions{Schema: "sales", InitSQL: "SET hive.exec.parallel=true"}})
	_ = s.Run(context.Background(), "SELECT 1", RunOptions{})
	got := strings.Join(f.queries, "|")
	if !strings.Contains(got, "USE `sales`") || !strings.Contains(got, "SET hive.exec.parallel=true") {
		t.Fatalf("queries = %q", got)
	}
}

func TestHiveIntrospect(t *testing.T) {
	f := &fakeHive{answers: map[string]fakeHiveAnswer{
		"SHOW DATABASES":      {cols: []string{"database_name"}, rows: [][]driver.Value{{"default"}, {"sales"}}},
		"SHOW TABLES IN `sa`": {cols: []string{"tab_name"}, rows: [][]driver.Value{{"orders"}, {"v_open"}}},
		"SHOW VIEWS IN `sa`":  {cols: []string{"view_name"}, rows: [][]driver.Value{{"v_open"}}},
		"DESCRIBE `sa`.`orders`": {cols: []string{"col_name", "data_type", "comment"}, rows: [][]driver.Value{
			{"id", "bigint", nil}, {"total", "decimal(10,2)", "sum"}, {"", nil, nil},
			{"# Partition Information", nil, nil}, {"# col_name", "data_type", "comment"}, {"day", "string", nil}}},
		"SHOW CREATE TABLE `sa`.`orders`": {cols: []string{"createtab_stmt"}, rows: [][]driver.Value{{"CREATE TABLE orders ("}, {"id bigint)"}}},
	}}
	s := openFakeHive(t, f, Conn{})
	ctx := context.Background()
	r, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || len(r.Databases) != 2 || r.Databases[1] != "sales" {
		t.Fatalf("databases = %+v %v", r, err)
	}
	r, err = s.Introspect(ctx, IntrospectionRequest{Database: "sa"})
	if err != nil || len(r.Schemas) != 1 || r.Schemas[0] != "sa" {
		t.Fatalf("schemas = %+v %v", r, err)
	}
	r, err = s.Introspect(ctx, IntrospectionRequest{Database: "sa", Schema: "sa"})
	if err != nil || len(r.Objects) != 2 || r.Objects[0] != (SchemaObject{Name: "orders", Kind: "BASE TABLE"}) || r.Objects[1] != (SchemaObject{Name: "v_open", Kind: "VIEW"}) {
		t.Fatalf("objects = %+v %v", r.Objects, err)
	}
	r, err = s.Introspect(ctx, IntrospectionRequest{Schema: "sa", Object: "orders"})
	if err != nil || len(r.Columns) != 3 || r.Columns[1].Name != "total" || r.Columns[1].Type != "decimal(10,2)" || r.Columns[2].Name != "day" {
		t.Fatalf("columns = %+v %v", r.Columns, err)
	}
	if r.DDL != "CREATE TABLE orders (\nid bigint)" {
		t.Errorf("ddl = %q", r.DDL)
	}
}

// Spark's SHOW TABLES has three columns: database, tableName, isTemporary.
func TestHiveIntrospectSparkShowTables(t *testing.T) {
	f := &fakeHive{answers: map[string]fakeHiveAnswer{
		"SHOW TABLES IN `d`": {cols: []string{"database", "tableName", "isTemporary"}, rows: [][]driver.Value{{"d", "t1", false}}},
		"SHOW VIEWS IN `d`":  {err: errors.New("no views")},
	}}
	s := openFakeHive(t, f, Conn{Driver: "spark"})
	r, err := s.Introspect(context.Background(), IntrospectionRequest{Schema: "d"})
	if err != nil || len(r.Objects) != 1 || r.Objects[0].Name != "t1" || r.Objects[0].Kind != "BASE TABLE" {
		t.Fatalf("objects = %+v %v", r.Objects, err)
	}
}
