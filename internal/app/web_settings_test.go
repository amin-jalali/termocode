package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeUserConfig(t *testing.T, body string) {
	t.Helper()
	path, err := settingsConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Spec (web-easy, ADR 0005): the web panel on this computer needs no
// setting (web_panel unset is on for 127.0.0.1); "off" turns it off; only
// "on" is Explicit (SSH mode and a script's --yes need it). Bad values are
// the default and a problem line.
func TestWebSettingsDefaultsAndBadValues(t *testing.T) {
	withConfigDir(t)
	c := webSettings()
	if !c.On || c.Explicit || c.Tailnet || c.Port != 0 || c.IdleMinutes != 15 || c.MaxHours != 12 || !c.OpenBrowser || c.MaxViewers != 4 || len(c.Problems) != 0 {
		t.Fatalf("defaults %+v", c)
	}
	writeUserConfig(t, `{"web_panel":"yes","web_port":80,"web_idle_minutes":0,"web_max_hours":99,"web_max_viewers":9,"web_open_browser":false}`)
	c = webSettings()
	if !c.On || c.Explicit || c.Port != 0 || c.IdleMinutes != 15 || c.MaxHours != 12 || c.MaxViewers != 4 || c.OpenBrowser || len(c.Problems) != 5 {
		t.Fatalf("bad values %+v", c)
	}
	writeUserConfig(t, `{"web_panel":"on","web_port":41237,"web_idle_minutes":30,"web_max_hours":2,"web_max_viewers":1}`)
	c = webSettings()
	if !c.On || !c.Explicit || c.Port != 41237 || c.IdleMinutes != 30 || c.MaxHours != 2 || c.MaxViewers != 1 || len(c.Problems) != 0 {
		t.Fatalf("good values %+v", c)
	}
	writeUserConfig(t, `{"web_panel":"off"}`)
	if c = webSettings(); c.On || c.Explicit || len(c.Problems) != 0 {
		t.Fatalf("off %+v", c)
	}
	writeUserConfig(t, `{"web_panel":"auto"}`)
	if c = webSettings(); !c.On || c.Explicit || len(c.Problems) != 0 {
		t.Fatalf("auto (the default, written out) %+v", c)
	}
}

// Spec (T31, WEB-10): every web_* setting is aiNever and documented as
// file only.
func TestAISettingsNeverWeb(t *testing.T) {
	n := 0
	for _, d := range fileOnlySettings {
		if !strings.HasPrefix(d.Key, "web_") {
			continue
		}
		n++
		p, ok := aiSettingPolicies[d.Key]
		if !ok || p.Gate != aiGateNever {
			t.Errorf("%s: the AI may change it", d.Key)
		}
		if !strings.Contains(d.Category, "file only") {
			t.Errorf("%s: category %q", d.Key, d.Category)
		}
	}
	if n < 6 {
		t.Fatalf("only %d web settings documented", n)
	}
}

// Spec (WEB-10): a project's own config files can never change the web
// panel (turn it on for SSH, open it to the tailnet, change limits): only
// the user's config.json counts.
func TestProjectConfigCannotEnableWeb(t *testing.T) {
	withConfigDir(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "settings.json"} {
		_ = os.WriteFile(filepath.Join(root, ".termocode", name), []byte(`{"web_panel":"on","web_listen":"tailnet","web_max_viewers":8}`), 0o644)
	}
	t.Chdir(root)
	if c := webSettings(); c.Explicit || c.Tailnet || c.MaxViewers != 4 {
		t.Fatalf("project config changed the web settings: %+v", c)
	}
}
