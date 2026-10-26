package app

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/httpclient"
)

// HC-8: security checks for the HTTP client's AI paths (plan §4.3, §5).

// The audit line never shows a literal token in the arguments.
func TestAIAuditArgsRedactSecrets(t *testing.T) {
	args := map[string]any{
		"method":  "POST",
		"url":     "https://api.example.com/x?token=sekrit-token-123",
		"headers": map[string]any{"Authorization": "Bearer sekrit-token-123"},
	}
	if got := aiAuditArgs(args); strings.Contains(got, "sekrit") {
		t.Fatalf("audit args leak a token: %s", got)
	}
}

// Prefixed tokens (GitHub, GitLab, Slack, Google, Linear) are hidden in
// the audit line too.
func TestAIAuditArgsRedactPrefixedTokens(t *testing.T) {
	for _, tok := range []string{
		"ghp_0123456789abcdefABCDEF0123456789", "github_pat_11ABCDEFG0123456789abcdef",
		"glpat" + "-0123456789abcdefABCD", "xoxb-1234567890-abcdefghijkl", "AIzaSyA0123456789abcdefghij",
		"lin_api_0123456789abcdefABCDEF",
	} {
		got := aiAuditArgs(map[string]any{"note": tok, "text": "use " + tok + " here"})
		if strings.Contains(got, tok[:len(tok)-4]) {
			t.Errorf("audit args leak %s: %s", tok, got)
		}
	}
}

// The per-call gate (plan §4.3), first match wins.
func TestHTTPAIGateTable(t *testing.T) {
	cases := []struct {
		method, url string
		mutating    bool
		class       string
	}{
		{"GET", "http://localhost:8080/x", false, ""},
		{"HEAD", "http://127.0.0.1/x", false, ""},
		{"OPTIONS", "http://[::1]:3000/", false, ""},
		{"GET", "http://0.0.0.0:9/", false, ""},
		{"get", "http://LOCALHOST:8080/", false, ""},
		{"POST", "http://localhost:8080/users", true, "http:localhost:8080/write"},
		{"DELETE", "http://127.0.0.1:5000/u/1", true, "http:127.0.0.1:5000/write"},
		{"PUT", "http://localhost/", true, "http:localhost/write"},
		{"GET", "https://api.example.com/users", true, "http:api.example.com"},
		{"HEAD", "https://api.example.com:8443/", true, "http:api.example.com:8443"},
		{"PATCH", "https://api.example.com/u", true, "http:api.example.com/write"},
		{"DELETE", "https://api.prod.example/x", true, "http:api.prod.example/write"},
		{"GET", "http://10.0.0.1/", true, ""},
		{"GET", "http://192.168.1.10:8080/", true, ""},
		{"GET", "http://172.16.0.5/", true, ""},
		{"GET", "http://169.254.169.254/latest/meta-data/", true, ""},
		{"POST", "http://[fd00::1]/", true, ""},
		{"GET", "http://db:5432/", true, ""},
		{"GET", "http://api.internal/", true, ""},
		{"GET", "http://printer.local/", true, ""},
	}
	for _, c := range cases {
		m, cl := httpAIGate(c.method, c.url)
		if m != c.mutating || cl != c.class {
			t.Errorf("%s %s: got (%v, %q), want (%v, %q)", c.method, c.url, m, cl, c.mutating, c.class)
		}
	}
}

