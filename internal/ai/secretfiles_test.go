package ai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretFile(t *testing.T) {
	for _, p := range []string{"http-client.private.env.json", "api/http-client.private.env.json",
		".git-credentials", ".netrc", ".pgpass", "keys/id_rsa", "id_ed25519", "id_ecdsa_work"} {
		if !SecretFile(p) {
			t.Errorf("SecretFile(%q) = false", p)
		}
	}
	// .env is read on purpose (values redacted): the services flow.
	for _, p := range []string{"http-client.env.json", ".env", ".env.local", ".env.example",
		"id_rsa.pub", "id_ecdsa_work.pub", "token.go", "password_reset.go", "main.go", "envs/dev.json"} {
		if SecretFile(p) {
			t.Errorf("SecretFile(%q) = true", p)
		}
	}
}

// Spec (SCN-2): the AI can not read a secret value from the workspace:
// read_file refuses the user's secret files (the private HTTP env file,
// credentials and private keys), and search leaves their lines out.
func TestWorkspaceToolsKeepSecretFilesFromTheModel(t *testing.T) {
	root := t.TempDir()
	canary := "scn" + "canary" + "42"
	for name, body := range map[string]string{
		"http-client.private.env.json": `{"dev":{"token":"` + canary + `"}}`,
		".pgpass":                      "localhost:5432:app:app:" + canary + "\n",
		"notes.txt":                    "the word " + canary + " in a normal file\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tools := WorkspaceTools(Workspace{Root: root})
	read, _ := FindTool(tools, "read_file")
	for _, p := range []string{"http-client.private.env.json", ".pgpass"} {
		out, err := read.Run(context.Background(), map[string]any{"path": p})
		if err == nil || strings.Contains(out, canary) || !strings.Contains(err.Error(), "secret") {
			t.Errorf("read_file %s: %q %v", p, out, err)
		}
	}
	search, _ := FindTool(tools, "search")
	out, err := search.Run(context.Background(), map[string]any{"query": canary})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "notes.txt") || strings.Contains(out, ".pgpass") || strings.Contains(out, "private") {
		t.Errorf("search: %q", out)
	}
}
