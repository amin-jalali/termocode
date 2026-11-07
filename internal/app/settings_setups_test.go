package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CMP-101: Preferences: Save Setup to This Project… writes the shareable
// settings files (no secrets, no state) to .termocode/setups/<name>.json
// after a Cancel-first card; Use a Project Setup… picks one and imports it
// through a Cancel-first card.
func TestSettingsProjectSetups(t *testing.T) {
	cfg := filepath.Join(withConfigDir(t), "termocode")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"config.json":      `{"tab_size": 2, "ai": {"api_key": "sk-secret", "model": "m"}}`,
		"keymap.json":      `{"ctrl+alt+j": "ToggleTerminal"}`,
		"recents.json":     `["/a"]`,
		"databases.json":   `{"x": 1}`,
		"credentials.json": `{"y": 2}`,
	} {
		if err := os.WriteFile(filepath.Join(cfg, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws := t.TempDir()
	m := covModelAt(t, ws)
	for _, id := range []string{"settings-setup-save", "settings-setup-use"} {
		if _, ok := aiActionPolicies[id]; !ok {
			t.Errorf("%s: no AI policy", id)
		}
	}

	// No setups yet.
	m.dispatchPaletteAction("settings-setup-use")
	if !toastHas(m, "No setups in this project") {
		t.Error("no hint without setups")
	}

	m.dispatchPaletteAction("settings-setup-save")
	if !m.promptOpen || m.promptKind != promptKindGitx || m.prompt.Value() != "team" {
		t.Fatalf("name prompt open=%v value=%q", m.promptOpen, m.prompt.Value())
	}
	m.promptOpen = false
	m.gitx.onPrompt(m, "../evil")
	if m.confirmOpen || !toastHas(m, "letters") {
		t.Error("a bad name was taken")
	}
	m.gitx.onPrompt(m, "team")
	if !m.confirmOpen || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no Cancel-first card")
	}
	if v := m.confirm.View(); !strings.Contains(v, "keymap.json") || strings.Contains(v, "databases.json") {
		t.Errorf("card:\n%s", v)
	}
	m.confirmOpen = false
	m.gitx.onConfirm(m, "save")
	p := filepath.Join(ws, ".termocode", "setups", "team.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "keymap.json") || !strings.Contains(s, "config.json") ||
		strings.Contains(s, "sk-secret") || strings.Contains(s, "recents.json") ||
		strings.Contains(s, "databases.json") || strings.Contains(s, "credentials.json") {
		t.Errorf("setup file:\n%s", s)
	}

	// Use it: a picker, then a Cancel-first import card.
	_ = os.WriteFile(filepath.Join(cfg, "keymap.json"), []byte(`{}`), 0o644)
	m.dispatchPaletteAction("settings-setup-use")
	if !m.pickerOpen || len(m.gitx.items) != 1 || m.gitx.items[0].ID != "team" {
		t.Fatalf("picker items %+v", m.gitx.items)
	}
	m.pickerOpen = false
	m.gitx.onPick(m, "team")
	if !m.confirmOpen || m.confirmKind != confirmKindSettingsImport || m.confirm.SafeID() != "cancel" {
		t.Fatalf("import card open=%v kind=%v safe=%q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	if !strings.Contains(m.confirm.View(), "team") {
		t.Errorf("the card does not name the setup:\n%s", m.confirm.View())
	}
	m.confirmOpen = false
	m.onSettingsImportConfirm("import")
	if b, _ := os.ReadFile(filepath.Join(cfg, "keymap.json")); !strings.Contains(string(b), "ctrl+alt+j") {
		t.Errorf("keymap not imported: %s", b)
	}
}
