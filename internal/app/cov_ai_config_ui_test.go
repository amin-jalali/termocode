package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
)

// covToastText is the text of every toast on screen.
func covToastText(m *Model) string {
	var rows []string
	for _, r := range m.toast.View(200, 60) {
		rows = append(rows, ansi.Strip(r))
	}
	return strings.Join(rows, "\n")
}

// AI: Configure Provider… and the other AI settings screens
// (ai_config_ui.go), driven through the picker / prompt / confirm
// routers like the keys would.

func covPick(t *testing.T, m *Model, id string) {
	t.Helper()
	if !m.pickerOpen {
		t.Fatalf("no picker open to pick %q", id)
	}
	m.pickerOpen = false
	if _, ok := m.handleAIPickerSelect(id); !ok {
		t.Fatalf("pick %q was not routed (kind %v)", id, m.pickerKind)
	}
}

func covSubmit(t *testing.T, m *Model, v string) {
	t.Helper()
	if !m.promptOpen {
		t.Fatalf("no prompt open to submit %q", v)
	}
	m.promptOpen = false
	if _, ok := m.handleAIPromptSubmit(v); !ok {
		t.Fatalf("submit %q was not routed (kind %v)", v, m.promptKind)
	}
}

func covSettings(t *testing.T) ai.Settings {
	t.Helper()
	p, err := ai.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	return ai.LoadSettings(p)
}

// An OpenAI-compatible server: base URL, key, then a custom model id.
func TestCovAIConfigureCompatibleFlow(t *testing.T) {
	m := aiTestModel(t)
	m.openAIConfigure()
	covPick(t, &m, ai.KindCompatible)
	if !m.promptOpen || m.promptKind != promptKindAIBaseURL {
		t.Fatal("no base URL prompt")
	}
	covSubmit(t, &m, " http://127.0.0.1:9/v1 ")
	if !m.promptOpen || m.promptKind != promptKindAIKey {
		t.Fatal("no key prompt after the base URL")
	}
	covSubmit(t, &m, "sk-cov-compat-1234567890")
	if !m.pickerOpen || m.pickerKind != pickerKindAIModel {
		t.Fatal("no model picker after the key")
	}
	covPick(t, &m, "__custom")
	if !m.promptOpen || m.promptKind != promptKindAIModel {
		t.Fatal("Custom model id… did not ask")
	}
	covSubmit(t, &m, "  my-model  ")
	s := covSettings(t)
	if s.Provider != ai.KindCompatible || s.BaseURL != "http://127.0.0.1:9/v1" || s.Model != "my-model" {
		t.Errorf("settings = %+v", s)
	}
	if !m.aiConfigured() {
		t.Error("the provider is not ready after the key")
	}
	if b, _ := os.ReadFile(filepath.Join(filepath.Dir(mustConfigPath(t)), "config.json")); strings.Contains(string(b), "sk-cov-compat") {
		t.Error("the key landed in config.json")
	}
}

