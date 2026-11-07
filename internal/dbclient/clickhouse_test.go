package dbclient

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeClickHouse is a tiny ClickHouse HTTP interface: it answers the
// queries the session sends with canned JSONCompactEachRowWithNamesAndTypes
// bodies and records every request.
type fakeClickHouse struct {
	mu   sync.Mutex
	reqs []fakeCHReq
}
type fakeCHReq struct {
	query, user, key string
	params           map[string]string
}

func (f *fakeClickHouse) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	p := map[string]string{}
	for k, v := range r.URL.Query() {
		p[k] = v[0]
	}
	f.mu.Lock()
	f.reqs = append(f.reqs, fakeCHReq{query: string(body), user: r.Header.Get("X-ClickHouse-User"), key: r.Header.Get("X-ClickHouse-Key"), params: p})
	f.mu.Unlock()
	q := string(body)
	switch {
	case r.Header.Get("X-ClickHouse-Key") != "pw":
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "Code: 516. DB::Exception: default: Authentication failed. (AUTHENTICATION_FAILED)")
	case q == "SELECT 1":
		_, _ = io.WriteString(w, "[\"1\"]\n[\"UInt8\"]\n[1]\n")
	case strings.Contains(q, "system.databases"):
		_, _ = io.WriteString(w, "[\"name\"]\n[\"String\"]\n[\"default\"]\n[\"system\"]\n")
	case strings.Contains(q, "system.tables"):
		_, _ = io.WriteString(w, "[\"name\",\"kind\"]\n[\"String\",\"String\"]\n[\"events\",\"BASE TABLE\"]\n[\"daily\",\"VIEW\"]\n")
	case strings.Contains(q, "system.columns"):
		_, _ = io.WriteString(w, "[\"name\",\"type\",\"pk\"]\n[\"String\",\"String\",\"UInt8\"]\n[\"id\",\"UInt64\",1]\n[\"tags\",\"Array(String)\",0]\n")
	case strings.HasPrefix(q, "SHOW CREATE TABLE"):
		_, _ = io.WriteString(w, "[\"statement\"]\n[\"String\"]\n[\"CREATE TABLE default.events (id UInt64) ENGINE = MergeTree ORDER BY id\"]\n")
	case strings.HasPrefix(q, "SELECT * FROM events"):
		_, _ = io.WriteString(w, "[\"id\",\"tags\",\"note\"]\n[\"UInt64\",\"Array(String)\",\"Nullable(String)\"]\n[\"1\",[\"a\",\"b\"],null]\n[\"2\",[],\"x\"]\n[\"3\",[],\"y\"]\n")
	case strings.HasPrefix(q, "INSERT"):
		w.Header().Set("X-ClickHouse-Summary", `{"written_rows":"2"}`)
	case strings.HasPrefix(q, "BROKEN"):
		_, _ = io.WriteString(w, "[\"x\"]\n[\"UInt8\"]\n[1]\nCode: 395. DB::Exception: boom. (FUNCTION_THROW_IF_VALUE_IS_NON_ZERO)\n")
	default:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "Code: 62. DB::Exception: Syntax error. (SYNTAX_ERROR)")
	}
}

func (f *fakeClickHouse) last() fakeCHReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1]
}

func newFakeClickHouse(t *testing.T) (*fakeClickHouse, Conn) {
	t.Helper()
	f := &fakeClickHouse{}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	n, _ := strconv.Atoi(port)
	return f, Conn{ID: "ch", Driver: "clickhouse", Host: host, Port: n, User: "default", Database: "default"}
}

func TestClickHouseSessionPingSendsCredentialsInHeaders(t *testing.T) {
	f, c := newFakeClickHouse(t)
	s, err := NewClickHouseSession(context.Background(), c, "pw")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := f.last()
	if r.user != "default" || r.key != "pw" {
		t.Fatalf("credentials = %q/%q, want headers default/pw", r.user, r.key)
	}
	for k, v := range r.params {
		if strings.Contains(v, "pw") {
			t.Fatalf("password leaked into the URL param %s=%q", k, v)
		}
	}
	if r.params["database"] != "default" {
		t.Fatalf("database param = %q", r.params["database"])
	}
}

func TestClickHouseSessionWrongPasswordIsAnError(t *testing.T) {
	_, c := newFakeClickHouse(t)
	if _, err := NewClickHouseSession(context.Background(), c, "nope"); err == nil || !strings.Contains(err.Error(), "Authentication failed") {
		t.Fatalf("err = %v, want the server's authentication error", err)
	}
}

