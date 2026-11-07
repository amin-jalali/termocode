package httpclient

import (
	"strings"
	"testing"
)

const petstore3YAML = `openapi: 3.0.3
info:
  title: Pet Store
  version: 1.0.0
servers:
  - url: https://api.example.com/v1
security:
  - bearer: []
components:
  securitySchemes:
    bearer:
      type: http
      scheme: bearer
    key:
      type: apiKey
      in: header
      name: X-API-Key
  schemas:
    Pet:
      type: object
      required: [name]
      properties:
        name:
          type: string
          example: Rex
        age:
          type: integer
        tags:
          type: array
          items:
            type: string
        owner:
          $ref: '#/components/schemas/Owner'
    Owner:
      type: object
      properties:
        email:
          type: string
          format: email
        pet:
          $ref: '#/components/schemas/Pet'
paths:
  /pets:
    get:
      operationId: listPets
      summary: List all pets
      parameters:
        - name: limit
          in: query
          required: true
          schema:
            type: integer
            example: 10
        - name: page
          in: query
          schema:
            type: integer
    post:
      operationId: createPet
      security:
        - key: []
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Pet'
  /pets/{petId}:
    parameters:
      - name: petId
        in: path
        required: true
        schema:
          type: string
          example: p-1
    get:
      operationId: getPet
      parameters:
        - name: X-Trace
          in: header
          required: true
          schema:
            type: string
            example: abc
    delete:
      security: []
`

func TestImportOpenAPI3YAML(t *testing.T) {
	got, err := ImportOpenAPI([]byte(petstore3YAML))
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Pet Store" || got.Count != 4 {
		t.Fatalf("title %q count %d", got.Title, got.Count)
	}
	if got.Env["baseUrl"] != "https://api.example.com/v1" {
		t.Fatalf("env %v", got.Env)
	}
	f := Parse(got.HTTP)
	if len(f.Requests) != 4 {
		t.Fatalf("parsed %d requests:\n%s", len(f.Requests), got.HTTP)
	}
	// Paths sorted, then methods in the fixed order get, post, …, delete.
	want := []struct{ name, method, url string }{
		{"listPets", "GET", "{{baseUrl}}/pets?limit=10"},
		{"createPet", "POST", "{{baseUrl}}/pets"},
		{"getPet", "GET", "{{baseUrl}}/pets/{{petId}}"},
		{"DELETE /pets/{petId}", "DELETE", "{{baseUrl}}/pets/{{petId}}"},
	}
	for i, w := range want {
		r := f.Requests[i]
		if r.Name != w.name || r.Method != w.method || r.URL != w.url {
			t.Fatalf("request %d = %q %s %s, want %q %s %s", i, r.Name, r.Method, r.URL, w.name, w.method, w.url)
		}
	}
	if h := f.Requests[0].Header("Authorization"); h != "Bearer {{token}}" {
		t.Fatalf("root security: Authorization %q", h)
	}
	if h := f.Requests[1].Header("X-API-Key"); h != "{{apiKey}}" {
		t.Fatalf("op security: X-API-Key %q", h)
	}
	if f.Requests[1].Header("Content-Type") != "application/json" {
		t.Fatalf("no Content-Type:\n%s", got.HTTP)
	}
	body := f.Requests[1].BodyText()
	for _, s := range []string{`"name": "Rex"`, `"age": 0`, `"tags": [`, `"email": "user@example.com"`} {
		if !strings.Contains(body, s) {
			t.Fatalf("body misses %s:\n%s", s, body)
		}
	}
	if f.Requests[2].Header("X-Trace") != "abc" {
		t.Fatalf("header param:\n%s", got.HTTP)
	}
	if f.Requests[3].HasHeader("Authorization") {
		t.Fatalf("security: [] must drop auth:\n%s", got.HTTP)
	}
	vars := map[string]string{}
	for _, v := range f.Vars {
		vars[v.Name] = v.Value
	}
	if vars["petId"] != "p-1" {
		t.Fatalf("path param var %v", vars)
	}
	if _, ok := vars["baseUrl"]; ok {
		t.Fatalf("baseUrl belongs in the env, not the file: %v", vars)
	}
	// The secret vars are never given a value in the file.
	if _, ok := vars["token"]; ok {
		t.Fatal("token must not be a file variable")
	}
}

