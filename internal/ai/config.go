package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/cfgfile"
	secretstore "github.com/amin-jalali/termocode/internal/secrets"
)

// Settings is the NON-secret AI configuration, stored in config.json next
// to the theme and editor settings (read-modify-write, unknown keys kept).
type Settings struct {
	Provider    string `json:"ai_provider,omitempty"`
	Model       string `json:"ai_model,omitempty"`
	BaseURL     string `json:"ai_base_url,omitempty"`
	Inline      *bool  `json:"ai_inline,omitempty"`
	InlineModel string `json:"ai_inline_model,omitempty"`
	// NextEdit is ai_next_edit: next edit suggestions (nil: same as
	// ai_inline).
	NextEdit    *bool    `json:"ai_next_edit,omitempty"`
	AutoApprove []string `json:"ai_auto_approve,omitempty"`
	MaxSteps    int      `json:"ai_max_steps,omitempty"` // 0: DefaultMaxSteps
	// EditReview is "review" (default: the agent's file changes open as a
	// diff to accept or reject) or "auto" (they are written after the
	// confirm dialog, or at once when fileWrite is auto-approved).
	EditReview string `json:"ai_edit_review,omitempty"`
	// Prices override or add model prices ($ per million tokens, by
	// model id prefix) over the built-in table (pricing.go).
	// ContextWindows do the same for context windows (tokens). Both are
	// edited by hand; SaveSettings leaves them as they are.
	Prices         map[string]Price `json:"ai_prices,omitempty"`
	ContextWindows map[string]int   `json:"ai_context_windows,omitempty"`
	// Models are the named model slots agent profiles use (profiles.go);
	// ParallelMax is how many pipeline steps may run at once (0: 1).
	// Both are edited by hand; SaveSettings leaves them as they are.
	Models      map[string]ModelSlot `json:"ai_models,omitempty"`
	ParallelMax int                  `json:"ai_parallel_max,omitempty"`
	// UntilDoneRounds (0: DefaultUntilDoneRounds) and UntilDoneBudgetUSD
	// (nil: DefaultUntilDoneBudgetUSD; 0 set on purpose: no budget) are
	// the limits of a "work until done" run whose profile does not set
	// loop.max_rounds / budget_usd.
	UntilDoneRounds    int      `json:"ai_until_done_rounds,omitempty"`
	UntilDoneBudgetUSD *float64 `json:"ai_until_done_budget_usd,omitempty"`
	// DayBudgetUSD (nil or 0: none) is a daily spend line the Runs page
	// draws today's spend against. It stops nothing; edited by hand.
	DayBudgetUSD *float64 `json:"ai_day_budget_usd,omitempty"`
	// Worktrees is "off" to keep parallel agents in the user's tree
	// (default: each gets its own git worktree; worktree.go).
	Worktrees string `json:"ai_worktrees,omitempty"`
	// PlanApproval is ai_plan_approval (G-PLAN): "ask" (default), "auto"
	// or "always" (planapproval.go).
	PlanApproval string `json:"ai_plan_approval,omitempty"`
	// PromptCache is ai_prompt_cache: "5m" (default), "1h" or "off"
	// (prompt_cache.go). Edited by hand; SaveSettings leaves it.
	PromptCache string `json:"ai_prompt_cache,omitempty"`
	// Reasoning is ai_reasoning: the reasoning level of the chat and of
	// the main slot (reasoning.go; "": the model's default). Resolve
	// leaves it out of the main Config, so ghost text never thinks; the
	// chat sends it per request. Edited by hand.
	Reasoning string `json:"ai_reasoning,omitempty"`
	// Fallback is ai_fallback: the main slot's backup models (slot names
	// or "<provider>/<model>"; fallback.go). Edited by hand.
	Fallback []string `json:"ai_fallback,omitempty"`
}

