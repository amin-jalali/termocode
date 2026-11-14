//go:build unix

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Spec (ADR 0022): the relay serves the relay page at / (relay.html:
// relay.js first, then app.js by its SRI hash) and the panel's built
// files under /assets/, with the panel's strict CSP, except that one
// Trusted Types policy ("relay") may load app.js. Nothing else.
func TestRelayServesThePage(t *testing.T) {
	h := secure(handler(nil, pageHandler(false)))
	get := func(path, host string) *http.Response {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Result()
	}
	resp := get("/", "relay.example.org")
	body, _ := io.ReadAll(resp.Body)
	csp := resp.Header.Get("Content-Security-Policy")
	if resp.StatusCode != 200 || !strings.Contains(string(body), `src="/assets/relay.js"`) || !strings.Contains(string(body), `name="termocode-app"`) {
		t.Fatalf("/ = %d %.200s", resp.StatusCode, body)
	}
	for _, want := range []string{"default-src 'none'", "script-src 'self'", "connect-src 'self'", "require-trusted-types-for 'script'", "trusted-types relay", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
	if strings.Contains(csp, "unsafe") {
		t.Errorf("CSP %q allows unsafe code", csp)
	}
	for _, p := range []string{"/assets/relay.js", "/assets/app.js", "/assets/app.css"} {
		if r := get(p, "relay.example.org"); r.StatusCode != 200 {
			t.Errorf("%s = %d", p, r.StatusCode)
		}
	}
	for _, p := range []string{"/index.html", "/assets/assets.lock", "/relay.html", "/x"} {
		if r := get(p, "relay.example.org"); r.StatusCode != 404 {
			t.Errorf("%s = %d, want 404", p, r.StatusCode)
		}
	}
	if r := get("/", "evil.example; script-src *"); r.StatusCode != 400 {
		t.Errorf("a bad Host = %d, want 400", r.StatusCode)
	}
}

// Spec (WEBP-10 finding, NEW-11): over https the relay page may also
// make the push worker's one policy, "sw" (lib/push.ts), beside "relay".
func TestRelayPageCSPAllowsThePushWorkerPolicy(t *testing.T) {
	h := secure(handler(nil, pageHandler(true)))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "relay.example.org"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if csp := w.Result().Header.Get("Content-Security-Policy"); !strings.HasSuffix(csp, "trusted-types relay sw") {
		t.Errorf("CSP %q", csp)
	}
}

// Spec (NEW-11): a phone on an https relay page can turn on pushes: the
// relay serves the panel's push-only worker at /sw.js (its own CSP,
// which loads and connects to nothing); on plain http there is none.
func TestRelayServesThePushWorker(t *testing.T) {
	get := func(tls bool) *http.Response {
		h := secure(handler(nil, pageHandler(tls)))
		r := httptest.NewRequest(http.MethodGet, "/sw.js?l=fa", nil)
		r.Host = "relay.example.org"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Result()
	}
	res := get(true)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/javascript; charset=utf-8" || !strings.Contains(string(body), `addEventListener("push"`) {
		t.Fatalf("https /sw.js = %d %q %.80s", res.StatusCode, res.Header.Get("Content-Type"), body)
	}
	if csp := res.Header.Get("Content-Security-Policy"); csp != "default-src 'none'" {
		t.Errorf("worker CSP %q", csp)
	}
	if res := get(false); res.StatusCode != 404 {
		t.Errorf("http /sw.js = %d, want 404", res.StatusCode)
	}
}
