package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp/auth"
)

// Remote servers (MC-5 / MC-6): Streamable HTTP with static headers
// (a PAT as ${secret:NAME}) or an OAuth sign-in kept in a TokenStore.
//
// Token binding: a stored sign-in is only sent to the URL it was made
// for (its resource) and only refreshed at its own issuer's token
// endpoint. When the server's URL changes, the sign-in is dropped.

// ErrSignInNeeded: a remote server answered 401 and there is no usable
// sign-in.
var ErrSignInNeeded = errors.New("sign in first: AI: MCP Servers… → the server → Sign in")

// TokenStore keeps the OAuth sign-ins by server name.
type TokenStore interface {
	Load(name string) (ai.MCPOAuth, bool)
	Save(name string, e ai.MCPOAuth) error
	Delete(name string) error
}

// CredStore is the TokenStore of credentials.json (mode 0600, folder
// 0700: ai.SaveCredentials). It also sets mcp_secrets.
type CredStore struct{ Path string }

var credMu sync.Mutex

func (c CredStore) update(fn func(*ai.Credentials)) error {
	credMu.Lock()
	defer credMu.Unlock()
	cr, err := ai.LoadCredentials(c.Path)
	if err != nil {
		return err
	}
	fn(&cr)
	return ai.SaveCredentials(c.Path, cr)
}

// Load returns the sign-in of a server.
func (c CredStore) Load(name string) (ai.MCPOAuth, bool) {
	credMu.Lock()
	defer credMu.Unlock()
	cr, _ := ai.LoadCredentials(c.Path)
	e, ok := cr.MCPOAuth[name]
	return e, ok
}

// Save stores a sign-in.
func (c CredStore) Save(name string, e ai.MCPOAuth) error {
	return c.update(func(cr *ai.Credentials) {
		if cr.MCPOAuth == nil {
			cr.MCPOAuth = map[string]ai.MCPOAuth{}
		}
		cr.MCPOAuth[name] = e
	})
}

// Delete forgets a sign-in.
func (c CredStore) Delete(name string) error {
	return c.update(func(cr *ai.Credentials) { delete(cr.MCPOAuth, name) })
}

// SetSecret stores mcp_secrets.NAME (the value of ${secret:NAME}).
func (c CredStore) SetSecret(name, value string) error {
	return c.update(func(cr *ai.Credentials) {
		if cr.MCPSecrets == nil {
			cr.MCPSecrets = map[string]string{}
		}
		cr.MCPSecrets[name] = value
	})
}

// hasAuthHeader: the entry sends its own Authorization (a PAT).
func hasAuthHeader(h map[string]string) bool {
	for k := range h {
		if strings.EqualFold(k, "Authorization") {
			return true
		}
	}
	return false
}

// boundTo reports a sign-in made for this server URL.
func boundTo(e ai.MCPOAuth, serverURL string) bool {
	r := strings.TrimSuffix(e.Resource, "/")
	return r != "" && strings.HasPrefix(strings.TrimSuffix(serverURL, "/"), r)
}

// signIn returns the usable sign-in of a server: one made for another
// URL is dropped.
func (m *Manager) signIn(name, serverURL string) (ai.MCPOAuth, bool) {
	if m.opts.Tokens == nil {
		return ai.MCPOAuth{}, false
	}
	e, ok := m.opts.Tokens.Load(name)
	if !ok {
		return e, false
	}
	if !boundTo(e, serverURL) {
		m.log(name, "the server's URL changed: its sign-in was dropped (sign in again)")
		_ = m.opts.Tokens.Delete(name)
		return ai.MCPOAuth{}, false
	}
	return e, true
}

// accessToken is the bearer for a call, refreshed first when it ends
// within a minute.
func (m *Manager) accessToken(ctx context.Context, name, serverURL string) string {
	e, ok := m.signIn(name, serverURL)
	if !ok {
		return ""
	}
	if e.Expired(time.Now()) && e.RefreshToken != "" {
		if ne, err := auth.Refresh(ctx, m.opts.HTTP, e); err == nil {
			_ = m.opts.Tokens.Save(name, ne)
			m.log(name, "token refreshed")
			return ne.AccessToken
		} else {
			m.log(name, "token refresh failed: %s", ai.Redact(err.Error()))
		}
	}
	return e.AccessToken
}

