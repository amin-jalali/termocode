package httpclient

import (
	"strings"
	"testing"
)

const gqlRich = `{"data":{"__schema":{
 "queryType":{"name":"Query"},"mutationType":null,"subscriptionType":{"name":"Sub"},
 "types":[
  {"kind":"OBJECT","name":"Query","fields":[{"name":"item","args":[],"type":{"kind":"INTERFACE","name":"Node","ofType":null}}]},
  {"kind":"OBJECT","name":"Sub","fields":[{"name":"ticks","args":[],"type":{"kind":"SCALAR","name":"Int","ofType":null}}]},
  {"kind":"INTERFACE","name":"Node","fields":[{"name":"id","args":[],"type":{"kind":"SCALAR","name":"ID","ofType":null}}],"possibleTypes":[{"name":"Doc"},{"name":"Pic"}]},
  {"kind":"OBJECT","name":"Doc","fields":[{"name":"id","args":[],"type":{"kind":"SCALAR","name":"ID","ofType":null}},{"name":"title","args":[],"type":{"kind":"SCALAR","name":"String","ofType":null}}]},
  {"kind":"UNION","name":"Hit","possibleTypes":[{"name":"Doc"},{"name":"Pic"}]},
  {"kind":"ENUM","name":"Color","enumValues":[{"name":"RED"},{"name":"BLUE"}]},
  {"kind":"INPUT_OBJECT","name":"Filter","inputFields":[{"name":"color","type":{"kind":"ENUM","name":"Color","ofType":null}},{"name":"deep","type":{"kind":"LIST","name":null,"ofType":null}}]},
  {"kind":"SCALAR","name":"Date"},
  {"name":"","kind":"OBJECT"}
 ]}}}`

func TestGQLDocsOfEveryKind(t *testing.T) {
	s, err := ParseGQLSchema([]byte(gqlRich))
	if err != nil {
		t.Fatal(err)
	}
	if s.Subscription != "Sub" || s.Mutation != "" {
		t.Errorf("roots = %q %q", s.Subscription, s.Mutation)
	}
	for name, want := range map[string]string{
		"Date":   "scalar Date",
		"Hit":    "union Hit = Doc | Pic",
		"Color":  "RED",
		"Filter": "input Filter {",
		"Node":   "interface Node {",
	} {
		if got := s.Docs(name); !strings.Contains(got, want) {
			t.Errorf("Docs(%s) = %q, want it to hold %q", name, got, want)
		}
	}
	if got := s.Docs("Filter"); !strings.Contains(got, "color: Color") || !strings.Contains(got, "deep: [?]") {
		t.Errorf("input fields = %q", got)
	}
	if got := s.Types["Node"].Possible; len(got) != 2 {
		t.Errorf("possible types = %v", got)
	}
}

func TestGQLWalkTokensAndRoots(t *testing.T) {
	s, err := ParseGQLSchema([]byte(gqlRich))
	if err != nil {
		t.Fatal(err)
	}
	names := func(src string) string {
		var out []string
		for _, f := range s.Complete(src) {
			out = append(out, f.Name)
		}
		return strings.Join(out, " ")
	}
	for _, c := range []struct{ src, want string }{
		{"subscription { ", "ticks"},
		{"fragment F on Doc { ", "id title"},
		{"fragment F { ", ""},
		{"fragment F on { ", ""},
		{"# a comment { \n{ ", "item"},
		{`{ item(arg: "text {") { `, "id"},
		{`{ item(arg: """block { """) { `, "id"},
		{`{ item(arg: """unterminated`, ""},
		{`{ item(arg: 1.5e+3, other: -2) { `, "id"},
		{"{ alias: item { ", "id"},
		{"{ item(a: {b: [1, (2)]}) @include(if: $x) { ", "id"},
		{"{ item { ... on Doc { ", "id title"},
		{"{ item { ...Spread } ", "item"},
	} {
		if got := names(c.src); got != c.want {
			t.Errorf("Complete(%q) = %q, want %q", c.src, got, c.want)
		}
	}
	if errs := s.Validate(`{ item { id } } # trailing`); len(errs) != 0 {
		t.Errorf("a trailing comment is no error: %v", errs)
	}
	if got := typeString(nil, 0); got != "?" {
		t.Errorf("typeString(nil) = %q", got)
	}
	deep := &gqlTypeRef{Kind: "LIST"}
	deep.OfType = deep
	if got := typeString(deep, 0); !strings.Contains(got, "?") {
		t.Errorf("a loop in the type ends with ?, got %q", got)
	}
	var nilType *GQLType
	if nilType.has("x") {
		t.Error("a missing type has no fields")
	}
}
