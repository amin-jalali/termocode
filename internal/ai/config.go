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
	"time"
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
	MaxSteps    int      `json:"ai_max_steps,omitempty"` // 0: DefaultMaxSteps
	// EditReview is "review" (default: the agent's file changes open as a
	// diff to accept or reject) or "auto" (they are written after the
	// confirm dialog, or at once when fileWrite is auto-approved).
	EditReview string `json:"ai_edit_review,omitempty"`
}

// Values of ai_edit_review.
const (
	EditReviewOn   = "review"
	EditReviewAuto = "auto"
)

// InlineEnabled reports the ai_inline setting (default on).
func (s Settings) InlineEnabled() bool { return s.Inline == nil || *s.Inline }

// ReviewEdits reports whether the agent's file changes are reviewed as a
// diff (ai_edit_review, default review).
func (s Settings) ReviewEdits() bool { return s.EditReview != EditReviewAuto }

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
	// MCPSecrets are the values of ${secret:NAME} in mcp.json (tokens,
	// connection strings), so mcp.json itself never holds a secret.
	MCPSecrets map[string]string `json:"mcp_secrets,omitempty"`
	// MCPOAuth are the OAuth sign-ins of remote MCP servers, by server
	// name (internal/mcp/auth).
	MCPOAuth map[string]MCPOAuth `json:"mcp_oauth,omitempty"`
	// MCPAccounts are the account labels of servers that have no OAuth
	// sign-in (the gh and token ways), by server name.
	MCPAccounts map[string]string `json:"mcp_accounts,omitempty"`
}

// MCPOAuth is one MCP server's OAuth sign-in. Issuer and Resource bind
// the tokens: they are only sent to Resource (the server URL) and only
// refreshed at this issuer's TokenEndpoint.
type MCPOAuth struct {
	Issuer        string          `json:"issuer"`
	Resource      string          `json:"resource"`
	TokenEndpoint string          `json:"token_endpoint"`
	ClientID      string          `json:"client_id"`
	ClientSecret  string          `json:"client_secret,omitempty"`
	AuthMethod    string          `json:"token_endpoint_auth_method,omitempty"`
	RedirectURI   string          `json:"redirect_uri,omitempty"`
	Registration  json.RawMessage `json:"registration,omitempty"`
	AccessToken   string          `json:"access_token"`
	RefreshToken  string          `json:"refresh_token,omitempty"`
	ExpiresAtMs   int64           `json:"expires_at_ms,omitempty"`
	Scope         string          `json:"scope,omitempty"`
	// RevocationEndpoint is where Sign out revokes the tokens ("" none).
	RevocationEndpoint string `json:"revocation_endpoint,omitempty"`
	// Account is who signed in ("amin-jalali"), from the service's
	// identity tool; shown as "connected as …", never sent anywhere.
	Account string `json:"account,omitempty"`
}

// Expired reports a token that ends within a minute (0: no end known).
func (e MCPOAuth) Expired(now time.Time) bool {
	return e.ExpiresAtMs != 0 && now.UnixMilli() >= e.ExpiresAtMs-60_000
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
	set("ai_edit_review", s.EditReview)
	if s.Inline != nil {
		merged["ai_inline"] = *s.Inline
	}
	if len(s.AutoApprove) > 0 {
		merged["ai_auto_approve"] = s.AutoApprove
	} else {
		delete(merged, "ai_auto_approve")
	}
	if s.MaxSteps > 0 {
		merged["ai_max_steps"] = s.MaxSteps
	} else {
		delete(merged, "ai_max_steps")
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
	// The folder may be older and wider (Group S6: shared servers).
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
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
	for _, v := range c.MCPSecrets {
		RegisterSecret(v)
	}
	for _, e := range c.MCPOAuth {
		RegisterSecret(e.AccessToken)
		RegisterSecret(e.RefreshToken)
		RegisterSecret(e.ClientSecret)
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

// RedactSecrets hides only the registered literal secrets (no
// patterns): the app passes every agent tool result through it, so a
// secret the IDE knows (an API key, a password from the project's .env)
// never reaches the model, while ordinary text stays as written.
func RedactSecrets(s string) string {
	if s == "" {
		return s
	}
	secretsMu.RLock()
	list := make([]string, 0, len(secrets))
	for k := range secrets {
		if strings.Contains(s, k) {
			list = append(list, k)
		}
	}
	secretsMu.RUnlock()
	sort.Slice(list, func(i, j int) bool { return len(list[i]) > len(list[j]) })
	for _, k := range list {
		s = strings.ReplaceAll(s, k, "[REDACTED]")
	}
	return s
}

// redactKeywords[i] are lower-case words one of which is in every match
// of redactPatterns[i].
var redactKeywords = [][]string{
	{"sk-ant-"}, {"sk-"}, {"bearer"}, {"x-api-key"},
	{"access_token", "refresh_token", "api_key", "apikey"}, {"code=", "key=", "token="},
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
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
	// A pattern runs only when its keyword is in the text: the
	// case-insensitive regexps cost ~2 ms each on a 32 KB HTTP body.
	lower := strings.ToLower(s)
	for i, re := range redactPatterns {
		if !containsAny(lower, redactKeywords[i]) {
			continue
		}
		if i < 2 {
			s = re.ReplaceAllString(s, "[REDACTED]")
			continue
		}
		s = re.ReplaceAllString(s, "${1}[REDACTED]")
	}
	return s
}
