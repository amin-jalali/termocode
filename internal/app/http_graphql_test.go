package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// The completion source is the body part of the block before the
// cursor: request line and headers are left out.
func TestHTTPGQLBodyBefore(t *testing.T) {
	block := "GRAPHQL https://x/graphql\nAuthorization: Bearer t\n\nquery { us"
	if got := httpGQLBodyBefore(block); got != "query { us" {
		t.Fatalf("body = %q", got)
	}
	if got := httpGQLBodyBefore("GRAPHQL https://x/graphql\nAuth"); got != "" {
		t.Fatalf("in the headers: %q", got)
	}
	if got := httpGQLPrefix("query { us"); got != "us" {
		t.Fatalf("prefix = %q", got)
	}
	if got := httpGQLPrefix("query { "); got != "" {
		t.Fatalf("prefix = %q", got)
	}
}

// Validate checks the query only, never the variables object after it.
func TestHTTPGQLQueryDropsVariables(t *testing.T) {
	body := "query($id: ID!) { user(id: $id) { name } }\n\n{\"id\": 1}"
	if got := httpGQLQuery(body); got != "query($id: ID!) { user(id: $id) { name } }" {
		t.Fatalf("query = %q", got)
	}
	if got := httpGQLQuery("{ users { id } }"); got != "{ users { id } }" {
		t.Fatalf("query = %q", got)
	}
}

// The introspection keeps the URL and auth header, drops the GraphQL
// marker header and sends JSON.
func TestHTTPGQLIntrospectionRequest(t *testing.T) {
	body := "{ users { id } }"
	rr := httpclient.Resolved{Request: httpclient.Request{Method: "POST", URL: "https://x/graphql", Body: &body,
		Headers: []httpclient.Header{{Name: "Authorization", Value: "Bearer t"}, {Name: "X-Request-Type", Value: "GraphQL"}}}}
	in := httpGQLIntrospection(rr)
	if in.Method != "POST" || in.URL != rr.URL || in.Body == nil || !strings.Contains(*in.Body, "__schema") {
		t.Fatalf("introspection = %+v", in.Request)
	}
	if in.Header("Authorization") != "Bearer t" || in.Header("X-Request-Type") != "" || in.Header("Content-Type") != "application/json" {
		t.Fatalf("headers = %+v", in.Headers)
	}
	if *rr.Body != body {
		t.Fatal("the request itself changed")
	}
}

// The four GraphQL commands are in the palette and have an AI policy.
func TestHTTPGQLPaletteAndPolicy(t *testing.T) {
	ids := map[string]bool{}
	for _, it := range httpPaletteItems() {
		ids[it.ID] = true
	}
	for _, id := range []string{"http-gql-schema", "http-gql-complete", "http-gql-validate", "http-gql-docs"} {
		if !ids[id] {
			t.Errorf("%s not in the palette", id)
		}
		if _, ok := aiActionPolicyFor(id); !ok {
			t.Errorf("%s has no AI policy", id)
		}
	}
}
