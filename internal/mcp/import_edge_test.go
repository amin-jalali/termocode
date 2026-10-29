package mcp

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (CX-6): a pasted block that is not usable says why; entries that
// are not objects are skipped with their name.
func TestParseImportRefusals(t *testing.T) {
	if _, _, err := ParseImport(`{"mcpServers": [1, 2]}`); err == nil || !strings.Contains(err.Error(), "mcpServers is not an object") {
		t.Fatalf("array: err = %v", err)
	}
	if _, _, err := ParseImport(`{"mcpServers": {}}`); err == nil || !strings.Contains(err.Error(), "no servers") {
		t.Fatalf("empty: err = %v", err)
	}
	out, problems, err := ParseImport(`{"mcpServers": {"bad": 7, "good": {"command": "npx"}}}`)
	if err != nil || len(out) != 1 || out[0].Name != "good" || len(problems) != 1 || problems[0] != "bad: not an object" {
		t.Fatalf("out %+v problems %v err %v", out, problems, err)
	}
}

// Spec (CX-6): a literal OAuth client secret moves to the secret store;
// odd characters in a header name become "_" in the secret's name.
func TestParseImportMovesClientSecret(t *testing.T) {
	secret := "cs_" + "canary" + "_0123456789abcdef"
	out, _, err := ParseImport(`"svc": {"url": "https://svc.example/mcp", "oauth": {"client_id": "app", "client_secret": "` + secret + `"},
		"headers": {"X.Api Key": "` + "sk-" + "canary0123456789abcdef" + `"}}`)
	if err != nil || len(out) != 1 {
		t.Fatalf("out %+v err %v", out, err)
	}
	im := out[0]
	if im.Secrets["svc_client_secret"] != secret || im.Entry["oauth"].(map[string]any)["client_secret"] != "${secret:svc_client_secret}" {
		t.Fatalf("client secret not moved: %+v", im)
	}
	if _, ok := im.Secrets["svc_x_api_key"]; !ok {
		t.Fatalf("header secret name: %v", im.Secrets)
	}
	if got := ImportSummary(nil, []string{"bad: not an object"}); got != "Skipped bad: not an object." {
		t.Fatalf("summary %q", got)
	}
}

// Spec (CX-7): a rename refuses a bad new name and a missing entry; an
// account label kept outside a sign-in moves too.
func TestRenameServerEdgeCases(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mcp.json")
	if err := PutServer(p, "gh-2", map[string]any{"type": "http", "url": "https://gh.example/mcp"}); err != nil {
		t.Fatal(err)
	}
	cs := CredStore{Path: filepath.Join(dir, "ai", "credentials.json")}
	if err := cs.SetAccount("gh-2", "amin"); err != nil {
		t.Fatal(err)
	}
	if err := RenameServer(p, cs, "gh-2", "bad name!"); err == nil || !strings.Contains(err.Error(), "is not a server name") {
		t.Fatalf("bad name: err = %v", err)
	}
	if err := RenameServer(p, cs, "nope", "gh-x"); err == nil || !strings.Contains(err.Error(), "no server named nope") {
		t.Fatalf("missing: err = %v", err)
	}
	if err := RenameServer(p, cs, "gh-2", "gh-amin"); err != nil {
		t.Fatal(err)
	}
	cr, _ := ai.LoadCredentials(cs.Path)
	if cr.MCPAccounts["gh-amin"] != "amin" || cr.MCPAccounts["gh-2"] != "" {
		t.Fatalf("accounts %v", cr.MCPAccounts)
	}
}

// Spec (CX-7): entry names are cut to 40 characters; an account with no
// usable letters gives no name.
func TestEntryNameEdgeCases(t *testing.T) {
	if got := EntryName("github", strings.Repeat("a", 30)+" "+strings.Repeat("b", 30)); len(got) > 40 || !strings.HasPrefix(got, "github-aaaa") || strings.HasSuffix(got, "-") {
		t.Fatalf("long: %q", got)
	}
	if got := EntryName("github", "!!!"); got != "" {
		t.Fatalf("no letters: %q", got)
	}
}
