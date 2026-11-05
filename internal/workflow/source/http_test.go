package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeToken is split so no scanner sees a key in the source.
var fakeToken = "sk-" + "test-" + "Zq9\"wX7/LmN0"

type fakeSecrets map[string]string

func (f fakeSecrets) Get(name string) (string, bool, error) {
	if name == "BROKEN" {
		return "", false, errors.New("backend failed near " + fakeToken)
	}
	v, ok := f[name]
	return v, ok, nil
}

func newHTTPServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/tickets":
			if auth != "Bearer "+fakeToken {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"data":[{"id":"T-1","title":"one"},{"id":"T-2","title":"two"}]}`)
		case "/echo":
			// A hostile server sends the header back in the answer.
			b, _ := io.ReadAll(r.Body)
			fmt.Fprintf(w, `{"data":[{"got":%q,"body":%q,"m":%q}]}`, auth, b, r.Method)
		case "/echo-bad":
			fmt.Fprintf(w, "not json %s", auth)
		case "/fail":
			http.Error(w, "nope "+auth, http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func bearer() map[string]string {
	return map[string]string{"Authorization": "Bearer ${secret:TICKETS_TOKEN}"}
}

func TestHTTPInlineWithSecretHeader(t *testing.T) {
	srv := newHTTPServer(t)
	s := &Sources{Workspace: t.TempDir(), Secrets: fakeSecrets{"TICKETS_TOKEN": fakeToken}}
	r, err := s.HTTP(context.Background(), HTTPInput{URL: srv.URL + "/tickets", Headers: bearer(), Shape: Shape{Items: "$.data", Key: "$.id"}})
	if err != nil {
		t.Fatal(err)
	}
	if keys(r) != "T-1 T-2" {
		t.Errorf("keys %q", keys(r))
	}
}

func TestHTTPRequestFile(t *testing.T) {
	srv := newHTTPServer(t)
	ws := t.TempDir()
	writeFile(t, ws, "requests/t.http", "@host = "+srv.URL+"\n\n### other\nGET {{host}}/fail\n\n### open\n# @name open\nGET {{host}}/tickets\nAccept: application/json\n")
	s := &Sources{Workspace: ws, Secrets: fakeSecrets{"TICKETS_TOKEN": fakeToken}}
	r, err := s.HTTP(context.Background(), HTTPInput{RequestFile: "requests/t.http", RequestName: "open", Headers: bearer(), Shape: Shape{Items: "$.data[*]", Key: "$.id"}})
	if err != nil {
		t.Fatal(err)
	}
	if keys(r) != "T-1 T-2" {
		t.Errorf("keys %q", keys(r))
	}
	writeFile(t, ws, "requests/bad.http", "GET {{nohost}}/x\n")
	if _, err := s.HTTP(context.Background(), HTTPInput{RequestFile: "requests/bad.http"}); err == nil || !strings.Contains(err.Error(), "nohost") {
		t.Errorf("unknown var: %v", err)
	}
	writeFile(t, ws, "requests/ref.http", "GET "+srv.URL+"/echo\nX-Key: ${env:HOME}\n")
	if _, err := s.HTTP(context.Background(), HTTPInput{RequestFile: "requests/ref.http"}); err == nil {
		t.Error("${env:} in the file: want an error")
	}
	out := t.TempDir()
	writeFile(t, out, "x.http", "GET "+srv.URL+"/tickets\n")
	if err := os.Symlink(filepath.Join(out, "x.http"), filepath.Join(ws, "link.http")); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"../x.http", filepath.Join(out, "x.http"), "link.http"} {
		if _, err := s.HTTP(context.Background(), HTTPInput{RequestFile: f}); !errors.Is(err, ErrOutside) {
			t.Errorf("%q: want ErrOutside, got %v", f, err)
		}
	}
}

func TestHTTPRefusals(t *testing.T) {
	srv := newHTTPServer(t)
	s := &Sources{Workspace: t.TempDir(), Secrets: fakeSecrets{"TICKETS_TOKEN": fakeToken}}
	u := srv.URL + "/echo"
	body := "${secret:TICKETS_TOKEN}"
	for name, in := range map[string]HTTPInput{
		"env ref":         {URL: u, Headers: map[string]string{"X": "${env:HOME}"}},
		"dotenv ref":      {URL: u, Headers: map[string]string{"X": "${dotenv:K}"}},
		"cmd ref":         {URL: u, Headers: map[string]string{"X": "${cmd:cat ~/.ssh/id_rsa}"}},
		"broken ref":      {URL: u, Headers: map[string]string{"X": "${secret:"}},
		"bad secret name": {URL: u, Headers: map[string]string{"X": "${secret:../x}"}},
		"missing secret":  {URL: u, Headers: map[string]string{"X": "${secret:NOPE}"}},
		"secret in url":   {URL: u + "?k=${secret:TICKETS_TOKEN}"},
		"secret in body":  {URL: u, Method: "POST", Body: &body},
		"bad header name": {URL: u, Headers: map[string]string{"X Y": "1"}},
		"no url":          {},
		"both":            {URL: u, RequestFile: "a.http"},
		"not http":        {URL: "file:///etc/passwd"},
		"404":             {URL: srv.URL + "/missing"},
	} {
		if _, err := s.HTTP(context.Background(), in); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := (&Sources{Workspace: t.TempDir()}).HTTP(context.Background(), HTTPInput{URL: u, Headers: bearer()}); err == nil {
		t.Error("no secret store: want an error")
	}
}

// TestHTTPSecretNeverLeaks is the grep test: every error and every item
// of runs that touch the secret must not hold its value.
func TestHTTPSecretNeverLeaks(t *testing.T) {
	srv := newHTTPServer(t)
	sec := fakeSecrets{"TICKETS_TOKEN": fakeToken, "BROKEN": ""}
	s := &Sources{Workspace: t.TempDir(), Secrets: sec}
	var seen []string
	run := func(in HTTPInput) {
		r, err := s.HTTP(context.Background(), in)
		if err != nil {
			seen = append(seen, err.Error())
		}
		for _, it := range r.Items {
			seen = append(seen, it.Key, string(it.Data))
		}
	}
	run(HTTPInput{URL: srv.URL + "/echo", Headers: bearer()})
	run(HTTPInput{URL: srv.URL + "/echo-bad", Headers: bearer()})
	run(HTTPInput{URL: srv.URL + "/fail", Headers: bearer()})
	run(HTTPInput{URL: srv.URL + "/echo", Headers: map[string]string{"Authorization": "${secret:BROKEN}"}})
	run(HTTPInput{URL: "http://127.0.0.1:1/x", Headers: bearer()})
	run(HTTPInput{URL: srv.URL + "/echo", Headers: map[string]string{"Authorization": "Bearer ${secret:TICKETS_TOKEN}", "X-Bad": "${env:HOME}"}})
	if len(seen) < 5 {
		t.Fatalf("too few outputs to check: %q", seen)
	}
	plain := strings.ReplaceAll(fakeToken, `"`, `\"`)
	for _, out := range seen {
		for _, needle := range []string{fakeToken, plain, "Zq9"} {
			if strings.Contains(out, needle) {
				t.Errorf("secret in output: %s", out)
			}
		}
	}
	if !strings.Contains(strings.Join(seen, "\n"), redactedText) {
		t.Error("the echoed secret was not replaced by the marker")
	}
}

// TestPackageDoesNotLog keeps the adapters free of log output: the only
// way out for text is a returned error, and those are scrubbed.
func TestPackageDoesNotLog(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{`"log"`, `"log/slog"`, "fmt.Print", "os.Stderr", "os.Stdout"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s uses %s", f, bad)
			}
		}
	}
}
