//go:build unix

package webgw

import (
	"io"
	"net/http"
	"regexp"
	"testing"
)

// The push-only worker is served only on an installable site, as
// JavaScript, with a policy that lets it load and connect to nothing.
func TestServiceWorkerOnlyWhenInstallable(t *testing.T) {
	mark := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	if got := getAsset(t, siteMux(LoopbackSite(41237), mark, mark, Assets()), http.MethodGet, SWPath).StatusCode; got != http.StatusNotFound {
		t.Errorf("loopback: GET %s = %d", SWPath, got)
	}
	mux := siteMux(Site{Scheme: "https", Hosts: []string{"box.tail1234.ts.net"}}, mark, mark, Assets())
	res := getAsset(t, mux, http.MethodGet, SWPath)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("GET %s = %d %q", SWPath, res.StatusCode, res.Header.Get("Content-Type"))
	}
	if res.Header.Get("Content-Security-Policy") != "default-src 'none'" {
		t.Errorf("CSP %q", res.Header.Get("Content-Security-Policy"))
	}
	if got := getAsset(t, mux, http.MethodPost, SWPath).StatusCode; got != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d", got)
	}
	b, _ := io.ReadAll(res.Body)
	if len(b) == 0 {
		t.Fatal("empty worker")
	}
}

// The worker handles push and taps only: no fetch handler, no cache, no
// imports, no network, no code from strings.
func TestServiceWorkerIsPushOnly(t *testing.T) {
	for _, bad := range []*regexp.Regexp{
		regexp.MustCompile(`addEventListener\(\s*["'](fetch|message|sync|periodicsync)["']`),
		regexp.MustCompile(`\bimportScripts\b|\bimport\s*\(|\bcaches\b|\bfetch\s*\(|\bXMLHttpRequest\b|\bWebSocket\b|\bindexedDB\b`),
		regexp.MustCompile(`\beval\s*\(|\bFunction\s*\(`),
		regexp.MustCompile(`(?i)\b(https?|wss?)://`),
	} {
		if m := bad.Find(swJS); m != nil {
			t.Errorf("sw.js has %q", m)
		}
	}
	for _, want := range []string{`addEventListener("push"`, `addEventListener("notificationclick"`, "showNotification"} {
		if !regexp.MustCompile(regexp.QuoteMeta(want)).Match(swJS) {
			t.Errorf("sw.js has no %s", want)
		}
	}
}
