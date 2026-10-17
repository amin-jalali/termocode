package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// memTokens is an in-memory TokenStore.
type memTokens struct {
	mu sync.Mutex
	m  map[string]ai.MCPOAuth
}

func (s *memTokens) Load(name string) (ai.MCPOAuth, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[name]
	return e, ok
}

func (s *memTokens) Save(name string, e ai.MCPOAuth) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]ai.MCPOAuth{}
	}
	s.m[name] = e
	return nil
}

func (s *memTokens) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, name)
	return nil
}

func (e *managerEnv) httpManager(t *testing.T, secrets map[string]string, tokens TokenStore, idle time.Duration) *Manager {
	t.Helper()
	m := NewManager(ManagerOptions{
		UserPath: e.user, ProjectPath: e.project, CacheDir: e.cache,
		Expander:    func() Expander { return Expander{Secrets: secrets, Workspace: e.dir} },
		CallTimeout: 5 * time.Second, IdleTimeout: idle, Tokens: tokens,
	})
	t.Cleanup(m.Close)
	return m
}

func authHeaders(f *mcptest.HTTPServer) []string {
	var out []string
	for _, r := range f.Requests() {
		out = append(out, r.Header.Get("Authorization"))
	}
	return out
}

// Spec (MC-5): an http entry with a PAT from ${secret:…} connects, lists
// and runs tools; it has no pid.
func TestManagerHTTPWithSecretHeader(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Bearer: "pat-secret-value-123"})
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"gh": map[string]any{"type": "http", "url": f.MCPURL(),
		"headers": map[string]string{"Authorization": "Bearer ${secret:gh_pat}"}}})
	m := e.httpManager(t, map[string]string{"gh_pat": "pat-secret-value-123"}, nil, 0)
	if n := countTools(m.Tools(context.Background(), nil)); n != 10 {
		t.Fatalf("tools %d (%+v)", n, status(m, "gh"))
	}
	r, err := m.Call(context.Background(), nil, "gh", "echo", map[string]any{"text": "pat"})
	if err != nil || r.Text() != "echo: pat" {
		t.Fatalf("call %q %v", r.Text(), err)
	}
	if st := status(m, "gh"); st.State != StateRunning || st.Pid != 0 || st.Type != TypeHTTP {
		t.Errorf("status %+v", st)
	}

	m2 := e.httpManager(t, nil, nil, 0)
	m2.Tools(context.Background(), nil) // from the cache: nothing to connect
	if _, err := m2.Call(context.Background(), nil, "gh", "echo", map[string]any{"text": "x"}); err == nil || !strings.Contains(err.Error(), "needs secret gh_pat") {
		t.Errorf("missing secret: %v", err)
	}
	// A static token the server refuses is "not authorized", not a sign-in.
	m3 := e.httpManager(t, map[string]string{"gh_pat": "wrong-token-value"}, &memTokens{}, 0)
	_, err = m3.Call(context.Background(), nil, "gh", "echo", map[string]any{"text": "x"})
	if err == nil || !strings.Contains(err.Error(), "not authorized") || status(m3, "gh").State == StateSignIn {
		t.Errorf("wrong PAT: %v, %+v", err, status(m3, "gh"))
	}
}

// oauthEnv is an OAuth-protected fake MCP server, its AS, and a Manager.
type oauthEnv struct {
	*managerEnv
	as     *mcptest.AuthServer
	srv    *mcptest.HTTPServer
	tokens *memTokens
	m      *Manager
}

func newOAuthEnv(t *testing.T, o mcptest.HTTPOptions) *oauthEnv {
	t.Helper()
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{})
	o.Validate, o.AuthServer = as.Valid, as.Issuer()
	srv := mcptest.NewHTTPServer(t, o)
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"lin": map[string]any{"type": "http", "url": srv.MCPURL()}})
	tk := &memTokens{}
	return &oauthEnv{managerEnv: e, as: as, srv: srv, tokens: tk, m: e.httpManager(t, nil, tk, 0)}
}

