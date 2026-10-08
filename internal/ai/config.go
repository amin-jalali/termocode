package ai

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Settings is the NON-secret AI configuration, stored in config.json next
// to the theme and editor settings (read-modify-write, unknown keys kept).
type Settings struct {
	Provider    string   `json:"ai_provider,omitempty"`
	Model       string   `json:"ai_model,omitempty"`
	BaseURL     string   `json:"ai_base_url,omitempty"`
	Inline      *bool    `json:"ai_inline,omitempty"`
	InlineModel string   `json:"ai_inline_model,omitempty"`
	AutoApprove []string `json:"ai_auto_approve,omitempty"`
}

// InlineEnabled reports the ai_inline setting (default on).
func (s Settings) InlineEnabled() bool { return s.Inline == nil || *s.Inline }

// OAuthTokens is a stored Claude OAuth session.
type OAuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAtMs  int64  `json:"expires_at_ms"`
}

// Credentials holds the secrets, stored in ai/credentials.json with mode
// 0600. Keys maps a provider kind to its API key.
type Credentials struct {
	Keys        map[string]string `json:"keys,omitempty"`
	ClaudeOAuth *OAuthTokens      `json:"claude_oauth,omitempty"`
}

// ConfigDir is termocode's config directory ($XDG_CONFIG_HOME/termocode or
// ~/.config/termocode).
func ConfigDir() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "termocode"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "termocode"), nil
}

// ConfigPath is config.json.
func ConfigPath() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

// CredentialsPath is ai/credentials.json.
func CredentialsPath() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "ai", "credentials.json"), nil
}

// LoadSettings reads the ai_* keys from config.json at path. Missing or
// corrupt files yield zero Settings (never an error that blocks startup).
func LoadSettings(path string) Settings {
	var s Settings
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

// SaveSettings merges s into config.json at path, keeping every other key.
// Empty strings remove their key.
func SaveSettings(path string, s Settings) error {
	merged := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &merged)
	}
	set := func(k, v string) {
		if v == "" {
			delete(merged, k)
		} else {
			merged[k] = v
		}
	}
	set("ai_provider", s.Provider)
	set("ai_model", s.Model)
	set("ai_base_url", s.BaseURL)
	set("ai_inline_model", s.InlineModel)
	if s.Inline != nil {
		merged["ai_inline"] = *s.Inline
	}
	if len(s.AutoApprove) > 0 {
		merged["ai_auto_approve"] = s.AutoApprove
	} else {
		delete(merged, "ai_auto_approve")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// LoadCredentials reads credentials.json. Missing → empty; corrupt →
// empty plus an error the caller may log.
func LoadCredentials(path string) (Credentials, error) {
	var c Credentials
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c, nil
		}
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Credentials{}, errors.New("ai/credentials.json is not valid JSON")
	}
	c.register()
	return c, nil
}

// SaveCredentials writes credentials.json atomically with mode 0600 (the
// directory gets 0700).
func SaveCredentials(path string, c Credentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".credentials-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	c.register()
	return os.Chmod(path, 0o600)
}

func (c Credentials) register() {
	for _, k := range c.Keys {
		RegisterSecret(k)
	}
	if c.ClaudeOAuth != nil {
		RegisterSecret(c.ClaudeOAuth.AccessToken)
		RegisterSecret(c.ClaudeOAuth.RefreshToken)
	}
}

// envKeyFor names the vendor environment variable for a kind's API key.
func envKeyFor(kind string) string {
	switch kind {
	case KindAnthropic:
		return "ANTHROPIC_API_KEY"
	case KindOpenAI:
		return "OPENAI_API_KEY"
	case KindOpenRouter:
		return "OPENROUTER_API_KEY"
	}
	return ""
}

