package httpclient

import (
	"encoding/json"
	"strings"
	"testing"
)

// gqlIntrospection is a cut-down introspection answer: Query { user(id): User, users: [User!]! }
// User { id: ID!, name: String, friends: [User] }.
const gqlIntrospection = `{"data":{"__schema":{
 "queryType":{"name":"Query"},"mutationType":{"name":"Mutation"},"subscriptionType":null,
 "types":[
  {"kind":"OBJECT","name":"Query","description":"root","fields":[
    {"name":"user","description":"one user","args":[{"name":"id","type":{"kind":"NON_NULL","name":null,"ofType":{"kind":"SCALAR","name":"ID","ofType":null}}}],
     "type":{"kind":"OBJECT","name":"User","ofType":null}},
    {"name":"users","args":[],"type":{"kind":"NON_NULL","name":null,"ofType":{"kind":"LIST","name":null,"ofType":{"kind":"NON_NULL","name":null,"ofType":{"kind":"OBJECT","name":"User","ofType":null}}}}}]},
  {"kind":"OBJECT","name":"Mutation","fields":[
    {"name":"rename","args":[],"type":{"kind":"OBJECT","name":"User","ofType":null}}]},
  {"kind":"OBJECT","name":"User","fields":[
    {"name":"id","args":[],"type":{"kind":"NON_NULL","name":null,"ofType":{"kind":"SCALAR","name":"ID","ofType":null}}},
    {"name":"name","args":[],"type":{"kind":"SCALAR","name":"String","ofType":null}},
    {"name":"friends","args":[],"type":{"kind":"LIST","name":null,"ofType":{"kind":"OBJECT","name":"User","ofType":null}}}]},
  {"kind":"SCALAR","name":"ID"},{"kind":"SCALAR","name":"String"}
 ]}}}`

func gqlSchema(t *testing.T) *GQLSchema {
	t.Helper()
	s, err := ParseGQLSchema([]byte(gqlIntrospection))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGQLIntrospectionBodyIsAGraphQLRequest(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal([]byte(GQLIntrospectionBody()), &m); err != nil {
		t.Fatal(err)
	}
	q, _ := m["query"].(string)
	if !strings.Contains(q, "__schema") || !strings.Contains(q, "ofType") {
		t.Fatalf("query = %q", q)
	}
}

func TestParseGQLSchema(t *testing.T) {
	s := gqlSchema(t)
	if s.Query != "Query" || s.Mutation != "Mutation" || s.Subscription != "" {
		t.Fatalf("roots = %q %q %q", s.Query, s.Mutation, s.Subscription)
	}
	u := s.Types["User"]
	if u == nil || len(u.Fields) != 3 {
		t.Fatalf("User = %+v", u)
	}
	q := s.Types["Query"]
	if got := q.Field("users").Type; got != "[User!]!" {
		t.Fatalf("users type = %q", got)
	}
	if f := q.Field("user"); f.Description != "one user" || len(f.Args) != 1 || f.Args[0].Type != "ID!" {
		t.Fatalf("user field = %+v", f)
	}
}

func TestParseGQLSchemaErrors(t *testing.T) {
	for _, b := range []string{"", "not json", `{"data":{}}`, `{"errors":[{"message":"introspection is off"}]}`} {
		_, err := ParseGQLSchema([]byte(b))
		if err == nil {
			t.Errorf("%q: want an error", b)
		}
	}
	_, err := ParseGQLSchema([]byte(`{"errors":[{"message":"introspection is off"}]}`))
	if err == nil || !strings.Contains(err.Error(), "introspection is off") {
		t.Fatalf("server error must be shown: %v", err)
	}
}

func TestGQLCompleteFollowsTheCursor(t *testing.T) {
	s := gqlSchema(t)
	cases := []struct {
		src  string
		want string
	}{
		{"{ ", "user users"},
		{"query { us", "user users"},
		{"query { user(id: 1) { ", "friends id name"},
		{"{ user(id: \"a{b\") { friends { na", "friends id name"},
		{"mutation { ", "rename"},
		{"{ users { id } ", "user users"},
		{"{ nope { ", ""},
	}
	for _, c := range cases {
		var names []string
		for _, f := range s.Complete(c.src) {
			names = append(names, f.Name)
		}
		if got := strings.Join(names, " "); got != c.want {
			t.Errorf("Complete(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}

func TestGQLValidate(t *testing.T) {
	s := gqlSchema(t)
	if errs := s.Validate("query Q { user(id: 1) { id name friends { id } } users { id } }"); len(errs) != 0 {
		t.Fatalf("valid query got errors: %v", errs)
	}
	errs := s.Validate("{ user { idd friends { nam } } nope }")
	got := strings.Join(errs, "\n")
	for _, want := range []string{`no field "idd" on User`, `no field "nam" on User`, `no field "nope" on Query`} {
		if !strings.Contains(got, want) {
			t.Errorf("errors %q miss %q", got, want)
		}
	}
	if errs := s.Validate("{ user { id "); len(errs) == 0 {
		t.Fatalf("an open brace must be reported")
	}
	if errs := s.Validate("{ user { id } } }"); len(errs) == 0 {
		t.Fatalf("an extra brace must be reported")
	}
	// __typename is on every object; fragments are skipped, not reported.
	if errs := s.Validate("{ __typename user { __typename ...F ... on User { id } } }"); len(errs) != 0 {
		t.Fatalf("got %v", errs)
	}
}

func TestGQLDocs(t *testing.T) {
	s := gqlSchema(t)
	d := s.Docs("User")
	for _, want := range []string{"type User", "id: ID!", "friends: [User]"} {
		if !strings.Contains(d, want) {
			t.Errorf("docs %q miss %q", d, want)
		}
	}
	if !strings.Contains(s.Docs("Query"), "user(id: ID!): User  # one user") {
		t.Errorf("Query docs = %q", s.Docs("Query"))
	}
	if s.Docs("Nope") != "" {
		t.Errorf("unknown type must give no docs")
	}
}
