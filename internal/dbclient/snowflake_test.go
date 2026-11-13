package dbclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeSnowflake is the SQL API of one account: statements answer from
// a table of SQL prefixes, the first one async (202 then a poll), and a
// big result comes in two partitions.
type fakeSnowflake struct {
	mu    sync.Mutex
	bodys []map[string]any
	auth  []string
	polls int
}

func (f *fakeSnowflake) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auth = append(f.auth, r.Header.Get("Authorization")+" "+r.Header.Get("X-Snowflake-Authorization-Token-Type"))
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v2/statements/h-slow":
			f.polls++
			_, _ = io.WriteString(w, `{"code":"090001","statementHandle":"h-slow","resultSetMetaData":{"numRows":1,"rowType":[{"name":"N","type":"fixed"}],"partitionInfo":[{"rowCount":1}]},"data":[["42"]]}`)
			return
		case r.Method == "GET" && r.URL.Path == "/api/v2/statements/h-big" && r.URL.Query().Get("partition") == "1":
			_, _ = io.WriteString(w, `{"data":[["3"],[null]]}`)
			return
		case r.Method != "POST" || r.URL.Path != "/api/v2/statements":
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(404)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.bodys = append(f.bodys, body)
		stmt, _ := body["statement"].(string)
		switch {
		case stmt == "SELECT SLOW":
			w.WriteHeader(202)
			_, _ = io.WriteString(w, `{"code":"333334","statementHandle":"h-slow","statementStatusUrl":"/api/v2/statements/h-slow"}`)
		case stmt == "SELECT BIG":
			_, _ = io.WriteString(w, `{"code":"090001","statementHandle":"h-big","resultSetMetaData":{"numRows":4,"rowType":[{"name":"N","type":"fixed"}],"partitionInfo":[{"rowCount":2},{"rowCount":2}]},"data":[["1"],["2"]]}`)
		case strings.HasPrefix(stmt, "INSERT"):
			_, _ = io.WriteString(w, `{"code":"090001","statementHandle":"h-ins","resultSetMetaData":{"numRows":1,"rowType":[{"name":"number of rows inserted","type":"fixed"}]},"data":[["2"]],"stats":{"numRowsInserted":2}}`)
		case strings.HasPrefix(stmt, "SELEC "):
			w.WriteHeader(422)
			_, _ = io.WriteString(w, `{"code":"001003","message":"SQL compilation error: syntax error line 1 at position 0 unexpected 'SELEC'."}`)
		case strings.HasPrefix(stmt, "SHOW TERSE DATABASES"):
			_, _ = io.WriteString(w, `{"code":"090001","resultSetMetaData":{"numRows":2,"rowType":[{"name":"created_on"},{"name":"name"}]},"data":[["x","ANALYTICS"],["x","RAW"]]}`)
		case strings.HasPrefix(stmt, "SHOW TERSE SCHEMAS IN DATABASE"):
			_, _ = io.WriteString(w, `{"code":"090001","resultSetMetaData":{"numRows":1,"rowType":[{"name":"created_on"},{"name":"name"}]},"data":[["x","PUBLIC"]]}`)
		case strings.Contains(stmt, "INFORMATION_SCHEMA.TABLES"):
			_, _ = io.WriteString(w, `{"code":"090001","resultSetMetaData":{"numRows":1,"rowType":[{"name":"TABLE_NAME"},{"name":"TABLE_TYPE"}]},"data":[["ORDERS","BASE TABLE"]]}`)
		case strings.Contains(stmt, "INFORMATION_SCHEMA.COLUMNS"):
			_, _ = io.WriteString(w, `{"code":"090001","resultSetMetaData":{"numRows":2,"rowType":[{"name":"COLUMN_NAME"},{"name":"DATA_TYPE"}]},"data":[["ID","NUMBER"],["NOTE","TEXT"]]}`)
		case strings.HasPrefix(stmt, "SELECT GET_DDL"):
			_, _ = io.WriteString(w, `{"code":"090001","resultSetMetaData":{"numRows":1,"rowType":[{"name":"DDL"}]},"data":[["create or replace TABLE ORDERS (ID NUMBER);"]]}`)
		default:
			_, _ = io.WriteString(w, `{"code":"090001","statementHandle":"h1","resultSetMetaData":{"numRows":2,"rowType":[{"name":"ID","type":"fixed"},{"name":"NOTE","type":"text"}]},"data":[["1","hi"],["2",null]]}`)
		}
	}
}

