package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// browseAndFinish plays the browser for a sign-in of name and stores it.
func browseAndFinish(t *testing.T, m *Manager, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fl, err := m.BeginSignIn(ctx, name)
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
	if err := m.FinishSignIn(name, tok); err != nil {
		t.Fatal(err)
	}
}

// Spec (AU-10): an entry's oauth.metadata_url signs in at that
// authorization server when the server names none.
func TestSignInWithMetadataURL(t *testing.T) {
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{})
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid})
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"lin": map[string]any{"type": "http", "url": srv.MCPURL(),
		"oauth": map[string]any{"metadata_url": as.URL + "/.well-known/oauth-authorization-server"}}})
	tk := &memTokens{}
	m := e.httpManager(t, nil, tk, 0)
	browseAndFinish(t, m, "lin")
	if r, err := m.Call(context.Background(), nil, "lin", "echo", map[string]any{"text": "ok"}); err != nil || r.Text() != "echo: ok" {
		t.Fatalf("call %q %v", r.Text(), err)
	}
	if e, _ := tk.Load("lin"); e.Issuer != as.Issuer() {
		t.Errorf("issuer %q", e.Issuer)
	}
}

// Spec (AU-11): a 403 insufficient_scope asks for a new sign-in with the
// scopes the server named added; after it the call works.
func TestStepUpOnInsufficientScope(t *testing.T) {
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{})
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid, AuthServer: as.Issuer(), Scopes: []string{"read"},
		StepUp: func(tok, tool string) string {
			if tool == "write_note" && !strings.Contains(as.ScopeOf(tok), "write") {
				return "read write"
			}
			return ""
		}})
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"lin": map[string]any{"type": "http", "url": srv.MCPURL()}})
	m := e.httpManager(t, nil, &memTokens{}, 0)
	_ = m.Connect(context.Background(), nil, "lin") // 401: learns the metadata
	browseAndFinish(t, m, "lin")
	if _, err := m.Call(context.Background(), nil, "lin", "echo", map[string]any{"text": "a"}); err != nil {
		t.Fatal(err)
	}
	_, err := m.Call(context.Background(), nil, "lin", "write_note", map[string]any{"title": "t"})
	if !errors.Is(err, ErrMoreAccess) {
		t.Fatalf("403: %v", err)
	}
	if st := status(m, "lin"); st.State != StateSignIn || !strings.Contains(st.Problem, "write") {
		t.Errorf("status %+v", st)
	}
	browseAndFinish(t, m, "lin")
	if got := as.Authorizes[len(as.Authorizes)-1].Get("scope"); got != "read write" {
		t.Errorf("asked scope %q", got)
	}
	if r, err := m.Call(context.Background(), nil, "lin", "write_note", map[string]any{"title": "t"}); err != nil || r.Text() != "wrote t" {
		t.Fatalf("after step-up %q %v", r.Text(), err)
	}
}

// Spec (AU-7): a sign-in belongs to its issuer. When the server now
// names another authorization server, the old sign-in is dropped (never
// refreshed at the old one) and a new sign-in is needed.
func TestSignInDroppedWhenIssuerChanges(t *testing.T) {
	o := newOAuthEnv(t, mcptest.HTTPOptions{})
	var oldHits int
	var mu sync.Mutex
	old := newRawServer(t, func(w http.ResponseWriter, r *http.Request, body string) {
		mu.Lock()
		oldHits++
		mu.Unlock()
		http.Error(w, "no", http.StatusBadRequest)
	})
	_ = o.tokens.Save("lin", ai.MCPOAuth{Issuer: old.srv.URL, TokenEndpoint: old.srv.URL + "/token", ClientID: "c",
		Resource: o.srv.MCPURL(), AccessToken: "stale-" + "token", RefreshToken: "rt-" + "old"})
	_, err := o.m.Call(context.Background(), nil, "lin", "echo", map[string]any{"text": "a"})
	if !errors.Is(err, ErrSignInNeeded) {
		t.Fatalf("call: %v", err)
	}
	if _, ok := o.tokens.Load("lin"); ok {
		t.Error("the old sign-in is kept")
	}
	mu.Lock()
	defer mu.Unlock()
	if oldHits != 0 {
		t.Errorf("the old issuer got %d requests", oldHits)
	}
}
