package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// The prompt title says what the request uses now: its own @proxy, then
// the environment's $proxy, then HTTP(S)_PROXY, else direct.
func TestHTTPProxyNow(t *testing.T) {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		t.Setenv(k, "")
	}
	req := httpclient.Request{Directives: map[string]string{"proxy": "socks5://127.0.0.1:1080"}}
	if got := httpProxyNow(req, "http://env:3128"); got != "@proxy socks5://127.0.0.1:1080" {
		t.Fatalf("own = %q", got)
	}
	if got := httpProxyNow(httpclient.Request{}, "http://env:3128"); got != "$proxy of the environment: http://env:3128" {
		t.Fatalf("env = %q", got)
	}
	if got := httpProxyNow(httpclient.Request{}, ""); got != "direct (no proxy)" {
		t.Fatalf("none = %q", got)
	}
	t.Setenv("HTTP_PROXY", "http://corp:8080")
	if got := httpProxyNow(httpclient.Request{}, ""); got != "HTTP_PROXY: http://corp:8080" {
		t.Fatalf("process = %q", got)
	}
	if httpProxyLine("") != "" || httpProxyLine(" none ") != "# @proxy none" {
		t.Fatal("directive line")
	}
}

// The command is in the palette and has an AI policy.
func TestHTTPProxyPaletteAndPolicy(t *testing.T) {
	found := false
	for _, it := range httpPaletteItems() {
		found = found || it.ID == "http-proxy-set"
	}
	if _, ok := aiActionPolicyFor("http-proxy-set"); !found || !ok {
		t.Fatalf("palette %v, policy %v", found, ok)
	}
}
