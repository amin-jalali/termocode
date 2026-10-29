package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/httpclient"
)

func writeHTTPSecret(t *testing.T, cfg, val string) {
	t.Helper()
	p := filepath.Join(cfg, "termocode", "ai", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"mcp_secrets": {"pass": "`+val+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// PERF-8: one resolver (one send, one buffer refresh) reads the
// credentials once, however many {{$secret}} it meets; the next
// resolver reads them again, so a changed secret is used.
func TestHTTPResolverReadsSecretsOnce(t *testing.T) {
	cfg := withConfigDir(t)
	writeHTTPSecret(t, cfg, "pw-one-123456")
	loads := 0
	orig := httpLoadCredentials
	httpLoadCredentials = func(p string) (ai.Credentials, error) { loads++; return orig(p) }
	t.Cleanup(func() { httpLoadCredentials = orig })

	body := "{{$secret pass}} {{$secret pass}} {{$secret nope}}"
	req := httpclient.Request{Method: "POST", URL: "http://x/{{$secret pass}}", Headers: []httpclient.Header{{Name: "X", Value: "{{$secret pass}}"}}, Body: &body}
	rv := httpResolver("", nil, "-")
	if !rv.Known("$secret pass") || rv.Known("$secret nope") {
		t.Fatal("Known")
	}
	rr, un := rv.Resolve(req)
	if rr.URL != "http://x/pw-one-123456" || rr.Headers[0].Value != "pw-one-123456" || len(un) != 1 {
		t.Fatalf("resolved %q %q, unresolved %v", rr.URL, rr.Headers[0].Value, un)
	}
	if loads != 1 {
		t.Fatalf("credentials read %d times for one resolver", loads)
	}
	writeHTTPSecret(t, cfg, "pw-two-654321")
	rr, _ = httpResolver("", nil, "-").Resolve(req)
	if rr.URL != "http://x/pw-two-654321" {
		t.Fatalf("a new resolver used the old secret: %q", rr.URL)
	}
}
