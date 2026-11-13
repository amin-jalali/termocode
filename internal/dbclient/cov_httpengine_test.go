package dbclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPEngineBaseURLRules(t *testing.T) {
	for name, c := range map[string]struct {
		conn Conn
		want string
	}{
		"url with user":   {Conn{URL: "http://me:pw@host:9200/path/?q=1#f"}, "http://host:9200/path"},
		"https url":       {Conn{URL: "https://host"}, "https://host"},
		"tls flag on url": {Conn{URL: "http://host:1", TLS: true}, "https://host:1"},
		"host and port":   {Conn{Host: "h", Port: 5}, "http://h:5"},
		"default port":    {Conn{Driver: "etcd", Host: "h"}, "http://h:2379"},
		"tls default":     {Conn{Driver: "etcd", Host: "h", TLS: true}, "https://h:2379"},
		"unknown driver":  {Conn{Driver: "nope", Host: "h"}, "http://h:0"},
		"not a http url":  {Conn{URL: "etcd://x", Host: "h", Port: 1}, "http://h:1"},
	} {
		e, err := newHTTPEngine("t", c.conn, "")
		if err != nil || e.base != c.want {
			t.Errorf("%s: base = %q, %v, want %q", name, e.base, err, c.want)
		}
	}
	if e, _ := newHTTPEngine("t", Conn{URL: "http://me@h"}, ""); e.user != "me" {
		t.Errorf("the user of the URL = %q", e.user)
	}
	if e, _ := newHTTPEngine("t", Conn{URL: "http://me@h", User: "own"}, ""); e.user != "own" {
		t.Errorf("a saved user wins over the URL = %q", e.user)
	}
	if _, err := newHTTPEngine("etcd", Conn{}, ""); err == nil || !strings.Contains(err.Error(), "no server address") {
		t.Errorf("no address = %v", err)
	}
	if _, err := newHTTPEngine("t", Conn{Host: "h", TLS: true, SSL: &SSLConfig{Mode: "verify-full", CA: "/no/such/ca.pem"}}, ""); err == nil {
		t.Error("an unreadable CA file: want error")
	}
}

func TestHTTPEngineRequestsErrorsAndCancel(t *testing.T) {
	var gotAuth, gotType, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotType, gotAccept = r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.Header.Get("Accept")
		b, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte(`{"n":` + string(b) + `}`))
		case "/err-obj":
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"type":"parse_exception","reason":"bad secret-pw query"}}`))
		case "/err-list":
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"errors":[{"message":"first"},"second"]}`))
		case "/err-type":
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"type":"only_type"}}`))
		case "/err-text":
			w.WriteHeader(502)
			_, _ = w.Write([]byte("bad gateway\nsecond line"))
		case "/err-long":
			w.WriteHeader(500)
			_, _ = w.Write([]byte(strings.Repeat("x", 400)))
		case "/err-empty":
			w.WriteHeader(404)
		case "/not-json":
			_, _ = w.Write([]byte("<html>"))
		}
	}))
	defer srv.Close()
	e, err := newHTTPEngine("demo", Conn{URL: srv.URL, User: "me"}, "secret-pw")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var out struct{ N int }
	if err := e.doJSON(ctx, "POST", "/ok", 5, &out); err != nil || out.N != 5 {
		t.Fatalf("doJSON = %+v, %v", out, err)
	}
	if gotAuth == "" || gotType != "application/json" || gotAccept != "application/json" {
		t.Errorf("headers: auth %q type %q accept %q", gotAuth, gotType, gotAccept)
	}
	if _, err := e.do(ctx, "POST", "/ok", []byte("7"), "text/plain"); err != nil || gotType != "text/plain" {
		t.Errorf("custom content type = %q, %v", gotType, err)
	}
	for path, want := range map[string]string{
		"/err-obj": "bad *** query", "/err-list": "first", "/err-type": "only_type",
		"/err-text": "HTTP 502: bad gateway", "/err-long": "…", "/err-empty": "HTTP 404",
	} {
		_, err := e.do(ctx, "GET", path, nil, "")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s = %v, want %q", path, err, want)
		}
		if err != nil && strings.Contains(err.Error(), "secret-pw") {
			t.Errorf("%s leaks the password: %v", path, err)
		}
	}
	if err := e.doJSON(ctx, "GET", "/not-json", nil, &out); err == nil || !strings.Contains(err.Error(), "unexpected answer") {
		t.Errorf("an answer that is no JSON = %v", err)
	}
	if err := e.doJSON(ctx, "POST", "/ok", make(chan int), nil); err == nil {
		t.Error("a body that cannot be JSON: want error")
	}
	if err := e.doJSON(ctx, "GET", "/not-json", nil, nil); err != nil {
		t.Errorf("an ignored answer = %v", err)
	}
	if _, err := e.do(ctx, "BAD METHOD", "/x", nil, ""); err == nil {
		t.Error("a bad method: want error")
	}
	// A header set on the engine (a token) is not replaced by basic auth.
	e.header.Set("Authorization", "Bearer tok")
	_, _ = e.do(ctx, "GET", "/ok", nil, "")
	if gotAuth != "Bearer tok" {
		t.Errorf("auth header = %q", gotAuth)
	}
	// Close ends the session.
	e.mu.Lock()
	e.closed = true
	e.mu.Unlock()
	if _, err := e.do(ctx, "GET", "/ok", nil, ""); err == nil || !strings.Contains(err.Error(), "session closed") {
		t.Errorf("closed = %v", err)
	}
	if jsonMessage(map[string]any{}) != "" || jsonMessage([]any{}) != "" || jsonMessage(3) != "" {
		t.Error("jsonMessage of nothing")
	}
}

func TestHTTPEngineRegistryAndSchemes(t *testing.T) {
	if len(HTTPEngines()) < 5 {
		t.Errorf("engines = %d", len(HTTPEngines()))
	}
	titles := HTTPEngines()
	for i := 1; i < len(titles); i++ {
		if titles[i-1].Title > titles[i].Title {
			t.Errorf("engines are listed by title: %s before %s", titles[i-1].Title, titles[i].Title)
		}
	}
	if _, err := OpenHTTPEngine(Conn{Driver: "nope"}, ""); err == nil {
		t.Error("an unknown engine: want error")
	}
	if engineClassify("nope") != nil || engineClassify("etcd") == nil {
		t.Error("engineClassify")
	}
	if e, tls, ok := engineForScheme("etcds"); !ok || !tls || e.ID != "etcd" {
		t.Errorf("etcds = %v %v %v", e.ID, tls, ok)
	}
	if e, tls, ok := engineForScheme("etcd"); !ok || tls || e.ID != "etcd" {
		t.Errorf("etcd = %v %v %v", e.ID, tls, ok)
	}
	if _, _, ok := engineForScheme("gopher"); ok {
		t.Error("an unknown scheme")
	}
	if httpEngineDefaultPort("nope", false) != 0 {
		t.Error("an unknown engine has no port")
	}
}