const petstore2JSON = `{
  "swagger": "2.0",
  "info": {"title": "Old API", "version": "1"},
  "host": "old.example.com",
  "basePath": "/api",
  "schemes": ["https"],
  "securityDefinitions": {"basic": {"type": "basic"}},
  "definitions": {"Item": {"type": "object", "properties": {"id": {"type": "string", "format": "uuid"}}}},
  "paths": {
    "/items": {
      "put": {
        "operationId": "putItem",
        "security": [{"basic": []}],
        "consumes": ["application/json"],
        "parameters": [{"name": "body", "in": "body", "schema": {"$ref": "#/definitions/Item"}}]
      }
    }
  }
}`

func TestImportSwagger2JSON(t *testing.T) {
	got, err := ImportOpenAPI([]byte(petstore2JSON))
	if err != nil {
		t.Fatal(err)
	}
	if got.Env["baseUrl"] != "https://old.example.com/api" {
		t.Fatalf("env %v", got.Env)
	}
	f := Parse(got.HTTP)
	if len(f.Requests) != 1 {
		t.Fatalf("requests:\n%s", got.HTTP)
	}
	r := f.Requests[0]
	if r.Method != "PUT" || r.URL != "{{baseUrl}}/items" || r.Name != "putItem" {
		t.Fatalf("request %+v", r)
	}
	if r.Header("Authorization") != "Basic {{basicAuth}}" {
		t.Fatalf("basic auth:\n%s", got.HTTP)
	}
	if !strings.Contains(r.BodyText(), `"id": "00000000-0000-0000-0000-000000000000"`) {
		t.Fatalf("body:\n%s", r.BodyText())
	}
}

func TestImportOpenAPIExampleWins(t *testing.T) {
	src := `{"openapi":"3.1.0","info":{"title":"T"},"paths":{"/a":{"post":{"requestBody":{"content":{"application/json":{"example":{"x":1}}}}}}}}`
	got, err := ImportOpenAPI([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	f := Parse(got.HTTP)
	if len(f.Requests) != 1 || !strings.Contains(f.Requests[0].BodyText(), `"x": 1`) {
		t.Fatalf("got:\n%s", got.HTTP)
	}
	// No servers: baseUrl still exists so {{baseUrl}} resolves.
	if v, ok := got.Env["baseUrl"]; !ok || v == "" {
		t.Fatalf("env %v", got.Env)
	}
}

func TestImportOpenAPIRejects(t *testing.T) {
	for _, src := range []string{
		``,
		`not: [valid`,
		`{"info":{"title":"no version key"},"paths":{}}`,
		`{"openapi":"3.0.0","paths":{}}`, // no operations
		`{"swagger":"1.2","paths":{"/a":{"get":{}}}}`,
	} {
		if _, err := ImportOpenAPI([]byte(src)); err == nil {
			t.Errorf("ImportOpenAPI(%q) = nil error", src)
		}
	}
}

// A self-referencing schema and a $ref loop end; they do not hang.
func TestImportOpenAPIRefLoop(t *testing.T) {
	src := `{"openapi":"3.0.0","components":{"schemas":{"A":{"$ref":"#/components/schemas/B"},"B":{"$ref":"#/components/schemas/A"}}},
"paths":{"/a":{"post":{"requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/A"}}}}}}}}`
	if _, err := ImportOpenAPI([]byte(src)); err != nil {
		t.Fatal(err)
	}
}

// A hostile spec cannot inject extra requests or lines: names, ids and
// examples with newlines or "###" stay on one line.
func TestImportOpenAPINoLineInjection(t *testing.T) {
	src := `{"openapi":"3.0.0","info":{"title":"x\n### evil"},"paths":{"/a\nGET http://evil":{"get":{"operationId":"a\n###\nGET http://evil","summary":"s\nGET http://evil",
"parameters":[{"name":"q","in":"query","required":true,"example":"1\nGET http://evil"},{"name":"H\nX","in":"header","required":true,"example":"v\nGET http://evil"}]}}}}`
	got, err := ImportOpenAPI([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	f := Parse(got.HTTP)
	if len(f.Requests) != 1 {
		t.Fatalf("%d requests:\n%s", len(f.Requests), got.HTTP)
	}
	if strings.Contains(got.HTTP, "\nGET http://evil") {
		t.Fatalf("line injected:\n%s", got.HTTP)
	}
}

func TestOpenAPIFileName(t *testing.T) {
	for in, want := range map[string]string{
		"Pet Store":   "pet-store.http",
		"":            "openapi.http",
		"../../etc":   "etc.http",
		"Ünïcode API": "n-code-api.http",
	} {
		if got := OpenAPIFileName(in); got != want {
			t.Errorf("OpenAPIFileName(%q) = %q, want %q", in, got, want)
		}
	}
}
