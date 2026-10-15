package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The mcp_* keys of config.json, with defaults when missing or bad.
func TestLoadSettings(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	d := LoadSettings(p)
	if !d.Enabled || d.IdleTimeout != 10*time.Minute || d.CallTimeout != 60*time.Second ||
		d.MaxResultKB != 32 || !d.TrustAnnotations {
		t.Errorf("defaults %+v", d)
	}
	writeFile(t, p, `{"theme":"x","mcp_enabled":false,"mcp_idle_timeout":"0","mcp_call_timeout":"2m",
		"mcp_max_result_kb":8,"mcp_trust_annotations":false,"mcp_trusted":{"a":"h1"}}`)
	s := LoadSettings(p)
	if s.Enabled || s.IdleTimeout != 0 || s.CallTimeout != 2*time.Minute || s.MaxResultKB != 8 ||
		s.TrustAnnotations || s.Trusted["a"] != "h1" {
		t.Errorf("loaded %+v", s)
	}
	writeFile(t, p, `{"mcp_idle_timeout":"soon","mcp_max_result_kb":-1}`)
	if s := LoadSettings(p); s.IdleTimeout != 10*time.Minute || s.MaxResultKB != 32 {
		t.Errorf("bad values not defaulted: %+v", s)
	}
}

// SaveTrusted adds one name → hash and keeps every other key.
func TestSaveTrusted(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, p, `{"theme":"dark","mcp_trusted":{"old":"h0"}}`)
	if err := SaveTrusted(p, "proj", "h1"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	tr, _ := m["mcp_trusted"].(map[string]any)
	if m["theme"] != "dark" || tr["old"] != "h0" || tr["proj"] != "h1" {
		t.Errorf("config %s", b)
	}
	if s := LoadSettings(p); s.Trusted["proj"] != "h1" {
		t.Errorf("trusted %v", s.Trusted)
	}
}

// SaveTrustedTools keeps the accepted tool-list hash per server
// (mcp_trusted_tools), next to every other key.
func TestSaveTrustedTools(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, p, `{"theme":"dark","mcp_trusted":{"a":"h0"}}`)
	if err := SaveTrustedTools(p, "fake", "t1"); err != nil {
		t.Fatal(err)
	}
	s := LoadSettings(p)
	if s.TrustedTools["fake"] != "t1" || s.Trusted["a"] != "h0" {
		t.Errorf("settings %+v", s)
	}
}

// SaveSetting writes one mcp_* key (nil removes it) and keeps the rest;
// mcp_oauth_port is read back.
func TestSaveSetting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, p, `{"theme":"dark","mcp_enabled":false}`)
	if err := SaveSetting(p, "mcp_oauth_port", 8765); err != nil {
		t.Fatal(err)
	}
	if err := SaveSetting(p, "mcp_enabled", nil); err != nil {
		t.Fatal(err)
	}
	s := LoadSettings(p)
	if s.OAuthPort != 8765 || !s.Enabled {
		t.Errorf("settings %+v", s)
	}
	b, _ := os.ReadFile(p)
	if !json.Valid(b) || !strings.Contains(string(b), `"theme": "dark"`) {
		t.Errorf("config %s", b)
	}
	if err := SaveSetting(p, "theme", "x"); err == nil {
		t.Error("SaveSetting wrote a key that is not mcp_*")
	}
}
