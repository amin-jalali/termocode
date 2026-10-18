package app

import (
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

func TestPlainSecretStoreTracksConfigDirectory(t *testing.T) {
	first := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", first)
	configurePlainSecretStore()
	firstPath := filepath.Join(first, "termocode", "ai", "credentials.json")
	if err := ai.SaveCredentials(firstPath, ai.Credentials{MCPOAuth: map[string]ai.MCPOAuth{
		"github": {Resource: "https://api.githubcopilot.com/mcp/", AccessToken: "stored-token"},
	}}); err != nil {
		t.Fatal(err)
	}

	second := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", second)
	configurePlainSecretStore()
	secondPath := filepath.Join(second, "termocode", "ai", "credentials.json")
	got, err := ai.LoadCredentials(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.MCPOAuth) != 0 {
		t.Fatalf("credentials from previous config directory leaked: %+v", got.MCPOAuth)
	}
	t.Cleanup(func() { ai.SetSecretStore(nil) })
}
