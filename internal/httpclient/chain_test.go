package httpclient

import (
	"reflect"
	"slices"
	"testing"
)

func strPtr(s string) *string { return &s }

// chainResolver answers {{login...}} from one stored JSON response and
// {{feed...}} from an XML one.
func chainResolver(registered *[]string) *Resolver {
	login := &Response{
		Status:  201,
		Headers: []Header{{Name: "content-type", Value: "application/json"}, {Name: "set-cookie", Value: "sid=abc123"}, {Name: "x-request-id", Value: "r-77"}},
		Body:    []byte(`{"token":"tok-` + `xyz","user":{"id":42,"name":"Ada","admin":true,"tags":["a","b"]},"items":[{"id":1},{"id":2}],"none":null}`),
	}
	feed := &Response{
		Status: 200,
		Body:   []byte(`<?xml version="1.0"?><feed><entry id="e1"><title>First</title></entry><entry id="e2"><title>Second</title></entry></feed>`),
	}
	return &Resolver{
		Responses: func(name string) (*Response, bool) {
			switch name {
			case "login":
				return login, true
			case "feed":
				return feed, true
			}
			return nil, false
		},
		OnSecret: func(v string) { *registered = append(*registered, v) },
	}
}

func TestChainResolvesResponseParts(t *testing.T) {
	var reg []string
	rv := chainResolver(&reg)
	cases := map[string]string{
		"{{login.response.status}}":                   "201",
		"{{login.response.headers.X-Request-Id}}":     "r-77",
		"{{login.response.headers.content-type}}":     "application/json",
		"{{login.response.body.$.token}}":             "tok-xyz",
		"{{ login.response.body.$.user.id }}":         "42",
		"{{login.response.body.$.user.admin}}":        "true",
		"{{login.response.body.$.user.tags[1]}}":      "b",
		"{{login.response.body.$['user']['name']}}":   "Ada",
		"{{login.response.body.$.items[-1].id}}":      "2",
		"{{login.response.body.$.user.tags}}":         `["a","b"]`,
		"{{login.response.body.$.none}}":              "null",
		"{{feed.response.body.//entry[2]/title}}":     "Second",
		"{{feed.response.body./feed/entry/title}}":    "First",
		"{{feed.response.body.//entry[1]/@id}}":       "e1",
		"{{feed.response.body.//title/text()}}":       "First",
		"Bearer {{login.response.body.$.token}} done": "Bearer tok-xyz done",
	}
	for in, want := range cases {
		rr, un := rv.Resolve(Request{Method: "GET", URL: "http://x/", Body: strPtr(in)})
		if *rr.Body != want || len(un) != 0 {
			t.Errorf("%s = %q (unresolved %v), want %q", in, *rr.Body, un, want)
		}
	}
	whole, _ := rv.Resolve(Request{URL: "{{feed.response.body.*}}"})
	if whole.URL[:5] != "<?xml" {
		t.Errorf("body.* = %q", whole.URL)
	}
}

func TestChainUnknownStaysRaw(t *testing.T) {
	var reg []string
	rv := chainResolver(&reg)
	for _, in := range []string{
		"{{other.response.status}}",             // no such response
		"{{login.response.body.$.missing}}",     // path finds nothing
		"{{login.response.body.$.items[9].id}}", // index out of range
		"{{login.response.headers.X-Absent}}",   // no header
		"{{feed.response.body.//nothing}}",      // no element
		"{{login.response.body.//title}}",       // XPath on JSON
		"{{feed.response.body.$.title}}",        // JSONPath on XML
		"{{login.response.cookies.sid}}",        // unknown part
	} {
		rr, un := rv.Resolve(Request{Body: strPtr(in)})
		if *rr.Body != in || len(un) != 1 {
			t.Errorf("%s resolved to %q (unresolved %v)", in, *rr.Body, un)
		}
		if rv.Known(in[2 : len(in)-2]) {
			t.Errorf("Known(%s) = true", in)
		}
	}
	if !rv.Known("login.response.body.$.token") {
		t.Error("Known(login token) = false")
	}
	// Without a Responses source the name is just unknown.
	plain := &Resolver{}
	if rr, un := plain.Resolve(Request{URL: "{{login.response.status}}"}); rr.URL != "{{login.response.status}}" || len(un) != 1 {
		t.Errorf("no source: %q %v", rr.URL, un)
	}
}

