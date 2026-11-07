package httpclient

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProxy answers every request itself and says so in the body.
func fakeProxy(t *testing.T) *httptest.Server {
	t.Helper()
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("via proxy " + r.URL.Host))
	}))
	t.Cleanup(p.Close)
	return p
}

func TestProxyDirectiveSendsThroughThatProxy(t *testing.T) {
	p := fakeProxy(t)
	c := New(Options{Proxy: func(*http.Request) (*url.URL, error) { return nil, nil }})
	defer c.Close()
	r := Request{Method: "GET", URL: "http://api.example.invalid/x", Directives: map[string]string{"proxy": p.URL}}
	resp := c.Send(context.Background(), Resolved{Request: r}, Hooks{})
	if resp.Err != "" || string(resp.Body) != "via proxy api.example.invalid" {
		t.Fatalf("resp = %q err %q", resp.Body, resp.Err)
	}
}

func TestProxyNoneGoesDirect(t *testing.T) {
	p := fakeProxy(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("direct")) }))
	defer target.Close()
	pu := mustURL(p.URL)
	c := New(Options{Proxy: http.ProxyURL(pu)})
	defer c.Close()
	r := Request{Method: "GET", URL: target.URL, Directives: map[string]string{"proxy": "none"}}
	resp := c.Send(context.Background(), Resolved{Request: r}, Hooks{})
	if resp.Err != "" || string(resp.Body) != "direct" {
		t.Fatalf("resp = %q err %q", resp.Body, resp.Err)
	}
}

func TestProxyDirectiveBadURLFails(t *testing.T) {
	c := New(Options{})
	defer c.Close()
	r := Request{Method: "GET", URL: "http://api.example.invalid/x", Directives: map[string]string{"proxy": "ftp://user:pw@p:1"}}
	resp := c.Send(context.Background(), Resolved{Request: r}, Hooks{})
	if resp.Err == "" || resp.ErrKind != ErrKindOther {
		t.Fatalf("want a clear error, got %q", resp.Err)
	}
	if strings.Contains(resp.Err, "pw") {
		t.Fatalf("proxy password leaked: %q", resp.Err)
	}
}

func TestAISendIgnoresProxyDirective(t *testing.T) {
	p := fakeProxy(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("direct")) }))
	defer target.Close()
	c := New(Options{Proxy: func(*http.Request) (*url.URL, error) { return nil, nil }})
	defer c.Close()
	r := Request{Method: "GET", URL: target.URL, Directives: map[string]string{"proxy": p.URL}}
	resp := c.Send(context.Background(), Resolved{Request: r}, Hooks{AllowIP: func(net.IP) bool { return true }})
	if resp.Err != "" || string(resp.Body) != "direct" {
		t.Fatalf("AI send must not use a request proxy: %q err %q", resp.Body, resp.Err)
	}
}

func TestRetryDirectiveRetriesWithBackoff(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c := New(Options{})
	defer c.Close()
	r := Request{Method: "GET", URL: srv.URL, Directives: map[string]string{"retry": "3", "retry-delay": "1ms"}}
	resp := c.Send(context.Background(), Resolved{Request: r}, Hooks{})
	if resp.Status != 200 || resp.Attempts != 3 {
		t.Fatalf("status %d attempts %d", resp.Status, resp.Attempts)
	}

	n.Store(0)
	r.Directives = nil
	resp = c.Send(context.Background(), Resolved{Request: r}, Hooks{})
	if resp.Status != 503 || resp.Attempts != 1 {
		t.Fatalf("no retry: status %d attempts %d", resp.Status, resp.Attempts)
	}
}

func TestRetryStopsAtLimitAndOn4xx(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if r.URL.Path == "/bad" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := New(Options{})
	defer c.Close()
	r := Request{Method: "GET", URL: srv.URL + "/busy", Directives: map[string]string{"retry": "2", "retry-delay": "1ms"}}
	resp := c.Send(context.Background(), Resolved{Request: r}, Hooks{})
	if resp.Status != 429 || resp.Attempts != 3 || n.Load() != 3 {
		t.Fatalf("429: status %d attempts %d calls %d", resp.Status, resp.Attempts, n.Load())
	}
	n.Store(0)
	r.URL = srv.URL + "/bad"
	resp = c.Send(context.Background(), Resolved{Request: r}, Hooks{})
	if resp.Status != 400 || resp.Attempts != 1 || n.Load() != 1 {
		t.Fatalf("400 is not retried: status %d attempts %d", resp.Status, resp.Attempts)
	}
}

func TestRetryDelayDoubles(t *testing.T) {
	if d := retryDelay(mustDur("100ms"), 0); d.Milliseconds() != 100 {
		t.Fatalf("first = %v", d)
	}
	if d := retryDelay(mustDur("100ms"), 2); d.Milliseconds() != 400 {
		t.Fatalf("third = %v", d)
	}
	if d := retryDelay(mustDur("5s"), 10); d > maxRetryDelay {
		t.Fatalf("capped: %v", d)
	}
	if retryLimit("99") != maxRetries || retryLimit("x") != 0 || retryLimit("-1") != 0 {
		t.Fatal("retry limit bounds")
	}
}

func TestResolveExpandsDirectivesAndEnvProxy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, EnvFile), []byte(`{"dev": {"$proxy": "http://proxy.dev:3128", "p": "http://other:1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rv := &Resolver{Env: "dev", Envs: LoadEnvs(dir, dir)}
	got, _ := rv.Resolve(Request{Method: "GET", URL: "http://x/"})
	if got.Directives["proxy"] != "http://proxy.dev:3128" {
		t.Fatalf("env proxy = %q", got.Directives["proxy"])
	}
	got, _ = rv.Resolve(Request{Method: "GET", URL: "http://x/", Directives: map[string]string{"proxy": "{{p}}"}})
	if got.Directives["proxy"] != "http://other:1" {
		t.Fatalf("request proxy = %q", got.Directives["proxy"])
	}
	tmpl := Request{Method: "GET", URL: "http://x/", Directives: map[string]string{"proxy": "{{p}}"}}
	_, _ = rv.Resolve(tmpl)
	if tmpl.Directives["proxy"] != "{{p}}" {
		t.Fatal("Resolve must not change the template's directives")
	}
}

func mustDur(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		panic(err)
	}
	return d
}
