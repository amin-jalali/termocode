package httpclient

import (
	"reflect"
	"testing"
)

func TestToCurl(t *testing.T) {
	body := `{"name":"it's"}`
	tmplBody := body
	rr := Resolved{
		Request: Request{Method: "POST", URL: "https://api.example.com/users", Headers: []Header{
			{"Content-Type", "application/json"}, {"Authorization", "Bearer sekrit-token-123"}}, Body: &body},
		Template: Request{Method: "POST", URL: "{{host}}/users", Headers: []Header{
			{"Content-Type", "application/json"}, {"Authorization", "Bearer {{token}}"}}, Body: &tmplBody},
		Secrets: []string{"sekrit-token-123"},
	}
	want := `curl -X POST 'https://api.example.com/users' -H 'Content-Type: application/json' -H 'Authorization: Bearer {{token}}' --data-raw '{"name":"it'\''s"}' --compressed`
	if got := ToCurl(rr, false); got != want {
		t.Fatalf("without secrets\n got  %s\n want %s", got, want)
	}
	want = `curl -X POST 'https://api.example.com/users' -H 'Content-Type: application/json' -H 'Authorization: Bearer sekrit-token-123' --data-raw '{"name":"it'\''s"}' --compressed`
	if got := ToCurl(rr, true); got != want {
		t.Fatalf("with secrets\n got  %s\n want %s", got, want)
	}
	// A secret inside the URL falls back to the template URL.
	rr2 := Resolved{Request: Request{Method: "GET", URL: "https://a.com/?k=sekrit-token-123"}, Template: Request{Method: "GET", URL: "https://a.com/?k={{key}}"}, Secrets: []string{"sekrit-token-123"}}
	if got := ToCurl(rr2, false); got != `curl -X GET 'https://a.com/?k={{key}}' --compressed` {
		t.Fatalf("url secret: %s", got)
	}
}

func TestFromCurl(t *testing.T) {
	cases := []struct {
		name string
		line string
		want Request
	}{
		{"bare", `curl https://a.com/x`, Request{Method: "GET", URL: "https://a.com/x"}},
		{"-X and -H", `curl -X DELETE 'https://a.com/x' -H 'Accept: application/json'`,
			Request{Method: "DELETE", URL: "https://a.com/x", Headers: []Header{{"Accept", "application/json"}}}},
		{"long flags with =", `curl --request=PUT --url=https://a.com/x --header "X-A: 1"`,
			Request{Method: "PUT", URL: "https://a.com/x", Headers: []Header{{"X-A", "1"}}}},
		{"glued -X", `curl -XPATCH https://a.com`, Request{Method: "PATCH", URL: "https://a.com"}},
		{"-d means POST", `curl https://a.com -d 'a=1' -d 'b=2'`, Request{Method: "POST", URL: "https://a.com", Body: sp("a=1&b=2")}},
		{"--data-raw json", `curl 'https://a.com/u' -H 'Content-Type: application/json' --data-raw '{"name":"Ada"}'`,
			Request{Method: "POST", URL: "https://a.com/u", Headers: []Header{{"Content-Type", "application/json"}}, Body: sp(`{"name":"Ada"}`)}},
		{"--data-binary", `curl --data-binary 'x y' https://a.com`, Request{Method: "POST", URL: "https://a.com", Body: sp("x y")}},
		{"--json", `curl --json '{"a":1}' https://a.com`,
			Request{Method: "POST", URL: "https://a.com", Headers: []Header{{"Content-Type", "application/json"}, {"Accept", "application/json"}}, Body: sp(`{"a":1}`)}},
		{"-u basic", `curl -u ada:pw https://a.com`, Request{Method: "GET", URL: "https://a.com", Headers: []Header{{"Authorization", "Basic YWRhOnB3"}}}},
		{"-G moves data to query", `curl -G https://a.com/s -d q=go -d n=2`, Request{Method: "GET", URL: "https://a.com/s?q=go&n=2"}},
		{"-L -k --compressed ignored", `curl -L -k --compressed -s https://a.com`, Request{Method: "GET", URL: "https://a.com"}},
		{"line continuations and double quotes", "curl \\\n  -H \"Authorization: Bearer x\\\"y\" \\\n  https://a.com",
			Request{Method: "GET", URL: "https://a.com", Headers: []Header{{"Authorization", `Bearer x"y`}}}},
		{"-A and -b", `curl -A agent/1 -b 'a=1' https://a.com`, Request{Method: "GET", URL: "https://a.com", Headers: []Header{{"User-Agent", "agent/1"}, {"Cookie", "a=1"}}}},
		{"no curl word", `https://a.com -X HEAD`, Request{Method: "HEAD", URL: "https://a.com"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := FromCurl(c.line)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(viewOf([]Request{got}), viewOf([]Request{c.want})) {
				t.Fatalf("FromCurl(%q)\n got  %#v\n want %#v", c.line, viewOf([]Request{got}), viewOf([]Request{c.want}))
			}
		})
	}
	for _, bad := range []string{``, `curl`, `curl -H`, `curl 'unterminated`, `curl -d @file.json https://a.com`, `curl --frobnicate https://a.com`} {
		if _, err := FromCurl(bad); err == nil {
			t.Errorf("FromCurl(%q) must fail", bad)
		}
	}
}

func TestParseQuick(t *testing.T) {
	cases := []struct {
		line string
		want Request
	}{
		{"https://httpbin.org/get", Request{Method: "GET", URL: "https://httpbin.org/get"}},
		{"  get https://a.com  ", Request{Method: "GET", URL: "https://a.com"}},
		{`POST https://a.com/u {"a": 1, "b": [1, 2]}`, Request{Method: "POST", URL: "https://a.com/u", Body: sp(`{"a": 1, "b": [1, 2]}`)}},
		{`PUT https://a.com/u -H 'Authorization: Bearer {{token}}' -H "X-A: 1" [1,2]`,
			Request{Method: "PUT", URL: "https://a.com/u", Headers: []Header{{"Authorization", "Bearer {{token}}"}, {"X-A", "1"}}, Body: sp("[1,2]")}},
		{"{{host}}/users", Request{Method: "GET", URL: "{{host}}/users"}},
		{"localhost:8080/health", Request{Method: "GET", URL: "http://localhost:8080/health"}},
		{"DELETE http://localhost/x?a=[1]", Request{Method: "DELETE", URL: "http://localhost/x?a=[1]"}},
	}
	for _, c := range cases {
		got, err := ParseQuick(c.line)
		if err != nil {
			t.Fatalf("%q: %v", c.line, err)
		}
		if !reflect.DeepEqual(viewOf([]Request{got}), viewOf([]Request{c.want})) {
			t.Fatalf("ParseQuick(%q)\n got  %#v\n want %#v", c.line, viewOf([]Request{got}), viewOf([]Request{c.want}))
		}
	}
	for _, bad := range []string{"", "POST", "-H 'a: b'", "GET https://a.com -H"} {
		if _, err := ParseQuick(bad); err == nil {
			t.Errorf("ParseQuick(%q) must fail", bad)
		}
	}
}
