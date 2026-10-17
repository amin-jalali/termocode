package httpclient

import (
	"reflect"
	"testing"
)

// view is the part of a Request the Dart tests look at.
type view struct {
	Method  string
	URL     string
	Headers []Header
	Body    *string
	Name    string
}

func viewOf(rs []Request) []view {
	out := make([]view, 0, len(rs))
	for _, r := range rs {
		h := r.Headers
		if len(h) == 0 {
			h = nil
		}
		out = append(out, view{r.Method, r.URL, h, r.Body, r.Name})
	}
	return out
}

func sp(s string) *string { return &s }

func hs(kv ...string) []Header {
	var out []Header
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, Header{Name: kv[i], Value: kv[i+1]})
	}
	return out
}

// TestParseDartCases ports every parse case of mobocode's
// test/rest_parser_test.dart (groups "single request", "multiple
// requests", "variable placeholders" and "edge cases"). The Go parser
// must give the same result for each.
func TestParseDartCases(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []view
	}{
		{"minimal GET", "GET https://example.com/items\n",
			[]view{{Method: "GET", URL: "https://example.com/items"}}},
		{"upper-cases the method", "get http://localhost/\n",
			[]view{{Method: "GET", URL: "http://localhost/"}}},
		{"strips HTTP/1.1 suffix", "GET https://example.com/ HTTP/1.1\n",
			[]view{{Method: "GET", URL: "https://example.com/"}}},
		{"request headers", "POST https://api.example.com/data\nContent-Type: application/json\nAuthorization: Bearer mytoken123\n\n",
			[]view{{Method: "POST", URL: "https://api.example.com/data", Headers: hs("Content-Type", "application/json", "Authorization", "Bearer mytoken123")}}},
		{"body after blank line", "POST https://api.example.com/items\nContent-Type: application/json\n\n{\"name\":\"foo\",\"value\":42}\n",
			[]view{{Method: "POST", URL: "https://api.example.com/items", Headers: hs("Content-Type", "application/json"), Body: sp(`{"name":"foo","value":42}`)}}},
		{"trailing blank lines stripped", "POST https://x.com/\n\nbody line\n\n\n",
			[]view{{Method: "POST", URL: "https://x.com/", Body: sp("body line")}}},
		{"empty string", "", []view{}},
		{"comment-only file", "# just a comment\n# another\n", []view{}},
		{"comment in header block", "GET https://example.com\n# this is a comment\nAccept: application/json\n\n",
			[]view{{Method: "GET", URL: "https://example.com", Headers: hs("Accept", "application/json")}}},
		{"@name directive", "# @name getUserById\nGET https://example.com/users/42\n\n",
			[]view{{Method: "GET", URL: "https://example.com/users/42", Name: "getUserById"}}},
		{"body-less no blank line", "DELETE https://api.example.com/items/1\n",
			[]view{{Method: "DELETE", URL: "https://api.example.com/items/1"}}},
		{"two requests by ###", "GET https://example.com/one\n\n###\n\nPOST https://example.com/two\nContent-Type: application/json\n\n{\"hello\":\"world\"}\n",
			[]view{
				{Method: "GET", URL: "https://example.com/one"},
				{Method: "POST", URL: "https://example.com/two", Headers: hs("Content-Type", "application/json"), Body: sp(`{"hello":"world"}`)},
			}},
		{"### title names the request", "### Create Item\nPOST https://example.com/items\n\n{}\n###\nGET https://example.com/items\n",
			[]view{
				{Method: "POST", URL: "https://example.com/items", Body: sp("{}"), Name: "Create Item"},
				{Method: "GET", URL: "https://example.com/items"},
			}},
		{"first block before ###", "GET https://example.com/first\n\n###\nGET https://example.com/second\n",
			[]view{{Method: "GET", URL: "https://example.com/first"}, {Method: "GET", URL: "https://example.com/second"}}},
		{"three requests", "GET https://a.com\n###\nPOST https://b.com\n\n{\"x\":1}\n###\nDELETE https://c.com/1\n",
			[]view{{Method: "GET", URL: "https://a.com"}, {Method: "POST", URL: "https://b.com", Body: sp(`{"x":1}`)}, {Method: "DELETE", URL: "https://c.com/1"}}},
		{"comment block skipped", "GET https://a.com\n###\n# This block has no request line\n###\nDELETE https://c.com\n",
			[]view{{Method: "GET", URL: "https://a.com"}, {Method: "DELETE", URL: "https://c.com"}}},
		{"{{var}} in URL", "GET https://example.com/{{version}}/items\n",
			[]view{{Method: "GET", URL: "https://example.com/{{version}}/items"}}},
		{"{{var}} in headers", "GET https://example.com\nAuthorization: Bearer {{token}}\n\n",
			[]view{{Method: "GET", URL: "https://example.com", Headers: hs("Authorization", "Bearer {{token}}")}}},
		{"{{var}} in body", "POST https://example.com\nContent-Type: application/json\n\n{\"id\": \"{{userId}}\"}\n",
			[]view{{Method: "POST", URL: "https://example.com", Headers: hs("Content-Type", "application/json"), Body: sp(`{"id": "{{userId}}"}`)}}},
		{"CRLF", "GET https://example.com\r\nAccept: text/plain\r\n",
			[]view{{Method: "GET", URL: "https://example.com", Headers: hs("Accept", "text/plain")}}},
		{"multi-line body", "POST https://example.com\nContent-Type: application/json\n\n{\n  \"name\": \"Alice\",\n  \"age\": 30\n}\n",
			[]view{{Method: "POST", URL: "https://example.com", Headers: hs("Content-Type", "application/json"), Body: sp("{\n  \"name\": \"Alice\",\n  \"age\": 30\n}")}}},
		{"blank body is nil", "POST https://example.com\n\n",
			[]view{{Method: "POST", URL: "https://example.com"}}},
		{"leading blank lines", "\n\nGET https://example.com\n",
			[]view{{Method: "GET", URL: "https://example.com"}}},
		{"header value with colons", "GET https://example.com\nX-Custom: value:with:colons\n\n",
			[]view{{Method: "GET", URL: "https://example.com", Headers: hs("X-Custom", "value:with:colons")}}},
		// Dart behaviour that is easy to get wrong.
		{"header line without colon skipped", "GET https://a.com\nnot a header\nAccept: x\n",
			[]view{{Method: "GET", URL: "https://a.com", Headers: hs("Accept", "x")}}},
		{"empty header name skipped", "GET https://a.com\n: x\n",
			[]view{{Method: "GET", URL: "https://a.com"}}},
		{"no URL after method dropped", "GET \n", []view{}},
		{"method only dropped", "GET\n", []view{}},
		{"#### is a separator, title keeps the 4th #", "GET https://a.com\n#### Two\nGET https://b.com\n",
			[]view{{Method: "GET", URL: "https://a.com"}, {Method: "GET", URL: "https://b.com", Name: "# Two"}}},
		{"@name overrides ### title", "### Title\n# @name real\nGET https://a.com\n",
			[]view{{Method: "GET", URL: "https://a.com", Name: "real"}}},
		{"body keeps comment lines", "POST https://a.com\n\n{}\n\n# note\n###\nGET https://b.com\n",
			[]view{{Method: "POST", URL: "https://a.com", Body: sp("{}\n\n# note")}, {Method: "GET", URL: "https://b.com"}}},
		{"any method word", "propfind https://dav.example/x\n",
			[]view{{Method: "PROPFIND", URL: "https://dav.example/x"}}},
		{"HTTP/2 suffix", "GET https://a.com HTTP/2\n",
			[]view{{Method: "GET", URL: "https://a.com"}}},
		{"named second request serialization shape", "###\n### Name\nGET https://a.com\n",
			[]view{{Method: "GET", URL: "https://a.com", Name: "Name"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := viewOf(Parse(c.src).Requests)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Parse(%q)\n got  %#v\n want %#v", c.src, got, c.want)
			}
		})
	}
}

