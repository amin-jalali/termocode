package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// remoteCanary is a fake secret built from parts.
var remoteCanary = "sk-" + "canary-" + "remote-4410"

// logRec keeps the manager's log lines.
type logRec struct {
	mu    sync.Mutex
	lines []string
}

func (l *logRec) add(server, line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, server+": "+line)
}

func (l *logRec) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func (e *managerEnv) loggedManager(t *testing.T, tokens TokenStore, lr *logRec, tr *trustRec) *Manager {
	t.Helper()
	o := ManagerOptions{
		UserPath: e.user, ProjectPath: e.project, CacheDir: e.cache,
		Expander:    func() Expander { return Expander{Workspace: e.dir} },
		CallTimeout: 5 * time.Second, Tokens: tokens, Log: lr.add,
	}
	if tr != nil {
		o.IsTrusted, o.SaveTrust = tr.isTrusted, tr.save
	}
	m := NewManager(o)
	t.Cleanup(m.Close)
	return m
}

// Spec (MC-7): when the refresh fails (the refresh token expired), the
// server asks for a new sign-in; the log says why and never shows a
// token.
func TestManagerRefreshFailureAsksSignIn(t *testing.T) {
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{})
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid, AuthServer: as.Issuer()})
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"lin": map[string]any{"type": "http", "url": srv.MCPURL()}})
	tk := &memTokens{}
	_ = tk.Save("lin", ai.MCPOAuth{Resource: srv.MCPURL(), Issuer: as.Issuer(), TokenEndpoint: as.Issuer() + "/token",
		ClientID: "c", AccessToken: remoteCanary + "-access", RefreshToken: remoteCanary + "-refresh",
		ExpiresAtMs: time.Now().Add(-time.Minute).UnixMilli()})
	lr := &logRec{}
	m := e.loggedManager(t, tk, lr, nil)
	_, err := m.Call(context.Background(), nil, "lin", "echo", map[string]any{"text": "x"})
	if !errors.Is(err, ErrSignInNeeded) {
		t.Fatalf("err = %v", err)
	}
	if st := status(m, "lin"); st.State != StateSignIn {
		t.Fatalf("state %v", st.State)
	}
	log := lr.text()
	if strings.Count(log, "token refresh failed") < 2 || !strings.Contains(log, "sign-in needed") {
		t.Fatalf("log:\n%s", log)
	}
	if strings.Contains(log, remoteCanary) || strings.Contains(err.Error(), remoteCanary) {
		t.Fatalf("a token reached the log or the error:\n%s\n%v", log, err)
	}
}

// Spec (MC-5): a static token in "headers" that the server refuses says
// to check its scopes; no sign-in is offered.
func TestManagerStaticTokenRefused(t *testing.T) {
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Bearer: "the-right-one"})
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"s": map[string]any{"type": "http", "url": srv.MCPURL(),
		"headers": map[string]string{"Authorization": "Bearer " + remoteCanary}}})
	m := e.loggedManager(t, &memTokens{}, &logRec{}, nil)
	_, err := m.Call(context.Background(), nil, "s", "echo", map[string]any{"text": "x"})
	if err == nil || !strings.Contains(err.Error(), `refused the token in "headers"`) || errors.Is(err, ErrSignInNeeded) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), remoteCanary) {
		t.Fatalf("the token is in the error: %v", err)
	}
}

// Spec (CX-2): a gh login whose token the server refuses says how to add
// scopes; a login gh does not have fails before any request.
func TestManagerGHTokenErrors(t *testing.T) {
	mcptest.FakeGH(t, []string{"amin"}, "amin")
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Bearer: mcptest.GHToken("someone-else")})
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{
		"gh":  map[string]any{"type": "http", "url": srv.MCPURL(), "auth": map[string]string{"gh": "amin"}},
		"gh2": map[string]any{"type": "http", "url": srv.MCPURL(), "auth": map[string]string{"gh": "bob"}},
	})
	m := e.loggedManager(t, &memTokens{}, &logRec{}, nil)
	_, err := m.Call(context.Background(), nil, "gh", "echo", map[string]any{"text": "x"})
	if err == nil || !strings.Contains(err.Error(), "refused the gh token of amin") {
		t.Fatalf("refused: err = %v", err)
	}
	if strings.Contains(err.Error(), mcptest.GHToken("amin")) {
		t.Fatalf("the gh token is in the error: %v", err)
	}
	before := len(srv.Requests())
	_, err = m.Call(context.Background(), nil, "gh2", "echo", map[string]any{"text": "x"})
	if err == nil || !strings.Contains(err.Error(), "gh (bob)") {
		t.Fatalf("unknown login: err = %v", err)
	}
	if len(srv.Requests()) != before {
		t.Fatal("a request went out without a token")
	}
}

// Spec (MC-6): only a trusted remote server with a place for the tokens
// can start a sign-in.
func TestBeginSignInRefusals(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{
		"local":  map[string]string{},
		"broken": map[string]any{"type": "http", "url": "http://remote.example/mcp"},
		"remote": map[string]any{"type": "http", "url": "https://mcp.example/mcp"},
	})
	m := e.loggedManager(t, &memTokens{}, &logRec{}, nil)
	for name, want := range map[string]string{"nope": "no MCP server", "local": "only remote", "broken": "insecure"} {
		if _, err := m.BeginSignIn(context.Background(), name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", name, err, want)
		}
	}
	noStore := e.loggedManager(t, nil, &logRec{}, nil)
	if _, err := noStore.BeginSignIn(context.Background(), "remote"); err == nil || !strings.Contains(err.Error(), "no place to keep") {
		t.Fatalf("no token store: err = %v", err)
	}
	if err := noStore.FinishSignIn("remote", ai.MCPOAuth{}); err == nil || !strings.Contains(err.Error(), "no place to keep") {
		t.Fatalf("finish without a store: err = %v", err)
	}
}

// failTokens is a TokenStore whose saves fail.
type failTokens struct{ memTokens }

func (*failTokens) Save(string, ai.MCPOAuth) error {
	return errors.New("credentials.json is read-only")
}

// Spec (MC-6): a sign-in that can not be stored is an error and the
// server still asks for a sign-in.
func TestFinishSignInSaveFails(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"remote": map[string]any{"type": "http", "url": "https://mcp.example/mcp"}})
	m := e.loggedManager(t, &failTokens{}, &logRec{}, nil)
	if err := m.FinishSignIn("remote", ai.MCPOAuth{AccessToken: remoteCanary}); err == nil || strings.Contains(err.Error(), remoteCanary) {
		t.Fatalf("err = %v", err)
	}
}

// Spec (MC-2): Trust with always saves the entry; an unknown name does
// nothing.
func TestManagerTrustAlwaysSaves(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.project, map[string]any{"p": map[string]string{}})
	tr := &trustRec{}
	m := e.loggedManager(t, nil, &logRec{}, tr)
	m.Trust("nope", true)
	if len(tr.saved) != 0 {
		t.Fatalf("saved %v", tr.saved)
	}
	m.Trust("p", true)
	if len(tr.saved) != 1 || !status(m, "p").Trusted {
		t.Fatalf("saved %v, status %+v", tr.saved, status(m, "p"))
	}
}

// Spec (MC-6): a damaged credentials file is never overwritten by a
// sign-in.
func TestCredStoreDamagedFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(p, []byte("{damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (CredStore{Path: p}).Save("x", ai.MCPOAuth{AccessToken: remoteCanary}); err == nil {
		t.Fatal("Save over a damaged file worked")
	}
	if b, _ := os.ReadFile(p); string(b) != "{damaged" {
		t.Fatal("the damaged file was overwritten")
	}
}
