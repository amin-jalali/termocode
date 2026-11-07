package httpclient

import (
	"context"
	"strings"
	"testing"
)

const mockOpenAPI3 = `openapi: 3.0.0
info: {title: Pets}
servers:
  - url: https://api.example.com/v1
paths:
  /pets:
    get:
      responses:
        "200":
          content:
            application/json:
              example: [{"id": 1, "name": "rex"}]
    post:
      responses:
        "201":
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: {type: integer, example: 9}
  /pets/{petId}:
    delete:
      responses:
        "204": {description: gone}
`

const mockSwagger2 = `{"swagger":"2.0","info":{"title":"T"},"basePath":"/api","produces":["application/json"],
"paths":{"/items/{id}":{"get":{"responses":{"200":{"schema":{"type":"object","properties":{"ok":{"type":"boolean","example":true}}}}}}}}}`

// API-22: an OpenAPI 3 file is a mock source. Each operation answers
// with its first 2xx response and the example (or one built from the
// schema), under the server URL's path.
func TestMockServerFromOpenAPI(t *testing.T) {
	m, err := NewMockOpenAPI([]byte(mockOpenAPI3))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(m.Routes()); n != 3 {
		t.Fatalf("routes %+v", m.Routes())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr, err := m.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + addr
	if code, typ, body := mockGet(t, "GET", base+"/v1/pets"); code != 200 || body != `[{"id":1,"name":"rex"}]` || !strings.HasPrefix(typ, "application/json") {
		t.Errorf("list: %d %q %q", code, typ, body)
	}
	if code, _, body := mockGet(t, "POST", base+"/v1/pets"); code != 201 || body != `{"id":9}` {
		t.Errorf("create: %d %q", code, body)
	}
	if code, _, body := mockGet(t, "DELETE", base+"/v1/pets/5"); code != 204 || body != "" {
		t.Errorf("delete: %d %q", code, body)
	}
	if code, _, _ := mockGet(t, "GET", base+"/pets"); code != 404 {
		t.Errorf("path without the server prefix: %d, want 404", code)
	}
	cancel()
	m.Wait()
}

// Swagger 2.0 (JSON): basePath prefix, schema example, produces type.
func TestMockServerFromSwagger2(t *testing.T) {
	m, err := NewMockOpenAPI([]byte(mockSwagger2))
	if err != nil {
		t.Fatal(err)
	}
	rs := m.Routes()
	if len(rs) != 1 || rs[0].Method != "GET" || rs[0].Path != "/api/items/:id" || rs[0].Status != 200 || rs[0].Body != `{"ok":true}` || rs[0].Type != "application/json" {
		t.Fatalf("routes %+v", rs)
	}
}

// Not a spec, or a spec with no operations, is an error.
func TestMockServerOpenAPIErrors(t *testing.T) {
	if _, err := NewMockOpenAPI([]byte("hello: 1")); err == nil {
		t.Error("plain YAML accepted")
	}
	if _, err := NewMockOpenAPI([]byte("openapi: 3.0.0\npaths: {}\n")); err == nil {
		t.Error("no operations accepted")
	}
}

// LooksOpenAPI tells a spec file from other YAML / JSON.
func TestLooksOpenAPI(t *testing.T) {
	cases := []struct {
		name, src string
		want      bool
	}{
		{"api.yaml", mockOpenAPI3, true},
		{"api.json", mockSwagger2, true},
		{"api.yml", "openapi: 3.1.0\n", true},
		{"compose.yaml", "services: {}\n", false},
		{"api.http", mockOpenAPI3, false},
		{"pkg.json", `{"name":"x"}`, false},
	}
	for _, c := range cases {
		if got := LooksOpenAPI(c.name, []byte(c.src)); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