// CheckUntilDoneRounds checks an ai_until_done_rounds value.
func CheckUntilDoneRounds(n int) error {
	if n < 1 || n > agentdag.MaxRounds {
		return fmt.Errorf("ai_until_done_rounds must be 1 to %d (it is %d)", agentdag.MaxRounds, n)
	}
	return nil
}

// CheckUntilDoneBudget checks an ai_until_done_budget_usd value (0 = no
// budget).
func CheckUntilDoneBudget(usd float64) error {
	if math.IsNaN(usd) || math.IsInf(usd, 0) || usd < 0 {
		return fmt.Errorf("ai_until_done_budget_usd must be a dollar amount above 0, or 0 for no budget (it is %v)", usd)
	}
	return nil
}

// UntilDone is the effective rounds and budget of "work until done"
// runs; a value that is out of range falls back to its default and is
// named in problems.
func (s Settings) UntilDone() (rounds int, budget float64, problems []string) {
	rounds, budget = DefaultUntilDoneRounds, DefaultUntilDoneBudgetUSD
	if s.UntilDoneRounds != 0 {
		if err := CheckUntilDoneRounds(s.UntilDoneRounds); err != nil {
			problems = append(problems, "config.json: "+err.Error()+"; using "+strconv.Itoa(rounds))
		} else {
			rounds = s.UntilDoneRounds
		}
	}
	if s.UntilDoneBudgetUSD != nil {
		if err := CheckUntilDoneBudget(*s.UntilDoneBudgetUSD); err != nil {
			problems = append(problems, fmt.Sprintf("config.json: %s; using $%.2f", err, budget))
		} else {
			budget = *s.UntilDoneBudgetUSD
		}
	}
	return rounds, budget, problems
}

// MaxParallel caps ai_parallel_max.
const MaxParallel = 8

// Parallel is the effective ai_parallel_max: 1 by default, at most
// MaxParallel.
func (s Settings) Parallel() int {
	switch {
	case s.ParallelMax < 1:
		return 1
	case s.ParallelMax > MaxParallel:
		return MaxParallel
	}
	return s.ParallelMax
}

// DefaultPlannedParallel is how many nodes of a planned run (a profile
// with a planner step) run at once when ai_parallel_max is not set.
const DefaultPlannedParallel = 3

// PlannedParallel is the effective ai_parallel_max of a planned run:
// DefaultPlannedParallel when it is not set, at most MaxParallel.
func (s Settings) PlannedParallel() int {
	if s.ParallelMax < 1 {
		return DefaultPlannedParallel
	}
	return s.Parallel()
}

// Values of ai_edit_review.
const (
	EditReviewOn   = "review"
	EditReviewAuto = "auto"
)

// InlineEnabled reports the ai_inline setting (default on).
func (s Settings) InlineEnabled() bool { return s.Inline == nil || *s.Inline }

// NextEditEnabled reports the ai_next_edit setting: on when ai_inline is
// on, unless it is set on purpose.
func (s Settings) NextEditEnabled() bool {
	if s.NextEdit != nil {
		return *s.NextEdit
	}
	return s.InlineEnabled()
}

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

	// locked marks a view loaded while the secret store was locked: it is
	// empty and SaveCredentials refuses it.
	locked bool
}

// Locked reports a view loaded while the secret store was locked (SS-3):
// its values are missing, not deleted.
func (c Credentials) Locked() bool { return c.locked }

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

var credentialStore struct {
	sync.RWMutex
	store secretstore.Store
	path  string
}

// SetSecretStore sets the store used by credential loads and saves. A nil
// store keeps the original credentials.json behavior.
func SetSecretStore(store secretstore.Store) {
	credentialStore.Lock()
	credentialStore.store = store
	credentialStore.path = ""
	credentialStore.Unlock()
}

// SetSecretStoreForPath sets the store used for one credentials file.
func SetSecretStoreForPath(path string, store secretstore.Store) {
	credentialStore.Lock()
	credentialStore.store = store
	credentialStore.path = path
	credentialStore.Unlock()
}

