//go:build unix

package webgw

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A tailnet site (WEB-31) is one https origin: the machine's exact tailnet
// name, no port (Tailscale Serve answers on 443), never 127.0.0.1.
func TestTailnetSiteHostExact(t *testing.T) {
	s, err := TailnetSite("box.tail1234.ts.net")
	if err != nil {
		t.Fatal(err)
	}
	if s.Scheme != "https" || len(s.Hosts) != 1 || s.Hosts[0] != "box.tail1234.ts.net" {
		t.Fatalf("site = %+v", s)
	}
	for _, tc := range []struct {
		host, origin string
		code         int
	}{
		{"box.tail1234.ts.net", "https://box.tail1234.ts.net", http.StatusOK},
		{"box.tail1234.ts.net", "http://box.tail1234.ts.net", http.StatusForbidden},
		{"box.tail1234.ts.net", "https://evil.tail1234.ts.net", http.StatusForbidden},
		{"BOX.tail1234.ts.net", "https://BOX.tail1234.ts.net", http.StatusMisdirectedRequest},
		{"box.tail1234.ts.net:443", "https://box.tail1234.ts.net:443", http.StatusMisdirectedRequest},
		{"127.0.0.1:41237", "http://127.0.0.1:41237", http.StatusMisdirectedRequest},
		{"evil.com", "https://evil.com", http.StatusMisdirectedRequest},
	} {
		r := httptest.NewRequest(http.MethodPost, "/auth", nil)
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		Guard(s, &okHandler{}).ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Errorf("host %q origin %q: %d, want %d", tc.host, tc.origin, w.Code, tc.code)
		}
	}
}

// Only a real tailnet machine name is a tailnet site: no IP, no port, no
// name outside ts.net, nothing a browser would not send as Host exactly.
func TestTailnetSiteRefusesOtherNames(t *testing.T) {
	for _, h := range []string{"", "127.0.0.1", "100.64.0.1", "box.tail1234.ts.net:443", "box.example.com",
		"ts.net", ".ts.net", "BOX.tail1234.ts.net", "box..ts.net", "box.tail1234.ts.net.", "a b.ts.net", "box/x.ts.net"} {
		if _, err := TailnetSite(h); err == nil {
			t.Errorf("TailnetSite(%q) took it", h)
		}
	}
}

// HSTS goes out on a tailnet site and never on loopback (a browser would
// pin https for 127.0.0.1 and break every later local link).
func TestHSTSOnlyInTailnet(t *testing.T) {
	s, _ := TailnetSite("box.tail1234.ts.net")
	for _, tc := range []struct {
		site Site
		host string
		want string
	}{
		{s, "box.tail1234.ts.net", "max-age=31536000"},
		{testSite, "127.0.0.1:41237", ""},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = tc.host
		w := httptest.NewRecorder()
		Guard(tc.site, &okHandler{}).ServeHTTP(w, r)
		if got := w.Header().Get("Strict-Transport-Security"); got != tc.want {
			t.Errorf("%s: HSTS %q, want %q", tc.host, got, tc.want)
		}
	}
}