func mustConfigPath(t *testing.T) string {
	t.Helper()
	p, err := ai.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Ollama goes straight to the model picker; a model without a key warns;
// Turn AI off saves "none".
func TestCovAIConfigureOllamaAndOff(t *testing.T) {
	m := aiTestModel(t)
	m.openAIConfigure()
	covPick(t, &m, ai.KindOllama)
	if !m.pickerOpen || m.pickerKind != pickerKindAIModel {
		t.Fatal("ollama: no model picker")
	}
	covPick(t, &m, ai.DefaultModel(ai.KindOllama))
	if got := covSettings(t); got.Provider != ai.KindOllama || got.Model != ai.DefaultModel(ai.KindOllama) {
		t.Errorf("ollama settings = %+v", got)
	}
	m.openAIConfigure()
	covPick(t, &m, ai.KindAnthropic)
	if m.promptKind != promptKindAIKey {
		t.Fatal("anthropic: no key prompt")
	}
	covSubmit(t, &m, "") // empty = keep (none yet)
	covPick(t, &m, ai.DefaultModel(ai.KindAnthropic))
	if m.aiConfigured() {
		t.Error("configured without a key")
	}
	m.openAIConfigure()
	covPick(t, &m, "none")
	if got := covSettings(t); got.Provider != "none" {
		t.Errorf("off = %+v", got)
	}
	// With no provider the model picker sends you to the provider list.
	m.aiS().cfg.Kind, m.aiS().settings.Provider = "", ""
	m.openAIModelPicker()
	if m.pickerKind != pickerKindAIProvider {
		t.Errorf("model picker without a provider opened %v", m.pickerKind)
	}
}

// Settings that cannot be written are an error toast, never a silent
// "saved".
func TestCovAIConfigureSaveErrors(t *testing.T) {
	m := aiTestModel(t)
	p := mustConfigPath(t)
	if err := os.MkdirAll(p, 0o755); err != nil { // config.json is a folder
		t.Fatal(err)
	}
	for _, f := range []func() any{
		func() any { return m.onAIProviderPicked("none") },
		func() any { return m.onAIProviderPicked(ai.KindOllama) },
		func() any { return m.onAIBaseURLEntered("http://x") },
		func() any { return m.saveAIModel("x") },
		func() any { return m.onAIAutoApproveToggled(ai.ClassGit) },
	} {
		f()
		if m.promptOpen || m.pickerOpen {
			t.Error("a failed save went on to the next step")
		}
	}
	if !strings.Contains(covToastText(&m), "Settings error") {
		t.Errorf("toast = %q", covToastText(&m))
	}
}

// Auto-approve: the list shows the classes, an MCP server and an HTTP
// host; Enter switches ask and AUTO.
func TestCovAIAutoApprovePicker(t *testing.T) {
	m := aiTestModel(t)
	writeUserMCP(t, `{"mcpServers":{"gh":{"type":"http","url":"https://x.example/mcp"}}}`)
	m.aiMCP().Reload()
	if err := m.saveAISettings(func(st *ai.Settings) {
		st.AutoApprove = []string{ai.ClassGit, "mcp:gh", "http:api.example.com", "http:api.example.com/write"}
	}); err != nil {
		t.Fatal(err)
	}
	m.openAIAutoApprovePicker()
	if !m.pickerOpen || m.pickerKind != pickerKindAIAutoApprove {
		t.Fatal("no auto-approve picker")
	}
	m.h = 80
	m.picker.SetSize(200, 80)
	all := ansi.Strip(m.picker.View())
	for _, want := range []string{"mcp:gh — mutating tools of MCP server gh", "http:api.example.com — HTTP requests (GET / HEAD / OPTIONS)",
		"http:api.example.com/write — HTTP requests (all methods) to api.example.com", "AUTO", "ask"} {
		if !strings.Contains(all, want) {
			t.Errorf("auto-approve rows have no %q:\n%s", want, all)
		}
	}
	covPick(t, &m, ai.ClassRun)
	if !contains(covSettings(t).AutoApprove, ai.ClassRun) {
		t.Error("run is not AUTO after Enter")
	}
	m.openAIAutoApprovePicker()
	covPick(t, &m, ai.ClassGit)
	if contains(covSettings(t).AutoApprove, ai.ClassGit) {
		t.Error("git is still AUTO after Enter")
	}
}

// Sign in with Claude: the note first, then the browser and the code
// prompt; a bad paste and a state from another sign-in are refused;
// the tokens are saved and the provider switches; sign out undoes it.
func TestCovAIClaudeSignIn(t *testing.T) {
	m := aiTestModel(t)
	opened := covStubOpen(t, false)
	m.openAIConfigure()
	covPick(t, &m, ai.KindClaudeOAuth)
	if !m.confirmOpen || m.confirmKind != confirmKindAIOAuth {
		t.Fatal("no ToS note")
	}
	if cmd, ok := m.handleAIConfirm("cancel"); !ok || cmd != nil || m.promptOpen {
		t.Error("cancel went on")
	}
	m.confirmKind = confirmKindAIOAuth
	m.handleAIConfirm("continue")
	if !m.promptOpen || m.promptKind != promptKindAIOAuthCode || len(*opened) != 1 || !strings.Contains((*opened)[0], "code_challenge") {
		t.Fatalf("continue: prompt %v opened %v", m.promptOpen, *opened)
	}
	state := m.aiS().pkce.State
	if m.onClaudeCodePasted("two words") == nil || m.aiS().pkce == nil {
		t.Error("a bad paste ended the sign-in")
	}
	if m.onClaudeCodePasted("code123#someone-elses-state") == nil || m.aiS().pkce != nil {
		t.Error("a state mismatch did not end the sign-in")
	}
	if m.onClaudeCodePasted("code123") == nil {
		t.Error("a paste with no sign-in must say so")
	}
	m.handleAIConfirm("continue")
	if cmd := m.onClaudeCodePasted("code123"); cmd == nil {
		t.Error("a good code did not start the exchange")
	}
	_ = state
	m.onAIOAuthDone(aiOAuthDoneMsg{err: errors.New("invalid_grant")})
	if !strings.Contains(covToastText(&m), "Claude sign-in failed") {
		t.Errorf("toast = %q", covToastText(&m))
	}
	m.onAIOAuthDone(aiOAuthDoneMsg{tokens: ai.OAuthTokens{AccessToken: "at-cov-12345678", RefreshToken: "rt-cov-12345678"}})
	if got := covSettings(t).Provider; got != ai.KindClaudeOAuth {
		t.Errorf("provider after sign-in = %q", got)
	}
	m.claudeSignOut()
	if got := covSettings(t).Provider; got != "" {
		t.Errorf("provider after sign-out = %q", got)
	}
	m.claudeSignOut()
	if !strings.Contains(covToastText(&m), "Not signed in to Claude") {
		t.Errorf("second sign-out toast = %q", covToastText(&m))
	}
}

// Edit Credentials File… opens the plain key file, kept 0600.
func TestCovAIEditCredentialsFile(t *testing.T) {
	m := aiTestModel(t)
	m.aiS().cfgKind = ai.KindOpenAI
	m.aiS().keyOnly = true
	m.onAIKeyEntered("sk-cov-edit-1234567890")
	p, _ := ai.CredentialsPath()
	_ = os.Chmod(p, 0o644)
	m.editAICredentials()
	info, err := os.Stat(p)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials file: %v %v", info, err)
	}
	if m.focus != FocusEditor {
		t.Error("focus did not move to the editor")
	}
}

