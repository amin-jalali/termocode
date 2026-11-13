package dbclient

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func trinoConnOf(t *testing.T, srv *httptest.Server) Conn {
	t.Helper()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	return Conn{ID: "tr", Driver: "trino", Host: host, Port: p, User: "ana"}
}

func TestTrinoTransactionsAndReadOnlyClone(t *testing.T) {
	f := newFakeTrino(t)
	s, err := NewTrinoSession(context.Background(), f.conn(t), "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.Commit(ctx); err == nil || !strings.Contains(err.Error(), "no transaction") {
		t.Errorf("commit without a transaction = %v", err)
	}
	if err := s.Rollback(ctx); err == nil || !strings.Contains(err.Error(), "no transaction") {
		t.Errorf("rollback without a transaction = %v", err)
	}
	if err := s.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Begin(ctx); err == nil || !strings.Contains(err.Error(), "already open") {
		t.Errorf("a second begin = %v", err)
	}
	if err := s.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// A transaction left open is rolled back by Close.
	if err := s.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	if r := ro.Run(ctx, "SELECT 1", RunOptions{}); r.Err != nil {
		t.Errorf("read-only run = %v", r.Err)
	}
	s.Cancel()
	_ = s.Close()
	_ = s.Close() // twice is fine
	if _, last := f.last(); last != "ROLLBACK" && !strings.Contains(strings.Join(f.sqls, "|"), "ROLLBACK") {
		t.Error("Close rolls an open transaction back")
	}
	if r := s.Run(ctx, "SELECT 1", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "closed") {
		t.Errorf("a closed session = %v", r.Err)
	}
}

func TestTrinoConnectRefusals(t *testing.T) {
	f := newFakeTrino(t)
	c := f.conn(t)
	if _, err := NewTrinoSession(context.Background(), c, "secret"); err == nil || !strings.Contains(err.Error(), "only over TLS") {
		t.Errorf("a password without TLS = %v", err)
	}
	tls := c
	tls.TLS = true
	if _, err := NewTrinoSession(context.Background(), tls, ""); err == nil {
		t.Error("TLS to a plain HTTP coordinator: want error")
	}
	// Read-only starts a read-only transaction first.
	ro := c
	ro.ReadOnly = true
	s, err := NewTrinoSession(context.Background(), ro, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !strings.Contains(strings.Join(f.sqls, "|"), "START TRANSACTION READ ONLY") {
		t.Errorf("sqls = %v", f.sqls)
	}
	// Through the SSH tunnel hook.
	dialed := false
	via := c
	via.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = true
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	via.Port = 0
	via.Host = "127.0.0.1"
	if _, err := NewTrinoSession(context.Background(), via, ""); err == nil {
		t.Error("the default port is not the test server's: want error")
	}
	if !dialed {
		t.Error("the tunnel dial hook is used")
	}
}

func TestTrinoHTTPFailuresAndOddPages(t *testing.T) {
	var status int
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(204)
			return
		}
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "ping") || strings.HasPrefix(string(b), "SELECT 1") {
			_, _ = io.WriteString(w, `{"id":"q","columns":[{"name":"c","type":"integer"}],"data":[[1]]}`)
			return
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	s, err := NewTrinoSession(context.Background(), trinoConnOf(t, srv), "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for name, c := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"empty body": {503, "", "503"},
		"long body":  {500, strings.Repeat("x", 3000), "xxx"},
		"bad json":   {200, "<html>", "could not read"},
		"error name": {200, `{"error":{"errorName":"USER_ERROR"}}`, "USER_ERROR"},
	} {
		status, body = c.status, c.body
		r := s.Run(ctx, "FAIL", RunOptions{})
		if r.Err == nil || !strings.Contains(r.Err.Error(), c.want) {
			t.Errorf("%s = %v, want %q", name, r.Err, c.want)
		}
		if name == "long body" && len(r.Err.Error()) > 2100 {
			t.Errorf("a long body is cut: %d bytes", len(r.Err.Error()))
		}
	}
	if r := s.Run(ctx, "SELECT ?", RunOptions{Args: []any{1, 2}}); r.Err == nil {
		t.Error("too many arguments: want error")
	}
	// A server that is gone.
	srv.Close()
	if r := s.Run(ctx, "SELECT 1", RunOptions{}); r.Err == nil {
		t.Error("a coordinator that is down: want error")
	}
}

