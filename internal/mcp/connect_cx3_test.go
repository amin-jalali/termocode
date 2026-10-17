package mcp

import (
	"context"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// Spec (CX-3): Sign out revokes the tokens when the authorization server
// has a revocation endpoint, and forgets them in any case.
func TestSignOutRevokes(t *testing.T) {
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{Revocation: true})
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid, AuthServer: as.Issuer()})
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"lin": map[string]any{"type": "http", "url": srv.MCPURL()}})
	tk := &memTokens{}
	o := &oauthEnv{managerEnv: e, as: as, srv: srv, tokens: tk, m: e.httpManager(t, nil, tk, 0)}
	o.signIn(t)
	tok, _ := tk.Load("lin")
	o.m.SignOut(context.Background(), "lin")
	if _, ok := tk.Load("lin"); ok {
		t.Error("tokens kept")
	}
	if r := as.Revoked(); len(r) != 2 || as.Valid(tok.AccessToken) {
		t.Errorf("revoked %v", r)
	}
	// A failing revoke still signs out (and is only logged).
	_ = tk.Save("lin", ai.MCPOAuth{Resource: srv.MCPURL(), AccessToken: "at-x-12345678", RevocationEndpoint: as.Issuer() + "/missing"})
	o.m.SignOut(context.Background(), "lin")
	if _, ok := tk.Load("lin"); ok {
		t.Error("tokens kept after a failed revoke")
	}
}

// Spec: a sign-in made for https://host/mcp/ also serves the read-only
// endpoint (/mcp/readonly) and back: "Allow writes" keeps the token.
func TestBoundToReadOnlySwitch(t *testing.T) {
	e := ai.MCPOAuth{Resource: "https://api.githubcopilot.com/mcp/"}
	for _, u := range []string{"https://api.githubcopilot.com/mcp/", "https://api.githubcopilot.com/mcp/readonly", "https://api.githubcopilot.com/mcp"} {
		if !boundTo(e, u) {
			t.Errorf("not bound to %s", u)
		}
	}
	if boundTo(e, "https://evil.example/mcp/") {
		t.Error("bound to another host")
	}
	l := ai.MCPOAuth{Resource: "https://mcp.linear.app/mcp"}
	if !boundTo(l, "https://mcp.linear.app/mcp/readonly") {
		t.Error("linear readonly")
	}
}
