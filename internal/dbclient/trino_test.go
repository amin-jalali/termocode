package dbclient

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeTrino is a tiny coordinator: each query answers in two pages
// (columns first, then data through nextUri) and every request is kept.
type fakeTrino struct {
	srv   *httptest.Server
	mu    sync.Mutex
	reqs  []*http.Request
	sqls  []string
	nextX string // a nextUri to send instead of the real one
}

func newFakeTrino(t *testing.T) *fakeTrino {
	f := &fakeTrino{}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTrino) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, r.Clone(context.Background()))
	f.sqls = append(f.sqls, string(body))
	nextX := f.nextX
	f.mu.Unlock()
	page := map[string]any{"id": "q1"}
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodGet {
		page["columns"] = []map[string]string{{"name": "id", "type": "bigint"}, {"name": "tags", "type": "array(varchar)"}}
		page["data"] = [][]any{{1, []string{"a"}}, {2, nil}, {3, []string{}}}
		_ = json.NewEncoder(w).Encode(page)
		return
	}
	q := string(body)
	switch {
	case q == "START TRANSACTION READ ONLY" || q == "START TRANSACTION":
		w.Header().Set("X-Trino-Started-Transaction-Id", "tx-1")
	case q == "COMMIT" || q == "ROLLBACK":
		w.Header().Set("X-Trino-Clear-Transaction-Id", "true")
	case q == "USE hive.sales":
		w.Header().Set("X-Trino-Set-Catalog", "hive")
		w.Header().Set("X-Trino-Set-Schema", "sales")
	case q == "BAD":
		page["error"] = map[string]string{"message": "line 1:1: mismatched input 'BAD'", "errorName": "SYNTAX_ERROR"}
	case strings.HasPrefix(q, "SELECT * FROM t"):
		next := f.srv.URL + "/v1/statement/q1/1"
		if nextX != "" {
			next = nextX
		}
		page["nextUri"] = next
	case strings.HasPrefix(q, "INSERT"):
		page["updateType"] = "INSERT"
		page["updateCount"] = 2
	default:
		page["columns"] = []map[string]string{{"name": "_col0", "type": "integer"}}
		page["data"] = [][]any{{1}}
	}
	_ = json.NewEncoder(w).Encode(page)
}

func (f *fakeTrino) conn(t *testing.T) Conn {
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(f.srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	return Conn{ID: "tr", Driver: "trino", Host: host, Port: p, User: "ana", Database: "hive"}
}

func (f *fakeTrino) last() (*http.Request, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1], f.sqls[len(f.sqls)-1]
}

