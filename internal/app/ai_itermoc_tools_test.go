package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// aiItermocToolNames are the AI read tools of the Itermoc view (AH-7).
var aiItermocToolNames = []string{"profiles_list"}

// aiItermocTestModel is the profile test model (built-in profiles, an
// untrusted project profile "house", one fake main slot) with a
// synchronous IDE caller.
func aiItermocTestModel(t *testing.T) (*Model, aiIDECaller) {
	t.Helper()
	m, _ := aiProfileTestModel(t)
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(m)
		return out, err
	}
	return m, call
}

// aiItermocRun runs one Itermoc tool the way the chat does (through
// aiChatTools, so the shared redaction wraps it too).
func aiItermocRun(t *testing.T, m *Model, call aiIDECaller, name string, args map[string]any) string {
	t.Helper()
	tl, ok := ai.FindTool(m.aiChatTools(".", call), name)
	if !ok {
		t.Fatalf("no tool %s in the chat's tools", name)
	}
	if args == nil {
		args = map[string]any{}
	}
	out, err := tl.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out
}

// Spec: profiles_list answers "what does the review profile do?" from
// data: every profile with source, trust, active mark and its steps
// (title, slot, resolved model, tools preset, first prompt line).
func TestAIProfilesListShape(t *testing.T) {
	m, call := aiItermocTestModel(t)
	out := aiItermocRun(t, m, call, "profiles_list", nil)
	var got struct {
		Active   string `json:"active"`
		Profiles []struct {
			Name   string `json:"name"`
			Title  string `json:"title"`
			Source string `json:"source"`
			Trust  string `json:"trust"`
			Active bool   `json:"active"`
			Steps  []struct {
				ID     string `json:"id"`
				Title  string `json:"title"`
				Slot   string `json:"slot"`
				Model  string `json:"model"`
				Tools  string `json:"tools"`
				Prompt string `json:"prompt_first_line"`
			} `json:"steps"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.Active != ai.ProfileDefault {
		t.Errorf("active %q", got.Active)
	}
	by := map[string]int{}
	for i, p := range got.Profiles {
		by[p.Name] = i
	}
	for _, n := range []string{"default", "review", "fast", "local-only", "house"} {
		if _, ok := by[n]; !ok {
			t.Errorf("no profile %s in %s", n, out)
		}
	}
	def := got.Profiles[by["default"]]
	if !def.Active || def.Source != "built-in" || len(def.Steps) == 0 || def.Steps[0].Model != "fake-1" || def.Steps[0].Slot != ai.SlotMain {
		t.Errorf("default profile %+v", def)
	}
	house := got.Profiles[by["house"]]
	if house.Source != "project" || house.Trust != "not asked yet" || house.Title != "House review" || house.Active {
		t.Errorf("house profile %+v", house)
	}
	if len(house.Steps) != 1 || house.Steps[0].Title != "Reviewer" || house.Steps[0].Tools != "read-only" ||
		house.Steps[0].Prompt != "Follow the house style." {
		t.Errorf("house steps %+v", house.Steps)
	}
	if strings.Contains(out, "Second line.") {
		t.Error("profiles_list shows more than the first prompt line")
	}
}

// ── Secrets: planted everywhere, found nowhere ───────────────────────────

const (
	aiPlantAPIKey   = "sk-ant-PLANTEDAPIKEY0123456789"
	aiPlantEnvKey   = "PLANTED-ENV-VALUE-7f3a9c"
	aiPlantMCPToken = "PLANTED-MCP-ACCESS-4b2e"
	aiPlantRefresh  = "PLANTED-MCP-REFRESH-91d0"
	aiPlantHeader   = "PLANTED-HEADER-VALUE-55aa"
	aiPlantSecret   = "PLANTED-STORE-SECRET-c0de"
	aiPlantUserPass = "plantuser:PLANTEDPASS77"
	aiPlantQuery    = "PLANTED-QUERY-TOKEN-e1e1"
	aiPlantPrompt2  = "PLANTED-PROMPT-SECOND-LINE"
	aiPlantClientSc = "PLANTED-CLIENT-SECRET-0f0f"
)

var aiPlanted = []string{aiPlantAPIKey, aiPlantEnvKey, aiPlantMCPToken, aiPlantRefresh, aiPlantHeader,
	aiPlantSecret, aiPlantUserPass, "PLANTEDPASS77", aiPlantQuery, aiPlantPrompt2, aiPlantClientSc}

// aiPlantSecrets puts fake secrets in every place the Itermoc tools read
// near: the credential store (API key, MCP sign-in, ${secret:…} values),
// env vars, mcp.json (env, headers, URL user:pass, ?token=), a slot's
// base URL and a profile prompt's second line.
func aiPlantSecrets(t *testing.T, m *Model) {
	t.Helper()
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "TERMOCODE_AI_API_KEY", "GITHUB_TOKEN"} {
		t.Setenv(k, aiPlantEnvKey)
	}
	credPath, err := ai.CredentialsPath()
	if err != nil {
		t.Fatal(err)
	}
	creds := map[string]any{
		"keys":        map[string]string{"anthropic": aiPlantAPIKey, "openai": aiPlantAPIKey},
		"mcp_secrets": map[string]string{"NOTES_TOKEN": aiPlantSecret},
		"mcp_oauth": map[string]any{"remote": map[string]any{
			"issuer": "https://auth.example.com", "resource": "https://mcp.example.com/mcp",
			"token_endpoint": "https://auth.example.com/token", "client_id": "cid",
			"client_secret": aiPlantClientSc, "access_token": aiPlantMCPToken, "refresh_token": aiPlantRefresh,
		}},
	}
	b, _ := json.Marshal(creds)
	if err := os.MkdirAll(filepath.Dir(credPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	writeUserMCP(t, `{"mcpServers":{
		"notes":{"command":"notes-mcp","env":{"NOTES_TOKEN":"${secret:NOTES_TOKEN}","RAW":"`+aiPlantEnvKey+`"}},
		"remote":{"type":"http","url":"https://`+aiPlantUserPass+`@mcp.example.com/mcp?token=`+aiPlantQuery+`",
			"headers":{"Authorization":"Bearer `+aiPlantHeader+`","X-Api-Key":"`+aiPlantHeader+`"}},
		"quiet":{"command":"quiet-mcp","disabled":true}}}`)
	m.ai.slotFake = map[string]aiSlot{
		ai.SlotMain: {cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5", APIKey: aiPlantAPIKey}},
		"proxy": {cfg: ai.Config{Kind: ai.KindCompatible, Model: "gpt-x", APIKey: aiPlantAPIKey,
			BaseURL: "https://" + aiPlantUserPass + "@llm.example.com:8443/v1?token=" + aiPlantQuery}},
	}
	dir, _ := os.Getwd()
	proj := `{"profiles": {"house": {"title": "House review", "steps": [
		{"id": "rev", "title": "Reviewer", "system": "Follow the house style.\n` + aiPlantPrompt2 + `", "tools": {"preset": "read-only"}}]}}}`
	if err := os.WriteFile(filepath.Join(dir, ".termocode", "ai-profiles.json"), []byte(proj), 0o644); err != nil {
		t.Fatal(err)
	}
	m.ai.reloadProfiles()
	m.ai.mcp = nil // read mcp.json again
}

// Spec (security): no Itermoc tool shows a planted secret, in any form.
func TestAIItermocToolsNeverLeakSecrets(t *testing.T) {
	m, call := aiItermocTestModel(t)
	aiPlantSecrets(t, m)
	for _, name := range aiItermocToolNames {
		out := aiItermocRun(t, m, call, name, aiItermocSafeArgs(name))
		for _, s := range aiPlanted {
			if strings.Contains(out, s) {
				t.Errorf("%s leaks %q:\n%s", name, s, out)
			}
		}
		aiItermocNoSecretFields(t, name, out)
	}
}

// aiItermocSafeArgs are arguments a tool needs to run in the leak test.
func aiItermocSafeArgs(name string) map[string]any {
	if name == "ai_view_open" {
		return map[string]any{"section": "services", "row": "remote"}
	}
	return nil
}

// aiItermocNoSecretFields fails when JSON output has a field named like a
// secret (the design's deny list).
func aiItermocNoSecretFields(t *testing.T, name, out string) {
	t.Helper()
	var v any
	if json.Unmarshal([]byte(out), &v) != nil {
		return // a text answer: the value check above is enough
	}
	var walk func(any)
	walk = func(x any) {
		switch x := x.(type) {
		case map[string]any:
			for k, c := range x {
				lk := strings.ToLower(k)
				if lk == "key" || lk == "env" || lk == "url" {
					t.Errorf("%s: field %q", name, k)
				}
				for _, bad := range []string{"token", "secret", "header", "oauth", "password", "api_key", "apikey"} {
					if strings.Contains(lk, bad) {
						t.Errorf("%s: field %q", name, k)
					}
				}
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(v)
}

// Spec: the Itermoc tools are read tools: they never ask (auto), the docs
// catalog lists them with gate auto, and outside agents (termocode as an
// MCP server) do not get them: they describe the chat's own setup.
func TestAIItermocToolsPolicy(t *testing.T) {
	cat := map[string]AIToolDoc{}
	for _, d := range AIToolCatalog() {
		cat[d.Name] = d
	}
	exposed := map[string]bool{}
	for _, s := range MCPServeToolSpecs() {
		exposed[s.Name] = true
	}
	call := func(context.Context, aiIDEFunc) (string, error) { return "", nil }
	for _, n := range aiItermocToolNames {
		tl, ok := ai.FindTool(aiItermocTools(call), n)
		if !ok {
			t.Errorf("no tool %s", n)
			continue
		}
		if tl.Mutating || tl.Gate != nil || tl.Class != "" {
			t.Errorf("%s is not a plain read tool", n)
		}
		if d, ok := cat[n]; !ok || d.Gate != "auto" {
			t.Errorf("%s: catalog %+v", n, d)
		}
		if exposed[n] {
			t.Errorf("%s is exposed to outside agents", n)
		}
		if _, ok := mcpServeRow(n); ok {
			t.Errorf("%s is in the MCP allow list", n)
		}
	}
}
