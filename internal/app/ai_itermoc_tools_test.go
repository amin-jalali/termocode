package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/ai"
)

// aiItermocToolNames are the AI read tools of the Itermoc view (AH-7).
var aiItermocToolNames = []string{"profiles_list", "models_list", "mcp_services_list", "outside_agents_status", "ai_view_open"}

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

// Spec: models_list shows each slot's provider, model and whether a key
// is set (never the key), and a base URL without user, password, path or
// query.
func TestAIModelsListShape(t *testing.T) {
	m, call := aiItermocTestModel(t)
	m.ai.slotFake = map[string]aiSlot{
		ai.SlotMain:  {cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5", APIKey: aiPlantAPIKey}},
		ai.SlotLocal: {cfg: ai.Config{Kind: ai.KindOllama, Model: "qwen2.5-coder:7b", BaseURL: "http://127.0.0.1:11434"}},
		"gpt":        {cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
		"proxy":      {cfg: ai.Config{Kind: ai.KindCompatible, Model: "gpt-x", BaseURL: "https://u:p@llm.example.com:8443/v1?token=abc"}},
	}
	out := aiItermocRun(t, m, call, "models_list", nil)
	var got struct {
		Slots []struct {
			Name       string `json:"name"`
			Provider   string `json:"provider"`
			Model      string `json:"model"`
			Credential string `json:"credential"`
			Base       string `json:"base_host"`
		} `json:"slots"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	want := map[string][4]string{
		ai.SlotMain:  {ai.KindAnthropic, "claude-sonnet-4-5", "set", ""},
		ai.SlotLocal: {ai.KindOllama, "qwen2.5-coder:7b", "not needed", "http://127.0.0.1:11434"},
		"gpt":        {ai.KindOpenAI, "gpt-5-mini", "missing", ""},
		"proxy":      {ai.KindCompatible, "gpt-x", "none (optional)", "https://llm.example.com:8443"},
	}
	if len(got.Slots) == 0 || got.Slots[0].Name != ai.SlotMain {
		t.Fatalf("slots %+v", got.Slots)
	}
	seen := 0
	for _, s := range got.Slots {
		w, ok := want[s.Name]
		if !ok {
			continue // the pool's own slots (quick)
		}
		seen++
		if s.Provider != w[0] || s.Model != w[1] || s.Credential != w[2] || s.Base != w[3] {
			t.Errorf("slot %+v, want %v", s, w)
		}
	}
	if seen != len(want) {
		t.Errorf("slots %+v", got.Slots)
	}
}

// Spec: mcp_services_list answers "what is connected?": every MCP service
// with state (off included), transport, access, tool count and sign-in.
// (services_list is the project's dev services: another tool.)
func TestAIMCPServicesListShape(t *testing.T) {
	m, call := aiItermocTestModel(t)
	aiPlantSecrets(t, m)
	writeUserMCP(t, `{"mcpServers":{
		"notes":{"command":"notes-mcp"},
		"remote":{"type":"http","url":"https://mcp.example.com/mcp","readOnly":true},
		"quiet":{"command":"quiet-mcp","disabled":true}}}`)
	m.ai.mcp = nil
	out := aiItermocRun(t, m, call, "mcp_services_list", nil)
	var got struct {
		Services []struct {
			Name      string `json:"name"`
			State     string `json:"state"`
			Transport string `json:"transport"`
			Source    string `json:"source"`
			Access    string `json:"access"`
			Tools     int    `json:"tools"`
			SignedIn  bool   `json:"signed_in"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	by := map[string]int{}
	for i, s := range got.Services {
		by[s.Name] = i
	}
	if len(got.Services) != 3 {
		t.Fatalf("services %+v", got.Services)
	}
	if s := got.Services[by["quiet"]]; s.State != "off" || s.Transport != "stdio" || s.Source != "user" {
		t.Errorf("quiet %+v", s)
	}
	if s := got.Services[by["remote"]]; s.Transport != "http" || s.Access != "read only" || !s.SignedIn {
		t.Errorf("remote %+v", s)
	}
	if s := got.Services[by["notes"]]; s.Access != "read and write" || s.SignedIn {
		t.Errorf("notes %+v", s)
	}
}

// Spec: a service's problem text keeps no URL user, password or query.
func TestAIMCPServicesProblemCleaned(t *testing.T) {
	got := aiCleanURLs("connect https://" + aiPlantUserPass + "@mcp.example.com/mcp?token=" + aiPlantQuery + ": refused")
	if got != "connect https://mcp.example.com: refused" {
		t.Errorf("got %q", got)
	}
}

// Spec: outside_agents_status answers "who is connected?": listening
// state, the agents with their calls, the recent calls and what each
// agent may run without asking.
func TestAIOutsideAgentsStatusShape(t *testing.T) {
	m, call := aiItermocTestModel(t)
	out := aiItermocRun(t, m, call, "outside_agents_status", nil)
	if !strings.Contains(out, `"listening": false`) || !strings.Contains(out, `"not_listening_why"`) {
		t.Errorf("not listening:\n%s", out)
	}
	if err := mcpServeAllowSave(map[string][]string{"claude-code": {"tasks_run"}}); err != nil {
		t.Fatal(err)
	}
	m.mcpServe = &mcpServeState{clients: []*mcpServeAgent{{id: 7, name: "claude-code",
		since: time.Date(2026, 1, 1, 9, 41, 0, 0, time.UTC), calls: 14, asked: 2}}}
	out = aiItermocRun(t, m, call, "outside_agents_status", nil)
	var got struct {
		Mode      string `json:"mode"`
		Listening bool   `json:"listening"`
		Agents    []struct {
			Name  string `json:"name"`
			Since string `json:"since"`
			Calls int    `json:"calls"`
			Asked int    `json:"asked"`
		} `json:"agents"`
		Auto map[string][]string `json:"always_allowed"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if !got.Listening || got.Mode != "auto" || len(got.Agents) != 1 || got.Agents[0].Name != "claude-code" ||
		got.Agents[0].Calls != 14 || got.Agents[0].Asked != 2 || got.Agents[0].Since != "09:41" {
		t.Errorf("status %+v", got)
	}
	if strings.Join(got.Auto["claude-code"], ",") != "tasks_run" {
		t.Errorf("always allowed %+v", got.Auto)
	}
}

// Spec: ai_view_open shows the Itermoc view at a section and, given a
// row, puts the cursor on it. It changes nothing else: the keyboard stays
// where the user is typing.
func TestAIViewOpen(t *testing.T) {
	m, call := aiItermocTestModel(t)
	m.focus = FocusEditor
	out := aiItermocRun(t, m, call, "ai_view_open", map[string]any{"section": "profiles", "row": "house"})
	if m.activity.Active() != activity.ViewItermoc || !m.showExp {
		t.Fatalf("view not shown: %v", m.activity.Active())
	}
	if m.focus != FocusEditor {
		t.Errorf("focus moved to %v", m.focus)
	}
	if r, ok := m.itermoc.current(); !ok || r.kind != imRowProfile || r.name != "house" {
		t.Errorf("cursor on %+v", r)
	}
	if !strings.Contains(out, "PROFILES") || !strings.Contains(out, "house") {
		t.Errorf("answer %q", out)
	}
	m.mcpServe = &mcpServeState{clients: []*mcpServeAgent{{id: 7, name: "claude-code", since: time.Now()}}}
	aiItermocRun(t, m, call, "ai_view_open", map[string]any{"section": "outside-agents", "row": "claude-code"})
	if r, ok := m.itermoc.current(); !ok || r.kind != imRowAgent || r.name != agentKey(7) {
		t.Errorf("cursor on %+v", r)
	}
	out = aiItermocRun(t, m, call, "ai_view_open", map[string]any{"section": "models", "row": "nope"})
	if r, _ := m.itermoc.current(); r.kind != imRowSection || r.sec != imSecModels || !strings.Contains(out, "no row") {
		t.Errorf("unknown row: cursor %+v, answer %q", r, out)
	}
	tl, _ := ai.FindTool(m.aiChatTools(".", call), "ai_view_open")
	if err := tl.Validate(map[string]any{"section": "secrets"}); err == nil {
		t.Error("an unknown section passed")
	}
}

// Spec: the Itermoc palette commands stay the user's; their reason sends
// the model to ai_view_open.
func TestAIItermocPolicyPointsToViewOpen(t *testing.T) {
	for _, it := range itermocPaletteItems() {
		p, ok := aiActionPolicyFor(it.ID)
		if !ok || p.Gate != aiGateNever || !strings.Contains(p.Reason, "ai_view_open") || strings.Contains(p.Reason, "will get") {
			t.Errorf("%s: %+v", it.ID, p)
		}
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
	m.mcpServe = &mcpServeState{clients: []*mcpServeAgent{{id: 1, name: "claude-code", since: time.Now(), calls: 3}}}
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
