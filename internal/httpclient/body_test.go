package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bodyReq(method, body string, headers ...Header) Request {
	r := Request{Method: method, URL: "https://api.example.test/x", Headers: headers}
	if body != "" {
		r.Body = &body
	}
	return r
}

func writeBodyFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBuildBodyPlainUnchanged(t *testing.T) {
	wb, err := BuildBody(bodyReq("POST", `{"a":1}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if wb.Method != "POST" || string(wb.Body) != `{"a":1}` || !wb.HasBody || wb.ContentType != "" {
		t.Fatalf("got %+v", wb)
	}
	wb, err = BuildBody(bodyReq("GET", ""), "")
	if err != nil || wb.HasBody || wb.Body != nil {
		t.Fatalf("no body: got %+v, %v", wb, err)
	}
}

func TestBuildBodyFileRef(t *testing.T) {
	dir := t.TempDir()
	data := []byte{0, 1, 2, 0xff, '\n', 'x'}
	writeBodyFile(t, dir, "data.bin", data)
	for _, body := range []string{"< ./data.bin", "<  data.bin  "} {
		wb, err := BuildBody(bodyReq("PUT", body, Header{"Content-Type", "application/octet-stream"}), dir)
		if err != nil {
			t.Fatalf("%q: %v", body, err)
		}
		if !bytes.Equal(wb.Body, data) || wb.ContentType != "" {
			t.Fatalf("%q: got %q ct %q", body, wb.Body, wb.ContentType)
		}
	}
}

func TestBuildBodyFileRefStaysInDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "api")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeBodyFile(t, root, "secret.txt", []byte("no"))
	outside := filepath.Join(root, "secret.txt")
	if err := os.Symlink(outside, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"< ../secret.txt", "< " + outside, "< ./link.txt", "< ./missing.bin"} {
		if _, err := BuildBody(bodyReq("POST", body), dir); err == nil {
			t.Fatalf("%q: want an error", body)
		}
	}
	if _, err := BuildBody(bodyReq("POST", "< ./data.bin"), ""); err == nil {
		t.Fatal("no dir (unsaved request): want an error")
	}
}

func TestBuildBodyMultipart(t *testing.T) {
	dir := t.TempDir()
	writeBodyFile(t, dir, "a.txt", []byte("file text\n"))
	body := strings.Join([]string{
		"--B",
		`Content-Disposition: form-data; name="title"`,
		"",
		"Hello",
		"--B",
		`Content-Disposition: form-data; name="file"; filename="a.txt"`,
		"Content-Type: text/plain",
		"",
		"< ./a.txt",
		"--B--",
	}, "\n")
	wb, err := BuildBody(bodyReq("POST", body, Header{"Content-Type", "multipart/form-data; boundary=B"}), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "--B\r\n" +
		"Content-Disposition: form-data; name=\"title\"\r\n\r\nHello\r\n" +
		"--B\r\n" +
		"Content-Disposition: form-data; name=\"file\"; filename=\"a.txt\"\r\nContent-Type: text/plain\r\n\r\n" +
		"file text\n\r\n" +
		"--B--\r\n"
	if string(wb.Body) != want {
		t.Fatalf("got\n%q\nwant\n%q", wb.Body, want)
	}
}

func TestBuildBodyFormURLEncoded(t *testing.T) {
	ct := Header{"Content-Type", "application/x-www-form-urlencoded"}
	wb, err := BuildBody(bodyReq("POST", "name = Ada Lovelace\n&note=a&b+c\n\ncity=Zürich", ct), "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(wb.Body), "name=Ada+Lovelace&note=a%26b%2Bc&city=Z%C3%BCrich"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// One line is sent as written (it may be encoded already).
	wb, _ = BuildBody(bodyReq("POST", "a=1&b=x%20y", ct), "")
	if string(wb.Body) != "a=1&b=x%20y" {
		t.Fatalf("one line changed: %q", wb.Body)
	}
}

func TestBuildBodyGraphQL(t *testing.T) {
	q := "query User($id: ID!) {\n  user(id: $id) { name }\n}"
	for _, r := range []Request{
		bodyReq("GRAPHQL", q+"\n\n{\"id\": 7}"),
		bodyReq("POST", q+"\n\n{\"id\": 7}", Header{"X-Request-Type", "GraphQL"}),
	} {
		wb, err := BuildBody(r, "")
		if err != nil {
			t.Fatal(err)
		}
		if wb.Method != "POST" || wb.ContentType != "application/json" {
			t.Fatalf("got method %q ct %q", wb.Method, wb.ContentType)
		}
		var got struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(wb.Body, &got); err != nil {
			t.Fatalf("%s: %v", wb.Body, err)
		}
		if got.Query != q || got.Variables["id"] != float64(7) {
			t.Fatalf("got %+v", got)
		}
	}
	wb, err := BuildBody(bodyReq("GRAPHQL", "{ me { id } }"), "")
	if err != nil || string(wb.Body) != `{"query":"{ me { id } }"}` {
		t.Fatalf("no variables: %s %v", wb.Body, err)
	}
	if _, err := BuildBody(bodyReq("GRAPHQL", "{ me }\n\n{not json"), ""); err == nil {
		t.Fatal("bad variables: want an error")
	}
}

func TestSendFileBodyAndGraphQL(t *testing.T) {
	var gotMethod, gotCT string
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotCT = r.Method, r.Header.Get("Content-Type")
		got, _ = io.ReadAll(r.Body)
	}))
	defer srv.Close()
	dir := t.TempDir()
	writeBodyFile(t, dir, "up.bin", []byte{9, 8, 7})
	c := New(Options{})
	body := "< ./up.bin"
	r := Request{Method: "PUT", URL: srv.URL, Body: &body}
	resp := c.Send(context.Background(), Resolved{Request: r, Template: r, Dir: dir}, Hooks{})
	if resp.Err != "" || !bytes.Equal(got, []byte{9, 8, 7}) || gotMethod != "PUT" {
		t.Fatalf("file body: err %q got %v %s", resp.Err, got, gotMethod)
	}
	gq := "{ me { id } }"
	r = Request{Method: "GRAPHQL", URL: srv.URL, Body: &gq}
	resp = c.Send(context.Background(), Resolved{Request: r, Template: r}, Hooks{})
	if resp.Err != "" || gotMethod != "POST" || gotCT != "application/json" || string(got) != `{"query":"{ me { id } }"}` {
		t.Fatalf("graphql: err %q %s %s %s", resp.Err, gotMethod, gotCT, got)
	}
	r = Request{Method: "PUT", URL: srv.URL, Body: &body}
	resp = c.Send(context.Background(), Resolved{Request: r, Template: r}, Hooks{})
	if resp.Err == "" {
		t.Fatal("file body without a dir: want an error")
	}
}

func TestResolveKeepsBaseDir(t *testing.T) {
	rv := &Resolver{BaseDir: "/w/api"}
	out, _ := rv.Resolve(bodyReq("GET", ""))
	if out.Dir != "/w/api" {
		t.Fatalf("Dir = %q", out.Dir)
	}
}
