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

func TestNeo4jSessionCypherGraphAndTree(t *testing.T) {
	type call struct{ path, stmt, mode string }
	var got []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Statements []neo4jStatement `json:"statements"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		st := ""
		if len(body.Statements) > 0 {
			st = body.Statements[0].Statement
		}
		got = append(got, call{r.URL.Path, st, r.Header.Get("Access-Mode")})
		if u, p, ok := r.BasicAuth(); !ok || u != "neo4j" || p != "pw-1" {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"errors":[{"code":"Neo.ClientError.Security.Unauthorized","message":"Invalid username or password."}]}`)
			return
		}
		switch {
		case st == "RETURN 1":
			_, _ = io.WriteString(w, `{"results":[{"columns":["1"],"data":[{"row":[1]}]}],"errors":[]}`)
		case strings.HasPrefix(st, "SHOW DATABASES"):
			_, _ = io.WriteString(w, `{"results":[{"columns":["name"],"data":[{"row":["neo4j"]},{"row":["system"]}]}],"errors":[]}`)
		case strings.HasPrefix(st, "CALL db.labels"):
			_, _ = io.WriteString(w, `{"results":[{"columns":["label"],"data":[{"row":["Person"]}]}],"errors":[]}`)
		case strings.HasPrefix(st, "CALL db.relationshipTypes"):
			_, _ = io.WriteString(w, `{"results":[{"columns":["relationshipType"],"data":[{"row":["KNOWS"]}]}],"errors":[]}`)
		case strings.Contains(st, "UNWIND keys"):
			_, _ = io.WriteString(w, `{"results":[{"columns":["k"],"data":[{"row":["age"]},{"row":["name"]}]}],"errors":[]}`)
		case strings.HasPrefix(st, "MATCH (a)-[r]->(b)"):
			_, _ = io.WriteString(w, `{"results":[{"columns":["a","r","b"],"data":[{"row":[{"name":"Ann"},{},{"name":"Bob"}],"graph":{"nodes":[{"id":"1","labels":["Person"],"properties":{"name":"Ann"}},{"id":"2","labels":["Person"],"properties":{"name":"Bob"}}],"relationships":[{"id":"9","type":"KNOWS","startNode":"1","endNode":"2","properties":{}}]}}]}],"errors":[]}`)
		case strings.HasPrefix(st, "CREATE"):
			if r.Header.Get("Access-Mode") == "READ" {
				_, _ = io.WriteString(w, `{"results":[],"errors":[{"code":"Neo.ClientError.Statement.AccessMode","message":"Writing in read access mode not allowed."}]}`)
				return
			}
			_, _ = io.WriteString(w, `{"results":[{"columns":[],"data":[],"stats":{"nodes_created":1,"properties_set":2}}],"errors":[]}`)
		default:
			_, _ = io.WriteString(w, `{"results":[],"errors":[{"code":"Neo.ClientError.Statement.SyntaxError","message":"Invalid input"}]}`)
		}
	}))
	defer srv.Close()
	s, err := NewNeo4jSession(Conn{Driver: "neo4j", URL: srv.URL, User: "neo4j"}, "pw-1")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil || got[0].path != "/db/neo4j/tx/commit" {
		t.Fatalf("ping %v %+v", err, got)
	}
	res := s.Run(ctx, "MATCH (a)-[r]->(b) RETURN a, r, b;", RunOptions{})
	if res.Err != nil || len(res.Rows) != 1 || res.Rows[0][0].Text != `{"name":"Ann"}` || len(res.Notices) != 1 || !strings.Contains(res.Notices[0], "(1)-[:KNOWS]->(2)") {
		t.Fatalf("match = %+v", res)
	}
	w := s.Run(ctx, "CREATE (n:Person {name: 'C', age: 3})", RunOptions{})
	if w.Err != nil || w.Affected != 3 || !strings.Contains(strings.Join(w.Notices, " "), "nodes created: 1") {
		t.Fatalf("create = %+v", w)
	}
	if bad := s.Run(ctx, "MATCH", RunOptions{}); bad.Err == nil || !strings.Contains(bad.Err.Error(), "SyntaxError") {
		t.Fatalf("error = %+v", bad)
	}
	in, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || strings.Join(in.Databases, ",") != "neo4j,system" || got[len(got)-1].path != "/db/system/tx/commit" {
		t.Fatalf("dbs = %v %v", in.Databases, err)
	}
	in, err = s.Introspect(ctx, IntrospectionRequest{Database: "neo4j"})
	if err != nil || len(in.Objects) != 2 || in.Objects[0].Name != ":Person" || in.Objects[1].Name != "[:KNOWS]" {
		t.Fatalf("objects = %+v %v", in.Objects, err)
	}
	in, err = s.Introspect(ctx, IntrospectionRequest{Database: "neo4j", Object: "[:KNOWS]"})
	if err != nil || len(in.Columns) != 3 || !strings.Contains(got[len(got)-1].stmt, "[r:`KNOWS`]") {
		t.Fatalf("keys = %+v %v %+v", in.Columns, err, got[len(got)-1])
	}
	ro, _ := s.ReadOnlySession()
	if r := ro.Run(ctx, "CREATE (n)", RunOptions{}); r.Err == nil {
		t.Fatal("the read-only twin must refuse CREATE")
	}
	if r := ro.Run(ctx, "RETURN 1", RunOptions{}); r.Err != nil || got[len(got)-1].mode != "READ" {
		t.Fatalf("read-only read = %+v mode %q", r, got[len(got)-1].mode)
	}
	bad, _ := NewNeo4jSession(Conn{Driver: "neo4j", URL: srv.URL, User: "neo4j"}, "secret-pw")
	if err := bad.Ping(ctx); err == nil || strings.Contains(err.Error(), "secret-pw") || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("auth = %v", err)
	}
}

func TestNeo4jURLMovesTheBoltPort(t *testing.T) {
	c, err := ParseURL("neo4j+s://neo4j:pw@graph.example.com:7687")
	if err != nil || c.Driver != "neo4j" || !c.TLS || c.Port != 7473 || c.Database != "neo4j" || c.Password != "pw" {
		t.Fatalf("parse = %+v %v", c, err)
	}
	if k, _ := Classify("MATCH (n) DETACH DELETE n", "neo4j"); k == KindRead {
		t.Fatal("a Cypher delete is not a read")
	}
}
