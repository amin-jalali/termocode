package dbclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseESRequestForms(t *testing.T) {
	r, err := ParseESRequest("post _bulk\n{\"index\":{}}\n{\"a\":1}\n")
	if err != nil || r.Method != "POST" || r.Path != "/_bulk" || !strings.HasSuffix(string(r.Body), "\n") {
		t.Errorf("ndjson = %+v, %v", r, err)
	}
	r, err = ParseESRequest("GET /idx/_search\n{\"query\":{\"match_all\":{}}}")
	if err != nil || r.Method != "GET" || strings.HasSuffix(string(r.Body), "\n") {
		t.Errorf("json body = %+v, %v", r, err)
	}
	for in, why := range map[string]string{
		"GET":                      "write METHOD",
		"GET /a /b":                "write METHOD",
		"PATCH /a":                 "unknown method",
		"POST /a\n{nope":           "not JSON",
		"POST /a\n{\"a\":1}\n{bad": "not JSON",
	} {
		if _, err := ParseESRequest(in); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%q = %v, want %q", in, err, why)
		}
	}
	for in, want := range map[string]Kind{
		"GET /_cluster/health": KindRead, "HEAD /idx": KindRead,
		"POST /idx/_search\n{}": KindRead, "POST /_sql\n{}": KindRead, "POST /_sql/close\n{}": KindWrite,
		"POST /idx/_delete_by_query\n{}": KindDDL, "POST /idx/_doc\n{}": KindWrite,
		"DELETE /idx": KindDDL, "PUT /idx": KindWrite, "bogus": KindUnknown,
	} {
		if got := ClassifyESRequest(in); got != want {
			t.Errorf("ClassifyESRequest(%q) = %v, want %v", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"/_cat/indices": "/_cat/indices?format=json", "/_cat/indices?v": "/_cat/indices?v&format=json",
		"/_cat/x?format=yaml": "/_cat/x?format=yaml", "/idx/_search": "/idx/_search",
	} {
		if got := withJSONFormat(in); got != want {
			t.Errorf("withJSONFormat(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestESResultShapes(t *testing.T) {
	if r := ESResult([]byte("  "), 10); len(r.Rows) != 0 || r.Err != nil {
		t.Errorf("empty = %+v", r)
	}
	if r := ESResult([]byte(`[{"a":1},{"a":2}]`), 1); len(r.Rows) != 1 || !r.Truncated {
		t.Errorf("array = %+v", r)
	}
	r := ESResult([]byte(`{"hits":{"total":{"value":10000,"relation":"gte"},"hits":[{"_index":"i","_id":"1","_score":1.5,"_source":{"name":"a"}},{"_index":"i","_id":"2","_score":null}]},"aggregations":{"x":{"value":1}}}`), 10)
	if r.Err != nil || len(r.Rows) != 2 {
		t.Fatalf("hits = %+v", r)
	}
	notes := strings.Join(r.Notices, "\n")
	if !strings.Contains(notes, "10000 or more") || !strings.Contains(notes, "Aggregations:") {
		t.Errorf("notices = %v", r.Notices)
	}
	if r := ESResult([]byte(`{"hits":{"total":{"value":5,"relation":"eq"},"hits":[]}}`), 10); !strings.Contains(strings.Join(r.Notices, ""), "Total hits: 5") {
		t.Errorf("exact total = %v", r.Notices)
	}
	if r := ESResult([]byte(`{"hits":{"total":7,"hits":[]}}`), 10); !strings.Contains(strings.Join(r.Notices, ""), "Total hits: 7") {
		t.Errorf("plain total = %v", r.Notices)
	}
	sq := ESResult([]byte(`{"columns":[{"name":"a","type":"long"},{"name":"b","type":"text"}],"rows":[[1,"x"],[2],[3,"z"]]}`), 2)
	if len(sq.Columns) != 2 || len(sq.Rows) != 2 || !sq.Truncated || !sq.Rows[1][1].Null {
		t.Errorf("sql = %+v", sq)
	}
	if one := ESResult([]byte(`{"acknowledged":true}`), 10); len(one.Rows) != 1 {
		t.Errorf("a plain answer is one row: %+v", one)
	}
	if txt := ESResult([]byte("green open idx"), 10); len(txt.Columns) != 1 || txt.Columns[0].Name != "response" {
		t.Errorf("a text answer = %+v", txt)
	}
	for in, want := range map[string]string{`{"value":3}`: `{"value":3}`, `{"value":3,"relation":"gte"}`: "3 or more", `{"value":3,"relation":"eq"}`: "3"} {
		if got := esTotal([]byte(in)); got != want {
			t.Errorf("esTotal(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestElasticsearchSessionRunsAndIntrospects(t *testing.T) {
	var gotAuth, gotPath, gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotType = r.Header.Get("Authorization"), r.URL.RequestURI(), r.Header.Get("Content-Type")
		_, _ = io.Copy(io.Discard, r.Body)
		switch {
		case strings.HasPrefix(r.URL.Path, "/_cat/indices"):
			_, _ = w.Write([]byte(`[{"index":"b"},{"index":"a"}]`))
		case strings.HasSuffix(r.URL.Path, "/_mapping"):
			_, _ = w.Write([]byte(`{"a":{"mappings":{"properties":{"name":{"type":"keyword"},"addr":{"properties":{"city":{"type":"text"}}}}}}}`))
		case r.URL.Path == "/":
			_, _ = w.Write([]byte(`{"name":"n"}`))
		default:
			_, _ = w.Write([]byte(`{"acknowledged":true}`))
		}
	}))
	defer srv.Close()
	s, err := NewElasticsearchSession(Conn{URL: srv.URL}, "key-123")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil || gotAuth != "ApiKey key-123" {
		t.Errorf("ping %v auth %q", err, gotAuth)
	}
	if r := s.Run(ctx, "POST /_bulk\n{\"index\":{}}\n{\"a\":1}", RunOptions{}); r.Err != nil || gotType != "application/x-ndjson" {
		t.Errorf("bulk = %v type %q", r.Err, gotType)
	}
	if r := s.Run(ctx, "GET /_cat/indices", RunOptions{}); r.Err != nil || !strings.Contains(gotPath, "format=json") {
		t.Errorf("cat = %v path %q", r.Err, gotPath)
	}
	if r := s.Run(ctx, "nonsense", RunOptions{}); r.Err == nil {
		t.Error("a bad request: want error")
	}
	top, _ := s.Introspect(ctx, IntrospectionRequest{})
	idx, err := s.Introspect(ctx, IntrospectionRequest{Database: "indices"})
	if err != nil || len(top.Databases) != 1 || len(idx.Objects) != 2 || idx.Objects[0].Name != "a" {
		t.Errorf("indices = %+v %v", idx, err)
	}
	cols, err := s.Introspect(ctx, IntrospectionRequest{Object: "a"})
	if err != nil || len(cols.Columns) < 3 {
		t.Errorf("columns = %+v %v", cols, err)
	}
	if ro, _ := s.ReadOnlySession(); ro == nil {
		t.Error("read-only session")
	}
	srv.Close()
	if _, err := s.Introspect(ctx, IntrospectionRequest{Database: "indices"}); err == nil {
		t.Error("a server that is gone: want error")
	}
	if _, err := s.Introspect(ctx, IntrospectionRequest{Object: "a"}); err == nil {
		t.Error("a server that is gone: want error")
	}
	if r := s.Run(ctx, "GET /", RunOptions{}); r.Err == nil {
		t.Error("a server that is gone: want error")
	}
	// A user and a password use basic auth, not an API key.
	basic, _ := NewElasticsearchSession(Conn{Host: "h", User: "u"}, "p")
	if basic.header.Get("Authorization") != "" {
		t.Error("basic auth is set per request")
	}
	if _, err := NewElasticsearchSession(Conn{}, ""); err == nil {
		t.Error("no address: want error")
	}
}