func currentSecretStore(path string) secretstore.Store {
	credentialStore.RLock()
	defer credentialStore.RUnlock()
	if credentialStore.path != "" && credentialStore.path != path {
		return nil
	}
	return credentialStore.store
}

// LookupMCPSecret resolves a named MCP secret through the configured store.
func LookupMCPSecret(name string) (string, bool) {
	path, _ := CredentialsPath()
	store := currentSecretStore(path)
	if store == nil {
		return "", false
	}
	v, ok, err := store.Get("mcp.secret." + name)
	if err != nil || !ok {
		return "", false
	}
	RegisterSecret(v)
	return v, true
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
	return cfgfile.Update(path, func(merged map[string]any) error {
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
		set("ai_worktrees", s.Worktrees)
		set("ai_plan_approval", s.PlanApproval)
		if s.Inline != nil {
			merged["ai_inline"] = *s.Inline
		}
		if s.NextEdit != nil {
			merged["ai_next_edit"] = *s.NextEdit
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
		if s.UntilDoneRounds > 0 {
			merged["ai_until_done_rounds"] = s.UntilDoneRounds
		} else {
			delete(merged, "ai_until_done_rounds")
		}
		if s.UntilDoneBudgetUSD != nil {
			merged["ai_until_done_budget_usd"] = *s.UntilDoneBudgetUSD
		} else {
			delete(merged, "ai_until_done_budget_usd")
		}
		return nil
	})
}

// LoadCredentials reads credentials.json. Missing → empty; corrupt →
// empty plus an error the caller may log.
func LoadCredentials(path string) (Credentials, error) {
	if store := currentSecretStore(path); store != nil {
		return loadCredentialsFromStore(path, store)
	}
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
	if store := currentSecretStore(path); store != nil {
		return saveCredentialsToStore(path, c, store)
	}
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

func loadCredentialsFromStore(path string, store secretstore.Store) (Credentials, error) {
	if store.Locked() {
		// The values are behind the passphrase: an empty view, marked so
		// SaveCredentials refuses it (it must not delete what it never saw).
		return Credentials{locked: true}, nil
	}
	// The plain backend is the existing file and may hold names the index
	// does not know yet; other backends are listed by the index alone.
	old, err := loadCredentialsFromFile(path)
	if err != nil {
		return Credentials{}, err
	}
	oldValues := credentialEntries(old, true)
	names := map[string]bool{}
	for n := range oldValues {
		names[n] = true
	}
	for _, n := range store.Names() {
		if credentialName(n) {
			names[n] = true
		}
	}
	var c Credentials
	for n := range names {
		v := storeCredentialValue(store, n, oldValues[n])
		if v == "" {
			continue
		}
		switch {
		case n == "ai.claude_oauth":
			var t OAuthTokens
			if json.Unmarshal([]byte(v), &t) == nil {
				c.ClaudeOAuth = &t
			}
		case strings.HasPrefix(n, "ai.key."):
			c.Keys = setEntry(c.Keys, strings.TrimPrefix(n, "ai.key."), v)
		case strings.HasPrefix(n, "mcp.secret."):
			c.MCPSecrets = setEntry(c.MCPSecrets, strings.TrimPrefix(n, "mcp.secret."), v)
		case strings.HasPrefix(n, "mcp.oauth."):
			var o MCPOAuth
			if json.Unmarshal([]byte(v), &o) == nil {
				if c.MCPOAuth == nil {
					c.MCPOAuth = map[string]MCPOAuth{}
				}
				c.MCPOAuth[strings.TrimPrefix(n, "mcp.oauth.")] = o
			}
		case strings.HasPrefix(n, "mcp.account."):
			c.MCPAccounts = setEntry(c.MCPAccounts, strings.TrimPrefix(n, "mcp.account."), v)
		}
	}
	c.register()
	return c, nil
}

func setEntry(m map[string]string, k, v string) map[string]string {
	if m == nil {
		m = map[string]string{}
	}
	m[k] = v
	return m
}

// credentialName reports a store name the Credentials view owns. Other
// names (db.*, http.secret.*) belong to other features and are never
// touched by SaveCredentials.
func credentialName(n string) bool {
	if n == "ai.claude_oauth" {
		return true
	}
	for _, p := range []string{"ai.key.", "mcp.secret.", "mcp.oauth.", "mcp.account."} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// credentialEntries flattens a view into store names and values. With
// accounts false a nil MCPAccounts map is left out (see SaveCredentials).
func credentialEntries(c Credentials, accounts bool) map[string]string {
	out := map[string]string{}
	for k, v := range c.Keys {
		out["ai.key."+k] = v
	}
	if c.ClaudeOAuth != nil {
		out["ai.claude_oauth"] = marshalCredential(c.ClaudeOAuth)
	}
	for k, v := range c.MCPSecrets {
		out["mcp.secret."+k] = v
	}
	for k, v := range c.MCPOAuth {
		out["mcp.oauth."+k] = marshalCredential(v)
	}
	if accounts || c.MCPAccounts != nil {
		for k, v := range c.MCPAccounts {
			out["mcp.account."+k] = v
		}
	}
	return out
}

func storeCredentialValue(store secretstore.Store, name, old string) string {
	v, ok, err := store.Get(name)
	if err == nil && ok {
		return v
	}
	if old != "" && err == nil {
		if store.Set(name, old) == nil {
			return old
		}
	}
	return old
}

// saveCredentialsToStore makes the store hold exactly the view: names
// the view dropped are deleted, changed values are set. An unchanged
// value is not written again (an age write costs a scrypt run, and a
// ${cmd:} / ${env:} reference must not be replaced by what it resolved
// to). A nil MCPAccounts map means "not loaded": the stored account
// labels stay as they are.
func saveCredentialsToStore(path string, c Credentials, store secretstore.Store) error {
	if c.locked {
		return secretstore.ErrLocked
	}
	want := credentialEntries(c, false)
	// Seed the old names from an existing old-format file too, so keys
	// removed from the in-memory view are deleted through the store.
	old, _ := loadCredentialsFromFile(path)
	have := map[string]bool{}
	for n := range credentialEntries(old, true) {
		have[n] = true
	}
	for _, n := range store.Names() {
		if credentialName(n) {
			have[n] = true
		}
	}
	for n := range have {
		if _, keep := want[n]; keep {
			continue
		}
		if strings.HasPrefix(n, "mcp.account.") && c.MCPAccounts == nil {
			continue
		}
		if _, _, err := store.Get(n); err != nil {
			// A reference that does not resolve now (a failing command, an
			// empty variable) is missing from the view, not removed by the
			// user: keep it.
			continue
		}
		if err := store.Delete(n); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(want))
	for n := range want {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if v, ok, err := store.Get(n); err == nil && ok && v == want[n] {
			continue
		}
		if err := store.Set(n, want[n]); err != nil {
			return err
		}
	}
	c.register()
	return nil
}

func loadCredentialsFromFile(path string) (Credentials, error) {
	var c Credentials
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Credentials{}, errors.New("ai/credentials.json is not valid JSON")
	}
	return c, nil
}

func marshalCredential(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
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
	cfg := Config{Kind: kind, Model: s.Model, BaseURL: s.BaseURL, InlineModel: s.InlineModel, PromptCache: promptCacheMode(s.PromptCache)}
	if kind != KindClaudeOAuth && kind != KindOllama && kind != KindLMStudio {
		cfg.APIKey = keyFor(kind)
	}
	if v := getenv("TERMOCODE_AI_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := getenv("TERMOCODE_AI_BASE_URL"); v != "" {
		cfg.BaseURL = v
	} else if kind == KindOllama && getenv("OLLAMA_HOST") != "" {
		cfg.BaseURL = ollamaHostURL(getenv("OLLAMA_HOST"))
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

// ollamaHostURL turns OLLAMA_HOST ("box:11434") into the OpenAI-style
// base URL.
func ollamaHostURL(host string) string {
	h := trimSlash(host)
	if !strings.HasPrefix(h, "http") {
		h = "http://" + h
	}
	return h + "/v1"
}

// ── Redaction ─────────────────────────────────────────────────────────

var (
	secretsMu sync.RWMutex
	secrets   = map[string]bool{}
	// secretsByLen is the secrets, longest first (a longer secret that
	// holds a shorter one is hidden whole). It is replaced, never
	// changed, so a reader may keep the slice it read.
	secretsByLen []string
)

// secretList is the registered secrets, longest first.
func secretList() []string {
	secretsMu.RLock()
	defer secretsMu.RUnlock()
	return secretsByLen
}

// RegisterSecret adds a literal secret Redact must always hide. Short
// values (< 8 chars) are ignored to avoid shredding normal text.
func RegisterSecret(s string) {
	s = strings.TrimSpace(s)
	if len(s) < 8 {
		return
	}
	if _, err := secretstore.ParseRef(s); err == nil {
		return // a reference is not a secret; what it resolves to is registered
	}
	secretsMu.Lock()
	defer secretsMu.Unlock()
	if secrets[s] {
		return
	}
	secrets[s] = true
	list := make([]string, 0, len(secrets))
	for k := range secrets {
		list = append(list, k)
	}
	sort.Slice(list, func(i, j int) bool { return len(list[i]) > len(list[j]) })
	secretsByLen = list
}

var redactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{8,}`),
	regexp.MustCompile(`sk-(?:or-|proj-)?[A-Za-z0-9_\-]{16,}`),
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=\-]{8,}`),
	regexp.MustCompile(`(?i)(x-api-key["':=\s]+)[^\s"',}]{8,}`),
	regexp.MustCompile(`(?i)("?(?:access_token|refresh_token|api_key|apikey)"?\s*[:=]\s*"?)[^"\s,}&]{8,}`),
	regexp.MustCompile(`(?i)([?&](?:code|key|token)=)[^&\s"]{8,}`),
}

// TokenPrefixes start well-known service tokens (GitHub, GitLab, Slack,
// Google, Stripe, Linear, OpenAI). Redact hides a word that starts with
// one and goes on for at least TokenMinTail more token characters.
var TokenPrefixes = []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_", "glpat-", "xoxb-", "xoxp-",
	"xoxa-", "xapp-", "AIza", "sk-", "sk_live_", "rk_live_", "lin_api_"}

// TokenMinTail is how many characters a token has after its prefix.
const TokenMinTail = 12

// tokenPrefixRe matches a prefixed token at the start of a word.
var tokenPrefixRe = func() *regexp.Regexp {
	alts := make([]string, len(TokenPrefixes))
	for i, p := range TokenPrefixes {
		alts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile(`(^|[^A-Za-z0-9_])(?:` + strings.Join(alts, "|") + `)[A-Za-z0-9_\-]{` + strconv.Itoa(TokenMinTail) + `,}`)
}()

// redactTokenPrefixes hides the prefixed tokens in s.
func redactTokenPrefixes(s string) string {
	for _, p := range TokenPrefixes {
		if strings.Contains(s, p) {
			return tokenPrefixRe.ReplaceAllString(s, "${1}[REDACTED]")
		}
	}
	return s
}

// RedactSecrets hides only the registered literal secrets (no
// patterns): the app passes every agent tool result through it, so a
// secret the IDE knows (an API key, a password from the project's .env)
// never reaches the model, while ordinary text stays as written.
func RedactSecrets(s string) string {
	if s == "" {
		return s
	}
	for _, k := range secretList() {
		if strings.Contains(s, k) {
			s = strings.ReplaceAll(s, k, "[REDACTED]")
		}
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
	for _, k := range secretList() {
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
	return redactTokenPrefixes(s)
}