// TestParseSuperset covers what termocode accepts beyond the Dart
// parser. Each case is harmless for mobocode: the serializer writes the
// common subset.
func TestParseSuperset(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []view
		vars []Var
		dirs map[string]string
	}{
		{name: "// comment before request", src: "// hello\nGET https://a.com\n",
			want: []view{{Method: "GET", URL: "https://a.com"}}},
		{name: "// @name", src: "// @name viaSlash\nGET https://a.com\n",
			want: []view{{Method: "GET", URL: "https://a.com", Name: "viaSlash"}}},
		{name: "// comment in headers", src: "GET https://a.com\n// skip\nAccept: x\n",
			want: []view{{Method: "GET", URL: "https://a.com", Headers: hs("Accept", "x")}}},
		{name: "bare https URL is GET", src: "https://a.com/x\n",
			want: []view{{Method: "GET", URL: "https://a.com/x"}}},
		{name: "bare {{host}} URL is GET", src: "{{host}}/users\n",
			want: []view{{Method: "GET", URL: "{{host}}/users"}}},
		{name: "bare URL with HTTP suffix", src: "https://a.com/x HTTP/1.1\n",
			want: []view{{Method: "GET", URL: "https://a.com/x"}}},
		{name: "file variables", src: "@host = https://a.com\n@token=abc\n\n### one\nGET {{host}}/x\n",
			want: []view{{Method: "GET", URL: "{{host}}/x", Name: "one"}},
			vars: []Var{{Name: "host", Value: "https://a.com", Line: 1}, {Name: "token", Value: "abc", Line: 2}}},
		{name: "variable right above request", src: "@id = 42\nGET https://a.com/{{id}}\n",
			want: []view{{Method: "GET", URL: "https://a.com/{{id}}"}},
			vars: []Var{{Name: "id", Value: "42", Line: 1}}},
		{name: "directives", src: "# @no-redirect\n# @no-cookie-jar\n// @timeout 10s\nGET https://a.com\n",
			want: []view{{Method: "GET", URL: "https://a.com"}},
			dirs: map[string]string{"no-redirect": "", "no-cookie-jar": "", "timeout": "10s"}},
		{name: "blank line between comments", src: "# one\n\n# two\nGET https://a.com\n",
			want: []view{{Method: "GET", URL: "https://a.com"}}},
		{name: "tab between method and URL", src: "GET\thttps://a.com\n",
			want: []view{{Method: "GET", URL: "https://a.com"}}},
		{name: "plain word without URL dropped", src: "hello\n", want: []view{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := Parse(c.src)
			got := viewOf(f.Requests)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Parse(%q)\n got  %#v\n want %#v", c.src, got, c.want)
			}
			if c.vars != nil && !reflect.DeepEqual(f.Vars, c.vars) {
				t.Fatalf("vars got %#v want %#v", f.Vars, c.vars)
			}
			if c.dirs != nil && !reflect.DeepEqual(f.Requests[0].Directives, c.dirs) {
				t.Fatalf("directives got %#v want %#v", f.Requests[0].Directives, c.dirs)
			}
		})
	}
}

