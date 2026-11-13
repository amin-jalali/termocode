package httpclient

import (
	"strings"
	"testing"
)

func TestImportOpenAPIExampleShapes(t *testing.T) {
	doc := `
openapi: 3.0.3
info: {title: Shapes}
servers:
  - url: "{scheme}://api.example.org/{ver}"
    variables:
      scheme: {default: https}
      ver: {default: v2}
paths:
  /things/{id}:
    parameters:
      - {name: id, in: path, required: true, schema: {type: integer}}
      - {name: id, in: path, required: true, schema: {type: string, format: uuid}}
      - {in: query, schema: {type: string}}
    post:
      operationId: makeThing
      security: [{bearerAuth: []}, {other: []}]
      parameters:
        - {name: when, in: query, schema: {type: string, format: date-time}}
        - {name: day, in: query, schema: {type: string, format: date}}
        - {name: mail, in: query, schema: {type: string, format: email}}
        - {name: site, in: query, schema: {type: string, format: uri}}
        - {name: kind, in: query, schema: {type: string, enum: [alpha, beta]}}
        - {name: flag, in: query, schema: {type: boolean}}
        - {name: pick, in: query, schema: {type: ["string", "null"]}}
        - {name: ex, in: query, example: 7}
        - {name: tags, in: query, schema: {items: {type: string}}}
      requestBody:
        content:
          application/xml:
            schema: {type: string}
          text/plain:
            examples:
              first: {value: "hello ### world"}
  /t/{when}/{day}/{mail}/{site}/{kind}/{flag}/{pick}/{ex}/{tags}:
    get:
      parameters:
        - {name: when, in: path, required: true, schema: {type: string, format: date-time}}
        - {name: day, in: path, required: true, schema: {type: string, format: date}}
        - {name: mail, in: path, required: true, schema: {type: string, format: email}}
        - {name: site, in: path, required: true, schema: {type: string, format: uri}}
        - {name: kind, in: path, required: true, schema: {type: string, enum: [alpha, beta]}}
        - {name: flag, in: path, required: true, schema: {type: boolean}}
        - {name: pick, in: path, required: true, schema: {type: ["string", "null"]}}
        - {name: ex, in: path, required: true, example: 7}
        - {name: tags, in: path, required: true, schema: {items: {type: string}}}
  /merge:
    put:
      requestBody:
        content:
          application/json:
            schema:
              allOf:
                - {type: object, properties: {a: {type: integer}}}
                - {type: object, properties: {b: {type: string}}}
                - "not a schema"
  /either:
    put:
      requestBody:
        content:
          application/json:
            schema:
              oneOf:
                - {type: object, properties: {x: {type: number}}}
  /plain:
    post:
      requestBody:
        content:
          application/x-www-form-urlencoded:
            schema: {type: object, properties: {name: {type: string}}}
components:
  securitySchemes:
    bearerAuth: {type: http, scheme: bearer}
    other: {type: apiKey, in: header, name: "X-Key"}
`
	got, err := ImportOpenAPI([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if got.Env["baseUrl"] != "https://api.example.org/v2" {
		t.Errorf("baseUrl = %q", got.Env["baseUrl"])
	}
	for _, want := range []string{
		"Bearer {{token}}", "@when = 2024-01-01T00:00:00Z", "@day = 2024-01-01", "@site = https://example.com",
		"@kind = alpha", "@flag = true", "@ex = 7", `"a": 0`, `"b": "string"`, `"x": 0`,
	} {
		if !strings.Contains(got.HTTP, want) {
			t.Errorf("the .http text lacks %q:\n%s", want, got.HTTP)
		}
	}
	if strings.Contains(got.HTTP, "\n### world") {
		t.Errorf("a body line must not start a new request:\n%s", got.HTTP)
	}
	if got.Count != 5 {
		t.Errorf("Count = %d, want 5", got.Count)
	}
}

func TestImportOpenAPISwagger2Bodies(t *testing.T) {
	doc := `{
	  "swagger": "2.0", "info": {"title": "Old"}, "host": "old.example.org", "basePath": "/api/",
	  "schemes": ["http"], "consumes": ["application/xml"],
	  "securityDefinitions": {"b": {"type": "basic"}},
	  "security": [{"b": []}],
	  "paths": {
	    "/a": {"post": {"parameters": [{"name": "body", "in": "body", "schema": {"type": "object", "properties": {"n": {"type": "integer"}}}}]}},
	    "/b": {"post": {"consumes": ["application/json"], "parameters": [{"name": "body", "in": "body", "schema": {"type": "object", "properties": {"n": {"type": "integer"}}}}]}},
	    "/c": {"get": {"parameters": [{"name": "limit", "in": "query", "type": "integer"}]}}
	  }}`
	got, err := ImportOpenAPI([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if got.Env["baseUrl"] != "http://old.example.org/api" {
		t.Errorf("baseUrl = %q", got.Env["baseUrl"])
	}
	for _, want := range []string{"Basic {{basicAuth}}", "Content-Type: application/json", "Content-Type: application/xml", `"n": 0`} {
		if !strings.Contains(got.HTTP, want) {
			t.Errorf("lacks %q:\n%s", want, got.HTTP)
		}
	}
}

func TestImportOpenAPIFromAndLimits(t *testing.T) {
	doc := `{"openapi":"3.0.0","info":{"title":"T"},"servers":[{"url":"/v1"}],"paths":{"/x":{"get":{}}}}`
	got, err := ImportOpenAPIFrom([]byte(doc), "https://spec.example.org/openapi.json")
	if err != nil || got.Env["baseUrl"] != "https://spec.example.org/v1" {
		t.Errorf("relative server = %+v, %v", got.Env, err)
	}
	if _, err := ImportOpenAPIFrom([]byte(doc), "not a url"); err == nil {
		t.Error("a spec URL without a host: want error")
	}
	if _, err := ImportOpenAPI(make([]byte, MaxOpenAPIBytes+1)); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("a huge file = %v", err)
	}
	if _, err := ImportOpenAPI([]byte(`{"openapi": "3.0.0", "paths": {`)); err == nil || !strings.Contains(err.Error(), "bad JSON") {
		t.Errorf("broken JSON = %v", err)
	}
	if _, err := ImportOpenAPI([]byte("- a\n- b\n")); err == nil || !strings.Contains(err.Error(), "not an object") {
		t.Errorf("a YAML list = %v", err)
	}
	// A spec with no servers and an origin uses the origin; without an origin, localhost.
	noSrv := `{"openapi":"3.0.0","info":{"title":"T"},"paths":{"/x":{"get":{}}}}`
	if g, _ := ImportOpenAPIFrom([]byte(noSrv), "https://spec.example.org/o.json"); g.Env["baseUrl"] != "https://spec.example.org" {
		t.Errorf("origin base = %+v", g.Env)
	}
	if g, _ := ImportOpenAPI([]byte(noSrv)); g.Env["baseUrl"] != "http://localhost" {
		t.Errorf("default base = %+v", g.Env)
	}
}

func TestOpenAPIScalarAndNames(t *testing.T) {
	for _, c := range []struct {
		in   any
		want string
	}{{nil, ""}, {"s", "s"}, {true, "true"}, {3, "3"}, {2.5, "2.5"}, {[]any{1, "a"}, `[1,"a"]`}} {
		if got := openapiScalar(c.in); got != c.want {
			t.Errorf("openapiScalar(%v) = %q, want %q", c.in, got, c.want)
		}
	}
	if openapiScalar(make(chan int)) == "" {
		t.Error("a value that cannot be JSON is still shown")
	}
	if openapiVarName("") != "param" || openapiVarName("a b-c") != "a_b-c" {
		t.Errorf("openapiVarName: %q %q", openapiVarName(""), openapiVarName("a b-c"))
	}
	m := normYAML(map[any]any{1: []any{map[any]any{"k": "v"}}})
	if mm, ok := m.(map[string]any); !ok || mm["1"] == nil {
		t.Errorf("normYAML = %#v", m)
	}
}
