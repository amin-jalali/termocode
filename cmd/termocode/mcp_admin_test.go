package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// adminEnv is a temp HOME / XDG with the plain secret store (no keyring)
// and a project folder; it returns the project folder.
func adminEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	dir, err := ai.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"secret_store":"plain"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ai.SetSecretStore(nil)
	t.Cleanup(func() { ai.SetSecretStore(nil) })
	return t.TempDir()
}

// runAdmin runs `termocode mcp <args>` in-process.
func runAdmin(t *testing.T, cwd, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code, ok := runMCPAdmin(args, strings.NewReader(stdin), &out, &errb, cwd)
	if !ok {
		t.Fatalf("%v: not an admin command", args)
	}
	return code, out.String(), errb.String()
}

func userEntry(t *testing.T, name string) map[string]any {
	t.Helper()
	dir, _ := ai.ConfigDir()
	return fileEntry(t, mcp.UserPath(dir), name)
}

func fileEntry(t *testing.T, path, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f.MCPServers[name]
}

func storedSecret(t *testing.T, name string) string {
	t.Helper()
	st, err := openMCPSecretStore()
	if err != nil {
		t.Fatal(err)
	}
	v, _, _ := st.Get("mcp.secret." + name)
	return v
}

func TestMCPAdminNotForServeArgs(t *testing.T) {
	for _, args := range [][]string{nil, {"--dir", "x"}, {"-h"}} {
		if _, ok := runMCPAdmin(args, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, t.TempDir()); ok {
			t.Fatalf("%v: taken by the admin commands", args)
		}
	}
}

func TestMCPAdminAddCommandWithSecretFromStdin(t *testing.T) {
	cwd := adminEnv(t)
	tok := "sk-" + "test0123456789abcdef"
	code, out, errs := runAdmin(t, cwd, tok+"\n", "add", "--env", "LOG=debug", "--secret", "API_KEY", "files", "--", "my-server", "--root", "/srv")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if strings.Contains(out+errs, tok) {
		t.Fatalf("the secret was printed: %s %s", out, errs)
	}
	e := userEntry(t, "files")
	b, _ := json.Marshal(e)
	if want := `{"args":["--root","/srv"],"command":"my-server","env":{"API_KEY":"${secret:files_api_key}","LOG":"debug"}}`; string(b) != want {
		t.Fatalf("entry %s", b)
	}
	if got := storedSecret(t, "files_api_key"); got != tok {
		t.Fatal("the secret is not in the store")
	}
	dir, _ := ai.ConfigDir()
	raw, _ := os.ReadFile(mcp.UserPath(dir))
	if strings.Contains(string(raw), tok) {
		t.Fatal("mcp.json holds the secret")
	}
}

func TestMCPAdminAddRefusesSecretInArgv(t *testing.T) {
	cwd := adminEnv(t)
	tok := "sk-" + "test0123456789abcdef"
	code, out, errs := runAdmin(t, cwd, "", "add", "--env", "TOKEN="+tok, "x", "--", "srv")
	if code == 0 {
		t.Fatal("a token in --env was accepted")
	}
	if strings.Contains(out+errs, tok) {
		t.Fatalf("the token was printed: %s", out+errs)
	}
	code, _, _ = runAdmin(t, cwd, "", "add", "--secret", "K", "y", "--", "srv")
	if code == 0 {
		t.Fatal("an empty secret was accepted")
	}
	dir, _ := ai.ConfigDir()
	if _, err := os.Stat(mcp.UserPath(dir)); err == nil {
		t.Fatal("mcp.json was written")
	}
}

func TestMCPAdminAddPackageProjectScope(t *testing.T) {
	cwd := adminEnv(t)
	code, out, errs := runAdmin(t, cwd, "", "add", "--scope", "project", "--npm", "@scope/server-fs", "--", "/data")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	e := fileEntry(t, mcp.ProjectPath(cwd), "fs")
	if e["command"] != "npx" {
		t.Fatalf("entry %v", e)
	}
	if !strings.Contains(out, "trust") {
		t.Fatalf("no trust note: %s", out)
	}
	if code, _, _ := runAdmin(t, cwd, "", "add", "--scope", "project", "--npm", "@scope/server-fs"); code == 0 {
		t.Fatal("a second add of the same name worked")
	}
}

