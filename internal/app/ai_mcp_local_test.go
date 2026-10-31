package app

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/mcp"
)

func localCardText(c connectcard.Card) string {
	var b strings.Builder
	for _, r := range c.Rows {
		b.WriteString(r.Text + "\n")
	}
	for _, btn := range c.Buttons {
		b.WriteString("[" + btn.Title + "]\n")
	}
	return b.String()
}

func localReadEntry(t *testing.T, path, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f.MCPServers[name]
}

// Spec (MCPX-15, DI-7/DI-12): Connect → Local server… asks for a command
// or a package, shows the entry before it is written, asks each secret
// value in a hidden field and keeps it in the store, not in mcp.json.
func TestMCPLocalServerGuidedAdd(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	found := false
	for _, it := range m.mcpConnectItems() {
		found = found || it.ID == mcpConnectLocalID
	}
	if !found {
		t.Fatal("the Connect picker has no Local server row")
	}
	m.onMCPConnectPicked(mcpConnectLocalID)
	if w := m.wiz(); w == nil || w.step != wizLocal {
		t.Fatal("the local form did not open")
	}
	m.wiz().local.kind = 1 // npm package
	m.connect = connectcard.New(m.wizCard())
	m.connect.SetInputValue("lcmd", "@scope/server-fs /data")
	m.connect.SetInputValue("lenv", "API_KEY")
	m.onMCPWizButton("local-next")
	w := m.wiz()
	if w.step != wizLocalPreview {
		t.Fatalf("no preview: step %v, err %q", w.step, w.local.err)
	}
	prev := localCardText(m.wizCard())
	if !strings.Contains(prev, "npx") || !strings.Contains(prev, "${secret:fs_api_key}") || !strings.Contains(prev, mcpUserPath()) {
		t.Fatalf("preview:\n%s", prev)
	}
	if _, err := os.Stat(mcpUserPath()); err == nil && localReadEntry(t, mcpUserPath(), "fs") != nil {
		t.Fatal("written before Add")
	}
	m.onMCPWizButton("local-add")
	if m.wiz().step != wizLocalSecret {
		t.Fatal("the secret value was not asked")
	}
	for _, r := range m.wizCard().Rows {
		if r.Kind == connectcard.Input && !r.Masked {
			t.Fatal("the secret field is not hidden")
		}
	}
	tok := "sk-" + "test0123456789abcdef"
	m.onMCPWizSubmit("lsecret", tok)
	if w := m.wiz(); w == nil || w.step != wizDone {
		t.Fatal("not done after the last secret")
	}
	e := localReadEntry(t, mcpUserPath(), "fs")
	if e == nil || e["command"] != "npx" {
		t.Fatalf("entry %v", e)
	}
	raw, _ := os.ReadFile(mcpUserPath())
	if strings.Contains(string(raw), tok) {
		t.Fatal("mcp.json holds the secret")
	}
	cr, _ := ai.LoadCredentials(mcpCredPath())
	if cr.MCPSecrets["fs_api_key"] != tok {
		t.Fatal("the secret is not in the store")
	}
	if strings.Contains(localCardText(m.wizCard()), tok) {
		t.Fatal("the card shows the secret")
	}
}

// Spec (DI-17): the form can save into the project file; a bad command
// stays on the form with a reason.
func TestMCPLocalServerProjectScopeAndErrors(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.onMCPConnectPicked(mcpConnectLocalID)
	m.connect.SetInputValue("lcmd", `my-server "a b`)
	m.onMCPWizButton("local-next")
	if w := m.wiz(); w.step != wizLocal || w.local.err == "" {
		t.Fatal("an open quote was accepted")
	}
	m.wiz().local.scope = 1 // this project
	m.connect = connectcard.New(m.wizCard())
	m.connect.SetInputValue("lcmd", "my-server --flag")
	m.connect.SetInputValue("lname", "mine")
	m.onMCPWizButton("local-next")
	if !strings.Contains(localCardText(m.wizCard()), "trust") {
		t.Fatalf("no trust note:\n%s", localCardText(m.wizCard()))
	}
	m.onMCPWizButton("local-add")
	path := mcp.ProjectPath(aiWorkspaceRoot())
	if e := localReadEntry(t, path, "mine"); e == nil || e["command"] != "my-server" {
		t.Fatalf("project entry %v", e)
	}
	if _, err := os.Stat(mcpUserPath()); err == nil && localReadEntry(t, mcpUserPath(), "mine") != nil {
		t.Fatal("written to the user file too")
	}
}
