package dbclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNeo4jResultGridGraphAndCounters(t *testing.T) {
	var resp neo4jResponse
	data := `{"results":[{"columns":["n","m"],"data":[
	  {"row":[1],"graph":{"nodes":[{"id":"1","labels":["P"],"properties":{"name":"a"}}],"relationships":[{"id":"9","type":"KNOWS","startNode":"1","endNode":"2","properties":{"w":2}}]}},
	  {"row":[2,3],"graph":{"nodes":[{"id":"1","labels":["P"],"properties":{}},{"id":"2","labels":[],"properties":null}],"relationships":[{"id":"9","type":"KNOWS","startNode":"1","endNode":"2"}]}},
	  {"row":[4,5]}],
	 "stats":{"nodes_created":2,"properties_set":3,"labels_added":0,"indexes_added":"x"}}]}`
	if err := json.Unmarshal([]byte(data), &resp); err != nil {
		t.Fatal(err)
	}
	r := Neo4jResult(resp, 0)
	if len(r.Rows) != 3 || !r.Rows[0][1].Null || r.Affected != 5 {
		t.Errorf("grid = %+v", r)
	}
	notes := strings.Join(r.Notices, "\n")
	if !strings.Contains(notes, "Graph:") || !strings.Contains(notes, "nodes created: 2, properties set: 3") {
		t.Errorf("notices = %q", notes)
	}
	if cut := Neo4jResult(resp, 1); !cut.Truncated || len(cut.Rows) != 1 {
		t.Errorf("cut = %+v", cut)
	}
	if empty := Neo4jResult(neo4jResponse{}, 0); len(empty.Rows) != 0 {
		t.Errorf("no results = %+v", empty)
	}
	if neo4jParams(nil) != nil || neo4jParams([]any{1, "x"})["p2"] != "x" {
		t.Error("neo4jParams")
	}
	if cypherIdent("a`b") != "`a``b`" {
		t.Error("cypherIdent")
	}
}

func TestNeo4jSessionRunsAndIntrospects(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		paths = append(paths, r.URL.Path)
		q := string(b)
		switch {
		case strings.Contains(q, "SHOW DATABASES"):
			if strings.Contains(r.URL.Path, "bad") {
				w.WriteHeader(400)
			}
			_, _ = w.Write([]byte(`{"results":[{"columns":["name"],"data":[{"row":["neo4j"]},{"row":[null]},{"row":[]}]}],"errors":[]}`))
		case strings.Contains(q, "db.labels"):
			_, _ = w.Write([]byte(`{"results":[{"columns":["label"],"data":[{"row":["Person"]}]}],"errors":[]}`))
		case strings.Contains(q, "db.relationshipTypes"):
			_, _ = w.Write([]byte(`{"results":[{"columns":["t"],"data":[{"row":["KNOWS"]}]}],"errors":[]}`))
		case strings.Contains(q, "UNWIND keys"):
			_, _ = w.Write([]byte(`{"results":[{"columns":["k"],"data":[{"row":["name"]},{"row":["age"]}]}],"errors":[]}`))
		case strings.Contains(q, "FAIL"):
			_, _ = w.Write([]byte(`{"results":[],"errors":[{"code":"Neo.ClientError.Statement.SyntaxError","message":"bad secret-pw cypher"}]}`))
		case strings.Contains(q, "BOOM500"):
			w.WriteHeader(500)
		default:
			_, _ = w.Write([]byte(`{"results":[{"columns":["x"],"data":[{"row":[1]}]}],"errors":[]}`))
		}
	}))
	defer srv.Close()
	s, err := NewNeo4jSession(Conn{URL: srv.URL}, "secret-pw")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if r := s.Run(ctx, "RETURN $p1", RunOptions{Args: []any{1}}); r.Err != nil || len(r.Rows) != 1 {
		t.Errorf("run = %+v", r)
	}
	for q, why := range map[string]string{"": "write a Cypher", "FAIL": "SyntaxError", "BOOM500": "HTTP 500"} {
		r := s.Run(ctx, q, RunOptions{})
		if r.Err == nil || !strings.Contains(r.Err.Error(), why) || strings.Contains(r.Err.Error(), "secret-pw") {
			t.Errorf("%q = %v, want %q without the password", q, r.Err, why)
		}
	}
	ro, _ := s.ReadOnlySession()
	if r := ro.Run(ctx, "CREATE (n)", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "read-only") {
		t.Errorf("read-only create = %v", r.Err)
	}
	top, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || len(top.Databases) != 1 {
		t.Errorf("databases = %+v %v", top, err)
	}
	objs, err := s.Introspect(ctx, IntrospectionRequest{Database: "neo4j"})
	if err != nil || len(objs.Objects) != 2 || objs.Objects[0].Name != ":Person" || objs.Objects[1].Name != "[:KNOWS]" {
		t.Errorf("objects = %+v %v", objs, err)
	}
	for _, o := range []string{":Person", "[:KNOWS]", "Plain"} {
		cols, err := s.Introspect(ctx, IntrospectionRequest{Object: o})
		if err != nil || len(cols.Columns) != 3 {
			t.Errorf("%s columns = %+v %v", o, cols, err)
		}
	}
	for _, f := range []func(context.Context) error{s.Begin, s.Commit, s.Rollback} {
		if f(ctx) == nil {
			t.Error("Neo4j has no transactions")
		}
	}
	srv.Close()
	// A server without the system database falls back to the one database.
	if top, err := s.Introspect(ctx, IntrospectionRequest{}); err != nil || len(top.Databases) != 1 || top.Databases[0] != "neo4j" {
		t.Errorf("fallback = %+v %v", top, err)
	}
	if _, err := s.Introspect(ctx, IntrospectionRequest{Database: "neo4j"}); err == nil {
		t.Error("labels on a server that is gone: want error")
	}
	if _, err := s.Introspect(ctx, IntrospectionRequest{Object: "x"}); err == nil {
		t.Error("keys on a server that is gone: want error")
	}
	if _, err := NewNeo4jSession(Conn{}, ""); err == nil {
		t.Error("no address: want error")
	}
}
