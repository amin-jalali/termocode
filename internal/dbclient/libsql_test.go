package dbclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeLibSQL is a tiny remote libSQL server (the HTTP pipeline API,
// v2): it answers a few fixed statements and records what it got.
type fakeLibSQL struct {
	mu      sync.Mutex
	auth    []string
	sqls    []string
	batons  []string
	args    [][]libsqlValue
	nextBat int
}

func (f *fakeLibSQL) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/pipeline" || r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		var req libsqlPipelineReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad request body: %v", err)
			w.WriteHeader(400)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.batons = append(f.batons, req.Baton)
		resp := libsqlPipelineResp{}
		closed := false
		for _, rq := range req.Requests {
			switch rq.Type {
			case "close":
				closed = true
				resp.Results = append(resp.Results, libsqlStreamResult{Type: "ok", Response: &libsqlStreamResponse{Type: "close"}})
				continue
			case "execute":
			default:
				t.Errorf("unknown request type %q", rq.Type)
				continue
			}
			sql := rq.Stmt.SQL
			f.sqls = append(f.sqls, sql)
			f.args = append(f.args, rq.Stmt.Args)
			res := &libsqlExecResult{}
			switch {
			case strings.HasPrefix(sql, "SELECT id, name, score, pic FROM users"):
				res.Cols = []libsqlCol{{Name: "id", Decltype: "INTEGER"}, {Name: "name", Decltype: "TEXT"}, {Name: "score", Decltype: "REAL"}, {Name: "pic", Decltype: "BLOB"}}
				res.Rows = [][]libsqlValue{
					{{Type: "integer", Value: json.RawMessage(`"1"`)}, {Type: "text", Value: json.RawMessage(`"ann"`)}, {Type: "float", Value: json.RawMessage(`2.5`)}, {Type: "blob", Base64: "AQI="}},
					{{Type: "integer", Value: json.RawMessage(`"2"`)}, {Type: "null"}, {Type: "null"}, {Type: "null"}},
					{{Type: "integer", Value: json.RawMessage(`"3"`)}, {Type: "text", Value: json.RawMessage(`"cy"`)}, {Type: "float", Value: json.RawMessage(`1`)}, {Type: "null"}},
				}
			case strings.HasPrefix(sql, "UPDATE"):
				res.AffectedRowCount = 4
			case strings.HasPrefix(sql, "PRAGMA database_list"):
				res.Cols = []libsqlCol{{Name: "seq"}, {Name: "name"}, {Name: "file"}}
				res.Rows = [][]libsqlValue{{{Type: "integer", Value: json.RawMessage(`"0"`)}, {Type: "text", Value: json.RawMessage(`"main"`)}, {Type: "text", Value: json.RawMessage(`""`)}}}
			case strings.Contains(sql, "FROM sqlite_master WHERE type IN"):
				res.Cols = []libsqlCol{{Name: "name"}, {Name: "type"}}
				res.Rows = [][]libsqlValue{{{Type: "text", Value: json.RawMessage(`"users"`)}, {Type: "text", Value: json.RawMessage(`"table"`)}}}
			case strings.HasPrefix(sql, "BAD"):
				resp.Results = append(resp.Results, libsqlStreamResult{Type: "error", Error: &libsqlError{Message: "SQLite error: near \"BAD\": syntax error", Code: "SQLITE_ERROR"}})
				continue
			}
			resp.Results = append(resp.Results, libsqlStreamResult{Type: "ok", Response: &libsqlStreamResponse{Type: "execute", Result: res}})
		}
		if !closed {
			f.nextBat++
			resp.Baton = "b" + string(rune('0'+f.nextBat))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

func newFakeLibSQL(t *testing.T) (*fakeLibSQL, *httptest.Server) {
	t.Helper()
	f := &fakeLibSQL{}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	return f, srv
}

func TestParseURLLibSQLKeepsTheTokenOutOfTheURL(t *testing.T) {
	c, err := ParseURL("libsql://mydb-org.turso.io?authToken=" + "tok" + "123")
	if err != nil {
		t.Fatal(err)
	}
	if c.Driver != "libsql" || c.Host != "mydb-org.turso.io" {
		t.Fatalf("conn = %+v", c)
	}
	if c.Password != "tok123" {
		t.Fatalf("token not taken as the password: %q", c.Password)
	}
	if strings.Contains(c.URL, "tok123") || strings.Contains(c.URL, "authToken") {
		t.Fatalf("token stays in the URL: %q", c.URL)
	}
}

func TestLibSQLHTTPBase(t *testing.T) {
	for in, want := range map[string]string{
		"libsql://db.turso.io":          "https://db.turso.io",
		"libsql://db.turso.io/":         "https://db.turso.io",
		"https://db.example.com:8443":   "https://db.example.com:8443",
		"http://127.0.0.1:8080":         "http://127.0.0.1:8080",
		"wss://db.turso.io":             "https://db.turso.io",
		"libsql://127.0.0.1:8080?tls=0": "http://127.0.0.1:8080",
	} {
		got, err := libsqlHTTPBase(Conn{Driver: "libsql", URL: in})
		if err != nil || got != want {
			t.Errorf("libsqlHTTPBase(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := libsqlHTTPBase(Conn{Driver: "libsql", URL: "ftp://x"}); err == nil {
		t.Error("ftp URL accepted")
	}
}

func TestLibSQLRunSelectTypesAndToken(t *testing.T) {
	f, srv := newFakeLibSQL(t)
	s, err := NewLibSQLSession(Conn{Driver: "libsql", URL: srv.URL}, "secret-token")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := s.Run(context.Background(), "SELECT id, name, score, pic FROM users", RunOptions{MaxRows: 2})
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if len(r.Columns) != 4 || r.Columns[0].Name != "id" || r.Columns[0].Type != "INTEGER" {
		t.Fatalf("columns = %+v", r.Columns)
	}
	if len(r.Rows) != 2 || !r.Truncated {
		t.Fatalf("rows = %d truncated = %v, want 2 and true", len(r.Rows), r.Truncated)
	}
	row := r.Rows[0]
	if row[0].Text != "1" || row[1].Text != "ann" || row[2].Text != "2.5" || row[3].Text != "0x0102" || row[3].Kind != "binary" {
		t.Fatalf("row 0 = %+v", row)
	}
	if !r.Rows[1][1].Null {
		t.Fatalf("null cell = %+v", r.Rows[1][1])
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.auth) == 0 || f.auth[0] != "Bearer secret-token" {
		t.Fatalf("auth = %q", f.auth)
	}
}

func TestLibSQLRunArgsAffectedAndErrors(t *testing.T) {
	f, srv := newFakeLibSQL(t)
	s, _ := NewLibSQLSession(Conn{Driver: "libsql", URL: srv.URL}, "")
	defer s.Close()
	r := s.Run(context.Background(), "UPDATE users SET name = ? WHERE id = ?", RunOptions{Args: []any{"bo", 7, nil, 1.5}})
	if r.Err != nil || r.Affected != 4 {
		t.Fatalf("update = %+v", r)
	}
	f.mu.Lock()
	got := f.args[len(f.args)-1]
	f.mu.Unlock()
	if len(got) != 4 || got[0].Type != "text" || got[1].Type != "integer" || string(got[1].Value) != `"7"` || got[2].Type != "null" || got[3].Type != "float" {
		t.Fatalf("args = %+v", got)
	}
	bad := s.Run(context.Background(), "BAD SQL", RunOptions{})
	if bad.Err == nil || !strings.Contains(bad.Err.Error(), "syntax error") {
		t.Fatalf("error = %v", bad.Err)
	}
	if f.auth[0] != "" {
		t.Fatalf("empty token still sent: %q", f.auth[0])
	}
}

func TestLibSQLTransactionKeepsOneStream(t *testing.T) {
	f, srv := newFakeLibSQL(t)
	s, _ := NewLibSQLSession(Conn{Driver: "libsql", URL: srv.URL}, "")
	defer s.Close()
	ctx := context.Background()
	if err := s.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	if r := s.Run(ctx, "UPDATE users SET name = 'x'", RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if err := s.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	s.Run(ctx, "UPDATE users SET name = 'y'", RunOptions{})
	f.mu.Lock()
	defer f.mu.Unlock()
	// BEGIN opens a stream; the UPDATE and COMMIT send the newest baton
	// the server gave; the next statement outside the transaction starts
	// a new stream.
	if want := []string{"", "b1", "b2", ""}; strings.Join(f.batons, ",") != strings.Join(want, ",") {
		t.Fatalf("batons = %q", f.batons)
	}
	if f.sqls[0] != "BEGIN" || f.sqls[2] != "COMMIT" {
		t.Fatalf("sqls = %q", f.sqls)
	}
}

func TestLibSQLReadOnlySessionTurnsOnQueryOnly(t *testing.T) {
	f, srv := newFakeLibSQL(t)
	s, _ := NewLibSQLSession(Conn{Driver: "libsql", URL: srv.URL}, "")
	defer s.Close()
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	ro.Run(context.Background(), "SELECT id, name, score, pic FROM users", RunOptions{})
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sqls) < 2 || f.sqls[0] != "PRAGMA query_only = 1" {
		t.Fatalf("sqls = %q, want PRAGMA query_only first", f.sqls)
	}
}

func TestLibSQLPingAndIntrospect(t *testing.T) {
	_, srv := newFakeLibSQL(t)
	s, _ := NewLibSQLSession(Conn{Driver: "libsql", URL: srv.URL}, "")
	defer s.Close()
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	res, err := s.Introspect(context.Background(), IntrospectionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Databases) != 1 || res.Databases[0] != "main" || len(res.Objects) != 1 || res.Objects[0].Name != "users" {
		t.Fatalf("introspect = %+v", res)
	}
}

func TestLibSQLErrorNeverShowsTheToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"bad token ` + r.Header.Get("Authorization") + `"}`))
	}))
	defer srv.Close()
	s, _ := NewLibSQLSession(Conn{Driver: "libsql", URL: srv.URL}, "tok-"+"abc")
	defer s.Close()
	err := s.Ping(context.Background())
	if err == nil {
		t.Fatal("401 not reported")
	}
	if strings.Contains(err.Error(), "tok-abc") {
		t.Fatalf("token in error: %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v, want the status", err)
	}
}
