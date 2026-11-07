package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// chainDepFixture is a workspace with login (gives a token) and me
// (reads {{login.response.body.$.token}}). hits lists the paths in the
// order the server saw them.
func chainDepFixture(t *testing.T) (*Model, string, func() []string) {
	t.Helper()
	m, root, _ := covSvcHTTP(t)
	var mu sync.Mutex
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.URL.Path+" "+r.Header.Get("Authorization"))
		mu.Unlock()
		if r.URL.Path == "/login" {
			_, _ = w.Write([]byte(`{"token":"abc123"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	file := filepath.Join(root, "chain.http")
	src := "@host = " + srv.URL + "\n\n###\n# @name login\nPOST {{host}}/login\n\n###\n# @name me\nGET {{host}}/me\nAuthorization: Bearer {{login.response.body.$.token}}\n"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return m, file, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), hits...) }
}

func chainDepReq(t *testing.T, m *Model, file, name string) (httpclient.File, httpclient.Request) {
	t.Helper()
	f, ok := m.httpReadFile(file)
	if !ok {
		t.Fatal("read file")
	}
	for _, r := range f.Requests {
		if r.Name == name {
			return f, r
		}
	}
	t.Fatalf("no request %s", name)
	return f, httpclient.Request{}
}

// Sending a request whose dependency never ran asks first (Cancel is
// the first button); "Send login first" sends login, then the request
// with the fresh token.
func TestHTTPChainDepAsksAndSendsDependencyFirst(t *testing.T) {
	m, file, hits := chainDepFixture(t)
	f, me := chainDepReq(t, m, file, "me")
	if cmd := m.httpSendRequest(file, f.Vars, me, ""); cmd != nil {
		t.Fatal("sent without asking")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindHTTPChainDep || !strings.Contains(cardText(m), "login") {
		t.Fatalf("no dependency card: %s", cardText(m))
	}
	if b := m.confirm.Buttons(); len(b) < 2 || b[0].ID != "cancel" {
		t.Fatalf("Cancel is not first: %+v", b)
	}
	c, _ := m.handleHTTPConfirm("first")
	covSvcHTTPRun(t, m, c)
	got := hits()
	if len(got) != 2 || !strings.HasPrefix(got[0], "/login") || got[1] != "/me Bearer abc123" {
		t.Fatalf("hits = %q", got)
	}
	if len(m.httpS().chainNext) != 0 {
		t.Error("chain queue not empty")
	}
}

// Cancel sends nothing; once login has an answer, me goes out at once.
func TestHTTPChainDepCancelAndNoAskWhenAnswered(t *testing.T) {
	m, file, hits := chainDepFixture(t)
	f, me := chainDepReq(t, m, file, "me")
	m.httpSendRequest(file, f.Vars, me, "")
	if c, _ := m.handleHTTPConfirm("cancel"); c != nil || len(m.httpS().chainNext) != 0 || m.httpS().chainWait != nil {
		t.Fatal("cancel left work behind")
	}
	m.confirmOpen = false // the card closes on any answer (Update)
	_, login := chainDepReq(t, m, file, "login")
	covSvcHTTPRun(t, m, m.httpSendRequest(file, f.Vars, login, ""))
	cmd := m.httpSendRequest(file, f.Vars, me, "")
	if cmd == nil || m.confirmKind == confirmKindHTTPChainDep && m.confirmOpen {
		t.Fatal("asked although login has an answer")
	}
	covSvcHTTPRun(t, m, cmd)
	if got := hits(); len(got) != 2 || got[1] != "/me Bearer abc123" {
		t.Fatalf("hits = %q", got)
	}
}

// The AI's http_send reads answers of earlier sends in the same file,
// and says which request to send first when there is none.
func TestHTTPAIChainUsesEarlierAnswers(t *testing.T) {
	m, file, _ := chainDepFixture(t)
	root := filepath.Dir(file)
	args := map[string]any{"path": "chain.http", "name": "me"}
	r := httpAIBuild(root, args, m.httpS().chainSnapshot)
	err := r.validate()
	if err == nil || !strings.Contains(err.Error(), "login") || !strings.Contains(err.Error(), "first") {
		t.Fatalf("validate = %v", err)
	}
	m.httpS().remember(&httpRun{file: file, rr: httpclient.Resolved{Template: httpclient.Request{Name: "login"}},
		resp: &httpclient.Response{Status: 200, Body: []byte(`{"token":"zz9"}`)}})
	r = httpAIBuild(root, args, m.httpS().chainSnapshot)
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	if h := r.rr.Headers; len(h) != 1 || h[0].Value != "Bearer zz9" {
		t.Fatalf("headers = %+v", h)
	}
}
