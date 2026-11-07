package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const specRelative = `openapi: 3.0.0
info: {title: Pets}
servers: [{url: /v1}]
paths:
  /pets:
    get: {operationId: listPets}
`

func TestIsOpenAPIURL(t *testing.T) {
	for s, want := range map[string]bool{
		"https://x.io/openapi.yaml": true, "http://localhost:8080/spec.json": true,
		"api/openapi.yaml": false, "/abs/spec.json": false, "ftp://x/spec": false, "file:///etc/x": false,
	} {
		if IsOpenAPIURL(s) != want {
			t.Errorf("IsOpenAPIURL(%q) = %v", s, !want)
		}
	}
}

func TestImportOpenAPIFromURLMakesRelativeServerAbsolute(t *testing.T) {
	im, err := ImportOpenAPIFrom([]byte(specRelative), "https://api.example.com/docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if im.Env["baseUrl"] != "https://api.example.com/v1" {
		t.Fatalf("baseUrl = %q", im.Env["baseUrl"])
	}
	// No servers: the spec's own origin.
	im, err = ImportOpenAPIFrom([]byte("openapi: 3.1.0\ninfo: {title: T}\npaths:\n  /a:\n    get: {}\n"), "http://h:9000/x/spec.yaml")
	if err != nil || im.Env["baseUrl"] != "http://h:9000" {
		t.Fatalf("baseUrl = %q, %v", im.Env["baseUrl"], err)
	}
	// Swagger 2.0 without host: the spec's host.
	im, err = ImportOpenAPIFrom([]byte(`{"swagger":"2.0","basePath":"/api","paths":{"/a":{"get":{}}}}`), "https://s.io/swagger.json")
	if err != nil || im.Env["baseUrl"] != "https://s.io/api" {
		t.Fatalf("v2 baseUrl = %q, %v", im.Env["baseUrl"], err)
	}
}

func TestFetchOpenAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/spec.yaml":
			w.Write([]byte(specRelative))
		case "/big":
			w.Write([]byte(strings.Repeat("a", 2048)))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := New(Options{})
	data, err := FetchOpenAPI(context.Background(), c, srv.URL+"/spec.yaml", 1<<20)
	if err != nil || !strings.Contains(string(data), "listPets") {
		t.Fatalf("fetch = %q, %v", data, err)
	}
	if _, err := FetchOpenAPI(context.Background(), c, srv.URL+"/missing", 1<<20); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("404 err = %v", err)
	}
	if _, err := FetchOpenAPI(context.Background(), c, srv.URL+"/big", 1000); err == nil {
		t.Fatal("a too big spec must fail")
	}
	if _, err := FetchOpenAPI(context.Background(), c, "file:///etc/passwd", 1000); err == nil {
		t.Fatal("only http and https URLs")
	}
}
