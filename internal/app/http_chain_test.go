package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// {{login.response...}} reads the newest good answer of the request
// named login in the same .http file; failed sends, other files and
// unnamed requests do not count.
func TestHTTPChainSourceUsesNewestNamedRun(t *testing.T) {
	s := newHTTPState()
	named := func(name string) httpclient.Resolved {
		return httpclient.Resolved{Template: httpclient.Request{Name: name}}
	}
	s.remember(&httpRun{id: 1, file: "/w/a.http", rr: named("login"), resp: &httpclient.Response{Status: 200, Body: []byte(`{"token":"old"}`)}})
	s.remember(&httpRun{id: 2, file: "/w/a.http", rr: named("login"), resp: &httpclient.Response{Status: 201, Body: []byte(`{"token":"new"}`)}})
	s.remember(&httpRun{id: 3, file: "/w/a.http", rr: named("login"), resp: &httpclient.Response{Err: "dial tcp: refused"}})
	s.remember(&httpRun{id: 4, file: "/w/b.http", rr: named("login"), resp: &httpclient.Response{Status: 500}})
	s.remember(&httpRun{id: 5, file: "/w/a.http", rr: named(""), resp: &httpclient.Response{Status: 204}})

	src := s.chainSource("/w/a.http")
	r, ok := src("login")
	if !ok || string(r.Body) != `{"token":"new"}` {
		t.Fatalf("login = %v %v", r, ok)
	}
	if _, ok := src("other"); ok {
		t.Error("unknown name found")
	}
	if r, ok := s.chainSource("/w/b.http")("login"); !ok || r.Status != 500 {
		t.Errorf("b.http login = %v %v", r, ok)
	}
	if _, ok := s.chainSource("/w/c.http")("login"); ok {
		t.Error("another file's run was used")
	}

	rv := &httpclient.Resolver{Responses: src}
	if rr, un := rv.Resolve(httpclient.Request{URL: "http://x/{{login.response.body.$.token}}"}); rr.URL != "http://x/new" || len(un) != 0 {
		t.Errorf("resolved %q %v", rr.URL, un)
	}
}
