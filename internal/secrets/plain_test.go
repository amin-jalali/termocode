package secrets

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fixture is the old ai/credentials.json shape, all four secret kinds
// plus the non-secret mcp_accounts.
const fixture = `{
  "keys": {"anthropic": "sk-ant-FIXTURE"},
  "claude_oauth": {"access_token": "at-FIXTURE", "refresh_token": "rt-FIXTURE", "expires_at_ms": 42},
  "mcp_secrets": {"GITHUB_PAT": "ghp_FIXTURE"},
  "mcp_oauth": {"linear": {"issuer": "https://linear.app", "access_token": "lin-FIXTURE"}},
  "mcp_accounts": {"github": "amin"}
}`

func writeFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ai", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPlainReadsOldShape(t *testing.T) {
	p := newPlain(writeFixture(t))
	want := map[string]string{
		"ai.key.anthropic":      "sk-ant-FIXTURE",
		"mcp.secret.GITHUB_PAT": "ghp_FIXTURE",
	}
	for n, w := range want {
		v, ok, err := p.Get(n)
		if err != nil || !ok || v != w {
			t.Errorf("Get(%s) = %q %v %v, want %q", n, v, ok, err, w)
		}
	}
	// Blobs come back as JSON the callers decode into their structs.
	v, ok, err := p.Get("ai.claude_oauth")
	if err != nil || !ok {
		t.Fatalf("claude_oauth: %v %v", ok, err)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresAtMs int64  `json:"expires_at_ms"`
	}
	if err := json.Unmarshal([]byte(v), &tok); err != nil || tok.AccessToken != "at-FIXTURE" || tok.ExpiresAtMs != 42 {
		t.Fatalf("claude_oauth blob = %q (%v)", v, err)
	}
	v, ok, _ = p.Get("mcp.oauth.linear")
	if !ok || !json.Valid([]byte(v)) {
		t.Fatalf("mcp.oauth.linear = %q %v", v, ok)
	}
	if _, ok, _ := p.Get("ai.key.openai"); ok {
		t.Fatal("missing key reported as present")
	}
}

func TestPlainMissingFile(t *testing.T) {
	p := newPlain(filepath.Join(t.TempDir(), "ai", "credentials.json"))
	if _, ok, err := p.Get("ai.key.x"); ok || err != nil {
		t.Fatalf("missing file: %v %v", ok, err)
	}
	if err := p.Delete("ai.key.x"); err != nil {
		t.Fatalf("delete on a missing file: %v", err)
	}
}

func TestPlainWritesKeepOtherFields(t *testing.T) {
	path := writeFixture(t)
	p := newPlain(path)
	if err := p.Set("ai.key.openai", "sk-NEW"); err != nil {
		t.Fatal(err)
	}
	if err := p.Set("http.secret.TOKEN", "tok-NEW"); err != nil {
		t.Fatal(err)
	}
	if err := p.Set("mcp.oauth.slack", `{"access_token":"xoxb-NEW"}`); err != nil {
		t.Fatal(err)
	}
	if err := p.Delete("mcp.secret.GITHUB_PAT"); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, path); m != 0o600 {
		t.Errorf("mode = %o, want 600", m)
	}
	noTemps(t, filepath.Dir(path))

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(readFile(t, path)), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["mcp_accounts"]) == "" {
		t.Fatal("mcp_accounts (not ours) was dropped")
	}
	if _, ok := raw["mcp_secrets"]; ok {
		t.Fatal("an empty mcp_secrets map must be dropped (omitempty, as before)")
	}
	var keys map[string]string
	_ = json.Unmarshal(raw["keys"], &keys)
	if keys["openai"] != "sk-NEW" || keys["anthropic"] != "sk-ant-FIXTURE" {
		t.Fatalf("keys = %v", keys)
	}
	var other map[string]string
	_ = json.Unmarshal(raw["secrets"], &other)
	if other["http.secret.TOKEN"] != "tok-NEW" {
		t.Fatalf("names without an old place go to secrets: %v", other)
	}
	v, ok, _ := p.Get("mcp.oauth.slack")
	if !ok || v != `{"access_token":"xoxb-NEW"}` {
		t.Fatalf("blob round trip = %q", v)
	}
}

func TestPlainBlobMustBeJSONObject(t *testing.T) {
	p := newPlain(filepath.Join(t.TempDir(), "credentials.json"))
	for _, bad := range []string{"not json", `"a string"`, "[1]"} {
		if err := p.Set("ai.claude_oauth", bad); err == nil {
			t.Errorf("Set(ai.claude_oauth, %q) must fail", bad)
		}
	}
}

func TestPlainCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := newPlain(path)
	if _, _, err := p.Get("ai.key.x"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Get err = %v, want ErrCorrupt", err)
	}
	if err := p.Set("ai.key.x", "v"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Set err = %v, want ErrCorrupt", err)
	}
	if got := readFile(t, path); got != "{oops" {
		t.Fatal("a corrupt file must not be overwritten")
	}
}

// mcp.account.<server> is the old mcp_accounts.<server> (SS-3: the
// Credentials view saves account labels through the store).
func TestPlainMCPAccountSlot(t *testing.T) {
	path := writeFixture(t)
	p := newPlain(path)
	if v, ok, err := p.Get("mcp.account.github"); !ok || err != nil || v != "amin" {
		t.Fatalf("Get = %q %v %v", v, ok, err)
	}
	if err := p.Set("mcp.account.linear", "amin-jalali"); err != nil {
		t.Fatal(err)
	}
	if err := p.Delete("mcp.account.github"); err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Accounts map[string]string `json:"mcp_accounts"`
		Secrets  map[string]string `json:"secrets"`
	}
	if err := json.Unmarshal([]byte(readFile(t, path)), &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Accounts["linear"] != "amin-jalali" || raw.Accounts["github"] != "" || len(raw.Secrets) != 0 {
		t.Fatalf("file = %+v", raw)
	}
}
