package app

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// AH-9: mcp_server (termocode as an MCP server for outside agents) is a
// Settings row in the MCP group with the choices auto / on / off, not a
// file-only key, and the AI can never list or set it.
func TestMCPServerSettingRow(t *testing.T) {
	row, ok := findSettingsRow(mcpServeRowID)
	if !ok || row.Label != "mcp_server" || row.Kind != "enum" || row.Default != "auto" {
		t.Fatalf("row %+v ok=%v", row, ok)
	}
	if got := settingChoices(mcpServeRowID); !reflect.DeepEqual(got, []string{"auto", "on", "off"}) {
		t.Fatalf("choices %v", got)
	}
	if c := settingsCategories()[mcpServeRowID]; c != "MCP" {
		t.Fatalf("category %q", c)
	}
	n := 0
	for _, e := range SettingsCatalog() {
		if e.Key == "mcp_server" {
			n++
			if e.ID != mcpServeRowID || strings.Contains(e.Category, "file only") {
				t.Errorf("catalog entry %+v", e)
			}
		}
	}
	if n != 1 {
		t.Fatalf("mcp_server is in the catalog %d times", n)
	}
	if _, ok := aiSettingEntry("mcp_server"); ok {
		t.Fatal("the AI may change mcp_server")
	}
	for _, e := range aiExposedSettings() {
		if e.Key == "mcp_server" {
			t.Fatal("settings_list shows mcp_server")
		}
	}
}

// Picking a choice saves it in config.json (the same key the listener
// reads); a bad value changes nothing.
func TestMCPServerSettingApply(t *testing.T) {
	path, err := settingsConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
	if v, ok := aiSettingValue(mcpServeRowID); !ok || v != "auto" {
		t.Fatalf("default value %q %v", v, ok)
	}
	m := &Model{w: 120, h: 40}
	m.applySetting(mcpServeRowID, "off")
	if got := mcpServeMode(); got != "off" {
		t.Fatalf("after off: mode %q", got)
	}
	if v, _ := aiSettingValue(mcpServeRowID); v != "off" {
		t.Fatalf("row value %q", v)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"mcp_server": "off"`) {
		t.Fatalf("config.json:\n%s", b)
	}
	m.applySetting(mcpServeRowID, "sometimes")
	if got := mcpServeMode(); got != "off" {
		t.Fatalf("a bad value changed the mode to %q", got)
	}
	m.applySetting(mcpServeRowID, "auto")
	if got := mcpServeMode(); got != "auto" {
		t.Fatalf("after auto: mode %q", got)
	}
}
