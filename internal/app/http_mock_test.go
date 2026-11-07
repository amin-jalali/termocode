package app

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func httpMockGet(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// API-22: HTTP: Start Mock Server serves the open .http file on a random
// loopback port; Stop Mock Server ends it; Close ends it too.
func TestHTTPMockStartStop(t *testing.T) {
	m, root, _ := covSvcHTTP(t)
	m.dispatchHTTPPalette("http-mock-start")
	if !strings.Contains(covSvcToast(m), "Open an .http or OpenAPI file") {
		t.Fatalf("toast = %q", covSvcToast(m))
	}
	file := filepath.Join(root, "mock.http")
	src := "### users\n# @mock-body [{\"id\":1}]\nGET {{host}}/users\n"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	m.httpOpenInEditor(file)
	m.editor.SetMeta(file, false, "http")
	m.dispatchHTTPPalette("http-mock-start")
	addr := m.httpS().mockAddr
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("addr %q toast %q", addr, covSvcToast(m))
	}
	if !strings.Contains(covSvcToast(m), "http://"+addr) {
		t.Fatalf("toast = %q", covSvcToast(m))
	}
	if code, body := httpMockGet(t, "http://"+addr+"/users"); code != 200 || body != `[{"id":1}]` {
		t.Fatalf("mock: %d %q", code, body)
	}
	m.dispatchHTTPPalette("http-mock-stop")
	if m.httpS().mockAddr != "" {
		t.Fatal("mock still set after stop")
	}
	if _, err := http.Get("http://" + addr + "/users"); err == nil {
		t.Fatal("mock still answers after stop")
	}
	m.dispatchHTTPPalette("http-mock-stop")
	if !strings.Contains(covSvcToast(m), "No mock server") {
		t.Fatalf("toast = %q", covSvcToast(m))
	}
	m.dispatchHTTPPalette("http-mock-start")
	addr = m.httpS().mockAddr
	m.httpStopMock()
	if _, err := http.Get("http://" + addr + "/users"); err == nil {
		t.Fatal("mock still answers after httpStopMock")
	}
}

// An open OpenAPI file is a mock source too.
func TestHTTPMockFromOpenAPIFile(t *testing.T) {
	m, root, _ := covSvcHTTP(t)
	file := filepath.Join(root, "pets.yaml")
	src := "openapi: 3.0.0\ninfo: {title: Pets}\npaths:\n  /pets:\n    get:\n      responses:\n        \"200\":\n          content:\n            application/json:\n              example: [1, 2]\n"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	m.httpOpenInEditor(file)
	m.editor.SetMeta(file, false, "yaml")
	m.dispatchHTTPPalette("http-mock-start")
	defer m.httpStopMock()
	addr := m.httpS().mockAddr
	if addr == "" {
		t.Fatalf("no mock; toast %q", covSvcToast(m))
	}
	if code, body := httpMockGet(t, "http://"+addr+"/pets"); code != 200 || body != `[1,2]` {
		t.Fatalf("mock: %d %q", code, body)
	}
}