func TestTrinoTruncatedResultAbandonsTheQuery(t *testing.T) {
	var deletes int
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
			w.WriteHeader(204)
			return
		}
		_, _ = io.WriteString(w, `{"id":"q","nextUri":"`+srv.URL+`/v1/statement/q/2","columns":[{"name":"c","type":"integer"}],"data":[[1],[2],[3]]}`)
	}))
	defer srv.Close()
	s, err := NewTrinoSession(context.Background(), trinoConnOf(t, srv), "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	before := deletes
	r := s.Run(context.Background(), "SELECT * FROM big", RunOptions{MaxRows: 2})
	if r.Err != nil || !r.Truncated || len(r.Rows) != 2 {
		t.Errorf("result = %+v", r)
	}
	if deletes != before+1 {
		t.Errorf("the rest of the query is dropped on the server: %d deletes, was %d", deletes, before)
	}
	if _, err := s.sameServer("http://[bad"); err == nil {
		t.Error("a bad next link: want error")
	}
	if got, err := s.sameServer("/v1/statement/q/3"); err != nil || !strings.HasPrefix(got, srv.URL) {
		t.Errorf("a relative next link = %q, %v", got, err)
	}
}

func TestTrinoIntrospectLevels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		q := string(b)
		switch {
		case strings.Contains(q, "information_schema.schemata"):
			_, _ = io.WriteString(w, `{"columns":[{"name":"s","type":"varchar"}],"data":[["default"],[null],["sales"]]}`)
		case strings.Contains(q, "information_schema.tables"):
			_, _ = io.WriteString(w, `{"columns":[{"name":"n","type":"varchar"},{"name":"k","type":"varchar"}],"data":[["orders","BASE TABLE"],["v","VIEW"]]}`)
		case strings.Contains(q, "information_schema.columns"):
			_, _ = io.WriteString(w, `{"columns":[{"name":"n","type":"varchar"},{"name":"t","type":"varchar"}],"data":[["id","bigint"],["total","double"]]}`)
		case strings.HasPrefix(q, "SHOW CREATE TABLE"):
			_, _ = io.WriteString(w, `{"columns":[{"name":"c","type":"varchar"}],"data":[["CREATE TABLE orders (id bigint)"]]}`)
		default:
			_, _ = io.WriteString(w, `{"columns":[{"name":"c","type":"integer"}],"data":[[1]]}`)
		}
	}))
	defer srv.Close()
	s, err := NewTrinoSession(context.Background(), trinoConnOf(t, srv), "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	top, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || strings.Join(top.Databases, ",") != "default,sales" {
		t.Errorf("schemas = %+v, %v", top, err)
	}
	objs, err := s.Introspect(ctx, IntrospectionRequest{Database: "sales"})
	if err != nil || len(objs.Objects) != 2 || objs.Objects[1].Kind != "VIEW" {
		t.Errorf("objects = %+v, %v", objs, err)
	}
	cols, err := s.Introspect(ctx, IntrospectionRequest{Schema: "sales", Object: "orders"})
	if err != nil || len(cols.Columns) != 2 || !strings.HasPrefix(cols.DDL, "CREATE TABLE orders") {
		t.Errorf("columns = %+v, %v", cols, err)
	}
	d := TrinoDialect{}
	if d.QuoteIdent(`a"b`) != `"a""b"` || d.Placeholder(3) != "?" || d.Limit(0, 0) != "" || d.Limit(5, 0) != " LIMIT 5" {
		t.Error("TrinoDialect basics")
	}
	if q, _ := d.DDL("t"); !strings.HasPrefix(q, "SHOW CREATE TABLE") {
		t.Errorf("DDL = %q", q)
	}
	if d.Explain("SELECT 1", true) != "EXPLAIN ANALYZE SELECT 1" || d.Explain("SELECT 1", false) != "EXPLAIN SELECT 1" {
		t.Error("Explain")
	}
	if d.ReadOnlyBegin() == "" || len(d.CatalogQueries()) == 0 {
		t.Error("catalog queries and read-only begin")
	}
}

func TestTrinoIntrospectFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.HasPrefix(string(b), "SELECT 1") {
			_, _ = io.WriteString(w, `{"columns":[{"name":"c","type":"integer"}],"data":[[1]]}`)
			return
		}
		_, _ = io.WriteString(w, `{"error":{"message":"denied"}}`)
	}))
	defer srv.Close()
	s, err := NewTrinoSession(context.Background(), trinoConnOf(t, srv), "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for name, req := range map[string]IntrospectionRequest{
		"schemas": {}, "objects": {Schema: "s"}, "columns": {Schema: "s", Object: "t"},
	} {
		if _, err := s.Introspect(context.Background(), req); err == nil || !strings.Contains(err.Error(), "denied") {
			t.Errorf("%s = %v", name, err)
		}
	}
}
