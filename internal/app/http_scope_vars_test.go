package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A request with `# @prompt` lines asks each value before it is sent
// (a password-like name is masked); Esc sends nothing.
func TestHTTPPromptVarsAskThenSend(t *testing.T) {
	m, root, _ := covSvcHTTP(t)
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.URL.Path+" "+r.Header.Get("X-Pass"))
		mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	file := filepath.Join(root, "p.http")
	src := "###\n# @name who\n# @prompt user\n# @prompt password Your password\nGET " + srv.URL + "/u/{{user}}\nX-Pass: {{password}}\n"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	f, req := chainDepReq(t, m, file, "who")
	if cmd := m.httpSendRequest(file, f.Vars, req, ""); cmd != nil {
		t.Fatal("sent before asking")
	}
	if !m.promptOpen || m.promptKind != promptKindHTTPPromptVar || m.prompt.Masked() {
		t.Fatalf("no user prompt (kind %v)", m.promptKind)
	}
	if c, _ := m.handleHTTPPromptSubmit("ann"); c != nil {
		t.Fatal("sent after the first value")
	}
	if !m.promptOpen || !m.prompt.Masked() || !strings.Contains(m.prompt.View(), "Your password") {
		t.Fatal("password prompt not masked or label missing")
	}
	c, _ := m.handleHTTPPromptSubmit("s3cret")
	covSvcHTTPRun(t, m, c)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "/u/ann s3cret" {
		t.Fatalf("server saw %q", got)
	}
	if m.httpS().promptWait != nil {
		t.Error("prompt state left")
	}
}

// HTTP: Open Global Variables File makes the file (0600) and {{name}}
// reads it in every .http file.
func TestHTTPGlobalsFileOpensAndResolves(t *testing.T) {
	m, root, _ := covSvcHTTP(t)
	m.dispatchHTTPPalette("http-globals-open")
	p := httpGlobalsPath()
	st, err := os.Stat(p)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("globals file: %v %v", st, err)
	}
	if err := os.WriteFile(p, []byte(`{"tenant":"acme"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rv := httpResolver(filepath.Join(root, "api.http"), nil, "")
	if !rv.Known("tenant") {
		t.Fatal("global not known")
	}
	if _, ok := aiActionPolicies["http-globals-open"]; !ok {
		t.Error("no AI policy entry")
	}
}
