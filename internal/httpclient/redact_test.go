package httpclient

import (
	"net"
	"strings"
	"testing"
)

func TestSecretHeader(t *testing.T) {
	for _, n := range []string{"Authorization", "proxy-authorization", "Cookie", "Set-Cookie", "X-API-Key", "x-auth-token",
		"X-Amz-Security-Token", "X-Csrf-Token", "X-Client-Secret", "X-Password", "Api-Key", "X-Apikey"} {
		if !SecretHeader(n) {
			t.Errorf("%s must be secret", n)
		}
	}
	for _, n := range []string{"Accept", "Content-Type", "User-Agent", "X-Request-Id"} {
		if SecretHeader(n) {
			t.Errorf("%s must not be secret", n)
		}
	}
}

func TestRedactHeaders(t *testing.T) {
	in := []Header{
		{"Authorization", "Bearer sekrit-token-123"},
		{"authorization", "sekrit-token-123"},
		{"Cookie", "a=1; b=sekrit"},
		{"set-cookie", "session=sekrit; Path=/; HttpOnly"},
		{"X-Api-Key", "sekrit"},
		{"Accept", "application/json"},
	}
	got := RedactHeaders(in)
	want := []string{"Bearer [REDACTED]", "[REDACTED]", "a=[REDACTED]; b=[REDACTED]", "session=[REDACTED]; Path=/; HttpOnly", "[REDACTED]", "application/json"}
	for i, h := range got {
		if h.Value != want[i] {
			t.Errorf("%s: %q want %q", h.Name, h.Value, want[i])
		}
		if strings.Contains(h.Value, "sekrit") {
			t.Errorf("leak in %s", h.Value)
		}
	}
	if in[0].Value != "Bearer sekrit-token-123" {
		t.Fatal("RedactHeaders must copy")
	}
}

func TestRedactURL(t *testing.T) {
	cases := map[string]string{
		"https://a.com/x?token=abc123&page=2":        "https://a.com/x?token=[REDACTED]&page=2",
		"https://a.com/x?page=2&api_key=k1":          "https://a.com/x?page=2&api_key=[REDACTED]",
		"https://a.com/x?access_token=zz#frag":       "https://a.com/x?access_token=[REDACTED]#frag",
		"https://a.com/x?X-Amz-Signature=deadbeef":   "https://a.com/x?X-Amz-Signature=[REDACTED]",
		"https://user:pa55word@a.com/x":              "https://user:[REDACTED]@a.com/x",
		`Get "https://a.com/?password=hunter2": EOF`: `Get "https://a.com/?password=[REDACTED]": EOF`,
		"https://a.com/users/42?page=1":              "https://a.com/users/42?page=1",
	}
	for in, want := range cases {
		if got := RedactURL(in); got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClassifyHost(t *testing.T) {
	cases := map[string]HostClass{
		"localhost": HostLoopback, "127.0.0.1": HostLoopback, "127.1.2.3": HostLoopback, "::1": HostLoopback, "[::1]": HostLoopback, "0.0.0.0": HostLoopback,
		"10.0.0.1": HostPrivate, "192.168.1.5": HostPrivate, "172.16.0.1": HostPrivate, "169.254.169.254": HostPrivate,
		"fd00::1": HostPrivate, "fe80::1": HostPrivate, "100.64.0.1": HostPrivate,
		"db": HostPrivate, "redis": HostPrivate, "api.internal": HostPrivate, "printer.local": HostPrivate,
		"api.example.com": HostPublic, "8.8.8.8": HostPublic, "LOCALHOST": HostLoopback,
	}
	for h, want := range cases {
		if got := ClassifyHost(h); got != want {
			t.Errorf("ClassifyHost(%q) = %v, want %v", h, got, want)
		}
	}
	if IsPrivateIP(net.ParseIP("127.0.0.1")) || !IsPrivateIP(net.ParseIP("169.254.169.254")) || IsPrivateIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("IsPrivateIP wrong")
	}
	if HostKey("http://LocalHost:8080/x") != "localhost:8080" || HostKey("https://api.example.com/") != "api.example.com" {
		t.Fatal("HostKey wrong")
	}
}

func TestSummary(t *testing.T) {
	r := &Response{Status: 200, StatusText: "OK", Elapsed: 118e6, Size: 1432, IsJSON: true, Hops: []Hop{{301, "x"}, {307, "y"}}}
	if got := r.Summary(); got != "200 OK · 118 ms · 1.4 KB · json · 2 redirects" {
		t.Fatalf("summary %q", got)
	}
	r = &Response{Err: "timed out after 30 s"}
	if r.Summary() != "✘ timed out after 30 s" || r.StatusLine() != "ERR" {
		t.Fatal(r.Summary())
	}
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KB", 1536: "1.5 KB", 5 << 20: "5.0 MB"} {
		if got := FormatSize(n); got != want {
			t.Errorf("FormatSize(%d) = %q want %q", n, got, want)
		}
	}
}

// TestPrettyJSONDartCases ports rest_response_result_test.dart's
// prettyBody / isJson groups.
func TestPrettyJSONDartCases(t *testing.T) {
	if p, ok := PrettyJSON([]byte(`[1,2,3]`)); !ok || string(p) != "[\n  1,\n  2,\n  3\n]" {
		t.Fatalf("array %q", p)
	}
	if p, ok := PrettyJSON([]byte(`not json at all`)); ok || string(p) != "not json at all" {
		t.Fatal("raw body when not JSON")
	}
	if _, ok := PrettyJSON(nil); ok {
		t.Fatal("empty body is not JSON")
	}
	p, _ := PrettyJSON([]byte(`{"user":{"id":1,"name":"Bob"},"v":null,"a":true,"d":false,"f":1.50}`))
	if string(p) != "{\n  \"user\": {\n    \"id\": 1,\n    \"name\": \"Bob\"\n  },\n  \"v\": null,\n  \"a\": true,\n  \"d\": false,\n  \"f\": 1.50\n}" {
		t.Fatalf("nested %q", p)
	}
	for ct, want := range map[string]bool{"application/json": true, "application/vnd.api+json": true, "text/html": false, "": false} {
		if isJSONType(ct) != want {
			t.Errorf("isJSONType(%q) != %v", ct, want)
		}
	}
	if mt, cs := parseContentType("Application/JSON; Charset=UTF-8"); mt != "application/json" || cs != "utf-8" {
		t.Fatalf("parseContentType %q %q", mt, cs)
	}
}
