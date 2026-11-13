package httpclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionIsSafeWhenMissing(t *testing.T) {
	var s *Session
	s.Set("a", "1")
	s.Delete("a")
	s.Clear()
	if v, ok := s.Get("a"); ok || v != "" {
		t.Error("a missing session has no values")
	}
	if s.Names() != nil {
		t.Error("a missing session has no names")
	}
	s = NewSession()
	s.Set("b", "2")
	s.Set("a", "1")
	if got := strings.Join(s.Names(), ","); got != "a,b" {
		t.Errorf("Names = %q", got)
	}
	s.Clear()
	if len(s.Names()) != 0 {
		t.Error("Clear leaves no names")
	}
}

func TestRunPostPicksValuesAndSaysWhyNot(t *testing.T) {
	resp := &Response{
		Status: 201, Elapsed: 1500 * time.Millisecond,
		Headers: []Header{{Name: "x-id", Value: "old"}, {Name: "X-Id", Value: "new"}},
		Body:    []byte(`{"token":"T","n":{"k":3}}`),
	}
	sess := NewSession()
	sess.Set("gone", "1")
	res := RunPost([]string{
		"set s = status", "set ms = time", "set h = header X-Id", "set tok = body $.token", "set k = body $.n.k",
		"set raw = body", "unset gone", "set miss = header Nope", "set nopath = body $.zzz", "bogus line", "",
	}, resp, sess)
	pass := map[string]bool{}
	for _, r := range res {
		pass[r.Expr] = r.Pass
	}
	for _, want := range []string{"@post set s = status", "@post set ms = time", "@post set h = header X-Id", "@post set tok = body $.token", "@post set k = body $.n.k", "@post set raw = body", "@post unset gone"} {
		if !pass[want] {
			t.Errorf("%s should pass: %+v", want, res)
		}
	}
	for _, want := range []string{"@post set miss = header Nope", "@post set nopath = body $.zzz", "@post bogus line"} {
		if pass[want] {
			t.Errorf("%s should fail", want)
		}
	}
	for k, want := range map[string]string{"s": "201", "ms": "1500", "h": "new", "tok": "T", "k": "3"} {
		if v, _ := sess.Get(k); v != want {
			t.Errorf("%s = %q, want %q", k, v, want)
		}
	}
	if _, ok := sess.Get("gone"); ok {
		t.Error("unset removes the variable")
	}
	// A body that is not JSON.
	res = RunPost([]string{"set t = body $.a"}, &Response{Status: 200, Body: []byte("plain")}, sess)
	if res[0].Pass || !strings.Contains(res[0].Message, "not JSON") {
		t.Errorf("non-JSON body = %+v", res[0])
	}
	// The send failed.
	res = RunPost([]string{"set t = status"}, &Response{Err: "boom"}, sess)
	if res[0].Pass || !strings.Contains(res[0].Message, "no response") {
		t.Errorf("failed send = %+v", res[0])
	}
	res = RunPost([]string{"set t = status"}, nil, sess)
	if res[0].Pass {
		t.Error("no response is a failure")
	}
	if RunPost(nil, resp, sess) != nil {
		t.Error("no lines, no results")
	}
	// Too many lines stop with one message.
	lines := make([]string, maxScriptLines+3)
	for i := range lines {
		lines[i] = "set x = status"
	}
	res = RunPost(lines, resp, sess)
	last := res[len(res)-1]
	if len(res) != maxScriptLines+1 || last.Pass || !strings.Contains(last.Message, "more than") {
		t.Errorf("limit: %d results, last %+v", len(res), last)
	}
}

func TestParseScriptLimits(t *testing.T) {
	if _, err := ParseScript("set a = "+strings.Repeat("x", maxScriptValue+1), true); err == nil || !strings.Contains(err.Error(), "longer than") {
		t.Errorf("a long value = %v", err)
	}
	if !(&Request{Pre: []string{"set a = 1"}}).HasTests() || (&Request{}).HasTests() {
		t.Error("HasTests")
	}
}

func TestLoadGlobals(t *testing.T) {
	dir := t.TempDir()
	if g, err := LoadGlobals(filepath.Join(dir, "none.json")); g != nil || err != nil {
		t.Errorf("a missing file = %v, %v", g, err)
	}
	f := filepath.Join(dir, "g.json")
	if err := os.WriteFile(f, []byte(`{"a":"x","n":2,"b":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	g, err := LoadGlobals(f)
	if err != nil || g["a"] != "x" || g["n"] != "2" || g["b"] != "true" {
		t.Errorf("globals = %v, %v", g, err)
	}
	if err := os.WriteFile(f, []byte(`{nope`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGlobals(f); err == nil || !strings.Contains(err.Error(), "g.json") {
		t.Errorf("broken file = %v", err)
	}
	if _, err := LoadGlobals(dir); err == nil {
		t.Error("a folder is not a globals file")
	}
}
