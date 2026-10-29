package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/secrets/secretstest"
)

func TestOAuthTokenEndpointErrors(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"http error":      {400, `{"error":{"message":"invalid_grant"}}`, "invalid_grant"},
		"not json":        {200, `<html>`, "no access token"},
		"no access token": {200, `{"refresh_token":"r"}`, "no access token"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				fmt.Fprint(w, c.body)
			}))
			defer srv.Close()
			_, err := OAuthClient{TokenURL: srv.URL}.Exchange(ctx, "code", "st", "ver")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
		})
	}
	if _, err := (OAuthClient{TokenURL: deadURL(t)}).Refresh(ctx, "ref"); err == nil {
		t.Error("no connection must fail")
	}
	if _, err := (OAuthClient{TokenURL: "http://bad host"}).Refresh(ctx, "ref"); err == nil {
		t.Error("a bad token URL must fail")
	}
}

// A token answer without expires_in gets the default lifetime, so it is
// not taken as expired at once.
func TestOAuthDefaultLifetime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"access_token":"acc-nolife-12345678"}`)
	}))
	defer srv.Close()
	now := time.Unix(2_000_000, 0)
	tok, err := OAuthClient{TokenURL: srv.URL, Now: func() time.Time { return now }}.Exchange(context.Background(), "c", "s", "v")
	if err != nil {
		t.Fatal(err)
	}
	if tok.Expired(now) || tok.ExpiresAtMs != now.Add(defaultOAuthLifetime*time.Second).UnixMilli() {
		t.Errorf("expires at %d", tok.ExpiresAtMs)
	}
}

func TestTokenSourceStates(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	save := func(tok *OAuthTokens) string {
		p := filepath.Join(dir, fmt.Sprintf("c%d.json", time.Now().UnixNano()))
		if err := SaveCredentials(p, Credentials{ClaudeOAuth: tok}); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, err := (&TokenSource{Path: save(nil)}).Token(ctx); err == nil || !strings.Contains(err.Error(), "not signed in") {
		t.Errorf("signed out: %v", err)
	}
	expired := &OAuthTokens{AccessToken: "acc-old-12345678", ExpiresAtMs: 1}
	if _, err := (&TokenSource{Path: save(expired)}).Token(ctx); err == nil || !strings.Contains(err.Error(), "sign in again") {
		t.Errorf("expired without refresh token: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"message":"refresh token revoked"}}`)
	}))
	defer srv.Close()
	revoked := &OAuthTokens{AccessToken: "acc-old-12345678", RefreshToken: "ref-old-12345678", ExpiresAtMs: 1}
	_, err := (&TokenSource{Path: save(revoked), Client: OAuthClient{TokenURL: srv.URL}}).Token(ctx)
	if err == nil || !strings.Contains(err.Error(), "refresh failed") || !strings.Contains(err.Error(), "revoked") {
		t.Errorf("revoked: %v", err)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&TokenSource{Path: bad}).Token(ctx); err == nil {
		t.Error("corrupt credentials must fail")
	}
}

// A refreshed token that cannot be saved is an error the user sees, not
// a silent success that asks again on the next start.
func TestTokenSourceSaveError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"access_token":"acc-new-12345678","expires_in":3600}`)
	}))
	defer srv.Close()
	base := secretstest.New(t, secretstest.NewMemory())
	if err := base.Set("ai.claude_oauth", `{"refresh_token":"ref-12345678"}`); err != nil {
		t.Fatal(err)
	}
	disk := errors.New("disk full")
	useStore(t, failStore{Store: base, setErr: disk})
	path := filepath.Join(t.TempDir(), "credentials.json")
	if _, err := (&TokenSource{Path: path, Client: OAuthClient{TokenURL: srv.URL}}).Token(context.Background()); !errors.Is(err, disk) {
		t.Errorf("err = %v", err)
	}
}

func TestListModelsClaudeSignIn(t *testing.T) {
	ctx := context.Background()
	if _, err := ListModels(ctx, Config{Kind: KindClaudeOAuth}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("no token source: %v", err)
	}
	empty := Config{Kind: KindClaudeOAuth, Token: func(context.Context) (string, error) { return "", nil }}
	if _, err := ListModels(ctx, empty); err == nil || !strings.Contains(err.Error(), "not signed in") {
		t.Errorf("empty token: %v", err)
	}
	failing := Config{Kind: KindClaudeOAuth, Token: func(context.Context) (string, error) { return "", errors.New("expired") }}
	if _, err := ListModels(ctx, failing); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("token error: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer oauth-models-tok-1" || r.Header.Get("anthropic-beta") == "" || r.Header.Get("x-api-key") != "" {
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"claude-opus-5-5"}]}`)
	}))
	defer srv.Close()
	ok := Config{Kind: KindClaudeOAuth, BaseURL: srv.URL, Token: func(context.Context) (string, error) { return "oauth-models-tok-1", nil }}
	ms, err := ListModels(ctx, ok)
	if err != nil || len(ms) != 1 || ms[0].ID != "claude-opus-5-5" {
		t.Errorf("models = %v, %v", ms, err)
	}
}

func TestListModelsBadAnswers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data": [`)
	}))
	defer srv.Close()
	ctx := context.Background()
	if _, err := ListModels(ctx, Config{Kind: KindOpenAI, BaseURL: srv.URL, APIKey: "sk-x-1234567890"}); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("cut JSON: %v", err)
	}
	if _, err := ListModels(ctx, Config{Kind: KindOpenAI, BaseURL: "http://bad host", APIKey: "sk-x-1234567890"}); err == nil {
		t.Error("bad URL must fail")
	}
	// Ollama: /api/tags fails and /models fails too: the first error is shown.
	notFound := httptest.NewServer(http.NotFoundHandler())
	defer notFound.Close()
	if _, err := ListModels(ctx, Config{Kind: KindOllama, BaseURL: notFound.URL + "/v1"}); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("ollama both fail: %v", err)
	}
}
