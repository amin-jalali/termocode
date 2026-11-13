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

// fakeDatabricks is the SQL Statement Execution API of one workspace.
type fakeDatabricks struct {
	mu     sync.Mutex
	bodys  []map[string]any
	auth   []string
	cancel []string
}

func (f *fakeDatabricks) handler(t *testing.T) http.HandlerFunc {
	ok := func(cols, rows string) string {
		return `{"statement_id":"s1","status":{"state":"SUCCEEDED"},"manifest":{"schema":{"columns":[` + cols + `]}},"result":{"data_array":` + rows + `}}`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/2.0/sql/statements/s-slow":
			_, _ = io.WriteString(w, `{"statement_id":"s-slow","status":{"state":"SUCCEEDED"},"manifest":{"schema":{"columns":[{"name":"n","type_name":"INT"}]}},"result":{"data_array":[["42"]]}}`)
			return
		case r.Method == "GET" && r.URL.Path == "/api/2.0/sql/statements/s-big/result/chunks/1":
			_, _ = io.WriteString(w, `{"data_array":[["3"],[null]]}`)
			return
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/cancel"):
			f.cancel = append(f.cancel, r.URL.Path)
			return
		case r.Method != "POST" || r.URL.Path != "/api/2.0/sql/statements":
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
			_, _ = io.WriteString(w, `{"statement_id":"s-slow","status":{"state":"PENDING"}}`)
		case stmt == "SELECT BIG":
			_, _ = io.WriteString(w, `{"statement_id":"s-big","status":{"state":"SUCCEEDED"},"manifest":{"schema":{"columns":[{"name":"n","type_name":"INT"}]}},"result":{"data_array":[["1"],["2"]],"next_chunk_internal_link":"/api/2.0/sql/statements/s-big/result/chunks/1"}}`)
		case strings.HasPrefix(stmt, "SELEC "):
			_, _ = io.WriteString(w, `{"statement_id":"s-bad","status":{"state":"FAILED","error":{"error_code":"BAD_REQUEST","message":"[PARSE_SYNTAX_ERROR] Syntax error at or near 'SELEC'."}}}`)
		case strings.HasPrefix(stmt, "INSERT"):
			_, _ = io.WriteString(w, ok(`{"name":"num_affected_rows","type_name":"LONG"},{"name":"num_inserted_rows","type_name":"LONG"}`, `[["2","2"]]`))
		case stmt == "SHOW CATALOGS":
			_, _ = io.WriteString(w, ok(`{"name":"catalog","type_name":"STRING"}`, `[["main"],["samples"]]`))
		case strings.HasPrefix(stmt, "SHOW SCHEMAS IN"):
			_, _ = io.WriteString(w, ok(`{"name":"databaseName","type_name":"STRING"}`, `[["default"],["sales"]]`))
		case strings.HasPrefix(stmt, "SHOW TABLES IN"):
			_, _ = io.WriteString(w, ok(`{"name":"database","type_name":"STRING"},{"name":"tableName","type_name":"STRING"},{"name":"isTemporary","type_name":"BOOLEAN"}`, `[["sales","orders","false"]]`))
		case strings.Contains(stmt, "information_schema.columns"):
			_, _ = io.WriteString(w, ok(`{"name":"column_name","type_name":"STRING"},{"name":"data_type","type_name":"STRING"}`, `[["id","BIGINT"],["note","STRING"]]`))
		case strings.HasPrefix(stmt, "SHOW CREATE TABLE"):
			_, _ = io.WriteString(w, ok(`{"name":"createtab_stmt","type_name":"STRING"}`, `[["CREATE TABLE main.sales.orders (id BIGINT)"]]`))
		default:
			_, _ = io.WriteString(w, ok(`{"name":"id","type_name":"INT"},{"name":"note","type_name":"STRING"}`, `[["1","hi"],["2",null]]`))
		}
	}
}

