package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

func mcpActionIDList(m *Model, name string) string {
	var ids []string
	for _, it := range mcpServerActionItems(m, name) {
		ids = append(ids, it.ID)
	}
	return strings.Join(ids, " ")
}

// MCPX-9: Reset trust undoes "Always allow" for a server: the saved
// start trust of a project server and "mcp:<server>" in auto-approve.
// The card puts Cancel first; Cancel and Esc change nothing.
func TestMCPResetTrust(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	m := covMCPModel(t)
	pp := mcp.ProjectPath(aiWorkspaceRoot())
	_ = os.MkdirAll(filepath.Dir(pp), 0o755)
	if err := os.WriteFile(pp, []byte(mcptest.Config(map[string]map[string]string{"fake": nil})), 0o644); err != nil {
		t.Fatal(err)
	}
	m.aiMCP().Reload()
	if strings.Contains(mcpActionIDList(m, "fake"), "untrust") {
		t.Fatal("Reset trust offered with nothing to reset")
	}
	m.aiS().mcpSel = "fake"
	m.onMCPServerAction("trust")
	if err := m.saveAISettings(func(s *ai.Settings) { s.AutoApprove = append(s.AutoApprove, mcp.ClassPrefix+"fake") }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mcpActionIDList(m, "fake"), "untrust") {
		t.Fatalf("no Reset trust: %s", mcpActionIDList(m, "fake"))
	}
	m.onMCPServerAction("untrust")
	if !m.confirmOpen || m.confirmKind != confirmKindMCPUntrust || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card: open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	checkCardRule(t, "reset trust", *m, false)
	card := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"asks again before it starts", "ask again before they change"} {
		if !strings.Contains(card, want) {
			t.Errorf("card lacks %q:\n%s", want, card)
		}
	}
	m.onAIConfirmClosed() // Esc
	if !serverStatus(m, "fake").Trusted || !contains(m.aiS().settings.AutoApprove, "mcp:fake") {
		t.Fatal("Esc reset the trust")
	}
	m.onMCPServerAction("untrust")
	m.answerMCPUntrust("cancel")
	if !serverStatus(m, "fake").Trusted {
		t.Fatal("Cancel reset the trust")
	}
	m.onMCPServerAction("untrust")
	m.answerMCPUntrust("reset")
	if serverStatus(m, "fake").Trusted {
		t.Error("still trusted")
	}
	if _, ok := mcp.LoadSettings(mcpConfigPath()).Trusted["fake"]; ok {
		t.Error("mcp_trusted still has fake")
	}
	if contains(m.aiS().settings.AutoApprove, "mcp:fake") {
		t.Error("mcp:fake still auto-approved")
	}
}

// MCPX-9: a tool in the entry's autoApprove can be untrusted on its own:
// it leaves autoApprove in mcp.json (Cancel first), the rest stay.
func TestMCPUntrustTool(t *testing.T) {
	t.Chdir(t.TempDir())
	m := covMCPModel(t)
	writeUserMCP(t, `{"mcpServers":{"notes":{"command":"notes-mcp","autoApprove":["write_note","echo"]}}}`)
	m.aiMCP().Reload()
	if !strings.Contains(mcpActionIDList(m, "notes"), "untrust-tool:write_note") {
		t.Fatalf("no tool row: %s", mcpActionIDList(m, "notes"))
	}
	m.aiS().mcpSel = "notes"
	m.onMCPServerAction("untrust-tool:write_note")
	if !m.confirmOpen || m.confirmKind != confirmKindMCPUntrust || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card: open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	checkCardRule(t, "untrust tool", *m, false)
	if card := ansi.Strip(m.confirm.Box()); !strings.Contains(card, "write_note asks again") {
		t.Errorf("card:\n%s", card)
	}
	m.answerMCPUntrust("reset")
	e := mcpServerEntries("notes")
	if len(e) != 1 || strings.Join(e[0].AutoApprove, ",") != "echo" {
		t.Errorf("autoApprove = %+v", e)
	}
}
