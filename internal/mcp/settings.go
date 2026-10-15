package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Settings are the mcp_* keys of config.json (file only; documented in
// docs/reference/settings.md through internal/app/docs.go).
type Settings struct {
	Enabled          bool              // mcp_enabled (true): false hides every MCP tool
	IdleTimeout      time.Duration     // mcp_idle_timeout ("10m"; "0" never stops)
	CallTimeout      time.Duration     // mcp_call_timeout ("60s")
	MaxResultKB      int               // mcp_max_result_kb (32)
	TrustAnnotations bool              // mcp_trust_annotations (true)
	Trusted          map[string]string // mcp_trusted: server name → entry hash ("Always allow")
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
		MaxResultKB: DefaultMaxResultKB, TrustAnnotations: true, Trusted: map[string]string{}}
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
		Trusted          map[string]string `json:"mcp_trusted"`
	}
	if json.Unmarshal(b, &raw) != nil {
		return s
	}
	if raw.Enabled != nil {
		s.Enabled = *raw.Enabled
	}
	if raw.TrustAnnotations != nil {
		s.TrustAnnotations = *raw.TrustAnnotations
	}
	if d, ok := parseDuration(raw.IdleTimeout); ok {
		s.IdleTimeout = d
	}
	if d, ok := parseDuration(raw.CallTimeout); ok && d > 0 {
		s.CallTimeout = d
	}
	if raw.MaxResultKB > 0 {
		s.MaxResultKB = raw.MaxResultKB
	}
	for k, v := range raw.Trusted {
		s.Trusted[k] = v
	}
	return s
}

func parseDuration(v string) (time.Duration, bool) {
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

// SaveTrusted stores "Always allow" for one entry (name → hash) in
// config.json, keeping every other key.
func SaveTrusted(configPath, name, hash string) error {
	merged := map[string]any{}
	if b, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(b, &merged)
	}
	tr, _ := merged["mcp_trusted"].(map[string]any)
	if tr == nil {
		tr = map[string]any{}
	}
	tr[name] = hash
	merged["mcp_trusted"] = tr
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, out, 0o644)
}
