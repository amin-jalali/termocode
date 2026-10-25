package app

import (
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
)

func mcpDoneModel(t *testing.T, json string) *Model {
	t.Helper()
	m := aiTestModel(t)
	writeUserMCP(t, json)
	m.aiMCP().Reload()
	p, _ := mcp.PresetByName("github")
	w := m.newWizard(p, "github")
	w.step = wizDone
	w.result = mcp.IdentityResult{Tools: 3}
	return &m
}

func doneButtonIDs(m *Model) string {
	var ids []string
	for _, b := range m.wizDoneCard().Buttons {
		ids = append(ids, b.ID)
	}
	return strings.Join(ids, ",")
}

// Spec (MC-9): a read-only Done screen offers Show tools and Allow writes
// as buttons; Allow writes does what the services list row does.
func TestMCPDoneScreenButtons(t *testing.T) {
	m := mcpDoneModel(t, `{"mcpServers":{"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/readonly"}}}`)
	ids := doneButtonIDs(m)
	if !strings.Contains(ids, "tools") || !strings.Contains(ids, "writes") {
		t.Fatalf("buttons: %s", ids)
	}
	m.onMCPWizButton("writes")
	if m.wiz() != nil && m.connectOpen {
		t.Error("the wizard is still open")
	}
	b, _ := os.ReadFile(mcpUserPath())
	if strings.Contains(string(b), "githubcopilot.com/mcp/readonly") {
		t.Errorf("writes not allowed: %s", b)
	}
}

// Spec: Show tools opens the tools list (or says none are known yet);
// a full-access Done screen has no Allow writes button.
func TestMCPDoneScreenShowTools(t *testing.T) {
	m := mcpDoneModel(t, `{"mcpServers":{"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/"}}}`)
	m.wiz().writes = true
	ids := doneButtonIDs(m)
	if strings.Contains(ids, "writes") {
		t.Errorf("Allow writes on a full-access service: %s", ids)
	}
	if !strings.Contains(ids, "tools") {
		t.Fatalf("no Show tools: %s", ids)
	}
	m.onMCPWizButton("tools")
	if m.connectOpen {
		t.Error("the wizard is still open")
	}
	if !m.pickerOpen && !strings.Contains(aiToastText(m), "github") {
		t.Error("Show tools did nothing")
	}
}