// refresh runs after a 401 to a request that carried our token.
func (m *Manager) refresh(ctx context.Context, name, serverURL string) bool {
	e, ok := m.signIn(name, serverURL)
	if !ok || e.RefreshToken == "" {
		return false
	}
	ne, err := auth.Refresh(ctx, m.opts.HTTP, e)
	if err != nil {
		m.log(name, "token refresh failed: %s", ai.Redact(err.Error()))
		return false
	}
	_ = m.opts.Tokens.Save(name, ne)
	m.log(name, "token refreshed")
	return true
}

// startHTTP connects a remote server. A 401 becomes "sign-in needed"
// (or "not authorized" for a static token).
func (m *Manager) startHTTP(ctx context.Context, st *serverState, exp Server, opts Options) (*Process, error) {
	name := exp.Name
	static := hasAuthHeader(exp.Headers)
	for _, v := range exp.Headers {
		ai.RegisterSecret(v)
	}
	ho := HTTPOptions{Client: m.opts.HTTP}
	if !static && m.opts.Tokens != nil {
		ho.Token = func(ctx context.Context) string { return m.accessToken(ctx, name, exp.URL) }
		ho.Refresh = func(ctx context.Context) bool { return m.refresh(ctx, name, exp.URL) }
	}
	p, err := StartHTTP(ctx, exp, ho, opts)
	var ae *AuthError
	if errors.As(err, &ae) {
		if static || m.opts.Tokens == nil {
			return nil, fmt.Errorf("not authorized: the server refused the token in \"headers\" (check its scopes): %w", err)
		}
		m.mu.Lock()
		st.state, st.problem = StateSignIn, "sign-in needed"
		st.authMeta, st.authScope = ae.ResourceMetadata, ae.Scope
		m.mu.Unlock()
		m.log(name, "sign-in needed")
		return nil, fmt.Errorf("MCP %s: %w", name, ErrSignInNeeded)
	}
	return p, err
}

// BeginSignIn starts the OAuth sign-in of a remote server: discovery,
// client registration, the loopback listener. The caller opens
// Flow.AuthURL, waits with Flow.Wait (or feeds Flow.Paste) and calls
// FinishSignIn.
func (m *Manager) BeginSignIn(ctx context.Context, name string) (*auth.Flow, error) {
	st, err := m.get(name)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	cfg, trusted := st.cfg, m.trustedLocked(st)
	meta, scope := st.authMeta, st.authScope
	m.mu.Unlock()
	if cfg.Type != TypeHTTP {
		return nil, errors.New("only remote (http) servers sign in")
	}
	if cfg.Problem != "" {
		return nil, errors.New(cfg.Problem)
	}
	if !trusted {
		return nil, ErrNotTrusted
	}
	if m.opts.Tokens == nil {
		return nil, errors.New("no place to keep the sign-in")
	}
	var x Expander
	if m.opts.Expander != nil {
		x = m.opts.Expander()
	}
	exp, err := cfg.Expand(x)
	if err != nil {
		return nil, err
	}
	c := auth.Config{ServerURL: exp.URL, ResourceMetadata: meta, HTTP: m.opts.HTTP, CIMDURL: m.opts.CIMDURL, ClientName: "termocode"}
	if scope != "" {
		c.Scopes = strings.Fields(scope)
	}
	if exp.OAuth != nil {
		c.ClientID, c.ClientSecret = exp.OAuth.ClientID, exp.OAuth.ClientSecret
		if len(exp.OAuth.Scopes) > 0 {
			c.Scopes = exp.OAuth.Scopes
		}
	}
	if m.opts.OAuthPort != nil {
		c.Port = m.opts.OAuthPort()
	}
	m.log(name, "sign-in started")
	fl, err := auth.Begin(ctx, c)
	if err != nil {
		m.log(name, "sign-in failed: %s", ai.Redact(err.Error()))
	}
	return fl, err
}

// FinishSignIn stores a sign-in; the next call (or Connect) uses it.
func (m *Manager) FinishSignIn(name string, e ai.MCPOAuth) error {
	if m.opts.Tokens == nil {
		return errors.New("no place to keep the sign-in")
	}
	if err := m.opts.Tokens.Save(name, e); err != nil {
		return err
	}
	st, err := m.get(name)
	if err == nil {
		m.mu.Lock()
		if st.state == StateSignIn || st.state == StateFailed {
			st.state, st.problem = StateStopped, ""
		}
		m.mu.Unlock()
	}
	m.log(name, "signed in")
	return nil
}

// SignOut forgets a server's sign-in and ends its session.
func (m *Manager) SignOut(name string) {
	if m.opts.Tokens != nil {
		_ = m.opts.Tokens.Delete(name)
	}
	m.Stop(name)
	m.log(name, "signed out")
}