func TestHeadersGetLastWins(t *testing.T) {
	f := Parse("GET https://a.com\nAccept: one\naccept: two\n")
	r := f.Requests[0]
	if got := r.Header("ACCEPT"); got != "two" {
		t.Fatalf("Header = %q, want last duplicate", got)
	}
	if len(r.Headers) != 2 {
		t.Fatalf("headers keep both lines: %v", r.Headers)
	}
}

func TestLines(t *testing.T) {
	src := "@host = x\n\n### one\n# @name getUser\nGET {{host}}/users/1\nAccept: a\n\n###\n# only a comment\n###\nPOST {{host}}/users\n\n{}\n"
	f := Parse(src)
	if len(f.Requests) != 2 {
		t.Fatalf("requests = %d", len(f.Requests))
	}
	a, b := f.Requests[0], f.Requests[1]
	if a.Line != 5 || a.StartLine != 3 || a.EndLine != 7 {
		t.Fatalf("a lines: line=%d start=%d end=%d", a.Line, a.StartLine, a.EndLine)
	}
	if b.Line != 11 || b.StartLine != 10 || b.EndLine != 14 {
		t.Fatalf("b lines: line=%d start=%d end=%d", b.Line, b.StartLine, b.EndLine)
	}
	for line, want := range map[int]string{1: "getUser", 3: "getUser", 5: "getUser", 8: "getUser", 9: "getUser", 10: "", 11: "", 14: "", 99: ""} {
		r := f.AtLine(line)
		if r == nil {
			t.Fatalf("AtLine(%d) = nil", line)
		}
		if r.Name != want {
			t.Fatalf("AtLine(%d) = %q, want %q", line, r.Name, want)
		}
	}
	if f := Parse(""); f.AtLine(1) != nil {
		t.Fatal("AtLine on an empty file must be nil")
	}
}