func TestClickHouseRunReadsRowsAndTruncatesAtMaxRows(t *testing.T) {
	_, c := newFakeClickHouse(t)
	s, err := NewClickHouseSession(context.Background(), c, "pw")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := s.Run(context.Background(), "SELECT * FROM events", RunOptions{MaxRows: 2})
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if len(r.Columns) != 3 || r.Columns[1].Name != "tags" || r.Columns[1].Type != "Array(String)" {
		t.Fatalf("columns = %+v", r.Columns)
	}
	if len(r.Rows) != 2 || !r.Truncated {
		t.Fatalf("rows = %d truncated = %v, want 2 and true", len(r.Rows), r.Truncated)
	}
	if r.Rows[0][0].Text != "1" || r.Rows[0][1].Text != `["a","b"]` || !r.Rows[0][2].Null {
		t.Fatalf("row 0 = %+v", r.Rows[0])
	}
}

func TestClickHouseRunBindsArgsAsQueryParams(t *testing.T) {
	f, c := newFakeClickHouse(t)
	s, err := NewClickHouseSession(context.Background(), c, "pw")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.Run(context.Background(), "SELECT * FROM events WHERE id = {p1:String}", RunOptions{Args: []any{42}})
	if got := f.last().params["param_p1"]; got != "42" {
		t.Fatalf("param_p1 = %q, want 42", got)
	}
}

func TestClickHouseRunErrorsAndWrites(t *testing.T) {
	_, c := newFakeClickHouse(t)
	s, err := NewClickHouseSession(context.Background(), c, "pw")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if r := s.Run(context.Background(), "SELEKT", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "SYNTAX_ERROR") {
		t.Fatalf("bad query err = %v", r.Err)
	}
	if r := s.Run(context.Background(), "BROKEN", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "boom") {
		t.Fatalf("mid-stream err = %v", r.Err)
	}
	if r := s.Run(context.Background(), "INSERT INTO events VALUES (1),(2)", RunOptions{}); r.Err != nil || r.Affected != 2 {
		t.Fatalf("insert = %+v", r)
	}
}

func TestClickHouseReadOnlySessionIsServerEnforced(t *testing.T) {
	f, c := newFakeClickHouse(t)
	s, err := NewClickHouseSession(context.Background(), c, "pw")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	_ = ro.Run(context.Background(), "SELECT 1", RunOptions{})
	if got := f.last().params["readonly"]; got != "2" {
		t.Fatalf("readonly param = %q, want 2 (server read-only)", got)
	}
	c.ReadOnly = true
	s2, err := NewClickHouseSession(context.Background(), c, "pw")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if got := f.last().params["readonly"]; got != "2" {
		t.Fatalf("readOnly conn: readonly param = %q, want 2", got)
	}
}

func TestClickHouseHasNoTransactions(t *testing.T) {
	_, c := newFakeClickHouse(t)
	s, err := NewClickHouseSession(context.Background(), c, "pw")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Begin(context.Background()); err == nil {
		t.Fatal("Begin must fail: ClickHouse has no transactions")
	}
}

func TestClickHouseIntrospect(t *testing.T) {
	_, c := newFakeClickHouse(t)
	s, err := NewClickHouseSession(context.Background(), c, "pw")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	dbs, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || strings.Join(dbs.Databases, ",") != "default,system" {
		t.Fatalf("databases = %v %v", dbs.Databases, err)
	}
	objs, err := s.Introspect(ctx, IntrospectionRequest{Database: "default", Schema: "default"})
	if err != nil || len(objs.Objects) != 2 || objs.Objects[1] != (SchemaObject{Name: "daily", Kind: "VIEW"}) {
		t.Fatalf("objects = %+v %v", objs.Objects, err)
	}
	tbl, err := s.Introspect(ctx, IntrospectionRequest{Database: "default", Schema: "default", Object: "events"})
	if err != nil || len(tbl.Columns) != 2 || !tbl.Columns[0].PK || tbl.Columns[1].PK || strings.Join(tbl.Keys, ",") != "id" {
		t.Fatalf("table = %+v %v", tbl, err)
	}
	if !strings.HasPrefix(tbl.DDL, "CREATE TABLE default.events") {
		t.Fatalf("ddl = %q", tbl.DDL)
	}
}

func TestClickHouseConfigIsAccepted(t *testing.T) {
	c := Conn{ID: "ch", Driver: "clickhouse", Host: "localhost", Port: 8123}
	if err := Validate(c); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	if defaultPort("clickhouse") != 8123 {
		t.Fatalf("default port = %d", defaultPort("clickhouse"))
	}
	if DialectFor("clickhouse") == nil {
		t.Fatal("no ClickHouse dialect")
	}
	if got := (ClickHouseDialect{}).QuoteIdent("a`b"); got != "`a\\`b`" {
		t.Fatalf("QuoteIdent = %q", got)
	}
}
