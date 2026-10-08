package ai

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Claude OAuth sign-in (opt-in) — port of mobocode's claude_oauth.dart.
// PKCE authorization-code flow with the "paste the code" redirect: the
// browser shows a code (`code#state`) that the user pastes back into
// termocode. Tokens live in ai/credentials.json (0600).
//
// This uses the Claude Code OAuth client, so it bills the user's Claude
// subscription. Anthropic's terms may not allow third-party apps to do
// that — the UI shows a ToS note and recommends an API key instead.

const (
	ClaudeOAuthClientID  = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	ClaudeAuthorizeURL   = "https://claude.ai/oauth/authorize"
	ClaudeTokenURL       = "https://platform.claude.com/v1/oauth/token"
	ClaudeCallbackURL    = "https://platform.claude.com/oauth/code/callback"
	claudeOAuthScopes    = "org:create_api_key user:profile user:inference"
	oauthExpirySkew      = 60 * time.Second
	defaultOAuthLifetime = 3600
)

// ClaudeOAuthToSNote is shown before sign-in.
const ClaudeOAuthToSNote = "This signs in with the Claude Code OAuth client and uses your Claude " +
	"subscription. Anthropic's terms may not allow that for third-party apps — an " +
	"Anthropic API key is the supported option. Continue at your own risk?"

// PKCE is one pending sign-in.
type PKCE struct {
	Verifier  string
	Challenge string
	State     string
}

func randomURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// NewPKCE creates a verifier, its S256 challenge and a state value.
func NewPKCE() (PKCE, error) {
	v, err := randomURLSafe(64)
	if err != nil {
		return PKCE{}, err
	}
	st, err := randomURLSafe(32)
	if err != nil {
		return PKCE{}, err
	}
	return PKCE{Verifier: v, Challenge: S256Challenge(v), State: st}, nil
}

// S256Challenge is base64url(sha256(verifier)) without padding.
func S256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// AuthorizeURL is the browser URL that starts the sign-in.
func (p PKCE) AuthorizeURL() string {
	q := url.Values{}
	q.Set("code", "true")
	q.Set("client_id", ClaudeOAuthClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", ClaudeCallbackURL)
	q.Set("scope", claudeOAuthScopes)
	q.Set("code_challenge", p.Challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", p.State)
	return ClaudeAuthorizeURL + "?" + q.Encode()
}

// ParseCallback accepts what the user pastes: a full callback URL with
// ?code=&state=, the "code#state" form, or a bare code.
func ParseCallback(input string) (code, state string, ok bool) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", "", false
	}
	if u, err := url.Parse(s); err == nil && u.RawQuery != "" {
		if c := u.Query().Get("code"); c != "" {
			return c, u.Query().Get("state"), true
		}
	}
	if parts := strings.Split(s, "#"); len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1], true
	}
	if !strings.ContainsAny(s, " \t\n") {
		return s, "", true
	}
	return "", "", false
}

// OAuthClient talks to the token endpoint.
type OAuthClient struct {
	HTTP     *http.Client
	TokenURL string
	Now      func() time.Time
}

func (c OAuthClient) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c OAuthClient) post(ctx context.Context, body map[string]any, prevRefresh string) (OAuthTokens, error) {
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	u := c.TokenURL
	if u == "" {
		u = ClaudeTokenURL
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return OAuthTokens{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return OAuthTokens{}, errors.New(Redact(err.Error()))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OAuthTokens{}, httpError(resp.StatusCode, raw)
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.AccessToken == "" {
		return OAuthTokens{}, errors.New("token endpoint returned no access token")
	}
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = defaultOAuthLifetime
	}
	if out.RefreshToken == "" {
		out.RefreshToken = prevRefresh
	}
	RegisterSecret(out.AccessToken)
	RegisterSecret(out.RefreshToken)
	return OAuthTokens{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		ExpiresAtMs:  c.now().Add(time.Duration(out.ExpiresIn) * time.Second).UnixMilli(),
	}, nil
}

// Exchange trades an authorization code for tokens.
func (c OAuthClient) Exchange(ctx context.Context, code, state, verifier string) (OAuthTokens, error) {
	return c.post(ctx, map[string]any{
		"grant_type":    "authorization_code",
		"code":          code,
		"state":         state,
		"client_id":     ClaudeOAuthClientID,
		"redirect_uri":  ClaudeCallbackURL,
		"code_verifier": verifier,
	}, "")
}

// Refresh trades a refresh token for a new access token.
func (c OAuthClient) Refresh(ctx context.Context, refresh string) (OAuthTokens, error) {
	return c.post(ctx, map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": refresh,
		"client_id":     ClaudeOAuthClientID,
	}, refresh)
}

// Expired reports whether t needs a refresh (60 s skew).
func (t OAuthTokens) Expired(now time.Time) bool {
	return now.Add(oauthExpirySkew).UnixMilli() >= t.ExpiresAtMs
}

// TokenSource hands out a valid access token, refreshing (and persisting)
// it on demand. Safe for concurrent use; one refresh at a time.
type TokenSource struct {
	Path   string // credentials.json
	Client OAuthClient
	mu     sync.Mutex
}

// Token returns a non-expired access token.
func (s *TokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	creds, err := LoadCredentials(s.Path)
	if err != nil {
		return "", err
	}
	t := creds.ClaudeOAuth
	if t == nil || (t.AccessToken == "" && t.RefreshToken == "") {
		return "", errors.New("not signed in to Claude — run AI: Sign in with Claude")
	}
	if t.AccessToken != "" && !t.Expired(s.Client.now()) {
		return t.AccessToken, nil
	}
	if t.RefreshToken == "" {
		return "", errors.New("Claude session expired — sign in again")
	}
	nt, err := s.Client.Refresh(ctx, t.RefreshToken)
	if err != nil {
		return "", errors.New("Claude session refresh failed: " + err.Error())
	}
	creds.ClaudeOAuth = &nt
	if err := SaveCredentials(s.Path, creds); err != nil {
		return "", err
	}
	return nt.AccessToken, nil
}