// DBX-66: Databricks SQL over the Statement Execution API: a personal
// access token as a bearer, the warehouse, catalog and schema from the
// connection, a pending statement polled, chunks followed to MaxRows,
// the server's error, named parameters, the tree from SHOW, and a
// read-only twin.
func TestDatabricksStatementAPI(t *testing.T) {
	f := &fakeDatabricks{}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	s, err := OpenHTTPEngine(Conn{Driver: "databricks", URL: srv.URL, Database: "main", Params: map[string]string{"warehouse": "wh-1", "schema": "sales"}}, "dapi-secret-1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	res := s.Run(ctx, "SELECT id, note FROM orders WHERE id = ?", RunOptions{Args: []any{7}})
	if res.Err != nil || len(res.Columns) != 2 || res.Columns[1].Type != "STRING" || len(res.Rows) != 2 || res.Rows[0][1].Text != "hi" || !res.Rows[1][1].Null {
		t.Fatalf("select = %+v", res)
	}
	f.mu.Lock()
	b := f.bodys[len(f.bodys)-1]
	if b["warehouse_id"] != "wh-1" || b["catalog"] != "main" || b["schema"] != "sales" || b["disposition"] != "INLINE" || b["format"] != "JSON_ARRAY" {
		t.Errorf("request = %v", b)
	}
	if stmt := b["statement"].(string); !strings.Contains(stmt, ":p1") {
		t.Errorf("placeholder not named: %q", stmt)
	}
	if ps, _ := b["parameters"].([]any); len(ps) != 1 {
		t.Errorf("parameters = %v", b["parameters"])
	}
	if f.auth[0] != "Bearer dapi-secret-1" {
		t.Errorf("auth = %q", f.auth[0])
	}
	f.mu.Unlock()
	if r := s.Run(ctx, "SELECT SLOW", RunOptions{}); r.Err != nil || len(r.Rows) != 1 || r.Rows[0][0].Text != "42" {
		t.Fatalf("pending = %+v", r)
	}
	if r := s.Run(ctx, "SELECT BIG", RunOptions{}); r.Err != nil || len(r.Rows) != 4 || r.Rows[2][0].Text != "3" || !r.Rows[3][0].Null {
		t.Fatalf("chunks = %+v", r)
	}
	if r := s.Run(ctx, "SELECT BIG", RunOptions{MaxRows: 2}); len(r.Rows) != 2 || !r.Truncated {
		t.Fatalf("MaxRows = %+v", r)
	}
	if r := s.Run(ctx, "INSERT INTO t VALUES (1), (2)", RunOptions{}); r.Err != nil || r.Affected != 2 {
		t.Fatalf("insert = %+v", r)
	}
	if r := s.Run(ctx, "SELEC 1", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "PARSE_SYNTAX_ERROR") {
		t.Fatalf("error = %v", r.Err)
	}
	in, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || strings.Join(in.Databases, ",") != "main,samples" {
		t.Fatalf("catalogs = %v %v", in.Databases, err)
	}
	if in, err = s.Introspect(ctx, IntrospectionRequest{Database: "main"}); err != nil || strings.Join(in.Schemas, ",") != "default,sales" {
		t.Fatalf("schemas = %v %v", in.Schemas, err)
	}
	if in, err = s.Introspect(ctx, IntrospectionRequest{Database: "main", Schema: "sales"}); err != nil || len(in.Objects) != 1 || in.Objects[0].Name != "orders" {
		t.Fatalf("tables = %v %v", in.Objects, err)
	}
	if in, err = s.Introspect(ctx, IntrospectionRequest{Database: "main", Schema: "sales", Object: "orders"}); err != nil || len(in.Columns) != 2 || !strings.Contains(in.DDL, "CREATE TABLE") {
		t.Fatalf("columns = %+v %v", in, err)
	}
	ro, _ := s.ReadOnlySession()
	if r := ro.Run(ctx, "DROP TABLE orders", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "read-only") {
		t.Fatalf("read-only write = %v", r.Err)
	}
}

// A databricks:// URL: the workspace host on TLS, catalog/schema from
// the path, the warehouse from the query.
func TestDatabricksURL(t *testing.T) {
	c, err := ParseURL("databricks://dbc-1a2b.cloud.databricks.com/main/sales?warehouse=wh-1")
	if err != nil {
		t.Fatal(err)
	}
	if c.Driver != "databricks" || !c.TLS || c.Port != 443 || c.Database != "main" || c.Params["schema"] != "sales" || c.Params["warehouse"] != "wh-1" {
		t.Fatalf("conn = %+v", c)
	}
	if _, err := OpenHTTPEngine(Conn{Driver: "databricks", Host: "x.cloud.databricks.com"}, "t"); err == nil || !strings.Contains(err.Error(), "warehouse") {
		t.Fatalf("no warehouse: %v", err)
	}
}
