package httpclient

import (
	"strings"
	"testing"
)

// API-23: API docs straight from an OpenAPI file: the operations as
// requests (with summaries and example bodies) and the base URL.
func TestAPIDocsFromOpenAPI(t *testing.T) {
	src := `openapi: 3.0.0
info: {title: Pets API}
servers: [{url: "https://api.example.com/v1"}]
paths:
  /pets:
    post:
      operationId: createPet
      summary: Add a pet
      requestBody:
        content:
          application/json:
            example: {"name": "rex"}
      responses: {"201": {description: ok}}
  /pets/{petId}:
    get:
      operationId: getPet
      responses: {"200": {description: ok}}
`
	md, err := APIDocsOpenAPI([]byte(src), "pets.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# API docs: Pets API",
		"2 requests",
		"## createPet",
		"POST {{baseUrl}}/pets",
		"\"name\": \"rex\"",
		"## getPet",
		"GET {{baseUrl}}/pets/{{petId}}",
		"| `baseUrl` | `https://api.example.com/v1` |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in\n%s", want, md)
		}
	}
	if _, err := APIDocsOpenAPI([]byte("a: 1"), "x.yaml"); err == nil {
		t.Error("not a spec, no error")
	}
}

// No title in the spec: the file name is the title.
func TestAPIDocsFromOpenAPIUntitled(t *testing.T) {
	md, err := APIDocsOpenAPI([]byte(`{"swagger":"2.0","paths":{"/a":{"get":{}}}}`), "spec.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(md, "# API docs: spec.json\n") {
		t.Fatalf("docs:\n%s", md)
	}
}
