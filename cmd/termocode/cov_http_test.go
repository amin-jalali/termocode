package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func httpRun(root string, args ...string) (int, string, string) {
	var out, errw bytes.Buffer
	code := runHTTPIn(args, &out, &errw, root)
	return code, out.String(), errw.String()
}

func TestHTTPRunWritesReportsAndFailsOnBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/bad") {
			w.WriteHeader(500)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	root := t.TempDir()
	good := filepath.Join(root, "good.http")
	if err := os.WriteFile(good, []byte("GET "+srv.URL+"/fine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(root, "sub", "bad.http")
	if err := os.MkdirAll(filepath.Dir(bad), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("GET "+srv.URL+"/bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	js, junit := filepath.Join(root, "r.json"), filepath.Join(root, "r.xml")
	code, out, errw := httpRun(root, "run", good, "--json", js, "--junit", junit, "--timeout", "5s")
	if code != 0 {
		t.Fatalf("good run: %d %s %s", code, out, errw)
	}
	if b, err := os.ReadFile(js); err != nil || !strings.Contains(string(b), "{") {
		t.Fatalf("json report: %v", err)
	}
	if b, err := os.ReadFile(junit); err != nil || !strings.Contains(string(b), "<") {
		t.Fatalf("junit report: %v", err)
	}
	if code, _, _ := httpRun(root, "run", filepath.Join(root, "sub"), "--stop-on-fail"); code != 1 {
		t.Fatalf("a 500 should fail the run: %d", code)
	}
	if code, _, _ := httpRun(root, "run", "--", good); code != 0 {
		t.Fatalf("after --: %d", code)
	}
}

func TestHTTPRunErrors(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "a.http")
	if err := os.WriteFile(good, []byte("GET http://127.0.0.1:1/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args []string
		code int
		err  string
	}{
		{nil, 2, "run .http files"},
		{[]string{"bogus"}, 2, "unknown command"},
		{[]string{"run"}, 2, "give at least one"},
		{[]string{"run", "-iterations", "-1", good}, 2, "give at least one"},
		{[]string{"run", "--nosuchflag", good}, 2, ""},
		{[]string{"run", filepath.Join(root, "missing.http")}, 1, "termocode http run:"},
		{[]string{"run", good, "--data", filepath.Join(root, "no-data.csv")}, 1, "termocode http run:"},
		{[]string{"run", good, "--json", filepath.Join(root, "no", "dir", "r.json")}, 1, "json report"},
		{[]string{"run", good, "--junit", filepath.Join(root, "no", "dir", "r.xml")}, 1, "junit report"},
	} {
		code, _, errw := httpRun(root, c.args...)
		if code != c.code || !strings.Contains(errw, c.err) {
			t.Errorf("%v: %d %q", c.args, code, errw)
		}
	}
	if code, out, _ := httpRun(root, "--help"); code != 0 || out == "" {
		t.Errorf("help: %d", code)
	}
	if code, _, _ := httpRun(root, "run", "-h"); code != 0 {
		t.Errorf("run -h: %d", code)
	}
}

func TestHTTPDisplayPathAndReorder(t *testing.T) {
	if got := displayPath("/r", "/r/a/b.http"); got != "a/b.http" {
		t.Fatalf("%q", got)
	}
	if got := displayPath("/r", "/elsewhere/b.http"); got != "/elsewhere/b.http" {
		t.Fatalf("%q", got)
	}
	got := httpReorderFlags([]string{"a.http", "--env", "dev", "-", "--stop-on-fail", "--rate=2", "--", "--x"})
	if strings.Join(got, " ") != "--env dev --stop-on-fail --rate=2 a.http - --x" {
		t.Fatalf("%v", got)
	}
}
