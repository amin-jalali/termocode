package app

import (
	"strings"
	"testing"
)

// Spec (MC-15): UI text about pasting a config names no other editor.
func TestMCPPasteTextNamesNoOtherEditor(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	var text strings.Builder
	for _, it := range m.mcpConnectItems() {
		text.WriteString(it.Title + " " + it.Hint + "\n")
	}
	m.onMCPConnectPicked(mcpConnectJSONID)
	for _, r := range m.wizCard().Rows {
		text.WriteString(r.Text + "\n")
	}
	got := strings.ToLower(text.String())
	for _, bad := range []string{"cursor", "windsurf", "vs code", "vscode", "zed"} {
		if strings.Contains(got, bad) {
			t.Errorf("paste text names %q:\n%s", bad, text.String())
		}
	}
	if !strings.Contains(got, "mcpservers") {
		t.Errorf("paste text lost the mcpServers hint:\n%s", text.String())
	}
}
