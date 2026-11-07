package dbclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeCouchbase answers the query service: buckets, keyspaces, sample
// documents, indexes and one write.
func fakeCouchbase(t *testing.T, got *[]n1qlRequest) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/query/service" || r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		if u, p, ok := r.BasicAuth(); !ok || u != "admin" || p != "pw-1" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"errors":[{"code":10000,"msg":"Authentication failed"}],"status":"fatal"}`))
			return
		}
		var req n1qlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad body: %v", err)
		}
		*got = append(*got, req)
		st := req.Statement
		switch {
		case st == "SELECT 1":
			_, _ = w.Write([]byte(`{"results":[{"$1":1}],"status":"success"}`))
		case strings.Contains(st, "system:buckets"):
			_, _ = w.Write([]byte(`{"results":["beer","travel"],"status":"success"}`))
		case strings.Contains(st, "system:keyspaces"):
			_, _ = w.Write([]byte(`{"results":["inventory.airline","inventory.hotel"],"status":"success"}`))
		case strings.Contains(st, "system:indexes"):
			_, _ = w.Write([]byte(`{"results":["#primary","by_name"],"status":"success"}`))
		case strings.HasPrefix(st, "SELECT RAW d FROM"):
			_, _ = w.Write([]byte(`{"results":[{"name":"A","id":1},{"name":"B","country":{"code":"FR"}}],"status":"success"}`))
		case strings.HasPrefix(st, "UPSERT"):
			if req.ReadOnly {
				w.WriteHeader(500)
				_, _ = w.Write([]byte(`{"errors":[{"code":1000,"msg":"The server or request is read-only"}],"status":"errors"}`))
				return
			}
			_, _ = w.Write([]byte(`{"results":[],"status":"success","metrics":{"mutationCount":1}}`))
		default:
			_, _ = w.Write([]byte(`{"results":[{"name":"x","tags":["a","b"],"n":null}],"status":"success"}`))
		}
	}))
}

func TestCouchbaseSessionRunsN1QLAndBrowses(t *testing.T) {
	var got []n1qlRequest
	srv := fakeCouchbase(t, &got)
	defer srv.Close()
	s, err := NewCouchbaseSession(Conn{Driver: "couchbase", URL: srv.URL, User: "admin"}, "pw-1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	res := s.Run(ctx, "SELECT * FROM t WHERE a = $1", RunOptions{Args: []any{"v"}})
	if res.Err != nil || len(res.Columns) != 3 || res.Columns[1].Name != "tags" || res.Rows[0][1].Text != `["a","b"]` || !res.Rows[0][2].Null {
		t.Fatalf("run = %+v", res)
	}
	if last := got[len(got)-1]; len(last.Args) != 1 || last.Args[0] != "v" {
		t.Fatalf("args = %+v", last.Args)
	}
	in, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || strings.Join(in.Databases, ",") != "beer,travel" {
		t.Fatalf("buckets = %v %v", in.Databases, err)
	}
	in, err = s.Introspect(ctx, IntrospectionRequest{Database: "travel", Schema: "travel"})
	if err != nil || len(in.Objects) != 2 || in.Objects[0].Name != "inventory.airline" {
		t.Fatalf("collections = %+v %v", in.Objects, err)
	}
	in, err = s.Introspect(ctx, IntrospectionRequest{Database: "travel", Object: "inventory.airline"})
	if err != nil || len(in.Columns) != 3 || in.Columns[0].Name != "id" || !in.Columns[0].PK || in.Columns[2].Type != "object" || len(in.Indexes) != 2 {
		t.Fatalf("columns = %+v indexes %+v %v", in.Columns, in.Indexes, err)
	}
	if !strings.Contains(got[len(got)-2].Statement, "`travel`.`inventory`.`airline`") {
		t.Fatalf("sample = %q", got[len(got)-2].Statement)
	}
	if w := s.Run(ctx, "UPSERT INTO t VALUES ('k', {})", RunOptions{}); w.Err != nil || w.Affected != 1 {
		t.Fatalf("write = %+v", w)
	}
	if err := s.Begin(ctx); err == nil {
		t.Fatal("Begin should say there are no transactions")
	}
}

// The read-only twin sends readonly=true, so the server refuses writes.
func TestCouchbaseReadOnlyIsTheServerFlag(t *testing.T) {
	var got []n1qlRequest
	srv := fakeCouchbase(t, &got)
	defer srv.Close()
	s, _ := NewCouchbaseSession(Conn{Driver: "couchbase", URL: srv.URL, User: "admin"}, "pw-1")
	ro, _ := s.ReadOnlySession()
	res := ro.Run(context.Background(), "UPSERT INTO t VALUES ('k', {})", RunOptions{})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "read-only") || !got[0].ReadOnly {
		t.Fatalf("read-only write = %+v sent %+v", res, got)
	}
}

func TestCouchbaseWrongPasswordNeverShowsIt(t *testing.T) {
	var got []n1qlRequest
	srv := fakeCouchbase(t, &got)
	defer srv.Close()
	s, _ := NewCouchbaseSession(Conn{Driver: "couchbase", URL: srv.URL, User: "admin"}, "secret-pw")
	err := s.Ping(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret-pw") || !strings.Contains(err.Error(), "Authentication failed") {
		t.Fatalf("err = %v", err)
	}
}

// couchbase:// URLs and connections validate; the password leaves the URL.
func TestCouchbaseURLAndValidate(t *testing.T) {
	c, err := ParseURL("couchbases://admin:pw@cb.example.com/travel")
	if err != nil || c.Driver != "couchbase" || !c.TLS || c.Port != 18093 || c.Password != "pw" || c.URL != "" || c.Database != "travel" || c.User != "admin" {
		t.Fatalf("parse = %+v %v", c, err)
	}
	c.ID = "cb"
	c.Password = ""
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	if !IsHTTPEngine("couchbase") || defaultPort("couchbase") != 8093 {
		t.Fatal("couchbase is not registered")
	}
	conns, err := ParseConfig([]byte(`{"connections":[{"id":"cb","driver":"couchbase","host":"h","user":"u","password":"literal"}]}`), SourceProject, "f", "/r")
	if err != nil || len(conns) != 1 || conns[0].Problem == "" {
		t.Fatalf("a literal project password must be a problem: %+v %v", conns, err)
	}
}

func TestDocsResultUnionOfKeysInOrder(t *testing.T) {
	docs := []json.RawMessage{json.RawMessage(`{"b":1,"a":"x"}`), json.RawMessage(`{"c":true}`), json.RawMessage(`{"a":"y"}`)}
	res := DocsResult(docs, 2)
	if len(res.Columns) != 3 || res.Columns[0].Name != "b" || res.Columns[2].Name != "c" || len(res.Rows) != 2 || !res.Truncated || !res.Rows[1][0].Null {
		t.Fatalf("res = %+v", res)
	}
}