// signIn runs the whole sign-in as the user would (the test is the
// browser).
func (o *oauthEnv) signIn(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fl, err := o.m.BeginSignIn(ctx, "lin")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	resp, err := http.Get(fl.AuthURL())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	tok, err := fl.Wait(ctx)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if err := o.m.FinishSignIn("lin", tok); err != nil {
		t.Fatal(err)
	}
}

// Spec (MC-6): an OAuth server without a token says "sign-in needed";
// after the sign-in it connects, the token is stored for the server and
// sent as a bearer; Sign out forgets it.
func TestManagerOAuthSignIn(t *testing.T) {
	o := newOAuthEnv(t, mcptest.HTTPOptions{})
	if n := countTools(o.m.Tools(context.Background(), nil)); n != 0 {
		t.Fatalf("tools before sign-in: %d", n)
	}
	if st := status(o.m, "lin"); st.State != StateSignIn || st.SignedIn {
		t.Fatalf("before sign-in: %+v", st)
	}
	_, err := o.m.Call(context.Background(), nil, "lin", "echo", map[string]any{"text": "x"})
	if !errors.Is(err, ErrSignInNeeded) || !strings.Contains(err.Error(), "Sign in") {
		t.Fatalf("call before sign-in: %v", err)
	}
	o.signIn(t)
	if err := o.m.Connect(context.Background(), nil, "lin"); err != nil {
		t.Fatal(err)
	}
	if st := status(o.m, "lin"); st.State != StateRunning || !st.SignedIn || st.Tools != 10 {
		t.Errorf("after sign-in: %+v", st)
	}
	r, err := o.m.Call(context.Background(), nil, "lin", "echo", map[string]any{"text": "oauth"})
	if err != nil || r.Text() != "echo: oauth" {
		t.Fatalf("call %q %v", r.Text(), err)
	}
	tok, ok := o.tokens.Load("lin")
	if !ok || tok.Resource != o.srv.MCPURL() || tok.Issuer != o.as.Issuer() || !o.as.Valid(tok.AccessToken) {
		t.Errorf("stored %+v", tok)
	}
	if hs := authHeaders(o.srv); hs[len(hs)-1] != "Bearer "+tok.AccessToken {
		t.Errorf("last Authorization %q", hs[len(hs)-1])
	}
	o.m.SignOut("lin")
	if _, ok := o.tokens.Load("lin"); ok || status(o.m, "lin").State == StateRunning {
		t.Errorf("after sign out: %+v", status(o.m, "lin"))
	}
}

// Spec: a token that ends within a minute is refreshed before the call;
// a revoked one is refreshed after the 401 and the call goes through.
func TestManagerOAuthRefresh(t *testing.T) {
	o := newOAuthEnv(t, mcptest.HTTPOptions{})
	o.signIn(t)
	tok, _ := o.tokens.Load("lin")
	tok.ExpiresAtMs = time.Now().Add(30 * time.Second).UnixMilli()
	_ = o.tokens.Save("lin", tok)
	if _, err := o.m.Call(context.Background(), nil, "lin", "echo", map[string]any{"text": "x"}); err != nil {
		t.Fatal(err)
	}
	nt, _ := o.tokens.Load("lin")
	if nt.AccessToken == tok.AccessToken {
		t.Error("expiring token not refreshed")
	}
	o.m.Stop("lin")
	o.as.RevokeAccess()
	if _, err := o.m.Call(context.Background(), nil, "lin", "echo", map[string]any{"text": "y"}); err != nil {
		t.Fatalf("after revoke: %v", err)
	}
	if nt2, _ := o.tokens.Load("lin"); nt2.AccessToken == nt.AccessToken || !o.as.Valid(nt2.AccessToken) {
		t.Error("revoked token not refreshed")
	}
}