func TestChainFileVarWithSameNameWins(t *testing.T) {
	var reg []string
	rv := chainResolver(&reg)
	rv.FileVars = []Var{{Name: "login.response.status", Value: "file"}}
	if rr, _ := rv.Resolve(Request{URL: "{{login.response.status}}"}); rr.URL != "file" {
		t.Errorf("got %q", rr.URL)
	}
}

func TestChainSecretsAreRegistered(t *testing.T) {
	var reg []string
	rv := chainResolver(&reg)
	rr, _ := rv.Resolve(Request{
		Headers: []Header{{Name: "Authorization", Value: "Bearer {{login.response.body.$.token}}"}, {Name: "Cookie", Value: "{{login.response.headers.Set-Cookie}}"}},
		Body:    strPtr("{{login.response.body.$.user.name}}"),
	})
	for _, want := range []string{"tok-xyz", "sid=abc123"} {
		if !slices.Contains(rr.Secrets, want) || !slices.Contains(reg, want) {
			t.Errorf("%q not hidden: secrets %v registered %v", want, rr.Secrets, reg)
		}
	}
	if slices.Contains(rr.Secrets, "Ada") {
		t.Errorf("a plain name was marked secret: %v", rr.Secrets)
	}
}

func TestResponseRefsListsDependencies(t *testing.T) {
	req := Request{
		URL:     "{{base}}/users/{{login.response.body.$.user.id}}",
		Headers: []Header{{Name: "Authorization", Value: "Bearer {{ login.response.body.$.token }}"}, {Name: "X", Value: "{{feed.response.status}}"}},
		Body:    strPtr(`{"a":"{{zeta.response.headers.Etag}}","b":"{{$guid}}"}`),
	}
	if got := ResponseRefs(req); !reflect.DeepEqual(got, []string{"feed", "login", "zeta"}) {
		t.Errorf("refs = %v", got)
	}
	if got := ResponseRefs(Request{URL: "http://x/{{host}}"}); len(got) != 0 {
		t.Errorf("plain refs = %v", got)
	}
}

func TestJSONPathEdgeCases(t *testing.T) {
	body := []byte(`{"a.b":{"c":"x"},"arr":[[1,2],[3]],"s":"q\"uote","n":1.50}`)
	for path, want := range map[string]string{
		`$['a.b'].c`:  "x",
		`$.arr[0][1]`: "2",
		`$.arr[1]`:    "[3]",
		`$.s`:         `q"uote`,
		`$.n`:         "1.50",
		`$`:           string(body),
	} {
		if got, ok := JSONPath(body, path); !ok || got != want {
			t.Errorf("JSONPath(%s) = %q %v, want %q", path, got, ok, want)
		}
	}
	for _, path := range []string{"", "a", "$.", "$[", "$['x", "$.arr[x]", "$.arr[0", "$..a", "$.*"} {
		if got, ok := JSONPath(body, path); ok {
			t.Errorf("JSONPath(%q) = %q, want no match", path, got)
		}
	}
	if _, ok := JSONPath([]byte("not json"), "$.a"); ok {
		t.Error("bad JSON matched")
	}
}

func FuzzJSONPath(f *testing.F) {
	f.Add([]byte(`{"a":[1,{"b":2}]}`), "$.a[1].b")
	f.Add([]byte(`[]`), "$[-1]")
	f.Fuzz(func(t *testing.T, body []byte, path string) {
		_, _ = JSONPath(body, path)
		_, _ = XPath(body, path)
	})
}

// A server's answer is data: {{...}} inside a chained value is never
// expanded again (it could pull a secret into the next request).
func TestChainValueIsNotExpandedAgain(t *testing.T) {
	evil := &Response{Status: 200, Body: []byte(`{"next":"{{$secret gh}}-{{host}}"}`)}
	rv := &Resolver{
		FileVars:  []Var{{Name: "host", Value: "h"}},
		Secret:    func(string) (string, bool) { return "ghp_" + "realsecret", true },
		Responses: func(string) (*Response, bool) { return evil, true },
	}
	rr, _ := rv.Resolve(Request{URL: "http://x/{{a.response.body.$.next}}/{{host}}", Body: strPtr("{{a.response.body.*}}")})
	if rr.URL != "http://x/{{$secret gh}}-{{host}}/h" {
		t.Errorf("url = %q", rr.URL)
	}
	if *rr.Body != string(evil.Body) || len(rr.Secrets) != 0 {
		t.Errorf("body = %q secrets %v", *rr.Body, rr.Secrets)
	}
}