func TestMCPAdminAddURLAndJSON(t *testing.T) {
	cwd := adminEnv(t)
	if code, _, errs := runAdmin(t, cwd, "", "add", "https://mcp.example.com/mcp"); code != 0 {
		t.Fatalf("url: %s", errs)
	}
	if userEntry(t, "mcp-example-com")["url"] != "https://mcp.example.com/mcp" {
		t.Fatal("url entry missing")
	}
	if code, _, errs := runAdmin(t, cwd, "", "add", "docs", "https://docs.example.com/mcp"); code != 0 || userEntry(t, "docs") == nil {
		t.Fatalf("named url: %s", errs)
	}
	tok := "sk-" + "test0123456789abcdef"
	block := `{"mcpServers":{"pasted":{"command":"x","env":{"KEY":"` + tok + `"}}}}`
	code, out, errs := runAdmin(t, cwd, block, "add", "-")
	if code != 0 || strings.Contains(out+errs, tok) {
		t.Fatalf("json: %d %s %s", code, out, errs)
	}
	if storedSecret(t, "pasted_key") != tok {
		t.Fatal("the pasted token is not in the store")
	}
}

func TestMCPAdminListGetRemove(t *testing.T) {
	cwd := adminEnv(t)
	tok := "sk-" + "test0123456789abcdef"
	if code, _, errs := runAdmin(t, cwd, tok+"\n", "add", "--secret", "API_KEY", "--env", "MODE=ro", "one", "--", "srv"); code != 0 {
		t.Fatal(errs)
	}
	if code, _, errs := runAdmin(t, cwd, "", "add", "--scope", "project", "two", "--", "other"); code != 0 {
		t.Fatal(errs)
	}
	code, out, _ := runAdmin(t, cwd, "", "list")
	if code != 0 || !strings.Contains(out, "one") || !strings.Contains(out, "two") || !strings.Contains(out, "project") {
		t.Fatalf("list: %s", out)
	}
	code, out, _ = runAdmin(t, cwd, "", "list", "--json")
	var rows []map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 2 {
		t.Fatalf("list --json: %s", out)
	}
	code, out, _ = runAdmin(t, cwd, "", "get", "one")
	if code != 0 || !strings.Contains(out, "API_KEY") || !strings.Contains(out, "secret") || strings.Contains(out, tok) {
		t.Fatalf("get: %s", out)
	}
	if code, _, _ := runAdmin(t, cwd, "", "get", "nope"); code == 0 {
		t.Fatal("get of a missing server worked")
	}
	if code, _, errs := runAdmin(t, cwd, "", "remove", "one"); code != 0 {
		t.Fatal(errs)
	}
	if userEntry(t, "one") != nil {
		t.Fatal("still in mcp.json")
	}
	if storedSecret(t, "one_api_key") != "" {
		t.Fatal("its secret stayed in the store")
	}
	if code, _, _ := runAdmin(t, cwd, "", "remove", "one"); code == 0 {
		t.Fatal("a second remove worked")
	}
}

func TestMCPAdminAddShareUser(t *testing.T) {
	cwd := adminEnv(t)
	if code, _, errs := runAdmin(t, cwd, "", "add", "--share", "user", "notes", "--", "srv"); code != 0 {
		t.Fatal(errs)
	}
	if userEntry(t, "notes")["share"] != "user" {
		t.Fatalf("entry %v", userEntry(t, "notes"))
	}
	if code, _, _ := runAdmin(t, cwd, "", "add", "--share", "user", "--scope", "project", "p", "--", "srv"); code == 0 {
		t.Fatal("share in the project file was accepted")
	}
	if code, _, _ := runAdmin(t, cwd, "", "add", "--share", "all", "q", "--", "srv"); code == 0 {
		t.Fatal("share all was accepted")
	}
}

func TestMCPAdminAddWarnsMissingRuntime(t *testing.T) {
	cwd := adminEnv(t)
	t.Setenv("PATH", t.TempDir())
	code, _, errs := runAdmin(t, cwd, "", "add", "--npm", "some-mcp")
	if code != 0 || !strings.Contains(errs, "npx not found") || !strings.Contains(errs, "nodejs.org") {
		t.Fatalf("exit %d, stderr %q", code, errs)
	}
}