// Spec (issuer binding): when the server's URL changes, the stored
// token is never sent to the new URL; it is dropped and the server asks
// for a sign-in.
func TestManagerOAuthTokenBoundToServer(t *testing.T) {
	o := newOAuthEnv(t, mcptest.HTTPOptions{})
	o.signIn(t)
	other := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: func(string) bool { return false }, AuthServer: "https://other-as.example"})
	o.write(t, o.user, map[string]any{"lin": map[string]any{"type": "http", "url": other.MCPURL()}})
	o.m.Reload()
	_, err := o.m.Call(context.Background(), nil, "lin", "echo", map[string]any{"text": "x"})
	if !errors.Is(err, ErrSignInNeeded) {
		t.Fatalf("err %v", err)
	}
	for _, h := range authHeaders(other) {
		if h != "" {
			t.Errorf("a token went to the other server: %q", h)
		}
	}
	if _, ok := o.tokens.Load("lin"); ok {
		t.Error("the token of the old URL was kept")
	}
}

// Spec: the idle timer ends a remote session with DELETE; a session the
// server forgot is opened again without an error for the call.
func TestManagerHTTPSessionIdleAndExpiry(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Session: true, SSE: true})
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"s": map[string]any{"url": f.MCPURL()}})
	m := e.httpManager(t, nil, nil, 300*time.Millisecond)
	if _, err := m.Call(context.Background(), nil, "s", "echo", map[string]any{"text": "1"}); err != nil {
		t.Fatal(err)
	}
	f.ExpireSessions()
	if r, err := m.Call(context.Background(), nil, "s", "echo", map[string]any{"text": "2"}); err != nil || r.Text() != "echo: 2" {
		t.Fatalf("after expiry: %q %v", r.Text(), err)
	}
	end := time.Now().Add(5 * time.Second)
	for (f.Deletes() == 0 || status(m, "s").State != StateStopped) && time.Now().Before(end) {
		time.Sleep(20 * time.Millisecond)
	}
	if f.Deletes() == 0 || status(m, "s").State != StateStopped {
		t.Errorf("idle: deletes %d, state %q", f.Deletes(), status(m, "s").State)
	}
}

// Spec: a project server is not trusted by itself, so it can not start a
// sign-in until the user trusts it (Manager.Trust, the dialog's answer).
func TestManagerSignInNeedsTrust(t *testing.T) {
	o := newOAuthEnv(t, mcptest.HTTPOptions{})
	o.write(t, o.user, map[string]any{})
	o.write(t, o.project, map[string]any{"lin": map[string]any{"type": "http", "url": o.srv.MCPURL()}})
	o.m.Reload()
	if _, err := o.m.BeginSignIn(context.Background(), "lin"); !errors.Is(err, ErrNotTrusted) {
		t.Fatalf("untrusted: %v", err)
	}
	o.m.Trust("lin", false)
	fl, err := o.m.BeginSignIn(context.Background(), "lin")
	if err != nil {
		t.Fatal(err)
	}
	fl.Close()
	if !status(o.m, "lin").Trusted {
		t.Error("not trusted after Trust")
	}
}

// CredStore keeps sign-ins in credentials.json (0600) and secrets in
// mcp_secrets.
func TestCredStore(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ai", "credentials.json")
	s := CredStore{Path: p}
	if _, ok := s.Load("x"); ok {
		t.Error("empty store")
	}
	if err := s.Save("x", ai.MCPOAuth{AccessToken: "tok-1234567890"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSecret("pat", "pat-value-123456"); err != nil {
		t.Fatal(err)
	}
	c, _ := ai.LoadCredentials(p)
	if c.MCPOAuth["x"].AccessToken != "tok-1234567890" || c.MCPSecrets["pat"] != "pat-value-123456" {
		b, _ := json.Marshal(c)
		t.Errorf("credentials %s", b)
	}
	if err := s.Delete("x"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Load("x"); ok {
		t.Error("not deleted")
	}
}
