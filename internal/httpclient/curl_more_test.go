package httpclient

import (
	"strings"
	"testing"
)

// More pasted-curl shapes: backslash escapes, CRLF line ends, a referer,
// -G onto a URL that has a query, and each flag that lacks its value.
func TestFromCurlMoreShapes(t *testing.T) {
	r, err := FromCurl("curl -e https://ref.test -G 'https://a.com/s?x=1' -d q=go \\\r\n  -H X-A:\\ 1 https\\://ignored.example")
	if err == nil {
		t.Fatalf("two URLs must fail: %+v", r)
	}
	r, err = FromCurl("curl -e https://ref.test -G 'https://a.com/s?x=1' -d q=go \\\r\n  -H X-A:\\ 1")
	if err != nil {
		t.Fatal(err)
	}
	if r.URL != "https://a.com/s?x=1&q=go" || r.Header("Referer") != "https://ref.test" || r.Header("X-A") != "1" {
		t.Fatalf("%+v", r)
	}
	for _, line := range []string{
		"curl https://a.com -X", "curl https://a.com -d", "curl https://a.com -u", "curl https://a.com -A",
		"curl https://a.com -b", "curl https://a.com -e", "curl --url", `curl -H 'no colon' https://a.com`,
		`curl "unterminated https://a.com`,
	} {
		if _, err := FromCurl(line); err == nil {
			t.Errorf("FromCurl(%q) must fail", line)
		}
	}
}

// Quick lines: an unknown option, a stray word, a header without a
// name, and a broken quote are refused with a reason.
func TestParseQuickRefusals(t *testing.T) {
	for line, want := range map[string]string{
		"GET https://a.com --verbose": "unknown option --verbose",
		"GET https://a.com extra":     `unexpected "extra"`,
		"GET https://a.com -H ':v'":   "bad header",
		"GET 'https://a.com":          "unterminated",
		"G3T https://a.com":           `unexpected "https://a.com"`,
	} {
		_, err := ParseQuick(line)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseQuick(%q) = %v, want %q", line, err, want)
		}
	}
}

// The live pretty view: a body cut inside a string and broken nesting.
func TestIndentPrefixCutAndBroken(t *testing.T) {
	out, complete, ok := IndentPrefix([]byte(`{"a":"unfinished`), 100)
	if !ok || complete || string(out) != "{\n  \"a\": \"unfinished" {
		t.Fatalf("%q %v %v", out, complete, ok)
	}
	if _, _, ok := IndentPrefix([]byte(`{}}`), 100); ok {
		t.Fatal("extra closer accepted")
	}
	out, complete, ok = IndentPrefix([]byte(`{"a":1}`), 5)
	if !ok || complete || len(out) != 5 {
		t.Fatalf("cap: %q %v %v", out, complete, ok)
	}
}