func snowflakeTestSession(t *testing.T, f *fakeSnowflake, params map[string]string) Session {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	sess, err := OpenHTTPEngine(Conn{Driver: "snowflake", URL: srv.URL, Database: "ANALYTICS", Params: params}, "pat-secret-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// DBX-66: Snowflake over its SQL API: the token as a bearer
// (programmatic access token by default), the context from the
// connection, async statements polled, partitions read until MaxRows,
// the server's error text, and the tree from SHOW and
// INFORMATION_SCHEMA.
func TestSnowflakeSQLAPI(t *testing.T) {
	f := &fakeSnowflake{}
	s := snowflakeTestSession(t, f, map[string]string{"warehouse": "WH", "schema": "PUBLIC", "role": "ANALYST"})
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	res := s.Run(ctx, "SELECT id, note FROM orders WHERE id = ?", RunOptions{Args: []any{7}})
	if res.Err != nil || len(res.Columns) != 2 || res.Columns[0].Name != "ID" || len(res.Rows) != 2 || res.Rows[0][1].Text != "hi" || !res.Rows[1][1].Null {
		t.Fatalf("select = %+v", res)
	}
	f.mu.Lock()
	b := f.bodys[len(f.bodys)-1]
	if b["warehouse"] != "WH" || b["database"] != "ANALYTICS" || b["schema"] != "PUBLIC" || b["role"] != "ANALYST" {
		t.Errorf("context = %v", b)
	}
	if bind, _ := b["bindings"].(map[string]any); bind == nil || bind["1"] == nil {
		t.Errorf("bindings = %v", b["bindings"])
	}
	if f.auth[0] != "Bearer pat-secret-1 PROGRAMMATIC_ACCESS_TOKEN" {
		t.Errorf("auth = %q", f.auth[0])
	}
	f.mu.Unlock()

	if r := s.Run(ctx, "SELECT SLOW", RunOptions{}); r.Err != nil || len(r.Rows) != 1 || r.Rows[0][0].Text != "42" {
		t.Fatalf("async = %+v", r)
	}
	if r := s.Run(ctx, "SELECT BIG", RunOptions{}); r.Err != nil || len(r.Rows) != 4 || r.Rows[2][0].Text != "3" || !r.Rows[3][0].Null {
		t.Fatalf("partitions = %+v", r)
	}
	if r := s.Run(ctx, "SELECT BIG", RunOptions{MaxRows: 2}); len(r.Rows) != 2 || !r.Truncated {
		t.Fatalf("MaxRows = %+v", r)
	}
	if r := s.Run(ctx, "INSERT INTO t VALUES (1), (2)", RunOptions{}); r.Err != nil || r.Affected != 2 {
		t.Fatalf("insert = %+v", r)
	}
	if r := s.Run(ctx, "SELEC 1", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "SQL compilation error") || strings.Contains(r.Err.Error(), "pat-secret-1") {
		t.Fatalf("error = %v", r.Err)
	}

	in, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || strings.Join(in.Databases, ",") != "ANALYTICS,RAW" {
		t.Fatalf("databases = %v %v", in.Databases, err)
	}
	if in, err = s.Introspect(ctx, IntrospectionRequest{Database: "ANALYTICS"}); err != nil || strings.Join(in.Schemas, ",") != "PUBLIC" {
		t.Fatalf("schemas = %v %v", in.Schemas, err)
	}
	if in, err = s.Introspect(ctx, IntrospectionRequest{Database: "ANALYTICS", Schema: "PUBLIC"}); err != nil || len(in.Objects) != 1 || in.Objects[0].Name != "ORDERS" {
		t.Fatalf("objects = %v %v", in.Objects, err)
	}
	if in, err = s.Introspect(ctx, IntrospectionRequest{Database: "ANALYTICS", Schema: "PUBLIC", Object: "ORDERS"}); err != nil || len(in.Columns) != 2 || in.Columns[1].Type != "TEXT" || !strings.Contains(in.DDL, "TABLE ORDERS") {
		t.Fatalf("columns = %+v %v", in, err)
	}

	// The read-only twin refuses a write before it reaches the account.
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	n := len(f.bodys)
	f.mu.Unlock()
	if r := ro.Run(ctx, "DELETE FROM orders", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "read-only") {
		t.Fatalf("read-only write = %v", r.Err)
	}
	if r := ro.Run(ctx, "SELECT 1", RunOptions{}); r.Err != nil {
		t.Fatalf("read-only read = %v", r.Err)
	}
	f.mu.Lock()
	if len(f.bodys) != n+1 {
		t.Errorf("the refused write reached the server: %d requests, want %d", len(f.bodys), n+1)
	}
	f.mu.Unlock()
}

// A snowflake:// URL keeps its account host on TLS, the database and
// schema from the path and the warehouse and role from the query.
func TestSnowflakeURL(t *testing.T) {
	c, err := ParseURL("snowflake://ada@xy12345.eu-central-1.snowflakecomputing.com/ANALYTICS/PUBLIC?warehouse=WH&role=ANALYST")
	if err != nil {
		t.Fatal(err)
	}
	if c.Driver != "snowflake" || !c.TLS || c.Port != 443 || c.Host != "xy12345.eu-central-1.snowflakecomputing.com" || c.User != "ada" ||
		c.Database != "ANALYTICS" || c.Params["schema"] != "PUBLIC" || c.Params["warehouse"] != "WH" || c.Params["role"] != "ANALYST" {
		t.Fatalf("conn = %+v", c)
	}
}
