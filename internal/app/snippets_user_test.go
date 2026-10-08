package app

import (
	"strings"
	"testing"
)

func TestParseUserSnippets(t *testing.T) {
	in := `{
	  "global": { "todo": "// TODO $0" },
	  "Go": { "errw": ["if err != nil {", "\treturn err", "}"], "bad-trig": "x", "num": 3 }
	}`
	u, err := parseUserSnippets([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if u["*"]["todo"] != "// TODO $0" {
		t.Errorf("global alias not folded to *: %v", u)
	}
	if u["go"]["errw"] != "if err != nil {\n\treturn err\n}" {
		t.Errorf("array body not joined: %q", u["go"]["errw"])
	}
	if _, ok := u["go"]["bad-trig"]; ok {
		t.Error("invalid trigger should be skipped")
	}
	if _, ok := u["go"]["num"]; ok {
		t.Error("non-string body should be skipped")
	}
	if _, err := parseUserSnippets([]byte("{oops")); err == nil {
		t.Error("malformed JSON should error")
	}
	if u, err := parseUserSnippets(nil); err != nil || len(u) != 0 {
		t.Error("empty file should be an empty set")
	}
}

func TestUserSnippetsMarshalRoundTrip(t *testing.T) {
	u := userSnippets{}
	u.set("go", "errw", "a\nb")
	u.set("*", "todo", "x")
	out := u.marshal()
	if !strings.Contains(string(out), `"errw": [`) {
		t.Errorf("multi-line body should be an array:\n%s", out)
	}
	back, err := parseUserSnippets(out)
	if err != nil {
		t.Fatal(err)
	}
	if back["go"]["errw"] != "a\nb" || back["*"]["todo"] != "x" {
		t.Errorf("round trip mismatch: %v", back)
	}
}

func TestUserSnippetsSetRemoveListScopes(t *testing.T) {
	u := userSnippets{}
	if u.set("go", "b", "1") {
		t.Error("first set should not report replace")
	}
	if !u.set("go", "b", "2") {
		t.Error("second set should report replace")
	}
	u.set("go", "a", "x")
	u.set("python", "p", "y")
	u.set("global", "g", "z")

	if got := strings.Join(u.scopes(), ","); got != "*,go,python" {
		t.Errorf("scopes = %s", got)
	}
	var trigs []string
	for _, s := range u.list("") {
		trigs = append(trigs, s.Scope+":"+s.Trigger)
	}
	if got := strings.Join(trigs, ","); got != "*:g,go:a,go:b,python:p" {
		t.Errorf("list = %s", got)
	}
	if got := u.list("go"); len(got) != 2 || got[0].Trigger != "a" {
		t.Errorf("list(go) = %v", got)
	}
	if !u.remove(snippetRef{Scope: "python", Trigger: "p"}) {
		t.Error("remove existing should succeed")
	}
	if _, ok := u["python"]; ok {
		t.Error("empty scope should be dropped")
	}
	if u.remove(snippetRef{Scope: "python", Trigger: "p"}) {
		t.Error("remove missing should fail")
	}
}

func TestNextSnippetScope(t *testing.T) {
	scopes := []string{"*", "go"}
	seq := []string{""}
	cur := ""
	for i := 0; i < 3; i++ {
		cur = nextSnippetScope(scopes, cur)
		seq = append(seq, cur)
	}
	if got := strings.Join(seq, "|"); got != "|*|go|" {
		t.Errorf("cycle = %q", got)
	}
	if nextSnippetScope(nil, "") != "" {
		t.Error("no scopes should stay on all")
	}
}

func TestParseScopeTrigger(t *testing.T) {
	cases := []struct {
		in, scope, trig string
		ok              bool
	}{
		{"go:iferr", "go", "iferr", true},
		{"iferr", "*", "iferr", true},
		{"*:todo", "*", "todo", true},
		{"Python : main", "python", "main", true},
		{"go:", "", "", false},
		{"go:if-err", "", "", false},
	}
	for _, c := range cases {
		s, tr, err := parseScopeTrigger(c.in)
		if (err == nil) != c.ok || s != c.scope || tr != c.trig {
			t.Errorf("parseScopeTrigger(%q) = (%q, %q, %v)", c.in, s, tr, err)
		}
	}
}

func TestUnescapeBodyInput(t *testing.T) {
	if got := unescapeBodyInput(`a\nb\tc\\n\$1`); got != "a\nb\tc\\n\\$1" {
		t.Errorf("unescapeBodyInput = %q", got)
	}
}

func TestLuaLongString(t *testing.T) {
	if got := luaLongString("x"); got != "[[\nx]]" {
		t.Errorf("simple = %q", got)
	}
	if got := luaLongString("a]]b"); got != "[=[\na]]b]=]" {
		t.Errorf("level 1 = %q", got)
	}
	if got := luaLongString("a]]b]=]"); got != "[==[\na]]b]=]]==]" {
		t.Errorf("level 2 = %q", got)
	}
}
