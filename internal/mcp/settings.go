package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/cfgfile"
)

// Settings are the mcp_* keys of config.json (documented in
// docs/reference/settings.md through internal/app/docs.go; the Settings
// modal has an MCP section).
type Settings struct {
	Enabled          bool              // mcp_enabled (true): false hides every MCP tool
	IdleTimeout      time.Duration     // mcp_idle_timeout ("10m"; "0" never stops)
	CallTimeout      time.Duration     // mcp_call_timeout ("60s")
	MaxResultKB      int               // mcp_max_result_kb (32)
	TrustAnnotations bool              // mcp_trust_annotations (true)
	OAuthPort        int               // mcp_oauth_port (0: the first free of 47391–47395)
	GitHubClientID   string            // mcp_github_client_id ("" = the build-time GitHubClientID)
	Share            bool              // mcp_share (true): one local server process for every session
	Trusted          map[string]string // mcp_trusted: server name → entry hash ("Always allow")
	TrustedTools     map[string]string // mcp_trusted_tools: server name → accepted ToolsHash
	ToolSearch       string            // mcp_tool_search ("auto"): defer MCP tool schemas behind a search tool
	CatalogURL       string            // mcp_catalog_url (""): a registry API to browse instead of the official one
}

// Defaults of the mcp_* settings.
const (
	DefaultIdleTimeout = 10 * time.Minute
	DefaultCallTimeout = 60 * time.Second
	DefaultMaxResultKB = 32
)

// LoadSettings reads the mcp_* keys; missing or bad values get the
// defaults.
func LoadSettings(configPath string) Settings {
	s := Settings{Enabled: true, IdleTimeout: DefaultIdleTimeout, CallTimeout: DefaultCallTimeout,
		MaxResultKB: DefaultMaxResultKB, TrustAnnotations: true, Share: true, Trusted: map[string]string{},
		TrustedTools: map[string]string{}, ToolSearch: ToolSearchAuto}
	b, err := os.ReadFile(configPath)
	if err != nil {
		return s
	}
	var raw struct {
		Enabled          *bool             `json:"mcp_enabled"`
		IdleTimeout      string            `json:"mcp_idle_timeout"`
		CallTimeout      string            `json:"mcp_call_timeout"`
		MaxResultKB      int               `json:"mcp_max_result_kb"`
		TrustAnnotations *bool             `json:"mcp_trust_annotations"`
		OAuthPort        int               `json:"mcp_oauth_port"`
		GitHubClientID   string            `json:"mcp_github_client_id"`
		Share            *bool             `json:"mcp_share"`
		Trusted          map[string]string `json:"mcp_trusted"`
		TrustedTools     map[string]string `json:"mcp_trusted_tools"`
		ToolSearch       string            `json:"mcp_tool_search"`
		CatalogURL       string            `json:"mcp_catalog_url"`
	}
	if json.Unmarshal(b, &raw) != nil {
		return s
	}
	if raw.Enabled != nil {
		s.Enabled = *raw.Enabled
	}
	if raw.Share != nil {
		s.Share = *raw.Share
	}
	if raw.TrustAnnotations != nil {
		s.TrustAnnotations = *raw.TrustAnnotations
	}
	if d, ok := ParseDuration(raw.IdleTimeout); ok {
		s.IdleTimeout = d
	}
	if d, ok := ParseDuration(raw.CallTimeout); ok && d > 0 {
		s.CallTimeout = d
	}
	if raw.MaxResultKB > 0 {
		s.MaxResultKB = raw.MaxResultKB
	}
	if raw.OAuthPort > 0 && raw.OAuthPort < 65536 {
		s.OAuthPort = raw.OAuthPort
	}
	if ValidClientID(raw.GitHubClientID) {
		s.GitHubClientID = raw.GitHubClientID
	}
	s.ToolSearch = ParseToolSearchMode(raw.ToolSearch)
	if u := strings.TrimSpace(raw.CatalogURL); u != "" && CheckCatalogURL(u) == nil {
		s.CatalogURL = u
	}
	for k, v := range raw.Trusted {
		s.Trusted[k] = v
	}
	for k, v := range raw.TrustedTools {
		s.TrustedTools[k] = v
	}
	return s
}

// ValidClientID reports a plausible OAuth client id (letters, digits,
// . _ -; at most 100).
func ValidClientID(s string) bool {
	if s == "" || len(s) > 100 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// ParseDuration reads a duration setting: "0" or a Go duration ("90s",
// "10m"). ok=false for "" and bad values.
func ParseDuration(v string) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if v == "0" {
		return 0, true
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, false
	}
	return d, true
}

// configMu serialises the read-modify-write of config.json here.
var configMu sync.Mutex

// updateConfig applies fn to config.json's keys, keeping every other key.
func updateConfig(configPath string, fn func(map[string]any)) error {
	configMu.Lock()
	defer configMu.Unlock()
	return cfgfile.Update(configPath, func(merged map[string]any) error {
		fn(merged)
		return nil
	})
}

func saveMapEntry(configPath, key, name, value string) error {
	return updateConfig(configPath, func(merged map[string]any) {
		tr, _ := merged[key].(map[string]any)
		if tr == nil {
			tr = map[string]any{}
		}
		tr[name] = value
		merged[key] = tr
	})
}

// SaveTrusted stores "Always allow" for one entry (name → hash) in
// config.json, keeping every other key.
func SaveTrusted(configPath, name, hash string) error {
	return saveMapEntry(configPath, "mcp_trusted", name, hash)
}

// SaveTrustedTools stores the accepted tool list of a server (name →
// ToolsHash) in config.json mcp_trusted_tools.
func SaveTrustedTools(configPath, name, hash string) error {
	return saveMapEntry(configPath, "mcp_trusted_tools", name, hash)
}

// SaveSetting writes one mcp_* key of config.json (nil removes it, so the
// default applies again).
func SaveSetting(configPath, key string, value any) error {
	if !strings.HasPrefix(key, "mcp_") {
		return fmt.Errorf("%s is not an MCP setting", key)
	}
	return updateConfig(configPath, func(merged map[string]any) {
		if value == nil {
			delete(merged, key)
		} else {
			merged[key] = value
		}
	})
}