func TestTrinoRunFollowsNextURIAndSendsHeaders(t *testing.T) {
	f := newFakeTrino(t)
	s, err := NewTrinoSession(context.Background(), f.conn(t), "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := s.Run(context.Background(), "SELECT * FROM t;", RunOptions{})
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if len(r.Columns) != 2 || r.Columns[1].Type != "array(varchar)" || len(r.Rows) != 3 || !r.Rows[1][1].Null || r.Rows[0][1].Kind != "json" {
		t.Fatalf("result = %+v", r)
	}
	req, _ := f.last()
	if req.Method != http.MethodGet || req.Header.Get("X-Trino-User") != "ana" || req.Header.Get("X-Trino-Catalog") != "hive" {
		t.Fatalf("page request = %s %v", req.Method, req.Header)
	}
	f.mu.Lock()
	first := f.sqls[len(f.sqls)-2]
	f.mu.Unlock()
	if first != "SELECT * FROM t" {
		t.Fatalf("the trailing ; must go: %q", first)
	}
}

func TestTrinoTruncatesAndAbandons(t *testing.T) {
	f := newFakeTrino(t)
	s, err := NewTrinoSession(context.Background(), f.conn(t), "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := s.Run(context.Background(), "SELECT * FROM t", RunOptions{MaxRows: 2})
	if r.Err != nil || len(r.Rows) != 2 || !r.Truncated {
		t.Fatalf("result = %+v", r)
	}
}

func TestTrinoUseAndTransactions(t *testing.T) {
	f := newFakeTrino(t)
	s, err := NewTrinoSession(context.Background(), f.conn(t), "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if r := s.Run(context.Background(), "USE hive.sales", RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if err := s.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Run(context.Background(), "SELECT 1", RunOptions{})
	req, _ := f.last()
	if req.Header.Get("X-Trino-Schema") != "sales" || req.Header.Get("X-Trino-Transaction-Id") != "tx-1" {
		t.Fatalf("headers = %v", req.Header)
	}
	if err := s.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Run(context.Background(), "SELECT 1", RunOptions{})
	if req, _ := f.last(); req.Header.Get("X-Trino-Transaction-Id") != "NONE" {
		t.Fatalf("the transaction must be cleared: %v", req.Header)
	}
	if err := s.Rollback(context.Background()); err == nil {
		t.Fatal("rollback with no transaction must fail")
	}
	if r := s.Run(context.Background(), "INSERT INTO t VALUES (1), (2)", RunOptions{}); r.Err != nil || r.Affected != 2 {
		t.Fatalf("insert = %+v", r)
	}
}

func TestTrinoReadOnlyRunsInAReadOnlyTransaction(t *testing.T) {
	f := newFakeTrino(t)
	s, err := NewTrinoSession(context.Background(), f.conn(t), "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	ro.Run(context.Background(), "SELECT 1", RunOptions{})
	req, _ := f.last()
	if req.Header.Get("X-Trino-Transaction-Id") != "tx-1" {
		t.Fatalf("read-only run outside its transaction: %v", req.Header)
	}
	found := false
	f.mu.Lock()
	for _, q := range f.sqls {
		found = found || q == "START TRANSACTION READ ONLY"
	}
	f.mu.Unlock()
	if !found {
		t.Fatal("no START TRANSACTION READ ONLY")
	}
}

func TestTrinoErrorsAndSafety(t *testing.T) {
	f := newFakeTrino(t)
	c := f.conn(t)
	if _, err := NewTrinoSession(context.Background(), c, "secret"); err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("a password over plain HTTP must be refused: %v", err)
	}
	s, err := NewTrinoSession(context.Background(), c, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if r := s.Run(context.Background(), "BAD", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "mismatched input") {
		t.Fatalf("err = %v", r.Err)
	}
	f.mu.Lock()
	f.nextX = "http://evil.example/v1/statement/q1/1"
	f.mu.Unlock()
	if r := s.Run(context.Background(), "SELECT * FROM t", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "another host") {
		t.Fatalf("a nextUri on another host must stop: %v", r.Err)
	}
}

func TestPrestoUsesPrestoHeaders(t *testing.T) {
	var user string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user = r.Header.Get("X-Presto-User")
		_, _ = io.WriteString(w, `{"id":"q","columns":[{"name":"_col0","type":"integer"}],"data":[[1]]}`)
	}))
	defer srv.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	s, err := NewTrinoSession(context.Background(), Conn{Driver: "presto", Host: host, Port: p, User: "bo"}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if user != "bo" {
		t.Fatalf("X-Presto-User = %q", user)
	}
}

func TestTrinoWiring(t *testing.T) {
	for _, d := range []string{"trino", "presto"} {
		if _, ok := DialectFor(d).(TrinoDialect); !ok {
			t.Fatalf("%s has no dialect", d)
		}
		if defaultPort(d) != 8080 {
			t.Fatalf("%s port = %d", d, defaultPort(d))
		}
		if err := Validate(Conn{ID: "t", Driver: d, Host: "h", Port: 8080}); err != nil {
			t.Fatalf("%s: %v", d, err)
		}
	}
	if got := (TrinoDialect{}).Limit(10, 20); got != " OFFSET 20 LIMIT 10" {
		t.Fatalf("limit = %q", got)
	}
}
