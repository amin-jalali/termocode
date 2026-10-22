package ai

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	secretstore "github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/secrets/secretstest"
)

// useStore points LoadCredentials / SaveCredentials at s for this test.
func useStore(t *testing.T, s secretstore.Store) {
	t.Helper()
	SetSecretStore(s)
	t.Cleanup(func() { SetSecretStore(nil) })
}

// SS-3: an encrypted store has no credentials.json to list its names;
// the view is built from the store's index, and every kind round-trips.
func TestCredentialsThroughStoreWithoutPlainFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai", "credentials.json")
	mem := secretstest.NewMemory()
	useStore(t, secretstest.New(t, mem))
	want := Credentials{
		Keys:        map[string]string{"openai": "sk-age-only-1234567", "anthropic": "sk-ant-age-only-7654321"},
		ClaudeOAuth: &OAuthTokens{AccessToken: "claude-access-age-1", RefreshToken: "claude-refresh-age-1", ExpiresAtMs: 42},
		MCPSecrets:  map[string]string{"GITHUB_PAT": "ghp_age_only_value"},
		MCPOAuth:    map[string]MCPOAuth{"linear": {Issuer: "https://linear.app", AccessToken: "lin-access-age", Account: "amin"}},
		MCPAccounts: map[string]string{"github": "amin-jalali"},
	}
	if err := SaveCredentials(path, want); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a non-plain store wrote credentials.json: %v", err)
	}
	got, err := LoadCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Keys["openai"] != want.Keys["openai"] || got.Keys["anthropic"] != want.Keys["anthropic"] {
		t.Errorf("keys = %v", got.Keys)
	}
	if got.ClaudeOAuth == nil || got.ClaudeOAuth.RefreshToken != "claude-refresh-age-1" || got.ClaudeOAuth.ExpiresAtMs != 42 {
		t.Errorf("claude oauth = %+v", got.ClaudeOAuth)
	}
	if got.MCPSecrets["GITHUB_PAT"] != "ghp_age_only_value" {
		t.Errorf("mcp secrets = %v", got.MCPSecrets)
	}
	if o := got.MCPOAuth["linear"]; o.AccessToken != "lin-access-age" || o.Account != "amin" {
		t.Errorf("mcp oauth = %+v", got.MCPOAuth)
	}
	if got.MCPAccounts["github"] != "amin-jalali" {
		t.Errorf("mcp accounts = %v", got.MCPAccounts)
	}
	if got.Locked() {
		t.Error("an unlocked store gave a locked view")
	}
	// Removing from the view removes from the store.
	delete(got.Keys, "openai")
	got.ClaudeOAuth = nil
	delete(got.MCPAccounts, "github")
	if err := SaveCredentials(path, got); err != nil {
		t.Fatal(err)
	}
	vals := mem.Values()
	for _, n := range []string{"ai.key.openai", "ai.claude_oauth", "mcp.account.github"} {
		if _, ok := vals[n]; ok {
			t.Errorf("%s still stored", n)
		}
	}
	if vals["ai.key.anthropic"] != "sk-ant-age-only-7654321" {
		t.Errorf("kept key lost: %v", vals)
	}
}

// A locked store gives an empty, locked view with no error (features say
// "not configured"), and saving that view is refused: it must never
// delete the secrets it could not see.
func TestCredentialsLockedStoreNeverDeletes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai", "credentials.json")
	mem := secretstest.NewLockedMemory("correct horse battery")
	s := secretstest.New(t, mem)
	useStore(t, s)
	if err := s.Unlock("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredentials(path, Credentials{Keys: map[string]string{"openai": "sk-locked-test-123456"}}); err != nil {
		t.Fatal(err)
	}
	s.Lock()
	c, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("locked load: %v", err)
	}
	if !c.Locked() || len(c.Keys) != 0 {
		t.Fatalf("locked view = %+v locked=%v", c, c.Locked())
	}
	if err := s.Unlock("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	// Unlocked between load and save: the stale view still may not save.
	c.Keys = map[string]string{"anthropic": "sk-ant-new-1234567"}
	if err := SaveCredentials(path, c); !errors.Is(err, secretstore.ErrLocked) {
		t.Fatalf("save of a locked view: %v, want ErrLocked", err)
	}
	if v := mem.Values()["ai.key.openai"]; v != "sk-locked-test-123456" {
		t.Fatalf("stored key lost: %q", v)
	}
}

// SS-2 lost new MCP account labels with the plain store: they go
// through the store like the rest and land in mcp_accounts.
func TestCredentialsMCPAccountsThroughPlainStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai", "credentials.json")
	store, err := secretstore.Open(secretstore.Options{Dir: filepath.Dir(filepath.Dir(path)), PlainPath: path, Backend: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	useStore(t, store)
	if err := SaveCredentials(path, Credentials{MCPAccounts: map[string]string{"github": "amin"}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"mcp_accounts"`) || !strings.Contains(string(b), `"amin"`) {
		t.Fatalf("account not saved: %s", b)
	}
	c, err := LoadCredentials(path)
	if err != nil || c.MCPAccounts["github"] != "amin" {
		t.Fatalf("load = %+v %v", c.MCPAccounts, err)
	}
	c.MCPAccounts = map[string]string{}
	if err := SaveCredentials(path, c); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), `"amin"`) {
		t.Fatalf("account not removed: %s", b)
	}
}

// SS-5: a reference that does not resolve right now (a failing command,
// an empty variable) is missing from the loaded view; saving another key
// must not delete it.
func TestSaveCredentialsKeepsUnresolvedReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai", "credentials.json")
	store := secretstest.New(t, nil)
	useStore(t, store)
	if err := store.Set("ai.key.openai", "${env:SS5_NOT_SET}"); err != nil {
		t.Fatal(err)
	}
	c, err := LoadCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Keys["openai"] != "" {
		t.Fatalf("reference resolved unexpectedly: %v", c.Keys)
	}
	if c.Keys == nil {
		c.Keys = map[string]string{}
	}
	c.Keys["anthropic"] = "sk-ant-ss5-keep-1234567"
	if err := SaveCredentials(path, c); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range store.Names() {
		found = found || n == "ai.key.openai"
	}
	if !found {
		t.Fatalf("the unresolved reference was deleted: %v", store.Names())
	}
}
