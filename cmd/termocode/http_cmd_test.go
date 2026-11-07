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

// API-9: `termocode http run` runs .http files for CI: the report goes
// to stdout and to --json / --junit files; the exit code is 1 when a
// request failed and 2 on a usage error.
func TestRunHTTPCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	dir := t.TempDir()
	okFile := filepath.Join(dir, "api", "ok.http")
	os.MkdirAll(filepath.Dir(okFile), 0o755)
	os.WriteFile(okFile, []byte("### ping\nGET {{base}}/ok?id={{id}}\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"base":"`+srv.URL+`"}}`), 0o600)
	data := filepath.Join(dir, "rows.csv")
	os.WriteFile(data, []byte("id\n1\n2\n"), 0o600)
	run := func(args ...string) (int, string, string) {
		var out, errw bytes.Buffer
		code := runHTTPIn(args, &out, &errw, dir)
		return code, out.String(), errw.String()
	}

	jsonOut, junitOut := filepath.Join(dir, "r.json"), filepath.Join(dir, "r.xml")
	code, out, e := run("run", "--env", "dev", "--data", data, "--json", jsonOut, "--junit", junitOut, filepath.Join(dir, "api"))
	if code != 0 || !strings.Contains(out, "2 passed, 0 failed") {
		t.Fatalf("run: %d\n%s\n%s", code, out, e)
	}
	if b, err := os.ReadFile(jsonOut); err != nil || !strings.Contains(string(b), `"passed": 2`) {
		t.Errorf("json report: %s (%v)", b, err)
	}
	if b, err := os.ReadFile(junitOut); err != nil || !strings.Contains(string(b), "<testsuites") {
		t.Errorf("junit report: %s (%v)", b, err)
	}

	bad := filepath.Join(dir, "bad.http")
	os.WriteFile(bad, []byte("GET "+srv.URL+"/fail\n\n###\nGET "+srv.URL+"/ok\n"), 0o600)
	if code, out, _ := run("run", "--stop-on-fail", bad); code != 1 || !strings.Contains(out, "0 passed, 1 failed") {
		t.Errorf("stop on fail: %d\n%s", code, out)
	}
	if code, _, _ := run("run", "--iterations", "2", bad); code != 1 {
		t.Errorf("failures: %d", code)
	}

	if code, _, e := run("run"); code != 2 || !strings.Contains(e, "termocode http run") {
		t.Errorf("no file: %d %q", code, e)
	}
	if code, _, _ := run("run", "--delay", "soon", bad); code != 2 {
		t.Errorf("bad delay: %d", code)
	}
	if code, _, _ := run("nope"); code != 2 {
		t.Errorf("unknown sub command: %d", code)
	}
	if code, out, _ := run("--help"); code != 0 || !strings.Contains(out, "--junit") {
		t.Errorf("help: %d %q", code, out)
	}
}
