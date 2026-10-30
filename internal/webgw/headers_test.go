//go:build unix

package webgw

import (
	"net/http"
	"strings"
	"testing"
)

// wantHeaders is the exact set every response carries (T5, T6): a page that
// is framed, sniffed, cached, or that leaks a Referer is refused here.
func wantHeaders(site Site) map[string]string {
	return map[string]string{
		"Content-Security-Policy":      CSP(site),
		"X-Content-Type-Options":       "nosniff",
		"Referrer-Policy":              "no-referrer",
		"Cache-Control":                "no-store",
		"X-Frame-Options":              "DENY",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Permissions-Policy":           "camera=(), microphone=(), geolocation=(), usb=(), clipboard-read=()",
	}
}

func TestCSPExact(t *testing.T) {
	got := CSP(LoopbackSite(41237))
	want := "default-src 'none'; script-src 'self'; style-src 'self'; " +
		"connect-src 'self' ws://127.0.0.1:41237 ws://localhost:41237; " +
		"img-src 'self' data: blob:; font-src 'self'; frame-ancestors 'none'; " +
		"base-uri 'none'; form-action 'none'"
	if got != want {
		t.Fatalf("CSP\n got %q\nwant %q", got, want)
	}
}

// TestCSPHasNoUnsafeOrWildcard: no 'unsafe-*', no bare scheme and no
// wildcard source ever reaches the policy.
func TestCSPHasNoUnsafeOrWildcard(t *testing.T) {
	csp := CSP(LoopbackSite(8080))
	for _, bad := range []string{"unsafe", "*", " ws: ", " ws:;", " http:", " https:"} {
		if strings.Contains(csp, bad) {
			t.Errorf("CSP contains %q: %s", bad, csp)
		}
	}
}

func TestSecurityHeadersExact(t *testing.T) {
	site := LoopbackSite(41237)
	h := http.Header{}
	setSecurityHeaders(h, CSP(site), site)
	want := wantHeaders(site)
	for k, v := range want {
		if got := h.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	for k := range h {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected header %s: %q", k, h.Get(k))
		}
	}
}
