package httpclient

import (
	"strings"
	"testing"
	"time"
)

func TestParseCollectsAssertLines(t *testing.T) {
	f := Parse("# @assert status == 200\n# @assert body $.user.id exists\n// @assert time < 500\nGET https://x/a\n")
	if len(f.Requests) != 1 {
		t.Fatalf("requests = %d", len(f.Requests))
	}
	r := f.Requests[0]
	want := []string{"status == 200", "body $.user.id exists", "time < 500"}
	if strings.Join(r.Asserts, "|") != strings.Join(want, "|") {
		t.Fatalf("asserts = %q, want %q", r.Asserts, want)
	}
	if _, ok := r.Directives["assert"]; ok {
		t.Fatalf("assert lines must not land in Directives: %v", r.Directives)
	}
}

func TestParseAssertErrors(t *testing.T) {
	for _, s := range []string{
		"",
		"status",
		"status ==",
		"status ~~ 200",
		"header",
		"body $.a[",
		"body a.b == 1",
		"nothing == 1",
		"status == abc",
		"time < soon",
	} {
		if _, err := ParseAssert(s); err == nil {
			t.Errorf("ParseAssert(%q) = nil error, want an error", s)
		}
	}
}

func assertResp() *Response {
	return &Response{
		Status:  201,
		Elapsed: 120 * time.Millisecond,
		Headers: []Header{{Name: "content-type", Value: "application/json; charset=utf-8"}},
		Body:    []byte(`{"user":{"id":7,"name":"Ann","tags":["a","b"],"ok":true,"none":null},"items":[{"n":1},{"n":2}]}`),
	}
}

func TestRunAssertsPassAndFail(t *testing.T) {
	cases := []struct {
		expr string
		pass bool
	}{
		{"status == 201", true},
		{"status != 201", false},
		{"status >= 200", true},
		{"status < 300", true},
		{"status == 200", false},
		{"time < 500", true},
		{"time > 500", false},
		{"time <= 120ms", true},
		{"time < 1s", true},
		{"header Content-Type contains json", true},
		{"header content-type == text/html", false},
		{"header X-Missing exists", false},
		{"header X-Missing !exists", true},
		{"header content-type exists", true},
		{"body $.user.id == 7", true},
		{"body $.user.id > 6", true},
		{"body $.user.name == Ann", true},
		{`body $.user.name == "Ann"`, true},
		{"body $.user.name != Bob", true},
		{"body $.user.tags[1] == b", true},
		{"body $.user.tags[5] exists", false},
		{"body $.items[0].n == 1", true},
		{"body $.items[1].n == 1", false},
		{"body $.user.ok == true", true},
		{"body $.user.none == null", true},
		{"body $.user.none exists", true},
		{"body $.user.gone !exists", true},
		{"body $.user.tags contains a", true},
		{"body contains Ann", true},
		{"body contains Bob", false},
		{"body $.user.name matches ^A.n$", true},
		{"body $.user.name matches ^B", false},
	}
	for _, c := range cases {
		res := RunAsserts([]string{c.expr}, assertResp())
		if len(res) != 1 {
			t.Fatalf("%q: %d results", c.expr, len(res))
		}
		if res[0].Pass != c.pass {
			t.Errorf("%q: pass = %v, want %v (%s)", c.expr, res[0].Pass, c.pass, res[0].Message)
		}
		if res[0].Expr != c.expr {
			t.Errorf("%q: Expr = %q", c.expr, res[0].Expr)
		}
		if !res[0].Pass && res[0].Message == "" {
			t.Errorf("%q: a failed assert needs a message", c.expr)
		}
	}
}

func TestRunAssertsBadExprFailsWithMessage(t *testing.T) {
	res := RunAsserts([]string{"status ~~ 1"}, assertResp())
	if len(res) != 1 || res[0].Pass || res[0].Message == "" {
		t.Fatalf("a bad assert must fail with a message: %+v", res)
	}
}

func TestRunAssertsNotJSONBody(t *testing.T) {
	r := assertResp()
	r.Body = []byte("<html>")
	res := RunAsserts([]string{"body $.a exists", "body $.a !exists"}, r)
	if res[0].Pass {
		t.Fatalf("exists on a non-JSON body must fail: %+v", res[0])
	}
	if res[1].Pass || !strings.Contains(res[1].Message, "JSON") {
		t.Fatalf("a non-JSON body must say so: %+v", res[1])
	}
}

func TestRunAssertsFailedSend(t *testing.T) {
	r := &Response{Err: "dial failed", ErrKind: ErrKindDial}
	res := RunAsserts([]string{"status == 200"}, r)
	if res[0].Pass {
		t.Fatalf("an assert on a failed send must fail: %+v", res[0])
	}
}

func TestAssertSummary(t *testing.T) {
	res := RunAsserts([]string{"status == 201", "status == 200", "time < 500"}, assertResp())
	if got := AssertSummary(res); got != "2 of 3 checks passed" {
		t.Fatalf("summary = %q", got)
	}
	if got := AssertSummary(nil); got != "" {
		t.Fatalf("empty summary = %q", got)
	}
}

func TestMatchesRejectsHugePatterns(t *testing.T) {
	if _, err := ParseAssert("body $.a matches " + strings.Repeat("a", 2000)); err == nil {
		t.Fatalf("a pattern over the limit must be refused")
	}
}
