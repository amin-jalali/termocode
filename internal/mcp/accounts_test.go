package mcp

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (CX-7): a second account's entry is renamed after its sign-in
// ("github-2" → "github-perkino"): the mcp.json entry, its token secret
// reference, the sign-in and the account label all move.
func TestRenameServer(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mcp.json")
	if err := PutServer(p, "sentry-2", map[string]any{"type": "http", "url": "https://mcp.sentry.dev/mcp",
		"headers": map[string]string{"Authorization": "Sentry-Bearer ${secret:sentry-2_token}"}}); err != nil {
		t.Fatal(err)
	}
	cs := CredStore{Path: filepath.Join(dir, "ai", "credentials.json")}
	_ = cs.SetSecret("sentry-2_token", "sntrys_secret_123456")
	_ = cs.Save("sentry-2", ai.MCPOAuth{Resource: "https://mcp.sentry.dev/mcp", AccessToken: "at-123456789"})
	_ = cs.SetAccount("sentry-2", "amin")
	if err := RenameServer(p, cs, "sentry-2", "sentry-amin"); err != nil {
		t.Fatal(err)
	}
	b := mustRead(t, p)
	if strings.Contains(b, "sentry-2") || !strings.Contains(b, "${secret:sentry-amin_token}") {
		t.Errorf("mcp.json %s", b)
	}
	cr, _ := ai.LoadCredentials(cs.Path)
	if cr.MCPSecrets["sentry-amin_token"] != "sntrys_secret_123456" || cr.MCPSecrets["sentry-2_token"] != "" {
		t.Errorf("secrets %v", cr.MCPSecrets)
	}
	if _, ok := cr.MCPOAuth["sentry-amin"]; !ok || cs.Account("sentry-amin") != "amin" {
		t.Errorf("oauth %v account %q", cr.MCPOAuth, cs.Account("sentry-amin"))
	}
	if err := PutServer(p, "taken", map[string]any{"url": "https://x.example/mcp"}); err != nil {
		t.Fatal(err)
	}
	if err := RenameServer(p, cs, "sentry-amin", "taken"); err == nil {
		t.Error("renamed onto an existing entry")
	}
}

// Spec: the token way names its own secret (a second account keeps its
// own token).
func TestEntryForTokenSecret(t *testing.T) {
	p, _ := PresetByName("github")
	e := p.EntryFor(MethodToken, EntryOptions{Secret: "github-2_token"})
	if e["headers"].(map[string]string)["Authorization"] != "Bearer ${secret:github-2_token}" {
		t.Errorf("entry %v", e)
	}
}

// Spec: Status.Expired is a sign-in past its end with no refresh token
// (ClickUp): the row says "sign-in expired".
func TestStatusExpired(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"clickup": map[string]any{"type": "http", "url": "https://mcp.clickup.com/mcp"}})
	tk := &memTokens{}
	_ = tk.Save("clickup", ai.MCPOAuth{Resource: "https://mcp.clickup.com/mcp", AccessToken: "at-1234567890",
		ExpiresAtMs: time.Now().Add(-time.Hour).UnixMilli()})
	m := e.httpManager(t, nil, tk, 0)
	if st := status(m, "clickup"); !st.Expired || !st.SignedIn {
		t.Errorf("status %+v", st)
	}
	_ = tk.Save("clickup", ai.MCPOAuth{Resource: "https://mcp.clickup.com/mcp", AccessToken: "at-1234567890", RefreshToken: "rt-123456789",
		ExpiresAtMs: time.Now().Add(-time.Hour).UnixMilli()})
	if st := status(m, "clickup"); st.Expired {
		t.Error("a refreshable sign-in is not expired")
	}
}
