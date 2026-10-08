package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolvePrecedence(t *testing.T) {
	s := Settings{Provider: "anthropic", Model: "file-model"}
	c := Credentials{Keys: map[string]string{"anthropic": "file-key-123456"}}

	cfg := Resolve(s, c, envMap(nil))
	if cfg.Kind != KindAnthropic || cfg.APIKey != "file-key-123456" || cfg.Model != "file-model" || cfg.InlineModel != ModelClaudeHaiku {
		t.Fatalf("file config: %+v", cfg)
	}
	cfg = Resolve(s, c, envMap(map[string]string{"ANTHROPIC_API_KEY": "env-key-123456", "TERMOCODE_AI_MODEL": "env-model"}))
	if cfg.APIKey != "env-key-123456" || cfg.Model != "env-model" {
		t.Fatalf("env must win: %+v", cfg)
	}
	cfg = Resolve(s, c, envMap(map[string]string{"TERMOCODE_AI_PROVIDER": "ollama", "OLLAMA_HOST": "box:11434"}))
	if cfg.Kind != KindOllama || cfg.BaseURL != "http://box:11434/v1" || !cfg.Configured() {
		t.Fatalf("ollama: %+v", cfg)
	}
	// Auto-detect from a vendor key with no provider set.
	cfg = Resolve(Settings{}, Credentials{}, envMap(map[string]string{"OPENAI_API_KEY": "sk-abcdefghijklmnopqrstu"}))
	if cfg.Kind != KindOpenAI || !cfg.Configured() || cfg.BaseURL != DefaultBaseURL(KindOpenAI) {
		t.Fatalf("auto openai: %+v", cfg)
	}
	if Resolve(Settings{}, Credentials{}, envMap(nil)).Configured() {
		t.Fatal("nothing set must be unconfigured")
	}
	if Resolve(Settings{Provider: "none"}, c, envMap(map[string]string{"ANTHROPIC_API_KEY": "x-12345678"})).Configured() {
		t.Fatal("provider none disables AI")
	}
}

func TestSettingsRoundTripKeepsOtherKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"theme":"one-dark","tab_size":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	off := false
	if err := SaveSettings(path, Settings{Provider: "openai", Model: "m", Inline: &off}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"theme": "one-dark"`) || !strings.Contains(string(b), `"ai_provider": "openai"`) {
		t.Fatalf("merged: %s", b)
	}
	s := LoadSettings(path)
	if s.Provider != "openai" || s.InlineEnabled() {
		t.Fatalf("loaded: %+v", s)
	}
	if !(Settings{}).InlineEnabled() {
		t.Fatal("inline defaults on")
	}
}

func TestCredentialsFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai", "credentials.json")
	c := Credentials{Keys: map[string]string{"openai": "sk-filemode-abcdefghijkl"}}
	if err := SaveCredentials(path, c); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	got, err := LoadCredentials(path)
	if err != nil || got.Keys["openai"] != c.Keys["openai"] {
		t.Fatalf("load: %+v %v", got, err)
	}
	if _, err := LoadCredentials(filepath.Join(t.TempDir(), "missing.json")); err != nil {
		t.Fatal("missing file is not an error")
	}
}

func TestRedact(t *testing.T) {
	RegisterSecret("my-very-secret-value")
	in := `failed: my-very-secret-value; key sk-ant-api03-ABCDEFGHIJKL; Authorization: Bearer eyJhbGciOi.xyz; {"access_token":"tokenvalue123"} x-api-key: abcdefgh12345 sk-proj-abcdefghijklmnopqrs`
	out := Redact(in)
	for _, leak := range []string{"my-very-secret-value", "sk-ant-api03", "eyJhbGciOi", "tokenvalue123", "abcdefgh12345", "sk-proj-abc"} {
		if strings.Contains(out, leak) {
			t.Errorf("leaked %q in %q", leak, out)
		}
	}
	if !strings.Contains(out, "failed:") || !strings.Contains(out, "Bearer [REDACTED]") {
		t.Errorf("over-redacted: %q", out)
	}
	if Redact("plain text") != "plain text" {
		t.Error("plain text changed")
	}
}