// The key prompt from Itermoc only stores the key.
func TestCovAIKeyOnlyFromItermoc(t *testing.T) {
	m := aiTestModel(t)
	m.aiS().keyOnly = true
	m.aiS().cfgKind = ai.KindOpenAI
	m.onAIKeyEntered("sk-cov-only-1234567890")
	if m.pickerOpen || m.aiS().keyOnly {
		t.Error("key-only went on to the model picker")
	}
	p, _ := ai.CredentialsPath()
	if c, _ := ai.LoadCredentials(p); c.Keys[ai.KindOpenAI] != "sk-cov-only-1234567890" {
		t.Errorf("key not saved: %+v", c.Keys)
	}
}

// Safe answers on the AI cards: "keep" / "cancel" / Esc never remove a
// server, forget a secret or stop a shared server.
func TestCovAICardsSafeAnswers(t *testing.T) {
	m := aiTestModel(t)
	m.services = newServicesState()
	writeUserMCP(t, `{"mcpServers":{"keepme":{"type":"http","url":"https://k.example/mcp"}}}`)
	m.aiMCP().Reload()
	m.aiS().mcpSel = "keepme"
	for _, c := range []struct {
		kind confirmKindEnum
		id   string
	}{{confirmKindMCPRemove, "keep"}, {confirmKindMCPShareStop, "cancel"}, {confirmKindSecretForget, "cancel"},
		{confirmKindSecretsSwitch, "cancel"}, {confirmKindRegistryRemember, "cancel"}} {
		m.confirmKind = c.kind
		if _, ok := m.handleAIConfirm(c.id); !ok {
			t.Errorf("kind %v not routed", c.kind)
		}
		m.confirmKind = c.kind
		m.onAIConfirmClosed()
	}
	if len(mcpServerEntries("keepme")) != 1 {
		t.Error("a safe answer removed the server")
	}
	if m.services.regPending != (registryPending{}) {
		t.Error("Esc on the registry card kept the pending sign-in")
	}
	// Prompts that have nothing to do say so or do nothing.
	m.promptKind = promptKindAIEdit
	if cmd, ok := m.handleAIPromptSubmit("x"); !ok || cmd != nil {
		t.Error("edit instruction without a target")
	}
	m.promptKind = promptKindAIOAuthCode
	if cmd, ok := m.handleAIPromptSubmit("code"); !ok || cmd == nil {
		t.Error("an OAuth code without a sign-in must say so")
	}
	for _, k := range []pickerKindEnum{pickerKindAIAttach, pickerKindAISkills, pickerKindRunsMore, pickerKindMCPServeStatus} {
		m.pickerKind = k
		if _, ok := m.handleAIPickerSelect("no-such-row"); !ok {
			t.Errorf("picker kind %v not routed", k)
		}
	}
}

// The code exchange with no network ends in "Claude sign-in failed".
func TestCovAIClaudeExchangeFails(t *testing.T) {
	m := aiTestModel(t)
	covStubOpen(t, false)
	m.beginClaudeOAuth()
	cmd := m.onClaudeCodePasted("code-cov#" + m.aiS().pkce.State)
	done, ok := cmd().(aiOAuthDoneMsg)
	if !ok || done.err == nil {
		t.Fatalf("exchange without a network: %+v", done)
	}
	m.onAIOAuthDone(done)
	if !strings.Contains(covToastText(&m), "Claude sign-in failed") || m.aiS().pkce != nil {
		t.Errorf("toast = %q", covToastText(&m))
	}
}
