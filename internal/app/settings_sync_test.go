package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsExportableConfigFile(t *testing.T) {
	cases := map[string]bool{
		"config.json":                          true,
		"keymap.json":                          true,
		"snippets.json":                        true,
		"user_theme.json":                      true,
		"commands.json":                        true,
		"session.json":                         false,
		"recents.json":                         false,
		"workspaces.json":                      false,
		"palette_recents.json":                 false,
		"errors.log":                           false,
		"github_token.json":                    false,
		"credentials.json":                     false,
		"settings-backup-20260101-000000.json": false,
		"../evil.json":                         false,
		"sub/config.json":                      false,
	}
	for name, want := range cases {
		if got := isExportableConfigFile(name); got != want {
			t.Errorf("isExportableConfigFile(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestRedactSecrets(t *testing.T) {
	v := map[string]any{
		"theme":   "x",
		"api_key": "sk-123",
		"nested":  map[string]any{"Token": "t", "keep": 1.0},
		"list":    []any{map[string]any{"password": "p", "id": "a"}},
	}
	got := redactSecrets(v).(map[string]any)
	if _, ok := got["api_key"]; ok {
		t.Error("api_key should be dropped")
	}
	if _, ok := got["nested"].(map[string]any)["Token"]; ok {
		t.Error("nested Token should be dropped")
	}
	if got["nested"].(map[string]any)["keep"] != 1.0 {
		t.Error("non-secret nested key should stay")
	}
	item := got["list"].([]any)[0].(map[string]any)
	if _, ok := item["password"]; ok || item["id"] != "a" {
		t.Errorf("list item not redacted correctly: %v", item)
	}
}

func TestSettingsBundleRoundTrip(t *testing.T) {
	src := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("config.json", `{"theme":"one-dark","tab_size":2}`)
	write("keymap.json", `{"alt+s":"Save"}`)
	write("session.json", `{"open":["a.go"]}`)
	write("commands.json", `[{"id":"b","cmd":"make","token":"secret"}]`)
	write("broken.json", `{not json`)
	write("errors.log", "boom")

	data, names, err := buildSettingsBundle(src, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "commands.json,config.json,keymap.json" {
		t.Fatalf("names = %v", names)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "a.go") {
		t.Fatalf("export leaked secret or session state:\n%s", data)
	}

	files, err := parseSettingsBundle(data)
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := writeSettingsFiles(dst, files); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"one-dark"`) || !strings.Contains(string(got), `"tab_size": 2`) {
		t.Fatalf("config.json = %s", got)
	}
	if _, err := os.Stat(filepath.Join(dst, "session.json")); err == nil {
		t.Fatal("session.json must not be imported")
	}
}

func TestParseSettingsBundleRejects(t *testing.T) {
	for name, in := range map[string]string{
		"not json":    `nope`,
		"no version":  `{"files":{"config.json":{}}}`,
		"future":      `{"termocode_settings":99,"files":{"config.json":{}}}`,
		"only unsafe": `{"termocode_settings":1,"files":{"../x.json":{},"session.json":{}}}`,
	} {
		if _, err := parseSettingsBundle([]byte(in)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestExpandHome(t *testing.T) {
	t.Setenv("HOME", "/home/test")
	if got := expandHome("~/x.json"); got != "/home/test/x.json" {
		t.Errorf("expandHome = %q", got)
	}
	if got := expandHome("/abs/x.json"); got != "/abs/x.json" {
		t.Errorf("expandHome abs = %q", got)
	}
}