// The dial hook refuses private / metadata IPs for a host that did not
// go through the "always asks" path; redirect hops are re-gated.
func TestHTTPAIHooks(t *testing.T) {
	h := httpAIHooks("GET", "https://evil.example/x")
	if h.AllowIP == nil || h.AllowIP(net.ParseIP("169.254.169.254")) || h.AllowIP(net.ParseIP("10.0.0.1")) {
		t.Fatal("a public name must not reach a private IP")
	}
	if !h.AllowIP(net.ParseIP("127.0.0.1")) || !h.AllowIP(net.ParseIP("93.184.216.34")) {
		t.Fatal("loopback and public IPs are fine")
	}
	if h.Insecure {
		t.Fatal("AI sends never skip TLS checks")
	}
	if err := h.CheckHop("https://evil.example/y"); err != nil {
		t.Fatalf("same host: %v", err)
	}
	if err := h.CheckHop("http://169.254.169.254/"); err == nil {
		t.Fatal("a hop to a private address must stop the send")
	}
	if err := h.CheckHop("https://other.example/"); err == nil || !strings.Contains(err.Error(), "other.example") {
		t.Fatalf("a hop to a new host needs its own approval: %v", err)
	}
	if err := httpAIHooks("GET", "http://localhost:8080/").CheckHop("http://127.0.0.1:9000/"); err != nil {
		t.Fatalf("GET loopback → loopback runs at once: %v", err)
	}
	p := httpAIHooks("GET", "http://10.0.0.1/")
	if !p.AllowIP(net.ParseIP("10.0.0.1")) {
		t.Fatal("the user allowed this private host")
	}
}

// The tool result: status line, hops, redacted headers, capped body
// with a paging hint; no secret survives.
func TestHTTPAIResultText(t *testing.T) {
	ai.RegisterSecret("dev-secret-token-99")
	body := `{"echo":"dev-secret-token-99","items":[` + strings.Repeat(`{"id":1},`, 3000) + `{"id":2}]}`
	resp := &httpclient.Response{Status: 500, StatusText: "Internal Server Error", Proto: "HTTP/1.1", Elapsed: 41 * time.Millisecond,
		Size: int64(len(body)), ContentType: "application/json", IsJSON: true, Body: []byte(body),
		Headers: []httpclient.Header{{Name: "content-type", Value: "application/json"}, {Name: "set-cookie", Value: "s=cookie-77; Path=/"}},
		Hops:    []httpclient.Hop{{Status: 301, URL: "http://localhost:8080/new"}}}
	out := httpAIResultText(7, resp, "body", 0, 1024)
	for _, want := range []string{"response #7", "HTTP/1.1 500 Internal Server Error · 41 ms", "301 → http://localhost:8080/new",
		"set-cookie: s=[REDACTED]; Path=/", "\"items\": [", "[truncated:", `http_get_last_response(id:7, part:"body", offset:1024)`} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "dev-secret-token-99") || strings.Contains(out, "cookie-77") {
		t.Fatalf("result leaks a secret:\n%s", out)
	}
	page2 := httpAIResultText(7, resp, "body", 1024, 1024)
	if !strings.Contains(page2, "offset 1024") || strings.Contains(page2, "HTTP/1.1 500") {
		t.Fatalf("page 2:\n%s", page2)
	}
	bin := httpAIResultText(8, &httpclient.Response{Status: 200, StatusText: "OK", IsBinary: true, ContentType: "image/png", Size: 48 << 10, Body: []byte{1}}, "body", 0, 1024)
	if !strings.Contains(bin, "binary (image/png, 48.0 KB) — not shown") {
		t.Fatalf("binary:\n%s", bin)
	}
	fail := httpAIResultText(9, &httpclient.Response{Err: "blocked: private address 169.254.169.254 for evil.example", ErrKind: httpclient.ErrKindBlocked}, "summary", 0, 1024)
	if !strings.Contains(fail, "request failed: blocked: private address") {
		t.Fatalf("failure:\n%s", fail)
	}
}

// A 5 MB body: the result is built from a capped prefix (budget 5 ms).
func TestHTTPAIResultBigBody(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("[")
	for sb.Len() < 5<<20 {
		sb.WriteString(`{"id":1,"name":"user","tags":["a","b"]},`)
	}
	sb.WriteString(`{"id":2}]`)
	resp := &httpclient.Response{Status: 200, StatusText: "OK", IsJSON: true, ContentType: "application/json", Body: []byte(sb.String()), Size: int64(sb.Len())}
	start := time.Now()
	out := httpAIResultText(1, resp, "body", 0, 32<<10)
	if d := time.Since(start); d > 5*time.Millisecond && !raceEnabled {
		t.Errorf("result for a 5 MB body took %v", d)
	}
	if len(out) > 40<<10 {
		t.Fatalf("result is %d bytes", len(out))
	}
}
