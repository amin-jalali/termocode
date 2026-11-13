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

func TestCouchbaseSessionRunsAndIntrospects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req n1qlRequest
		_ = json.Unmarshal(b, &req)
		st := req.Statement
		switch {
		case strings.Contains(st, "system:buckets"):
			_, _ = w.Write([]byte(`{"status":"success","results":["b1","b2"]}`))
		case strings.Contains(st, "system:keyspaces"):
			_, _ = w.Write([]byte(`{"status":"success","results":["_default._default","s.c"]}`))
		case strings.Contains(st, "LIMIT"):
			_, _ = w.Write([]byte(`{"status":"success","results":[{"a":1,"b":"x","c":[1],"d":{"k":1},"e":true,"f":null},{"a":2},"not an object"]}`))
		case strings.Contains(st, "FAIL"):
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"status":"errors","errors":[{"code":3000,"msg":"syntax error near secret-pw"}]}`))
		case strings.Contains(st, "TIMEOUT"):
			_, _ = w.Write([]byte(`{"status":"timeout","results":[]}`))
		case strings.Contains(st, "HTTP500"):
			w.WriteHeader(500)
		case strings.Contains(st, "UPDATE"):
			_, _ = w.Write([]byte(`{"status":"success","results":[],"metrics":{"mutationCount":3}}`))
		default:
			_, _ = w.Write([]byte(`{"status":"success","results":[{"x":1}]}`))
		}
	}))
	defer srv.Close()
	s, err := NewCouchbaseSession(Conn{URL: srv.URL, User: "u"}, "secret-pw")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if r := s.Run(ctx, "UPDATE x SET a = 1", RunOptions{}); r.Err != nil || r.Affected != 3 {
		t.Errorf("update = %+v", r)
	}
	for stmt, why := range map[string]string{"FAIL": "syntax error", "TIMEOUT": "query timeout", "HTTP500": "HTTP 500"} {
		r := s.Run(ctx, "SELECT "+stmt, RunOptions{})
		if r.Err == nil || !strings.Contains(r.Err.Error(), why) || strings.Contains(r.Err.Error(), "secret-pw") {
			t.Errorf("%s = %v, want %q without the password", stmt, r.Err, why)
		}
	}
	top, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || len(top.Databases) != 2 {
		t.Errorf("buckets = %+v %v", top, err)
	}
	objs, err := s.Introspect(ctx, IntrospectionRequest{Schema: "b1"})
	if err != nil || len(objs.Objects) != 2 || objs.Objects[0].Kind != "collection" {
		t.Errorf("collections = %+v %v", objs, err)
	}
	for _, obj := range []string{"s.c", "plain"} {
		cols, err := s.Introspect(ctx, IntrospectionRequest{Database: "b1", Object: obj})
		if err != nil || len(cols.Columns) < 5 {
			t.Errorf("%s columns = %+v %v", obj, cols, err)
		}
	}
	for in, want := range map[string]string{"": "null", "null": "null", `{"a":1}`: "object", "[1]": "array", "true": "boolean", "false": "boolean", `"s"`: "string", "12": "number"} {
		if got := jsonType([]byte(in)); got != want {
			t.Errorf("jsonType(%q) = %q, want %q", in, got, want)
		}
	}
	if couchbaseKeyspace("b`x", "s.c") != "`b``x`.`s`.`c`" || couchbaseKeyspace("b", "c") != "`b`.`_default`.`c`" {
		t.Error("couchbaseKeyspace")
	}
	for _, f := range []func(context.Context) error{s.Begin, s.Commit, s.Rollback} {
		if f(ctx) == nil {
			t.Error("Couchbase has no transactions")
		}
	}
	srv.Close()
	if _, err := s.Introspect(ctx, IntrospectionRequest{}); err == nil {
		t.Error("buckets from a server that is gone: want error")
	}
	if _, err := s.Introspect(ctx, IntrospectionRequest{Database: "b"}); err == nil {
		t.Error("collections from a server that is gone: want error")
	}
	if _, err := s.Introspect(ctx, IntrospectionRequest{Database: "b", Object: "c"}); err == nil {
		t.Error("columns from a server that is gone: want error")
	}
	if _, err := NewCouchbaseSession(Conn{}, ""); err == nil {
		t.Error("no address: want error")
	}
}
