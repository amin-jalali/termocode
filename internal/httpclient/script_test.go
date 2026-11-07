package httpclient

import (
	"context"
	"strings"
	"testing"
)

func TestSchemaAssert(t *testing.T) {
	resp := &Response{Status: 200, Body: []byte(`{"id":7,"name":"ann","tags":["a","b"],"age":-1}`)}
	cases := []struct {
		expr string
		pass bool
	}{
		{`body schema {"type":"object","required":["id","name"]}`, true},
		{`body schema {"type":"object","required":["email"]}`, false},
		{`body $.id schema {"type":"integer","minimum":1}`, true},
		{`body $.age schema {"type":"number","minimum":0}`, false},
		{`body $.tags schema {"type":"array","items":{"type":"string"},"maxItems":2}`, true},
		{`body $.name schema {"type":"string","enum":["bob"]}`, false},
		{`body schema {"type":"object","properties":{"id":{"type":"string"}}}`, false},
		{`body schema {"type":"object","properties":{"id":{"type":"integer"}},"additionalProperties":false}`, false},
	}
	for _, c := range cases {
		res := RunAsserts([]string{c.expr}, resp)
		if res[0].Pass != c.pass {
			t.Errorf("%s: pass = %v (%s), want %v", c.expr, res[0].Pass, res[0].Message, c.pass)
		}
		if !res[0].Pass && res[0].Message == "" {
			t.Errorf("%s: a failed schema check needs a reason", c.expr)
		}
	}
	if _, err := ParseAssert(`body schema {not json`); err == nil {
		t.Fatal("a bad schema must not parse")
	}
}

func TestParseCollectsPreAndPostLines(t *testing.T) {
	f := Parse("# @pre set id = {{$uuid}}\n# @post set token = body $.token\nGET https://x/a\n")
	r := f.Requests[0]
	if len(r.Pre) != 1 || r.Pre[0] != "set id = {{$uuid}}" || len(r.Post) != 1 || r.Post[0] != "set token = body $.token" {
		t.Fatalf("pre = %q post = %q", r.Pre, r.Post)
	}
	if _, ok := r.Directives["pre"]; ok {
		t.Fatal("script lines must not land in Directives")
	}
}

func TestParseScriptErrors(t *testing.T) {
	for _, s := range []string{"", "set", "set = 1", "set a b = 1", "open /etc/passwd", "set x =", "unset"} {
		if _, err := ParseScript(s, true); err == nil {
			t.Errorf("ParseScript(%q) = nil error", s)
		}
	}
	if _, err := ParseScript("set t = body $.a", false); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScript("set t = nothing", false); err == nil {
		t.Fatal("a post value must be a subject")
	}
}

func TestPreScriptSetsVariableForRequestAndSession(t *testing.T) {
	sess := NewSession()
	rv := &Resolver{FileVars: []Var{{Name: "user", Value: "ann"}}, Session: sess}
	req := Parse("# @pre set who = {{user}}-x\n# @pre set id = 42\nGET https://x/{{who}}/{{id}}\n").Requests[0]
	rr, un := rv.Resolve(req)
	if len(un) > 0 || rr.URL != "https://x/ann-x/42" {
		t.Fatalf("url = %q un = %v", rr.URL, un)
	}
	if v, ok := sess.Get("who"); !ok || v != "ann-x" {
		t.Fatalf("session who = %q %v", v, ok)
	}
	// A later request sees the session value.
	rr, _ = (&Resolver{Session: sess}).Resolve(Request{Method: "GET", URL: "https://x/{{id}}"})
	if rr.URL != "https://x/42" {
		t.Fatalf("later url = %q", rr.URL)
	}
	// unset removes it.
	(&Resolver{Session: sess}).Resolve(Parse("# @pre unset id\nGET https://x/\n").Requests[0])
	if _, ok := sess.Get("id"); ok {
		t.Fatal("unset kept the value")
	}
}

func TestRunTestsAssertsAndPostScript(t *testing.T) {
	sess := NewSession()
	rr := Resolved{Request: Request{
		Asserts: []string{"status == 200", "body $.ok == false"},
		Post:    []string{"set token = body $.token", "set ct = header Content-Type", "set nope = body $.missing"},
	}}
	resp := &Response{Status: 200, Headers: []Header{{Name: "Content-Type", Value: "application/json"}}, Body: []byte(`{"ok":true,"token":"t-1"}`)}
	res := RunTests(rr, resp, sess)
	if len(res) != 5 {
		t.Fatalf("results = %+v", res)
	}
	if !res[0].Pass || res[1].Pass || !res[2].Pass || !res[3].Pass || res[4].Pass {
		t.Fatalf("results = %+v", res)
	}
	if v, _ := sess.Get("token"); v != "t-1" {
		t.Fatalf("token = %q", v)
	}
	if v, _ := sess.Get("ct"); v != "application/json" {
		t.Fatalf("ct = %q", v)
	}
	if !strings.HasPrefix(res[2].Expr, "@post ") {
		t.Fatalf("post expr = %q", res[2].Expr)
	}
}

func TestRunFailsRequestWhenAnAssertFails(t *testing.T) {
	srv, _ := runServer(t)
	src := "### one\n# @assert status == 201\nGET " + srv.URL + "/ok\n\n### two\n# @assert status == 200\nGET " + srv.URL + "/ok\n"
	rep := Run(context.Background(), New(Options{}), []RunFile{runFile("a.http", src)}, RunOptions{})
	if rep.Passed != 1 || rep.Failed != 1 {
		t.Fatalf("report = %+v", rep)
	}
	r := rep.Results[0]
	if r.Passed || !strings.Contains(r.Failure, "status == 201") || len(r.Checks) != 1 {
		t.Fatalf("result = %+v", r)
	}
}
