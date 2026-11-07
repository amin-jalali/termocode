package httpclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

const mockSrc = `@host = https://api.example.com

### list users
# @mock-status 200
# @mock-body [{"id":1}]
GET {{host}}/users?page=2

### one user
# @mock-body {"id":7}
GET {{host}}/users/{{id}}

### create
# @mock-status 201
# @mock-type text/plain
# @mock-body created
POST {{host}}/users
Content-Type: application/json

{"name":"a"}
`

func mockGet(t *testing.T, method, url string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(b)
}

// API-22: a mock server from a .http file answers every request block on
// a random loopback port with its # @mock-* status, type and body.
func TestMockServerFromHTTPFile(t *testing.T) {
	m, err := NewMock(Parse(mockSrc))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Routes()) != 3 {
		t.Fatalf("routes %+v", m.Routes())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr, err := m.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") || strings.HasSuffix(addr, ":0") {
		t.Fatalf("addr %q, want a random loopback port", addr)
	}
	base := "http://" + addr

	if code, typ, body := mockGet(t, "GET", base+"/users"); code != 200 || body != `[{"id":1}]` || !strings.HasPrefix(typ, "application/json") {
		t.Errorf("list: %d %q %q", code, typ, body)
	}
	if code, _, body := mockGet(t, "GET", base+"/users/42"); code != 200 || body != `{"id":7}` {
		t.Errorf("path var: %d %q", code, body)
	}
	if code, typ, body := mockGet(t, "POST", base+"/users"); code != 201 || body != "created" || !strings.HasPrefix(typ, "text/plain") {
		t.Errorf("create: %d %q %q", code, typ, body)
	}
	if code, _, _ := mockGet(t, "DELETE", base+"/users/1"); code != http.StatusNotFound {
		t.Errorf("unknown route: %d, want 404", code)
	}
	if code, _, _ := mockGet(t, "GET", base+"/users/1/posts"); code != http.StatusNotFound {
		t.Errorf("longer path: %d, want 404", code)
	}

	cancel()
	m.Wait()
	if _, err := http.Get(base + "/users"); err == nil {
		t.Error("server still answers after the context ended")
	}
}

// A file with no request blocks has nothing to mock.
func TestMockServerNeedsRequests(t *testing.T) {
	if _, err := NewMock(Parse("@a = 1\n")); err == nil {
		t.Error("want an error for a file with no requests")
	}
}

// A bad # @mock-status is an error that names the request line.
func TestMockServerBadStatus(t *testing.T) {
	_, err := NewMock(Parse("# @mock-status abc\nGET /x\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("err %v", err)
	}
}

func TestMockPathOf(t *testing.T) {
	for in, want := range map[string]string{
		"{{host}}/a/b?x=1":           "/a/b",
		"https://h.example/a/{{id}}": "/a/{{id}}",
		"http://h:8080":              "/",
		"/plain/path#frag":           "/plain/path",
		"{{base}}":                   "/",
	} {
		if got := mockPathOf(in); got != want {
			t.Errorf("mockPathOf(%q) = %q, want %q", in, got, want)
		}
	}
}
