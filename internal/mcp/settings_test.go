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
		d.MaxResultKB != 32 || !d.TrustAnnotations || !d.Share {
		t.Errorf("defaults %+v", d)
	}
	writeFile(t, p, `{"theme":"x","mcp_enabled":false,"mcp_idle_timeout":"0","mcp_call_timeout":"2m",
		"mcp_max_result_kb":8,"mcp_trust_annotations":false,"mcp_share":false,"mcp_trusted":{"a":"h1"}}`)
	s := LoadSettings(p)
	if s.Enabled || s.IdleTimeout != 0 || s.CallTimeout != 2*time.Minute || s.MaxResultKB != 8 ||
		s.TrustAnnotations || s.Share || s.Trusted["a"] != "h1" {
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

// Spec (fix 2): every writer of config.json keeps the other keys, keeps the
// file's mode, leaves a file that is not valid JSON untouched (with an
// error), and leaves no temp file behind.
func TestConfigWritersAreSafe(t *testing.T) {
	writers := map[string]func(p string) error{
		"SaveSetting":      func(p string) error { return SaveSetting(p, "mcp_oauth_port", 8765) },
		"SaveTrusted":      func(p string) error { return SaveTrusted(p, "srv", "hash") },
		"SaveTrustedTools": func(p string) error { return SaveTrustedTools(p, "srv", "hash") },
	}
	for name, write := range writers {
		t.Run(name+" keeps other keys and mode", func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(p, []byte(`{"theme":"x","tab_size":4}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := write(p); err != nil {
				t.Fatal(err)
			}
			var m map[string]any
			b, _ := os.ReadFile(p)
			if err := json.Unmarshal(b, &m); err != nil || m["theme"] != "x" || m["tab_size"] != float64(4) {
				t.Fatalf("keys lost: %s (%v)", b, err)
			}
			if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
				t.Errorf("mode %v", st.Mode().Perm())
			}
			if es, _ := os.ReadDir(filepath.Dir(p)); len(es) != 1 {
				t.Errorf("leftover files: %v", es)
			}
		})
		t.Run(name+" leaves invalid JSON untouched", func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.json")
			const bad = "{\"theme\": \"x\", // a comment\n"
			if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := write(p); err == nil {
				t.Error("no error for a file that is not valid JSON")
			}
			if b, _ := os.ReadFile(p); string(b) != bad {
				t.Errorf("file changed: %q", b)
			}
		})
	}
}

// mcp_disabled_tools: the tools turned off in Show tools, per server.
// SaveDisabledTools keeps other keys; an empty list removes the server.
func TestSaveDisabledTools(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, p, `{"theme":"dark","mcp_disabled_tools":{"other":["x"]}}`)
	if err := SaveDisabledTools(p, "fake", []string{"write_note", "echo"}); err != nil {
		t.Fatal(err)
	}
	s := LoadSettings(p)
	if got := s.DisabledTools["fake"]; strings.Join(got, ",") != "echo,write_note" {
		t.Errorf("fake = %v", got)
	}
	if !s.ToolOff("fake", "echo") || s.ToolOff("fake", "slow") || !s.ToolOff("other", "x") {
		t.Errorf("ToolOff wrong: %+v", s.DisabledTools)
	}
	if err := SaveDisabledTools(p, "fake", nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "fake") || !strings.Contains(string(b), `"theme"`) || !strings.Contains(string(b), `"other"`) {
		t.Errorf("config = %s", b)
	}
	if err := SaveDisabledTools(p, "other", nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); strings.Contains(string(b), "mcp_disabled_tools") {
		t.Errorf("an empty map stayed: %s", b)
	}
}