// Resolve merges settings, stored credentials and the environment into a
// provider Config. Environment variables win over files:
//
//	TERMOCODE_AI_PROVIDER / _MODEL / _BASE_URL / _API_KEY
//	ANTHROPIC_API_KEY, OPENAI_API_KEY, OPENROUTER_API_KEY, OLLAMA_HOST
//
// With no provider set anywhere, an Anthropic or OpenAI key (env first)
// selects that provider. getenv is os.Getenv in production.
func Resolve(s Settings, c Credentials, getenv func(string) string) Config {
	if getenv == nil {
		getenv = os.Getenv
	}
	kind := strings.ToLower(strings.TrimSpace(getenv("TERMOCODE_AI_PROVIDER")))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(s.Provider))
	}
	if kind == "none" || kind == "off" {
		return Config{}
	}
	keyFor := func(k string) string {
		if v := getenv("TERMOCODE_AI_API_KEY"); v != "" && (kind == "" || kind == k) {
			return v
		}
		if e := envKeyFor(k); e != "" {
			if v := getenv(e); v != "" {
				return v
			}
		}
		return c.Keys[k]
	}
	if kind == "" {
		switch {
		case getenv("ANTHROPIC_API_KEY") != "":
			kind = KindAnthropic
		case getenv("OPENAI_API_KEY") != "":
			kind = KindOpenAI
		case c.Keys[KindAnthropic] != "":
			kind = KindAnthropic
		case c.Keys[KindOpenAI] != "":
			kind = KindOpenAI
		default:
			return Config{}
		}
	}
	cfg := Config{Kind: kind, Model: s.Model, BaseURL: s.BaseURL, InlineModel: s.InlineModel}
	if kind != KindClaudeOAuth && kind != KindOllama && kind != KindLMStudio {
		cfg.APIKey = keyFor(kind)
	}
	if v := getenv("TERMOCODE_AI_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := getenv("TERMOCODE_AI_BASE_URL"); v != "" {
		cfg.BaseURL = v
	} else if kind == KindOllama && getenv("OLLAMA_HOST") != "" {
		h := trimSlash(getenv("OLLAMA_HOST"))
		if !strings.HasPrefix(h, "http") {
			h = "http://" + h
		}
		cfg.BaseURL = h + "/v1"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL(kind)
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel(kind)
	}
	if cfg.InlineModel == "" {
		cfg.InlineModel = DefaultInlineModel(kind, cfg.Model)
	}
	RegisterSecret(cfg.APIKey)
	return cfg
}

// ── Redaction ─────────────────────────────────────────────────────────

var (
	secretsMu sync.RWMutex
	secrets   = map[string]bool{}
)

// RegisterSecret adds a literal secret Redact must always hide. Short
// values (< 8 chars) are ignored to avoid shredding normal text.
func RegisterSecret(s string) {
	s = strings.TrimSpace(s)
	if len(s) < 8 {
		return
	}
	secretsMu.Lock()
	secrets[s] = true
	secretsMu.Unlock()
}

var redactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{8,}`),
	regexp.MustCompile(`sk-(?:or-|proj-)?[A-Za-z0-9_\-]{16,}`),
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=\-]{8,}`),
	regexp.MustCompile(`(?i)(x-api-key["':=\s]+)[^\s"',}]{8,}`),
	regexp.MustCompile(`(?i)("?(?:access_token|refresh_token|api_key|apikey)"?\s*[:=]\s*"?)[^"\s,}&]{8,}`),
	regexp.MustCompile(`(?i)([?&](?:code|key|token)=)[^&\s"]{8,}`),
}

// Redact hides API keys, OAuth tokens and bearer credentials in s. Every
// AI error is passed through it before it reaches a toast or errors.log.
func Redact(s string) string {
	if s == "" {
		return s
	}
	secretsMu.RLock()
	list := make([]string, 0, len(secrets))
	for k := range secrets {
		list = append(list, k)
	}
	secretsMu.RUnlock()
	sort.Slice(list, func(i, j int) bool { return len(list[i]) > len(list[j]) })
	for _, k := range list {
		s = strings.ReplaceAll(s, k, "[REDACTED]")
	}
	for i, re := range redactPatterns {
		if i < 2 {
			s = re.ReplaceAllString(s, "[REDACTED]")
			continue
		}
		s = re.ReplaceAllString(s, "${1}[REDACTED]")
	}
	return s
}
